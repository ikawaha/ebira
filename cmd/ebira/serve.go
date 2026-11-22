package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	genauth "github.com/ikawaha/ebira/gen/auth"
	genboards "github.com/ikawaha/ebira/gen/boards"
	assistantsvr "github.com/ikawaha/ebira/gen/http/assistant/server"
	authsvr "github.com/ikawaha/ebira/gen/http/auth/server"
	boardssvr "github.com/ikawaha/ebira/gen/http/boards/server"
	notessvr "github.com/ikawaha/ebira/gen/http/notes/server"
	mcpnotessvr "github.com/ikawaha/ebira/gen/jsonrpc/mcp_notes/server"
	mcpnotes "github.com/ikawaha/ebira/gen/mcp_notes"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/agent"
	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/board"
	"github.com/ikawaha/ebira/internal/claudecode"
	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/daemon"
	"github.com/ikawaha/ebira/internal/guide"
	"github.com/ikawaha/ebira/internal/history"
	"github.com/ikawaha/ebira/internal/httpguard"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/service"
	"github.com/ikawaha/ebira/internal/store"
	"github.com/ikawaha/ebira/internal/ui/web"
	"github.com/ikawaha/ebira/internal/watch"
	"github.com/ikawaha/ebira/internal/zenn"
	"goa.design/goa-ai/features/model/anthropic"
	"goa.design/goa-ai/features/model/openai"
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
		prov     = fs.String("provider", settings.Provider, "model provider of the assistant (POST /ask): anthropic (ANTHROPIC_API_KEY), gemini (GEMINI_API_KEY or GOOGLE_API_KEY) or openai (OPENAI_API_KEY); empty picks the one whose key is set")
		lang     = fs.String("lang", settings.Lang, "language of the web UI: "+strings.Join(web.Langs, " or ")+" (default: lang of the settings file, else of the defaults in "+store.ConfigFile+", else the browser's language)")
		theme    = fs.String("theme", settings.Theme, "color theme of the web UI: auto (the system's light or dark mode), light or dark (default: theme of the settings file, else of the defaults in "+store.ConfigFile+", else auto)")
		modelF   = fs.String("model", settings.Model, "model of the assistant (default: "+defaultModels[providerAnthropic]+" for anthropic, "+defaultModels[providerGemini]+" for gemini, "+defaultModels[providerOpenAI]+" for openai)")
		authF    = fs.String("auth", cmp.Or(settings.Auth, auth.ModeNone), "how the REST API authenticates people: none treats every request as from people; passkey serves only requests signed in with a passkey, as from people, and refuses the others; MCP stays open to AI clients (register a passkey with ebira auth passkey) (default: auth of the settings file, else none)")
		gitSync  = fs.String("git-sync", settings.GitSync, "interval of syncing the notes with their git repository, as ebira sync does, such as 10m, at least "+minGitSync.String()+"; empty or 0 does not sync (default: git_sync of the settings file, else of the defaults in "+store.ConfigFile+")")
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
		case "notes", "save-notes", "new-search", "new-lang":
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
	notesDir, addr, search, stopW, stopT, prov, modelF, watchF := o.notesDir, o.addr, o.search, o.stopW, o.stopT, o.prov, o.model, o.watch
	// The defaults shared with the notes are known with the notes directory.
	abs, err := notesDirOf(fs, *notesDir, false, nil, nil)
	if err != nil {
		return err
	}
	if *o.theme != "" && !web.ValidTheme(*o.theme) {
		return fmt.Errorf("unknown theme %q of -theme, theme in the settings file or its defaults (%s)", *o.theme, strings.Join(web.Themes, ", "))
	}
	if !auth.ValidMode(*o.auth) {
		return fmt.Errorf("unknown mode %q of -auth or auth in the settings file (%s)", *o.auth, strings.Join(auth.Modes, ", "))
	}
	if *o.lang != "" && !web.ValidLang(*o.lang) {
		return fmt.Errorf("unknown language %q of -lang, lang in the settings file or its defaults (%s)", *o.lang, strings.Join(web.Langs, ", "))
	}
	gitEvery, err := gitSyncInterval(*o.gitSync)
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

	// A notes directory made now gets the guide, but one with notes does
	// not, so that a guide deleted does not come back.
	fresh := guide.IsNew(*notesDir)
	st, err := store.New(*notesDir)
	if err != nil {
		return err
	}
	if fresh {
		r, err := guide.Install(st, *o.lang, time.Now().UTC().Truncate(time.Millisecond))
		if err != nil {
			return fmt.Errorf("add the guide: %w", err)
		}
		slog.Info("added the guide to the new notes directory", "notebook", r.Notebook)
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
	ctx, stop := daemon.NotifyStop(context.Background())
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
	// The clone of the repository of the articles of Zenn is ebira's own, in
	// the state directory; the settings are read at each use, so that a
	// change of them applies without a restart.
	svc.SetZenn(filepath.Join(stateDir, "zenn"), func() (zenn.Settings, error) {
		c, err := config.Load()
		if err != nil {
			return zenn.Settings{}, err
		}
		d, _, err := config.LoadDefaults(*notesDir)
		if err != nil {
			return zenn.Settings{}, err
		}
		c = config.Merge(c, d)
		return zenn.Settings{Repo: c.ZennRepo, Branch: c.ZennBranch, User: c.ZennUser}, nil
	})
	r, err := svc.Sync(ctx, nil)
	if err != nil {
		return err
	}
	slog.Info("index synced", "mode", mode, "mode_from", modeFrom, "added", r.Added, "updated", r.Updated, "removed", r.Removed)
	for _, e := range r.Errors {
		slog.Warn("skip note", "error", e)
	}
	if freed, err := ix.Compact(ctx); err != nil {
		slog.Warn("compact index failed", "error", err)
	} else if freed > 0 {
		slog.Info("index compacted", "freed_bytes", freed)
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

	asst, boardAsk, err := newAssistant(ctx, svc, *prov, *modelF)
	if err != nil {
		return err
	}
	// The whiteboards stay on this device, in the state directory, until
	// they are finished; the assistant answers their sticky notes.
	boards, err := board.New(filepath.Join(stateDir, "boards"))
	if err != nil {
		return err
	}
	if err := svc.SetBoards(boards, boardAsk); err != nil {
		return err
	}
	// Forks of the Claude Code session that opened a board answer the
	// sticky notes of the fork kind, with claude of this machine. Finding it
	// may ask the login shell, so the server does not wait for it.
	go func() {
		if f := newForker(ctx, *addr); f != nil {
			svc.SetForker(f)
		}
	}()

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
	if *o.auth != auth.ModePasskey && !loopback(listenHost) {
		slog.Warn("without passkeys (auth: none), anyone who reaches this address reads and changes the notes; set auth: passkey or listen on localhost", "addr", *addr)
	}

	// Only the REST API, which the UI uses, is marked as from people: every
	// request without passkeys (auth: none), and requests signed in with a
	// passkey with them. MCP (and the assistant) are treated as AI, which
	// cannot see private notebooks; their endpoints ignore sessions, except
	// that the assistant lets in only people signed in, without marking them.
	rest := service.REST{Notes: svc}
	gate := agent.Gate{Service: asst}
	if *o.auth == auth.ModePasskey {
		rest.Session = passkeys.Check
		gate.Session = passkeys.Check
	}
	mux := goahttp.NewMuxer()
	mountNotes(mux, svc, rest)
	eh := func(ctx context.Context, w http.ResponseWriter, err error) {
		slog.ErrorContext(ctx, "request failed", "error", err)
	}
	assistantsvr.Mount(mux, assistantsvr.New(genassistant.NewEndpoints(gate), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh, nil))
	authsvr.Mount(mux, authsvr.New(genauth.NewEndpoints(auth.NewService(*o.auth, passkeys)), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh, nil))

	bi, _ := debug.ReadBuildInfo()
	state := daemon.State{PID: os.Getpid(), Addr: *addr, Version: daemon.Version(), VersionLabel: versionLabel(bi), API: daemon.API, APIRev: daemon.APIRev, NotesDir: *notesDir, StartedAt: time.Now().UTC().Truncate(time.Second), Instance: rand.Text(), Flags: setFlags(fs)}
	root := http.NewServeMux()
	root.Handle("GET /health", daemon.HealthHandler(healthState(state, *o.auth)))
	root.Handle("GET /debug/stats", statsHandler(ix, state.StartedAt, rest.Session))
	claude := claudeHandler(*addr, claudecode.Find, rest.Session)
	root.Handle("GET /claude/mcp", claude)
	root.Handle("POST /claude/mcp", claude)
	previewCSS := ""
	if p, err := config.Path(); err == nil {
		previewCSS = filepath.Join(filepath.Dir(p), web.PreviewCSSFile)
	}
	svc.SetPreviewCSS(previewCSS)
	restartc := make(chan struct{}, 1)
	look := newPageLook(fs, *notesDir, *o.lang, *o.theme)
	svc.SetSettings(serverSettings(fs, *addr, passkeys.Registered, func() {
		select {
		case restartc <- struct{}{}:
		default: // asked already
		}
	}, look.reload))
	ui := web.Handler(web.Options{
		Look: look.get, Search: string(mode), PreviewCSS: previewCSS,
		Version: versionLabel(bi), VersionText: versionText(bi, daemon.Version()),
		GuideNames: guide.Names(),
	})
	root.Handle("GET /{$}", ui)
	root.Handle("GET "+web.AssetsPrefix, ui)
	root.Handle(web.LegacyPrefix, http.RedirectHandler(web.Prefix, http.StatusMovedPermanently))
	root.Handle("/", withBaseURL(mux))

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("%w; %s", err, addrHint())
	}
	// No WriteTimeout: the event stream (/events) and MCP streams stay open.
	srv := &http.Server{
		Handler:           httpguard.Guard(limitBodies(root), listenHost),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	srv.RegisterOnShutdown(svc.Close) // end event streams so that Shutdown does not wait for them
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	// Listening does not show that requests reach this server: another
	// program on the same port, such as on all addresses, may answer them,
	// and the TUI, the web UI and MCP clients would never reach this one.
	if err := daemon.CheckSelf(ctx, *addr, state.PID, selfCheckWait); err != nil {
		srv.Close()
		return fmt.Errorf("%w; %s", err, addrHint())
	}
	if err := daemon.WriteState(stateDir, state); err != nil {
		return err
	}
	defer daemon.RemoveState(stateDir)
	slog.Info("listening", "addr", "http://"+*addr, "ui", "http://"+*addr+web.Prefix, "mcp", "http://"+*addr+"/mcp", "auth", *o.auth, "version", state.Version, "pid", state.PID)

	restart := false
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	case <-restartc:
		restart = true
		slog.Info("restarting the server")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if restart {
		// The caller starts the process again once this returns, with the
		// lock and the index closed.
		return restartError{logPath: filepath.Join(stateDir, daemon.LogFile)}
	}
	return nil
}

// restartError is returned by runServe when it is asked to start again.
type restartError struct {
	logPath string // for the output of a new process, on Windows
}

func (restartError) Error() string { return "the server is asked to start again" }

// serve runs the server, and starts its process again as itself when it is
// asked to, such as to read the settings again: with the same executable,
// command line and environment, so that a server in the foreground stays in
// its terminal.
func serve(args []string) error {
	err := runServe(args)
	var r restartError
	if !errors.As(err, &r) {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return daemon.Reexec(exe, os.Args, r.logPath)
}

// Model providers of the assistant.
const (
	providerAnthropic = "anthropic"
	providerGemini    = "gemini"
	providerOpenAI    = "openai"
)

var defaultModels = map[string]string{
	providerAnthropic: "claude-sonnet-5-5",
	providerGemini:    "gemini-3.8-flash",
	providerOpenAI:    "gpt-6.2-sol",
}

// newAssistant returns the assistant service: the agent on the model of
// provider, or when no API key is set, a service that answers every question
// with unavailable.
// newAssistant returns the assistant of the API, and what answers the sticky
// notes of whiteboards with the same model, or nil when the assistant is not
// configured.
func newAssistant(ctx context.Context, svc gennotes.Service, provider, modelID string) (genassistant.Service, service.Asker, error) {
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	geminiKey := cmp.Or(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if provider == "" {
		switch {
		case anthropicKey != "":
			provider = providerAnthropic
		case geminiKey != "":
			provider = providerGemini
		case openaiKey != "":
			provider = providerOpenAI
		default:
			slog.Info("assistant disabled: set ANTHROPIC_API_KEY, GEMINI_API_KEY or OPENAI_API_KEY to enable POST /ask")
			return agent.Unavailable{Reason: "the assistant is not configured: start the server with ANTHROPIC_API_KEY, GEMINI_API_KEY or OPENAI_API_KEY set"}, nil, nil
		}
	}
	if _, ok := defaultModels[provider]; !ok {
		return nil, nil, fmt.Errorf("unknown model provider %q (anthropic, gemini, openai)", provider)
	}
	modelID = cmp.Or(modelID, defaultModels[provider])
	var (
		client model.Client
		err    error
	)
	switch provider {
	case providerAnthropic:
		if anthropicKey == "" {
			return nil, nil, errors.New("the anthropic provider needs ANTHROPIC_API_KEY")
		}
		client, err = anthropic.NewFromAPIKey(anthropicKey, modelID)
	case providerGemini:
		if geminiKey == "" {
			return nil, nil, errors.New("the gemini provider needs GEMINI_API_KEY or GOOGLE_API_KEY")
		}
		// The vertex adapter takes the genai models service, which also serves
		// the Gemini API (Google AI Studio keys).
		var gc *genai.Client
		gc, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiKey, Backend: genai.BackendGeminiAPI})
		if err == nil {
			client, err = vertex.New(gc.Models, vertex.Options{DefaultModel: modelID})
		}
	case providerOpenAI:
		if openaiKey == "" {
			return nil, nil, errors.New("the openai provider needs OPENAI_API_KEY")
		}
		client, err = openai.NewFromAPIKey(openaiKey, modelID)
	}
	if err != nil {
		return nil, nil, err
	}
	a, err := agent.New(ctx, client, svc, agent.Config{Model: modelID})
	if err != nil {
		return nil, nil, err
	}
	// The sticky notes are answered by an assistant of their own, with the
	// same model and tools, told to explain the text of the board.
	b, err := agent.New(ctx, client, svc, agent.Config{Model: modelID, Prompt: agent.BoardPrompt})
	if err != nil {
		return nil, nil, err
	}
	slog.Info("assistant enabled", "provider", provider, "model", modelID)
	return a, boardAsker(b), nil
}

// mountNotes mounts the notes service on mux: the REST API through rest, whose
// requests may be from people, and the MCP server through svc itself, whose
// requests are from AI; and the boards service, the REST API of whiteboards,
// as rest tells who the requests are from.
func mountNotes(mux goahttp.Muxer, svc *service.Notes, rest service.REST) {
	eh := func(ctx context.Context, w http.ResponseWriter, err error) {
		slog.ErrorContext(ctx, "request failed", "error", err)
	}
	notesEndpoints := gennotes.NewEndpoints(rest)
	mcpEndpoints := mcpnotes.NewEndpoints(mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(svc), &mcpnotes.MCPAdapterOptions{ErrorMapper: mcpError}))
	notessvr.Mount(mux, notessvr.New(notesEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh, nil))
	mcpnotessvr.Mount(mux, mcpnotessvr.New(mcpEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, tooLarge))
	boardssvr.Mount(mux, boardssvr.New(genboards.NewEndpoints(service.Boards{REST: rest}), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, eh, nil))
}

