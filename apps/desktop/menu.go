package main

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// pageState is what the Web UI tells the app with its "state" message: what
// its menus can do now.
type pageState struct {
	Note      bool `json:"note"`      // a note is shown
	Trash     bool `json:"trash"`     // the note shown is in the trash
	Editing   bool `json:"editing"`   // a note is being edited
	Notebook  bool `json:"notebook"`  // the notebook shown can be renamed
	TrashFull bool `json:"trashFull"` // the trash has notes
	Git       bool `json:"git"`       // the notes are in a git repository
}

// noteMenu holds the items that depend on the state of the page.
type noteMenu struct {
	mu    sync.Mutex
	items []stateItem
	trash *application.MenuItem
	text  func(string) string
}

type stateItem struct {
	item    *application.MenuItem
	enabled func(pageState) bool
}

func shown(p pageState) bool        { return p.Note && !p.Editing }
func shownOutside(p pageState) bool { return shown(p) && !p.Trash }
func shownInTrash(p pageState) bool { return shown(p) && p.Trash }
func editing(p pageState) bool      { return p.Editing }
func notEditing(p pageState) bool   { return !p.Editing }
func always(pageState) bool         { return true }
func renamable(p pageState) bool    { return p.Notebook && !p.Editing }
func trashFull(p pageState) bool    { return p.TrashFull && !p.Editing }
func inGit(p pageState) bool        { return p.Git }

func (n *noteMenu) add(item *application.MenuItem, enabled func(pageState) bool) {
	n.items = append(n.items, stateItem{item, enabled})
}

// update enables the items that do something in state p.
func (n *noteMenu) update(p pageState) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, i := range n.items {
		i.item.SetEnabled(i.enabled(p))
	}
	if p.Trash {
		n.trash.SetLabel(n.text("Delete Permanently…"))
	} else {
		n.trash.SetLabel(n.text("Move to Trash"))
	}
}

// reset enables every item, as for a page that tells nothing: one of a server
// built before the page told its state.
func (n *noteMenu) reset() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, i := range n.items {
		i.item.SetEnabled(true)
	}
	n.trash.SetLabel(n.text("Move to Trash"))
}

// call runs a function of window.ebira in the Web UI. A page without it, from a
// server of an earlier build, does nothing.
func (s *shell) call(function string, args ...any) {
	win := s.main()
	if win == nil {
		return
	}
	list := ""
	for i, a := range args {
		data, err := json.Marshal(a)
		if err != nil {
			panic(err) // the arguments are plain values
		}
		if i > 0 {
			list += ", "
		}
		list += string(data)
	}
	win.ExecJS("window.ebira?." + function + "?.(" + list + ");")
}

// onPageMessage reads a message of tellApp in the Web UI.
func (s *shell) onPageMessage(data string) {
	var m struct {
		Type string `json:"type"`
		pageState
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		log.Print("a message of the page: ", err)
		return
	}
	switch m.Type {
	case "state":
		s.notes.update(m.pageState)
	case "restart": // the mark of an outdated server (markOutdated)
		go s.askRestart()
	case "copy":
		if !s.app.Clipboard.SetText(m.Text) {
			log.Printf("the clipboard took no text of %d bytes", len(m.Text))
		}
	}
}

// issueURL is where requests and bug reports of ebira go (ISSUE_URL of the web
// UI).
const issueURL = "https://github.com/ikawaha/ebira/issues/new"

