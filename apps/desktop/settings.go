package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
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
func (s *shell) openSettings(problem string) { s.openSettingsWith(problem, "") }

// openSettingsWith opens the settings window as openSettings does, with an
// introduction above the fields when it opens, as at the first start. Then
// the window starts in English, intro too, and speaks the language chosen in
// it, which may not be the one of the system.
func (s *shell) openSettingsWith(problem, intro string) {
	s.mu.Lock()
	win, current, chosen, broken := s.settings, s.current, s.chosen, s.broken
	s.mu.Unlock()
	if win != nil {
		win.Focus()
		if problem != "" {
			s.showProblem(problem)
		}
		return
	}
	var page bytes.Buffer
	text, title := s.text, s.text("Settings")
	var messages map[string]map[string]string
	if intro != "" {
		text = func(english string) string { return english }
		title = "Settings"
		messages = map[string]map[string]string{"ja": catalog("ja")}
	}
	tmpl := template.Must(template.New("settings").Funcs(template.FuncMap{"t": text}).Parse(settingsHTML))
	// The folder as chosen: empty follows the config file of ebira, which the
	// placeholder shows as the server found it.
	if err := tmpl.Execute(&page, map[string]any{"Notes": chosen.Notes, "NotesFound": current.Notes, "Addr": chosen.Addr, "Problem": problem, "Intro": intro, "Messages": messages, "Broken": broken}); err != nil {
		panic(err) // embedded: a broken file is a broken build
	}
	height := settingsSize(intro != "", broken != "")
	win = s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:         title,
		Width:         settingsWidth,
		Height:        height,
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
	case message == "openSettingsFile":
		go s.openSettingsFile()
	case strings.HasPrefix(message, "apply:"), strings.HasPrefix(message, "safe:"):
		// safe: Apply in the safe mode, without the settings file of ebira
		// while it has an error.
		verb, data, _ := strings.Cut(message, ":")
		var m struct {
			target
			firstSetup
			Keep bool `json:"keep"` // the folder is taken although it is no notes of ebira
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			log.Print(err)
			return
		}
		if verb == "safe" {
			safeMode.Store(true)
		}
		go s.apply(m.target, m.firstSetup, m.Keep)
	}
}

// brokenSettingsText is the problem the settings window tells when the settings
// file of ebira has an error; %s is the error.
const brokenSettingsText = "The settings file of ebira has an error, and the server cannot start with it:\n%s\nFix the file and press Apply, or start the server in safe mode, which leaves the file out until it is fixed."

// chooseNotesText is added to brokenSettingsText when not even notes_dir
// reads from the settings file of ebira, as the safe mode needs a folder.
const chooseNotesText = "Its notes_dir cannot be read either: choose the notes folder above to start in safe mode."

// settingsWidth is the width of the settings window.
const settingsWidth = 560

// settingsSize returns the height of the settings window: taller with the
// introduction, and the language and the search mode, of the first start,
// and with the buttons of a settings file of ebira with an error
// (offerSafeMode).
func settingsSize(intro, broken bool) int {
	height := 300
	if intro {
		height += 190
	}
	if broken {
		height += 120 // the buttons, and room for the error
	}
	return height
}

// offerSafeMode tells in the settings window that the settings file of ebira
// has an error, when it does and the server of t is of this computer, which
// then cannot start, with buttons to open the file and to start the server in
// safe mode. It reports whether it did.
func (s *shell) offerSafeMode(t target) bool {
	if !local(t.Addr) {
		return false
	}
	st := brokenSettings()
	if st == nil {
		return false
	}
	s.mu.Lock()
	s.broken = st.SettingsFile
	s.mu.Unlock()
	problem := fmt.Sprintf(s.pageText(brokenSettingsText), st.SettingsError)
	if t.Notes == "" && st.Configured != nil && !*st.Configured {
		problem += "\n" + s.pageText(chooseNotesText)
	}
	if win := s.settingsWindow(); win != nil {
		s.mu.Lock()
		first := s.first
		s.mu.Unlock()
		win.SetSize(settingsWidth, settingsSize(first, true))
		s.callSettings("showBroken", st.SettingsFile)
		s.showProblem(problem)
		return true
	}
	s.openSettings(problem)
	return true
}

// openSettingsFile opens the settings file of ebira with an error in an
// editor of text, to fix it.
func (s *shell) openSettingsFile() {
	s.mu.Lock()
	path := s.broken
	s.mu.Unlock()
	if path == "" {
		return
	}
	if err := openTextFile(path); err != nil {
		log.Print("open the settings file: ", err)
		s.showProblem(err.Error())
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

// keepFolder asks whether to keep the notes in a folder with files but no
// notes of ebira; {dir} is the folder.
const keepFolder = "{dir} has files, but no notes of ebira. ebira takes the folders in it for notebooks and its YAML files for notes; other files are left as they are. Keep the notes there?"

// askKeep has the settings page ask keepFolder for dir, which applies again
// with keep when the user does.
func (s *shell) askKeep(dir string) {
	win := s.settingsWindow()
	if win == nil {
		return
	}
	text, _ := json.Marshal(s.pageText(keepFolder))
	path, _ := json.Marshal(dir)
	win.ExecJS("askKeep(" + string(text) + ", " + string(path) + ");")
}

// pageText is english as the settings page shows it: as it is at the first
// start, when the page speaks the language chosen in it, which translates
// it, else in the language of the app.
func (s *shell) pageText(english string) string {
	s.mu.Lock()
	first := s.first
	s.mu.Unlock()
	if first {
		return english
	}
	return s.text(english)
}

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
// is. Then the page decides, as it may hold unsaved changes. At the first
// start, setup has the language and the search mode chosen for new notes.
// A folder of this computer with files but no notes of ebira is taken only
// once the user keeps it, as setup of the commands asks.
func (s *shell) apply(t target, setup firstSetup, keep bool) {
	if _, _, err := net.SplitHostPort(t.Addr); err != nil {
		s.showProblem(s.pageText("Write the address as host:port."))
		return
	}
	if !keep && t.Notes != "" && local(t.Addr) && foreignFolder(t.Notes) {
		s.askKeep(t.Notes)
		return
	}
	// Each Apply connects on its own, and may take its time: only the last
	// one counts, and the ones before it are dropped as they end.
	s.mu.Lock()
	s.applies++
	this := s.applies
	// At the first start the folder goes to the config file of ebira, which
	// the commands follow too, and the app follows it as well.
	var first *firstSetup
	if s.first && t.Notes != "" && local(t.Addr) {
		first = &setup
	}
	s.mu.Unlock()
	h, err := connect(t, first)
	s.mu.Lock()
	if this != s.applies {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.mu.Unlock()
		if !s.offerSafeMode(t) {
			s.showProblem(err.Error())
		}
		return
	}
	chosen := t
	if first != nil {
		s.first = false
		chosen.Notes = ""
	}
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
	s.forgetTheme() // the page of another server may tell none
	if win == nil {
		s.openMain(t.url())
	} else {
		win.SetURL(t.url())
	}
	if settings != nil {
		settings.Close()
	}
}
