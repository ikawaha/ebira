package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/gitsync"
	"github.com/ikawaha/ebira/internal/service"
	"github.com/ikawaha/ebira/internal/ui/web"
)

// checkSettings fails for settings whose values the commands do not take, so
// that the web UI does not save a settings file the server cannot start with.
func checkSettings(c config.Config) error {
	if c.Theme != "" && !web.ValidTheme(c.Theme) {
		return fmt.Errorf("unknown theme %q (%s)", c.Theme, strings.Join(web.Themes, ", "))
	}
	if c.Auth != "" && !auth.ValidMode(c.Auth) {
		return fmt.Errorf("unknown auth %q (%s)", c.Auth, strings.Join(auth.Modes, ", "))
	}
	if c.Lang != "" && !web.ValidLang(c.Lang) {
		return fmt.Errorf("unknown lang %q (%s)", c.Lang, strings.Join(web.Langs, ", "))
	}
	if _, ok := defaultModels[c.Provider]; c.Provider != "" && !ok {
		return fmt.Errorf("unknown provider %q (%s, %s)", c.Provider, providerAnthropic, providerGemini)
	}
	if p := c.TUIPreview; p != "" && p != "rendered" && p != "source" {
		return fmt.Errorf("unknown tui_preview %q (rendered, source)", p)
	}
	if s := c.TUISearch; s != "" && s != "search" && s != "filter" {
		return fmt.Errorf("unknown tui_search %q (search, filter)", s)
	}
	if _, err := gitSyncInterval(c.GitSync); err != nil {
		return err
	}
	if c.ZennRepo != "" {
		if err := gitsync.CheckURL(c.ZennRepo); err != nil {
			return fmt.Errorf("zenn_repo: %w", err)
		}
	}
	if err := checkBranch(c.ZennBranch); err != nil {
		return err
	}
	if u := c.ZennUser; u != "" && !zennUserRe.MatchString(u) {
		return fmt.Errorf("invalid zenn_user %q: letters, digits, hyphens and underscores", u)
	}
	return nil
}

// checkBranch tells why b cannot be zenn_branch: it is no name of a branch
// as git takes it, such as the URL of a repository given in its place.
func checkBranch(b string) error {
	if b == "" {
		return nil
	}
	host, _, sub := strings.Cut(b, "/")
	if strings.Contains(b, "://") || strings.Contains(b, "@") || strings.Contains(b, ":") || strings.HasSuffix(b, ".git") || sub && strings.Contains(host, ".") {
		return fmt.Errorf("zenn_branch %q looks like the URL of a repository: give the URL as zenn_repo, and leave zenn_branch empty for the default branch", b)
	}
	if strings.HasPrefix(b, "-") || strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".") ||
		strings.HasSuffix(b, ".lock") || strings.Contains(b, "..") || strings.Contains(b, "//") ||
		strings.ContainsAny(b, "~^?*[\\") || strings.ContainsFunc(b, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return fmt.Errorf("invalid zenn_branch %q: not the name of a branch", b)
	}
	return nil
}

// zennUserRe matches a user name of Zenn, which goes into the URLs of the
// articles.
var zennUserRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// settingKeys are the keys of the settings by the flags of serve that replace
// them: of the settings file, and of the settings synced with the notes.
var settingKeys = map[string]string{
	"search":    "search",
	"stopwords": "stop_words",
	"stoptags":  "stop_tags",
	"addr":      "addr",
	"watch":     "watch",
	"provider":  "provider",
	"model":     "model",
	"lang":      "lang",
	"theme":     "theme",
	"auth":      "auth",
	"git-sync":  "git_sync",
}

// overriddenSettings returns the keys of the settings file whose values the
// server of fs does not take, and a restart would not either: those of the
// flags it was started with, and notes_dir and addr, which a restart keeps.
func overriddenSettings(fs *flag.FlagSet) []string {
	keys := []string{"notes_dir", "addr"}
	fs.Visit(func(f *flag.Flag) {
		if k, ok := settingKeys[f.Name]; ok && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	})
	return keys
}

// serverSettings returns how the server of fs, listening on addr, handles its
// settings: it restarts by restart, which stops it and starts its process
// again as itself (see serve).
func serverSettings(fs *flag.FlagSet, addr string, passkeys func() bool, restart func(), saved func()) service.Settings {
	return service.Settings{
		Check:         checkSettings,
		Overridden:    overriddenSettings(fs),
		Addr:          addr,
		Passkeys:      passkeys,
		DefaultModels: defaultModels,
		Restart: func() error {
			restart()
			return nil
		},
		Restartable: restartable,
		Saved:       saved,
	}
}

// restartable tells why the server cannot run its executable again, such as
// one built by go run, which removes it when it ends while the server it
// started in the background still runs; "" when it can. The path is not
// told, so that the page does not show the files of the machine.
func restartable() string {
	exe, err := os.Executable()
	if err != nil {
		return service.RestartUnsupported
	}
	if _, err := os.Stat(exe); err != nil {
		return service.RestartExecutableGone
	}
	return ""
}

// pageLook is the language and the theme of the web UI, which the server
// takes again from its settings when they are saved, so that the page shows
// them without a restart. The flags given win, as at the start.
type pageLook struct {
	fs       *flag.FlagSet
	notesDir string
	mu       sync.Mutex
	lang     string
	theme    string
}

// newPageLook returns the look of the server of fs at the start, of lang and
// theme as its flags, the settings file and the defaults in _ebira.yaml of
// notesDir gave them.
func newPageLook(fs *flag.FlagSet, notesDir, lang, theme string) *pageLook {
	return &pageLook{fs: fs, notesDir: notesDir, lang: lang, theme: theme}
}

// get returns the language and the theme.
func (l *pageLook) get() (lang, theme string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lang, l.theme
}

// reload reads the language and the theme again from the settings file and
// the defaults in _ebira.yaml, except those of the flags given.
func (l *pageLook) reload() {
	c, err := config.Load()
	if err != nil {
		slog.Warn("reading the settings file again", "error", err)
		return
	}
	d, _, err := config.LoadDefaults(l.notesDir)
	if err != nil {
		slog.Warn("reading the defaults again", "error", err)
		return
	}
	c = config.Merge(c, d)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !flagSet(l.fs, "lang") {
		l.lang = c.Lang
	}
	if !flagSet(l.fs, "theme") {
		l.theme = c.Theme
	}
}