// appMenu is the menu bar. It has no reload: WebKit would drop unsaved edits,
// since Wails does not ask about the page's beforeunload.
func (s *shell) appMenu() *application.Menu {
	n := &noteMenu{text: s.text}
	s.notes = n
	on := func(f func()) func(*application.Context) {
		return func(*application.Context) { f() }
	}
	menu := s.app.Menu.New()
	about := menu.AddSubmenu("ebira")
	about.Add("About ebira").OnClick(on(func() { go s.showAbout() }))
	about.AddSeparator()
	about.Add("Settings…").SetAccelerator("cmd+,").OnClick(on(func() { s.openSettings("") }))
	n.add(about.Add("Server Status…").OnClick(on(func() { s.call("serverStats") })), always)
	about.AddSeparator()
	about.AddRole(application.ServicesMenu)
	about.AddSeparator()
	about.AddRole(application.Hide)
	about.AddRole(application.HideOthers)
	about.AddRole(application.UnHide)
	about.AddSeparator()
	about.AddRole(application.Quit)

	file := menu.AddSubmenu("File")
	n.add(file.Add("New Note").SetAccelerator("cmd+n").OnClick(on(func() { s.call("create") })), always)
	n.add(file.Add("New Notebook…").SetAccelerator("cmd+shift+n").OnClick(on(func() { s.call("newNotebook") })), notEditing)
	file.AddSeparator()
	n.add(file.Add("Import Markdown…").OnClick(on(func() { s.call("importFiles") })), notEditing)
	n.add(file.Add("Check Images…").OnClick(on(func() { s.call("checkFiles") })), always)
	n.add(file.Add("Sync with Git").OnClick(on(func() { s.call("syncGit") })), inGit)
	file.AddSeparator()
	file.AddRole(application.CloseWindow)

	menu.AddRole(application.EditMenu)

	note := menu.AddSubmenu("Note")
	n.add(note.Add("Edit Note").SetAccelerator("cmd+e").OnClick(on(func() { s.call("edit") })), shownOutside)
	n.add(note.Add("Save").SetAccelerator("cmd+s").OnClick(on(func() { s.call("save") })), editing)
	note.AddSeparator()
	n.add(note.Add("Find in Note").SetAccelerator("cmd+f").OnClick(on(func() { s.call("find") })), shown)
	n.add(note.Add("Search Notes").SetAccelerator("cmd+alt+f").OnClick(on(func() { s.call("search") })), always)
	note.AddSeparator()
	n.add(note.Add("Mark…").OnClick(on(func() { s.call("mark") })), shownOutside)
	n.add(note.Add("History…").SetAccelerator("cmd+y").OnClick(on(func() { s.call("history") })), shown)
	note.AddSeparator()
	n.add(note.Add("Copy as Markdown").SetAccelerator("cmd+shift+c").OnClick(on(func() { s.call("copy") })), shown)
	n.add(note.Add("Copy Link").OnClick(on(func() { s.call("copyLink", false) })), shown)
	n.add(note.Add("Copy Markdown Link").OnClick(on(func() { s.call("copyLink", true) })), shown)
	note.AddSeparator()
	n.trash = note.Add("Move to Trash").SetAccelerator("cmd+backspace").OnClick(on(func() { s.call("trash") }))
	n.add(n.trash, shown)
	n.add(note.Add("Restore").OnClick(on(func() { s.call("restore") })), shownInTrash)

	notebook := menu.AddSubmenu("Notebook")
	n.add(notebook.Add("Rename Notebook").OnClick(on(func() { s.call("renameNotebook") })), renamable)
	notebook.AddSeparator()
	n.add(notebook.Add("Empty Trash…").SetAccelerator("cmd+shift+backspace").OnClick(on(func() { s.call("emptyTrash") })), trashFull)

	view := menu.AddSubmenu("View")
	n.add(view.Add("Preview Style…").OnClick(on(func() { s.call("previewStyle") })), notEditing)
	view.AddSeparator()
	// The size of the text of notes, as the minus and plus of the settings
	// menu, rather than a zoom of the whole page.
	n.add(view.Add("Larger Text").SetAccelerator("cmd+plus").OnClick(on(func() { s.call("textSize", 1) })), always)
	n.add(view.Add("Smaller Text").SetAccelerator("cmd+-").OnClick(on(func() { s.call("textSize", -1) })), always)
	n.add(view.Add("Default Text Size").SetAccelerator("cmd+0").OnClick(on(func() { s.call("textSize", 0) })), always)
	view.AddSeparator()
	view.AddRole(application.ToggleFullscreen)
	menu.AddRole(application.WindowMenu)

	help := menu.AddSubmenu("Help")
	n.add(help.Add("ebira Guide").OnClick(on(func() { s.call("openGuide") })), always)
	n.add(help.Add("How to Search").OnClick(on(func() { s.call("searchHelp") })), always)
	n.add(help.Add("Add the Guide").OnClick(on(func() { s.call("addGuide") })), always)
	help.AddSeparator()
	help.Add("Requests and Bug Reports…").OnClick(on(func() {
		// The browser, as WebKit opens no window for a page that the user did
		// not click.
		if err := s.app.Browser.OpenURL(issueURL); err != nil {
			log.Print("open the issues: ", err)
		}
	}))
	return menu
}
