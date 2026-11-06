package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// What the app does on Windows; see platform_darwin.go for macOS. WebView2
// shows the confirm and alert of the page by itself.

func platformInit() {}

// The settings of the app are in a file of its own, as macOS keeps them in
// the user defaults: %AppData%\ebira\desktop.json.
var settings struct {
	sync.Mutex
	values map[string]string
}

func settingsFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ebira", "desktop.json"), nil
}

func loadSettings() map[string]string {
	if settings.values != nil {
		return settings.values
	}
	settings.values = map[string]string{}
	if name, err := settingsFile(); err == nil {
		if b, err := os.ReadFile(name); err == nil {
			if err := json.Unmarshal(b, &settings.values); err != nil {
				log.Print("the settings of the app: ", err)
			}
		}
	}
	return settings.values
}

// setting reads key of the settings of the app.
func setting(key string) string {
	settings.Lock()
	defer settings.Unlock()
	return loadSettings()[key]
}

func setSetting(key, value string) {
	settings.Lock()
	defer settings.Unlock()
	values := loadSettings()
	values[key] = value
	name, err := settingsFile()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(name), 0o755)
	}
	var b []byte
	if err == nil {
		b, err = json.MarshalIndent(values, "", "  ")
	}
	if err == nil {
		err = os.WriteFile(name, append(b, '\n'), 0o644)
	}
	if err != nil {
		log.Print("save the settings of the app: ", err)
	}
}

// language is the language of the user interface of Windows, such as ja.
func language() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return "en"
	}
	lang, _, _ := strings.Cut(langs[0], "-")
	return lang
}

// translateDialogs does nothing: WebView2 names the buttons of its dialogs.
func translateDialogs(map[string]string) {}

// buildVersion is the version of ebira the app was built with, which the build
// sets with -ldflags "-X main.buildVersion=...": the version of the server
// beside it.
var buildVersion string

func appVersion() string {
	if buildVersion != "" {
		return buildVersion
	}
	return "devel"
}

// showAboutPanel shows the name, the version and credits in a dialog, as
// Windows has no about panel of its own.
func (s *shell) showAboutPanel(credits string) {
	s.app.Dialog.Info().
		SetTitle(s.text("About ebira")).
		SetMessage("ebira\n\n" + credits).
		Show()
}

// helperCommand runs the ebira beside the app without a console window, which
// Windows would open for it, as the app has none.
func helperCommand(bin string, args ...string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}

// frameKey is the setting the frame of the main window is kept in, as
// "x,y,width,height".
const frameKey = "frame"

// showMain shows the main window, once its page is up, with the frame it had
// when the app last ended.
func showMain(win *application.WebviewWindow) {
	var x, y, w, h int
	if _, err := fmt.Sscanf(setting(frameKey), "%d,%d,%d,%d", &x, &y, &w, &h); err == nil && w >= 400 && h >= 300 {
		win.SetSize(w, h)
		win.SetPosition(x, y)
	}
	win.Show()
}

// saveFrame keeps the frame of the main window as the app ends. A minimised,
// maximised or full screen window keeps the frame it had before: a minimised
// one is far off the screen.
func saveFrame(win *application.WebviewWindow) {
	if win == nil || win.IsMinimised() || win.IsMaximised() || win.IsFullscreen() {
		return
	}
	x, y := win.Position()
	w, h := win.Size()
	setSetting(frameKey, fmt.Sprintf("%d,%d,%d,%d", x, y, w, h))
}
