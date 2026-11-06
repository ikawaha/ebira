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
// reports the answer to the raw message handler. Pages of servers built before
// window.ebira have leaveEditor alone.
const leave = `window.webkit.messageHandlers.external.postMessage((window.ebira?.leave ?? leaveEditor)() ? "ebira:left" : "ebira:stay");`

// leaveFor is leave, for changing the server instead of ending the app.
const leaveFor = `window.webkit.messageHandlers.external.postMessage((window.ebira?.leave ?? leaveEditor)() ? "ebira:switch" : "ebira:stay");`

// shell holds the windows of the app.
type shell struct {
	app      *application.App
	messages map[string]string
	notes    *noteMenu   // the items of the menus that depend on the page
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
	if data, ok := strings.CutPrefix(message, "ebira:{"); ok {
		s.onPageMessage("{" + data)
		return
	}
	switch message {
	case "ebira:left":
		s.leaving.Store(true)
		go s.app.Quit()
	case "ebira:switch":
		go s.finishSwitch()
	case "ebira:restart":
		go s.restartServer()
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
	// Each page, as after a change of the server, is checked for its server.
	win.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		go s.markOutdated()
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
