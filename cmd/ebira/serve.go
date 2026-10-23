package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	genauth "github.com/ikawaha/ebira/gen/auth"
	assistantsvr "github.com/ikawaha/ebira/gen/http/assistant/server"
	authsvr "github.com/ikawaha/ebira/gen/http/auth/server"
	notessvr "github.com/ikawaha/ebira/gen/http/notes/server"
	mcpnotessvr "github.com/ikawaha/ebira/gen/jsonrpc/mcp_notes/server"
	mcpnotes "github.com/ikawaha/ebira/gen/mcp_notes"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/agent"
	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/daemon"
	"github.com/ikawaha/ebira/internal/history"
	"github.com/ikawaha/ebira/internal/httpguard"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/service"
	"github.com/ikawaha/ebira/internal/store"
	"github.com/ikawaha/ebira/internal/ui/web"
	"github.com/ikawaha/ebira/internal/watch"
	"goa.design/goa-ai/features/model/anthropic"
	"goa.design/goa-ai/features/model/vertex"
	"goa.design/goa-ai/runtime/agent/model"
	goahttp "goa.design/goa/v3/http"
	"google.golang.org/genai"
)

// serveOptions are the flags of ebira serve, which ebira server start passes
// on to the server it starts.
type serveOptions struct {
	notesDir, addr, search, stopW, stopT, prov, model, lang, theme, gitSync, auth *string
	watch                                                                         *bool
}

// newServeFlags defines the flags of ebira serve on a new flag set.
func newServeFlags(name string) (*flag.FlagSet, *serveOptions) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var (
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory: the notebooks, with the note YAML files, and "+store.ConfigFile+" (default: notes_dir of the settings file, else ./notes)")
		addr     = fs.String("addr", defaultAddr(), "address to listen on")
		search   = fs.String("search", "", "search mode: like, bigram, trigram, en (English words with their stems), or ja (Japanese morphological analysis) (default: search in "+store.ConfigFile+" of the notes directory, else "+string(index.ModeBigram)+")")
		watchF   = fs.Bool("watch", settings.Watch == nil || *settings.Watch, "watch the notes directory and update the index on changes")
		stopW    = fs.String("stopwords", "", "ja mode: file of stop words, one per line, replacing stop_words of "+store.ConfigFile+" (default: the list of kagome's ja filter, from Lucene)")
		stopT    = fs.String("stoptags", "", "ja mode: file of stop parts of speech, each written as the IPA dictionary names of its levels joined by \"-\", one per line, matched exactly as in Lucene, replacing stop_tags of "+store.ConfigFile+" (default: the list of kagome's ja filter, from Lucene)")
		prov     = fs.String("provider", settings.Provider, "model provider of the assistant (POST /ask): anthropic (ANTHROPIC_API_KEY) or gemini (GEMINI_API_KEY or GOOGLE_API_KEY); empty picks the one whose key is set")
		lang     = fs.String("lang", settings.Lang, "language of the web UI: "+strings.Join(web.Langs, " or ")+"; a language chosen in the web UI's settings menu wins (default: lang of the settings file, else the browser's language)")
		theme    = fs.String("theme", settings.Theme, "color theme of the web UI: auto (the system's light or dark mode), light or dark; a theme chosen in the web UI's settings menu wins (default: theme of the settings file, else auto)")
		modelF   = fs.String("model", settings.Model, "model of the assistant (default: "+defaultModels[providerAnthropic]+" for anthropic, "+defaultModels[providerGemini]+" for gemini)")
		authF    = fs.String("auth", cmp.Or(settings.Auth, auth.ModeNone), "how the REST API authenticates people: none treats every request as from people; passkey serves only requests signed in with a passkey, as from people, and refuses the others; MCP stays open to AI clients (register a passkey with ebira auth passkey) (default: auth of the settings file, else none)")
		gitSync  = fs.String("git-sync", settings.GitSync, "interval of syncing the notes with their git repository, as ebira sync does, such as 10m, at least "+minGitSync.String()+"; empty or 0 does not sync (default: git_sync of the settings file)")
	)
	return fs, &serveOptions{notesDir: notesDir, addr: addr, search: search, stopW: stopW, stopT: stopT, prov: prov, model: modelF, lang: lang, theme: theme, gitSync: gitSync, auth: authF, watch: watchF}
}

// setFlags returns the flags set on fs other than -notes, as -name=value.
// The files of -stopwords and -stoptags become absolute paths, so that the
// flags work from another directory.
func setFlags(fs *flag.FlagSet) []string {
	var flags []string
	fs.Visit(func(f *flag.Flag) {
		v := f.Value.String()
		switch f.Name {
		case "notes":
			return
		case "stopwords", "stoptags":
			if v != "" {
				if abs, err := filepath.Abs(v); err == nil {
					v = abs
				}
			}
		}
		flags = append(flags, "-"+f.Name+"="+v)
	})
	return flags
}

