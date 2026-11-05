// Command ebira-desktop is the desktop app on Wails v3: a window around the Web UI
// of the ebira server.
package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
void installDelegate(void);
*/
import "C"

import (
	"flag"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// ready tells Wails the page is up. The Web UI does not load the Wails runtime,
// and ExecJS holds its scripts back until this message arrives.
const ready = `window.webkit.messageHandlers.external.postMessage("wails:runtime:ready");`

// leave asks the page to close its editor, which confirms unsaved changes, and
// reports the answer to the raw message handler.
const leave = `window.webkit.messageHandlers.external.postMessage(leaveEditor() ? "ebira:left" : "ebira:stay");`

// leaveFor is leave, for changing the server instead of ending the app.
const leaveFor = `window.webkit.messageHandlers.external.postMessage(leaveEditor() ? "ebira:switch" : "ebira:stay");`

// shell holds the windows of the app.
type shell struct {
	app      *application.App
	messages map[string]string
	leaving  atomic.Bool // the page agreed to go: closing goes through

	mu       sync.Mutex
	win      *application.WebviewWindow // the Web UI; nil until a server answers
	settings *application.WebviewWindow // nil while closed
	current  target                     // what the Web UI shows, or what failed
	next     *target                    // connected, waiting for the page to agree
}

func main() {
	notes := flag.String("notes", "", "notes directory (default: the one saved in the app, or in the config file of ebira)")
	addr := flag.String("addr", "", "address of the server (default: the one saved in the app, or localhost:13579)")
	flag.Parse()

	// WebKit reads the methods of its delegate when the web view is created, so
	// this has to come before the first window.
	C.installDelegate()

	s := &shell{messages: catalog(language()), current: saved()}
	if *notes != "" {
		s.current.Notes = *notes
	}
	if *addr != "" {
		s.current.Addr = *addr
	}
	translateDialogs(s.messages)
	s.app = application.New(application.Options{
		Name:              "ebira",
		ShouldQuit:        s.shouldQuit,
		RawMessageHandler: s.onMessage,
	})
	menu := s.appMenu()
	translateMenu(menu, s.messages)
	s.app.Menu.Set(menu)

	h, err := connect(s.current)
	if err != nil {
		log.Print(err)
		s.openSettings(err.Error())
	} else {
		s.current.Notes = h.NotesDir
		s.openMain(s.current.url())
	}
	if err := s.app.Run(); err != nil {
		log.Fatal(err)
	}
}

func (s *shell) main() *application.WebviewWindow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.win
}

func (s *shell) shouldQuit() bool {
	win := s.main()
	if s.leaving.Load() || win == nil {
		return true
	}
	go win.ExecJS(leave)
	return false
}

func (s *shell) onMessage(from application.Window, message string, _ *application.OriginInfo) {
	if rest, ok := strings.CutPrefix(message, settingsPrefix); ok {
		s.onSettingsMessage(from, rest)
		return
	}
	if win := s.main(); win == nil || from.ID() != win.ID() {
		return
	}
	switch message {
	case "ebira:left":
		s.leaving.Store(true)
		go s.app.Quit()
	case "ebira:switch":
		go s.finishSwitch()
	}
}

// openMain opens the window of the Web UI.
func (s *shell) openMain(url string) {
	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "ebira",
		Width:  1280,
		Height: 800,
		URL:    url,
		JS:     ready,
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !s.leaving.Load() {
			e.Cancel()
			go win.ExecJS(leave)
		}
	})
	s.mu.Lock()
	s.win = win
	s.mu.Unlock()
}

// appMenu is the menu bar. It has no reload: WebKit would drop unsaved edits,
// since Wails does not ask about the page's beforeunload.
func (s *shell) appMenu() *application.Menu {
	menu := s.app.Menu.New()
	about := menu.AddSubmenu("ebira")
	about.AddRole(application.About)
	about.AddSeparator()
	about.Add("Settings…").SetAccelerator("cmd+,").OnClick(func(*application.Context) {
		s.openSettings("")
	})
	about.AddSeparator()
	about.AddRole(application.ServicesMenu)
	about.AddSeparator()
	about.AddRole(application.Hide)
	about.AddRole(application.HideOthers)
	about.AddRole(application.UnHide)
	about.AddSeparator()
	about.AddRole(application.Quit)
	file := menu.AddSubmenu("File")
	file.Add("New Note").SetAccelerator("cmd+n").OnClick(func(*application.Context) {
		if win := s.main(); win != nil {
			win.ExecJS(`document.getElementById("new-note").click();`)
		}
	})
	file.AddSeparator()
	file.AddRole(application.CloseWindow)
	menu.AddRole(application.EditMenu)
	view := menu.AddSubmenu("View")
	view.AddRole(application.ResetZoom)
	view.AddRole(application.ZoomIn)
	view.AddRole(application.ZoomOut)
	view.AddSeparator()
	view.AddRole(application.ToggleFullscreen)
	menu.AddRole(application.WindowMenu)
	return menu
}
