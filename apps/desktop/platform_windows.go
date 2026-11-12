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
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
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

// logPath is the log of the app: %LocalAppData%\ebira\desktop.log, on the
// machine rather than with the roaming settings.
func logPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ebira", "desktop.log"), nil
}

// watchReady tells Wails that each page of win is up, as ready does on macOS.
// Wails on Windows drops the JS of a window opened at a URL, and only puts
// the core of its runtime in the page, which does not tell it: the full
// runtime, which the Web UI does not load, would. Without this, the window
// created hidden is never shown, and ExecJS holds its scripts back.
func watchReady(win *application.WebviewWindow) {
	win.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		win.HandleMessage("wails:runtime:ready")
	})
}

// showMain shows the main window, once its page is up, with the frame it had
// when the app last ended, and gives the keys to its page. A window created
// hidden and shown later does not pass them on to WebView2 by itself: the page
// takes no typing and draws no caret, and the IME shows its candidates in a
// corner of the screen, until the window is clicked or switched to.
func showMain(win *application.WebviewWindow) {
	var x, y, w, h int
	if _, err := fmt.Sscanf(setting(frameKey), "%d,%d,%d,%d", &x, &y, &w, &h); err == nil && w >= 400 && h >= 300 {
		win.SetSize(w, h)
		win.SetPosition(x, y)
	}
	win.Show()
	win.Focus()
}

// themeFrame gives the frame of win, its title bar, the dark or the light
// theme.
func themeFrame(win *application.WebviewWindow, dark bool) {
	application.InvokeAsync(func() { w32.SetTheme(uintptr(win.NativeWindow()), dark) })
}

// frameTheme is the theme the main window is made with: the system's now,
// fixed rather than SystemDefault, under which Wails would set the frame to
// the system's theme on each change of it, racing with the page's theme (its
// listeners run in goroutines of their own). The app follows the changes
// itself (watchSystemTheme).
func frameTheme() application.Theme {
	if systemDark() {
		return application.Dark
	}
	return application.Light
}

// systemDark reports whether the system's apps are in the dark mode.
func systemDark() bool { return w32.IsCurrentlyDarkMode() }

// watchSystemTheme calls again when the theme of the system changes, for the
// frame of a page that follows the system, or of one that tells no theme.
func watchSystemTheme(app *application.App, again func()) {
	app.Event.OnApplicationEvent(events.Windows.SystemThemeChanged, func(*application.ApplicationEvent) { again() })
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

// downloadsDir is where the files of notes are saved: the Downloads folder of
// the user.
func downloadsDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
}

// markDownloaded marks a saved file as one from the internet, as browsers do,
// so that Windows checks it when it is opened.
func markDownloaded(path string) error {
	return os.WriteFile(path+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n"), 0o644)
}
