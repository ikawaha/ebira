package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/config"
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
	if _, err := gitSyncInterval(c.GitSync); err != nil {
		return err
	}
	return nil
}

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
func serverSettings(fs *flag.FlagSet, addr string, passkeys func() bool, restart func()) service.Settings {
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
