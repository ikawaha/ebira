package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/daemon"
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

// settingKeys are the keys of the settings file by the flags of serve that
// replace them.
var settingKeys = map[string]string{
	"addr":     "addr",
	"watch":    "watch",
	"provider": "provider",
	"model":    "model",
	"lang":     "lang",
	"theme":    "theme",
	"auth":     "auth",
	"git-sync": "git_sync",
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

// settingsFile returns how the server of notesDir handles the settings file:
// it restarts by ebira server restart, run in the background, which stops it
// and starts it again with its flags and environment.
func settingsFile(fs *flag.FlagSet, notesDir, stateDir string) service.SettingsFile {
	return service.SettingsFile{
		Check: func(b []byte) error {
			p, err := config.Path()
			if err != nil {
				return err
			}
			c, err := config.Parse(b, p)
			if err != nil {
				return err
			}
			return checkSettings(c)
		},
		Overridden: overriddenSettings(fs),
		Restart: func() error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			_, _, err = daemon.Spawn(exe, []string{"server", "restart", "-notes", notesDir}, filepath.Join(stateDir, daemon.LogFile))
			return err
		},
	}
}
