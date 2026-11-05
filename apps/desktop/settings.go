package main

/*
#include <stdlib.h>
char *loadSetting(const char *key);
void saveSetting(const char *key, const char *value);
*/
import "C"

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"log"
	"net"
	"strings"
	"unsafe"

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
	win, current := s.settings, s.current
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
	if err := tmpl.Execute(&page, map[string]string{"Notes": current.Notes, "Addr": current.Addr, "Problem": problem}); err != nil {
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
	h, err := connect(t)
	if err != nil {
		s.showProblem(err.Error())
		return
	}
	t.Notes = h.NotesDir
	s.mu.Lock()
	s.next = &t
	win := s.win
	s.mu.Unlock()
	if win == nil {
		s.finishSwitch()
		return
	}
	win.ExecJS(leaveFor)
	win.Focus()
}

// finishSwitch shows the server that apply connected to.
func (s *shell) finishSwitch() {
	s.mu.Lock()
	t, win, settings := s.next, s.win, s.settings
	if t == nil {
		s.mu.Unlock()
		return
	}
	s.next = nil
	s.current = *t
	s.mu.Unlock()
	t.save()
	if win == nil {
		s.openMain(t.url())
	} else {
		win.SetURL(t.url())
	}
	if settings != nil {
		settings.Close()
	}
}
