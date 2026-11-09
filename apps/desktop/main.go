// Command ebira-desktop is the desktop app on Wails v3: a window around the Web UI
// of the ebira server.
package main

import (
	"flag"
	"log"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// toApp is where the JS the app runs in the page sends the app messages: the
// handler of WebKit on macOS, WebView2 on Windows (appChannel of the page).
const toApp = `(window.webkit?.messageHandlers?.external ?? window.chrome?.webview)`

// ready tells Wails the page is up, as the Web UI does not load the Wails
// runtime and ExecJS holds its scripts back until this message arrives. Wails
// on Windows runs it for the settings window, opened with HTML, but drops it
// for the main window, opened at a URL (watchReady).
const ready = toApp + `?.postMessage("wails:runtime:ready");`

// leaves asks the page to close its editor, which confirms unsaved changes:
// true when it may go. Pages of servers built before window.ebira have
// leaveEditor alone, and a page that is not the web UI, as when the server is
// gone, has neither and nothing to keep, so it may go: else the app could
// never quit nor close its window.
const leaves = `(window.ebira?.leave ?? window.leaveEditor ?? (() => true))()`

// leave asks the page to leave and reports the answer to the raw message
// handler.
const leave = toApp + `.postMessage(` + leaves + ` ? "ebira:left" : "ebira:stay");`

// leaveFor is leave, for changing the server instead of ending the app.
const leaveFor = toApp + `.postMessage(` + leaves + ` ? "ebira:switch" : "ebira:stay");`

// shell holds the windows of the app.
type shell struct {
	app       *application.App
	messages  map[string]string
	notes     *noteMenu   // the items of the menus that depend on the page
	leaving   atomic.Bool // the page agreed to go: closing goes through
	downloads downloads   // the files the page is sending to save

	mu       sync.Mutex
	win      *application.WebviewWindow // the Web UI; nil until a server answers
	settings *application.WebviewWindow // nil while closed
	current  target                     // what the Web UI shows, or what failed
	chosen   target                     // current as the user gave it: see target
	next     *target                    // connected, waiting for the page to agree
	nextAs   target                     // next as the user gave it
	applies  uint64                     // the Applies of the settings so far: the last counts
}

func main() {
	notes := flag.String("notes", "", "notes directory (default: the one saved in the app, or in the config file of ebira)")
	addr := flag.String("addr", "", "address of the server (default: the one saved in the app, or localhost:13579)")
	flag.CommandLine.Parse(appArgs(os.Args[1:])) // exits on an error

	platformInit()
	logger := setupLog()

	s := &shell{messages: catalog(language()), current: saved()}
	if *notes != "" {
		s.current.Notes = *notes
	}
	if *addr != "" {
		s.current.Addr = *addr
	}
	s.chosen = s.current
	translateDialogs(s.messages)
	s.app = application.New(application.Options{
		Name:              "ebira",
		ShouldQuit:        s.shouldQuit,
		RawMessageHandler: s.onMessage,
		Logger:            logger,
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

// appArgs drops the arguments meant for Cocoa, which flag would refuse:
// "-Key value" pairs that it reads as user defaults, such as
// -ApplePersistenceIgnoreState YES or -NSDocumentRevisionsDebugMode YES, and the
// process serial number older systems give an app opened from the Finder.
func appArgs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case strings.HasPrefix(a, "-psn_"):
		case strings.HasPrefix(a, "-Apple"), strings.HasPrefix(a, "-NS"):
			i++ // and its value
		default:
			out = append(out, a)
		}
	}
	return out
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

func (s *shell) onMessage(from application.Window, message string, origin *application.OriginInfo) {
	if rest, ok := strings.CutPrefix(message, settingsPrefix); ok {
		s.onSettingsMessage(from, rest)
		return
	}
	if win := s.main(); win == nil || from.ID() != win.ID() {
		return
	}
	if !s.fromServer(origin) {
		log.Printf("a message from another page than the server's is ignored: %+v", origin)
		return
	}
	if data, ok := strings.CutPrefix(message, "ebira:{"); ok {
		s.onPageMessage("{" + data)
		return
	}
	switch message {
	case "ebira:left":
		saveFrame(s.main())
		s.leaving.Store(true)
		go s.app.Quit()
	case "ebira:switch":
		go s.finishSwitch()
	case "ebira:restart":
		go s.restartServer()
	case "ebira:stay":
		go s.stayed()
	}
}

// fromServer reports whether a message in the main window comes from the page
// of the server, or of the one it switches to: a link could take the window
// to another site, whose page could send the app messages too. The webview
// gives the URL of the frame that sent it (and on Windows of the top one),
// which the page of the server always has.
func (s *shell) fromServer(origin *application.OriginInfo) bool {
	if origin == nil {
		return false
	}
	s.mu.Lock()
	servers := []string{s.current.url()}
	if s.next != nil {
		servers = append(servers, s.next.url())
	}
	s.mu.Unlock()
	from := func(u string) bool {
		return slices.ContainsFunc(servers, func(server string) bool { return sameOrigin(u, server) })
	}
	return from(origin.Origin) && (origin.TopOrigin == "" || from(origin.TopOrigin))
}

// sameOrigin reports whether the URLs a and b have the same scheme, host and
// port.
func sameOrigin(a, b string) bool {
	u, err := url.Parse(a)
	if err != nil || u.Host == "" {
		return false
	}
	v, err := url.Parse(b)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, v.Scheme) && strings.EqualFold(u.Host, v.Host)
}

// openMain opens the window of the Web UI.
func (s *shell) openMain(url string) {
	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "ebira",
		Width:  1280,
		Height: 800,
		URL:    url,
		JS:     ready,
		// Shown by showMain, where it was, rather than moved there in sight.
		Hidden: true,
		// The menu of the app is the menu bar of the window on Windows.
		UseApplicationMenu: true,
		// The page shows the name; the title stays for the Window menu and
		// Mission Control.
		Mac: application.MacWindow{TitleBar: application.MacTitleBar{HideTitle: true}},
	})
	var shown sync.Once
	// The runtime is ready once a page is up: on macOS when the JS of the window
	// tells it (ready), on Windows when the page has loaded (watchReady).
	watchReady(win)
	win.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		shown.Do(func() { go showMain(win) })
		// Each page, as after a change of the server, is checked for its server.
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
