package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"log"
	"net"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// settingsPrefix starts the messages of the settings page.
const settingsPrefix = "ebira:settings:"

const defaultAddr = "localhost:13579"

//go:embed settings.html
var settingsHTML string

// target is a server to show: an empty Notes takes the notes directory of the
// config file of ebira.
type target struct {
	Notes string `json:"notes"`
	Addr  string `json:"addr"`
}

func (t target) url() string { return "http://" + t.Addr + "/" }

// saved reads the settings from UserDefaults.
func saved() target {
	t := target{Notes: setting("notesDir"), Addr: setting("addr")}
	if t.Addr == "" {
		t.Addr = defaultAddr
	}
	return t
}

func (t target) save() {
	setSetting("notesDir", t.Notes)
	setSetting("addr", t.Addr)
}

func (s *shell) text(english string) string {
	if t, ok := s.messages[english]; ok {
		return t
	}
	return english
}

// openSettings opens the settings window, or brings it up, with a problem to
// show if there is one.
func (s *shell) openSettings(problem string) {
	s.mu.Lock()
	win, current, chosen := s.settings, s.current, s.chosen
	s.mu.Unlock()
	if win != nil {
		win.Focus()
		if problem != "" {
			s.showProblem(problem)
		}
		return
	}
	var page bytes.Buffer
	tmpl := template.Must(template.New("settings").Funcs(template.FuncMap{"t": s.text}).Parse(settingsHTML))
	// The folder as chosen: empty follows the config file of ebira, which the
	// placeholder shows as the server found it.
	if err := tmpl.Execute(&page, map[string]string{"Notes": chosen.Notes, "NotesFound": current.Notes, "Addr": chosen.Addr, "Problem": problem}); err != nil {
		panic(err) // embedded: a broken file is a broken build
	}
	win = s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:         s.text("Settings"),
		Width:         560,
		Height:        300,
		DisableResize: true,
		HTML:          page.String(),
		JS:            ready,
	})
	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		s.mu.Lock()
		s.settings = nil
		alone := s.win == nil
		s.mu.Unlock()
		// The app ends with its last window. Wails can do this by itself, but
		// it counts the windows too early when one opens as another closes.
		if alone {
			go s.app.Quit()
		}
	})
	s.mu.Lock()
	s.settings = win
	s.mu.Unlock()
}

func (s *shell) settingsWindow() *application.WebviewWindow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

func (s *shell) onSettingsMessage(from application.Window, message string) {
	win := s.settingsWindow()
	if win == nil || from.ID() != win.ID() {
		return // the Web UI has no say in the settings
	}
	switch {
	case message == "close":
		go win.Close()
	case message == "choose":
		go s.chooseNotes()
	case strings.HasPrefix(message, "apply:"):
		var t target
		if err := json.Unmarshal([]byte(strings.TrimPrefix(message, "apply:")), &t); err != nil {
			log.Print(err)
			return
		}
		go s.apply(t)
	}
}

func (s *shell) chooseNotes() {
	dialog := s.app.Dialog.OpenFile().
		SetTitle(s.text("Notes folder")).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true)
	if win := s.settingsWindow(); win != nil {
		dialog.AttachToWindow(win)
	}
	dir, err := dialog.PromptForSingleSelection()
	if err != nil || dir == "" {
		return
	}
	s.callSettings("setNotes", dir)
}

func (s *shell) showProblem(problem string) { s.callSettings("showProblem", problem) }

func (s *shell) callSettings(function, arg string) {
	win := s.settingsWindow()
	if win == nil {
		return
	}
	data, err := json.Marshal(arg)
	if err != nil {
		panic(err) // a string always marshals
	}
	win.ExecJS(function + "(" + string(data) + ");")
}

// apply connects to the server first: while that can fail, the page stays as it
// is. Then the page decides, as it may hold unsaved changes.
func (s *shell) apply(t target) {
	if _, _, err := net.SplitHostPort(t.Addr); err != nil {
		s.showProblem(s.text("Write the address as host:port."))
		return
	}
	// Each Apply connects on its own, and may take its time: only the last
	// one counts, and the ones before it are dropped as they end.
	s.mu.Lock()
	s.applies++
	this := s.applies
	s.mu.Unlock()
	h, err := connect(t)
	s.mu.Lock()
	if this != s.applies {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.mu.Unlock()
		s.showProblem(err.Error())
		return
	}
	chosen := t
	t.Notes = h.NotesDir
	s.next, s.nextAs = &t, chosen
	win := s.win
	s.mu.Unlock()
	if win == nil {
		s.finishSwitch()
		return
	}
	win.ExecJS(leaveFor)
	win.Focus()
}

// stayed drops the server that apply connected to when the page keeps its
// editor instead, and tells the settings window why nothing changed.
func (s *shell) stayed() {
	s.mu.Lock()
	switching := s.next != nil
	s.next = nil
	s.mu.Unlock()
	if switching {
		s.showProblem(s.text("The note being edited was kept, so the server was not changed."))
	}
}

// finishSwitch shows the server that apply connected to.
func (s *shell) finishSwitch() {
	s.mu.Lock()
	t, chosen, win, settings := s.next, s.nextAs, s.win, s.settings
	if t == nil {
		s.mu.Unlock()
		return
	}
	s.next = nil
	s.current, s.chosen = *t, chosen
	s.mu.Unlock()
	// As chosen, so that an empty folder keeps following the config file.
	chosen.save()
	s.notes.reset() // until the page of the server tells its state
	if win == nil {
		s.openMain(t.url())
	} else {
		win.SetURL(t.url())
	}
	if settings != nil {
		settings.Close()
	}
}