// runServe runs the server in the foreground: the REST API (/notes, /search,
// /sync), the assistant (POST /ask), the MCP server (POST /mcp), the web UI
// (/) and GET /health on the same port. One server runs per data
// directory (see internal/daemon).
func runServe(args []string) error {
	fs, o := newServeFlags("serve")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *o.theme != "" && !web.ValidTheme(*o.theme) {
		return fmt.Errorf("unknown theme %q of -theme or theme in the settings file (%s)", *o.theme, strings.Join(web.Themes, ", "))
	}
	if !auth.ValidMode(*o.auth) {
		return fmt.Errorf("unknown mode %q of -auth or auth in the settings file (%s)", *o.auth, strings.Join(auth.Modes, ", "))
	}
	if *o.lang != "" && !web.ValidLang(*o.lang) {
		return fmt.Errorf("unknown language %q of -lang or lang in the settings file (%s)", *o.lang, strings.Join(web.Langs, ", "))
	}
	gitEvery, err := gitSyncInterval(*o.gitSync)
	if err != nil {
		return err
	}
	notesDir, addr, search, stopW, stopT, prov, modelF, watchF := o.notesDir, o.addr, o.search, o.stopW, o.stopT, o.prov, o.model, o.watch
	abs, err := notesDirOf(fs, *notesDir, false, nil, nil)
	if err != nil {
		return err
	}
	*notesDir = abs
	stateDir, err := config.StateDir(*notesDir)
	if err != nil {
		return err
	}
	if err := config.Prepare(stateDir, *notesDir); err != nil {
		return err
	}
	lock, err := daemon.Lock(stateDir)
	if errors.Is(err, daemon.ErrRunning) {
		return fmt.Errorf("%w (%s); see ebira server status", err, *notesDir)
	}
	if err != nil {
		return err
	}
	if lock != nil {
		defer lock.Close()
	}
	if err := removeLegacyDirs(*notesDir); err != nil {
		return err
	}
	cacheDir, err := config.CacheDir(*notesDir)
	if err != nil {
		return err
	}
	if err := config.Prepare(cacheDir, *notesDir); err != nil {
		return err
	}

	st, err := store.New(*notesDir)
	if err != nil {
		return err
	}
	conf, err := st.Config()
	if err != nil {
		return err
	}
	// The flag wins over the settings file, which the notes repository shares.
	modeName, modeFrom := *search, "-search"
	if modeName == "" {
		modeName, modeFrom = conf.Search, st.ConfigPath()
	}
	if modeName == "" {
		modeName, modeFrom = string(index.ModeBigram), "default"
	}
	mode, err := index.ParseMode(modeName)
	if err != nil {
		return fmt.Errorf("%s: %w", modeFrom, err)
	}
	// Stop lists: a flag's file, else the settings file, else (nil) the lists
	// of kagome's ja filter. An empty list in the settings file means none.
	cfg := index.Config{Mode: mode, StopWords: conf.StopWords, StopTags: conf.StopTags}
	if *stopW != "" {
		if cfg.StopWords, err = index.LoadWordList(*stopW); err != nil {
			return err
		}
	}
	if *stopT != "" {
		if cfg.StopTags, err = index.LoadWordList(*stopT); err != nil {
			return err
		}
	}
	if mode != index.ModeJa && (cfg.StopWords != nil || cfg.StopTags != nil) {
		slog.Warn("stop words and stop tags apply only to the ja search mode", "mode", mode)
	}
	if mode == index.ModeJa {
		slog.Info("ja stop lists", "stop_words", stopListLog(cfg.StopWords), "stop_tags", stopListLog(cfg.StopTags))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	moved, err := st.MigrateFlat()
	if len(moved) > 0 {
		slog.Info("moved notes into the inbox notebook", "ids", moved)
	}
	if err != nil {
		return err
	}
	ix, err := index.Open(ctx, filepath.Join(cacheDir, "index.sqlite"), cfg)
	if err != nil {
		return err
	}
	defer ix.Close()

	svc := service.New(st, ix)
	// The earlier versions of notes stay on this device, in the state
	// directory, apart from the notes that are synced.
	hist, err := history.New(filepath.Join(stateDir, "history"))
	if err != nil {
		return err
	}
	svc.SetHistory(hist)
	svc.SetMarkNames(conf.Marks)
	r, err := svc.Sync(ctx, nil)
	if err != nil {
		return err
	}
	slog.Info("index synced", "mode", mode, "mode_from", modeFrom, "added", r.Added, "updated", r.Updated, "removed", r.Removed)
	for _, e := range r.Errors {
		slog.Warn("skip note", "error", e)
	}
	// The earlier versions past the limits of the history go now and then
	// once a day, for a server left running.
	go sweepHistoryEvery(ctx, svc, 24*time.Hour)
	if *watchF {
		// The notes are as deep as trash/<notebook>/<sub>/<id>.yaml.
		const watchDepth = 3
		go func() {
			err := watch.Run(ctx, st.Dir(), store.Ext, 300*time.Millisecond, func() {
				r, err := svc.Sync(ctx, nil)
				if err != nil {
					slog.Error("sync failed", "error", err)
					return
				}
				if r.Added+r.Updated+r.Removed > 0 {
					slog.Info("index synced", "added", r.Added, "updated", r.Updated, "removed", r.Removed)
				}
				for _, e := range r.Errors {
					slog.Warn("skip note", "error", e)
				}
			}, func(err error) { slog.Warn("watch error", "error", err) }, watchDepth)
			if err != nil {
				slog.Error("watch stopped", "error", err)
			}
		}()
	}

	asst, err := newAssistant(ctx, svc, *prov, *modelF)
	if err != nil {
		return err
	}

	if gitEvery > 0 {
		go syncGitEvery(ctx, svc, gitEvery)
	}

	listenHost, port, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("-addr: %w", err)
	}
	// Passkeys belong to a domain name, so the web UI uses them on localhost
	// whatever address the server listens on.
	passkeys, err := auth.New(auth.Dir(stateDir), "http://localhost:"+port, "localhost")
	if err != nil {
		return err
	}
	if *o.auth == auth.ModePasskey && listenHost != "localhost" {
		slog.Warn("passkeys work only on the web UI at localhost", "url", "http://localhost:"+port+web.Prefix)
	}

	// Only the REST API, which the UI uses, is marked as from people: every
	// request without passkeys (auth: none), and requests signed in with a
	// passkey with them. MCP (and the assistant) are treated as AI, which
	// cannot see private notebooks; their endpoints ignore sessions.
	rest := service.REST{Notes: svc}
	if *o.auth == auth.ModePasskey {
		rest.Session = passkeys.Check
	}
	notesEndpoints := gennotes.NewEndpoints(rest)
	mcpEndpoints := mcpnotes.NewEndpoints(mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(svc), nil))

	mux := goahttp.NewMuxer()
	eh := func(ctx context.Context, w http.ResponseWriter, err error) {
		slog.ErrorContext(ctx, "request failed", "error", err)
	}
	notessvr.Mount(mux, notessvr.New(notesEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh, nil))
	mcpnotessvr.Mount(mux, mcpnotessvr.New(mcpEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh))
	assistantsvr.Mount(mux, assistantsvr.New(genassistant.NewEndpoints(asst), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh, nil))
	authsvr.Mount(mux, authsvr.New(genauth.NewEndpoints(auth.NewService(*o.auth, passkeys)), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh, nil))

	state := daemon.State{PID: os.Getpid(), Addr: *addr, Version: daemon.Version(), NotesDir: *notesDir, StartedAt: time.Now().UTC().Truncate(time.Second), Flags: setFlags(fs)}
	root := http.NewServeMux()
	root.Handle("GET /health", daemon.HealthHandler(state))
	previewCSS := ""
	if p, err := config.Path(); err == nil {
		previewCSS = filepath.Join(filepath.Dir(p), web.PreviewCSSFile)
	}
	svc.SetPreviewCSS(previewCSS)
	ui := web.Handler(web.Options{Lang: *o.lang, Theme: *o.theme, Search: string(mode), PreviewCSS: previewCSS})
	root.Handle("GET /{$}", ui)
	root.Handle("GET "+web.AssetsPrefix, ui)
	root.Handle(web.LegacyPrefix, http.RedirectHandler(web.Prefix, http.StatusMovedPermanently))
	root.Handle("/", mux)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: httpguard.Guard(root, listenHost), ReadHeaderTimeout: 10 * time.Second}
	srv.RegisterOnShutdown(svc.Close) // end event streams so that Shutdown does not wait for them
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	// Listening does not show that requests reach this server: another
	// program on the same port, such as on all addresses, may answer them,
	// and the TUI, the web UI and MCP clients would never reach this one.
	if err := daemon.CheckSelf(ctx, *addr, state.PID, selfCheckWait); err != nil {
		srv.Close()
		return fmt.Errorf("%w; choose another address with -addr or addr in the settings file", err)
	}
	if err := daemon.WriteState(stateDir, state); err != nil {
		return err
	}
	defer daemon.RemoveState(stateDir)
	slog.Info("listening", "addr", "http://"+*addr, "ui", "http://"+*addr+web.Prefix, "mcp", "http://"+*addr+"/mcp", "auth", *o.auth, "version", state.Version, "pid", state.PID)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Model providers of the assistant.