// boardAsker returns what answers the questions on whiteboards with the
// assistant a.
func boardAsker(a *agent.Assistant) service.Asker {
	return func(ctx context.Context, question string) (string, error) {
		r, err := a.Ask(ctx, &genassistant.AskPayload{Question: question})
		if err != nil {
			return "", err
		}
		return r.Answer, nil
	}
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
// history, and those of deleted notes, and the whiteboards finished or left
// long ago, now and then every interval until ctx is done.
func sweepHistoryEvery(ctx context.Context, svc *service.Notes, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := svc.SweepHistory(); err != nil {
			slog.Warn("cannot drop the old versions of notes", "error", err)
		}
		if err := svc.SweepBoards(); err != nil {
			slog.Warn("cannot delete the old whiteboards", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// loopback reports whether a server listening on host is reached from this
// computer only: localhost or a loopback IP. An empty host, as in :13579,
// listens on every address.
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// withBaseURL tells the service the URL each request came to, for the URLs
// it gives back, such as those of uploads for MCP clients, which reach the
// server at that host. The guard has checked the host already.
func withBaseURL(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(service.WithBaseURL(r.Context(), "http://"+r.Host)))
	})
}

// addrHint tells what to do when the server cannot have its address, as when
// another program uses the port: where to choose another, and that the MCP
// clients, whose URL has the address, are to be registered again.
func addrHint() string {
	where := "addr in the settings file"
	if p, err := config.Path(); err == nil {
		where += " (" + p + ")"
	}
	return "choose another address, such as localhost:13580, with -addr, " + where + " or addr in the defaults of " + store.ConfigFile +
		", then register ebira again with the MCP clients, as its URL has the address (ebira mcp add for Claude Code)"
}
