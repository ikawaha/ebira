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
	"time"
	"unsafe"

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
// theme, and leaves the menus that drop down to the system (plainDropdowns).
func themeFrame(win *application.WebviewWindow, dark bool) {
	application.InvokeAsync(func() {
		hwnd := uintptr(win.NativeWindow())
		w32.SetTheme(hwnd, dark)
		plainDropdowns(hwnd)
	})
}

// systemDark reports whether the system's apps are in the dark mode.
func systemDark() bool { return w32.IsCurrentlyDarkMode() }

// watchSystemTheme calls again when the theme of the system changes. Wails
// gives the frames the system's theme then, from a listener of its own
// goroutine, so again is called a while later to be the last.
func watchSystemTheme(app *application.App, again func()) {
	app.Event.OnApplicationEvent(events.Windows.SystemThemeChanged, func(*application.ApplicationEvent) {
		time.AfterFunc(systemThemeDelay, func() { application.InvokeAsync(again) })
	})
}

// systemThemeDelay is how long after a change of the system's theme the
// frame takes the page's again.
const systemThemeDelay = 300 * time.Millisecond

// themeKey is the setting with the theme the page last told: dark or light.
const themeKey = "theme"

// The menu bar is drawn by Wails, in the colours of the options of the window
// (CustomTheme), from brushes it makes when it makes the window and when the
// system's theme changes; it reads the colours of the text at each paint.
// The colours are those of the theme the page last told, kept in the
// settings, else of the system's mode, under both modes of the system, so
// that the menu bar matches the page from the start. They are not changed
// while the app runs, as the brushes would not follow the text: a theme the
// page changes to reaches the menu bar on the next start.
var menuBarColours struct {
	text, background, hoverText, hoverBackground uint32
}

// windowsOptions are the options of the main window on Windows: the menu bar
// in the colours of the theme the page last told, else of the system's mode.
func windowsOptions() application.WindowsWindow {
	dark := systemDark()
	switch setting(themeKey) {
	case "dark":
		dark = true
	case "light":
		dark = false
	}
	c := &menuBarColours
	if dark {
		c.text, c.background = w32.RGB(222, 222, 222), w32.RGB(33, 33, 33)
		c.hoverText, c.hoverBackground = w32.RGB(222, 222, 222), w32.RGB(48, 48, 48)
	} else {
		c.text, c.background = w32.RGB(0, 0, 0), w32.RGB(255, 255, 255)
		c.hoverText, c.hoverBackground = w32.RGB(0, 0, 0), w32.RGB(229, 229, 229)
	}
	normal := &application.TextTheme{Text: &c.text, Background: &c.background}
	hover := &application.TextTheme{Text: &c.hoverText, Background: &c.hoverBackground}
	bar := &application.MenuBarTheme{Default: normal, Hover: hover, Selected: hover}
	return application.WindowsWindow{CustomTheme: application.ThemeSettings{LightModeMenuBar: bar, DarkModeMenuBar: bar}}
}

// keepPageTheme keeps the theme the page told for the menu bar of the next
// start (see menuBarColours).
func keepPageTheme(dark bool) {
	v := "light"
	if dark {
		v = "dark"
	}
	if setting(themeKey) != v {
		setSetting(themeKey, v)
	}
}

// procGetSubMenu is GetSubMenu of user32, which w32 lacks.
var procGetSubMenu = windows.NewLazySystemDLL("user32.dll").NewProc("GetSubMenu")

// plainDropdowns gives the menus that drop down from the menu bar of the
// window hwnd the system's background again. Wails gives them that of the
// menu bar (MIIM_APPLYTOSUBMENUS) when it makes the window and when the
// system's theme changes, but Windows draws their text in the system's mode:
// a dark background on light Windows would have dark text on it.
func plainDropdowns(hwnd uintptr) {
	bar := w32.GetMenu(hwnd)
	if bar == 0 {
		return
	}
	for i := range w32.GetMenuItemCount(bar) {
		sub, _, _ := procGetSubMenu.Call(bar, uintptr(i))
		if sub == 0 {
			continue
		}
		mi := w32.MENUINFO{FMask: w32.MIIM_BACKGROUND | w32.MIIM_APPLYTOSUBMENUS}
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		w32.SetMenuInfo(sub, &mi)
	}
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