const (
	providerAnthropic = "anthropic"
	providerGemini    = "gemini"
)

var defaultModels = map[string]string{
	providerAnthropic: "claude-sonnet-5-5",
	providerGemini:    "gemini-3.8-flash",
}

// newAssistant returns the assistant service: the agent on the model of
// provider, or when no API key is set, a service that answers every question
// with unavailable.
func newAssistant(ctx context.Context, svc gennotes.Service, provider, modelID string) (genassistant.Service, error) {
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	geminiKey := cmp.Or(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
	if provider == "" {
		switch {
		case anthropicKey != "":
			provider = providerAnthropic
		case geminiKey != "":
			provider = providerGemini
		default:
			slog.Info("assistant disabled: set ANTHROPIC_API_KEY or GEMINI_API_KEY to enable POST /ask")
			return agent.Unavailable{Reason: "the assistant is not configured: start the server with ANTHROPIC_API_KEY or GEMINI_API_KEY set"}, nil
		}
	}
	if _, ok := defaultModels[provider]; !ok {
		return nil, fmt.Errorf("unknown model provider %q (anthropic, gemini)", provider)
	}
	modelID = cmp.Or(modelID, defaultModels[provider])
	var (
		client model.Client
		err    error
	)
	switch provider {
	case providerAnthropic:
		if anthropicKey == "" {
			return nil, errors.New("the anthropic provider needs ANTHROPIC_API_KEY")
		}
		client, err = anthropic.NewFromAPIKey(anthropicKey, modelID)
	case providerGemini:
		if geminiKey == "" {
			return nil, errors.New("the gemini provider needs GEMINI_API_KEY or GOOGLE_API_KEY")
		}
		// The vertex adapter takes the genai models service, which also serves
		// the Gemini API (Google AI Studio keys).
		var gc *genai.Client
		gc, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiKey, Backend: genai.BackendGeminiAPI})
		if err == nil {
			client, err = vertex.New(gc.Models, vertex.Options{DefaultModel: modelID})
		}
	}
	if err != nil {
		return nil, err
	}
	a, err := agent.New(ctx, client, svc, agent.Config{Model: modelID})
	if err != nil {
		return nil, err
	}
	slog.Info("assistant enabled", "provider", provider, "model", modelID)
	return a, nil
}

