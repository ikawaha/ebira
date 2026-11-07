package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
void installDelegate(void);
void keepFrame(void *window, const char *name);
char *loadSetting(const char *key);
void saveSetting(const char *key, const char *value);
char *preferredLanguage(void);
void setDialogLabels(const char *ok, const char *cancel);
char *bundleString(const char *key);
void showAbout(const char *credits);
*/
import "C"

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// What the app does on macOS through Cocoa (dialogs_darwin.m); see
// platform_windows.go for Windows.

// platformInit adds what WebKit lacks in Wails to its delegate. WebKit reads the
// methods of its delegate when the web view is created, so this has to come
// before the first window.
func platformInit() { C.installDelegate() }

// setting reads key of the settings of the app, in the user defaults.
func setting(key string) string {
	k := C.CString(key)
	defer C.free(unsafe.Pointer(k))
	v := C.loadSetting(k)
	defer C.free(unsafe.Pointer(v))
	return C.GoString(v)
}

func setSetting(key, value string) {
	k, v := C.CString(key), C.CString(value)
	defer C.free(unsafe.Pointer(k))
	defer C.free(unsafe.Pointer(v))
	C.saveSetting(k, v)
}

// language is the language of the system, such as ja.
func language() string {
	p := C.preferredLanguage()
	defer C.free(unsafe.Pointer(p))
	lang, _, _ := strings.Cut(C.GoString(p), "-")
	return lang
}

// translateDialogs names the buttons of the confirm and alert of the page.
func translateDialogs(messages map[string]string) {
	text := func(s string) *C.char {
		if t, ok := messages[s]; ok {
			s = t
		}
		return C.CString(s) // kept by the dialogs for the life of the app
	}
	C.setDialogLabels(text("OK"), text("Cancel"))
}

// appVersion is the version of ebira the app was built with, which build.sh
// writes to Info.plist: the version of the server in the bundle.
func appVersion() string {
	k := C.CString("EbiraVersion")
	defer C.free(unsafe.Pointer(k))
	v := C.bundleString(k)
	defer C.free(unsafe.Pointer(v))
	if s := C.GoString(v); s != "" {
		return s
	}
	return "devel"
}

// showAboutPanel shows the standard about panel with credits.
func (s *shell) showAboutPanel(credits string) {
	c := C.CString(credits)
	defer C.free(unsafe.Pointer(c))
	C.showAbout(c)
}

// frameName is the name the frame of the main window is saved under, in the
// user defaults as "NSWindow Frame <name>".
const frameName = "ebira"

// showMain shows the main window, once its page is up as Wails would, with the
// frame it had when the app last ended, which AppKit keeps.
func showMain(win *application.WebviewWindow) {
	name := C.CString(frameName)
	defer C.free(unsafe.Pointer(name))
	application.InvokeSync(func() { C.keepFrame(win.NativeWindow(), name) })
	win.Show()
}

// logPath is the log of the app: ~/Library/Logs/ebira/desktop.log, where
// Console finds the logs of apps.
func logPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "ebira", "desktop.log"), nil
}

// watchReady is left to the JS of the window on macOS (ready), which WebKit
// runs in each page.
func watchReady(*application.WebviewWindow) {}

// saveFrame is left to AppKit on macOS (showMain).
func saveFrame(*application.WebviewWindow) {}

// helperCommand runs the ebira of the bundle.
func helperCommand(bin string, args ...string) *exec.Cmd { return exec.Command(bin, args...) }