// stopListLog describes a stop list for the log.
func stopListLog(list []string) string {
	switch {
	case list == nil:
		return "default"
	case len(list) == 0:
		return "none"
	}
	return fmt.Sprintf("%d entries", len(list))
}

// minGitSync is the shortest interval of syncing with git, not to load the
// remote.
const minGitSync = time.Minute

// selfCheckWait is how long serve waits for its own answer on its address
// before it gives up (see daemon.CheckSelf).
const selfCheckWait = 5 * time.Second

// gitSyncInterval parses -git-sync: 0 for none.
func gitSyncInterval(s string) (time.Duration, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || (d != 0 && d < minGitSync) {
		return 0, fmt.Errorf("invalid interval %q of -git-sync or git_sync in the settings file: a duration of at least %s, such as 10m, or 0", s, minGitSync)
	}
	return d, nil
}

// syncGitEvery syncs the notes with git at the start and every interval,
// until ctx is done. A failure is logged, and retried at the next time.
func syncGitEvery(ctx context.Context, svc *service.Notes, every time.Duration) {
	slog.Info("syncing with git", "every", every.String())
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		r, err := svc.GitSync(service.AsPerson(ctx), &gennotes.GitSyncPayload{})
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			slog.Warn("git sync failed", "error", err)
		case r.Committed+r.Merged > 0 || r.Pushed || len(r.Conflicts) > 0:
			conflicts := make([]string, len(r.Conflicts))
			for i, c := range r.Conflicts {
				conflicts[i] = c.Path
			}
			slog.Info("git synced", "committed", r.Committed, "merged", r.Merged, "pushed", r.Pushed, "conflicts", conflicts)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sweepHistoryEvery drops the earlier versions of notes past the limits of the
// history, and those of deleted notes, now and then every interval until ctx
// is done.
func sweepHistoryEvery(ctx context.Context, svc *service.Notes, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := svc.SweepHistory(); err != nil {
			slog.Warn("cannot drop the old versions of notes", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
