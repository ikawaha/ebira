package main

import (
	"encoding/json"
	"log"
	"runtime"
	"strings"
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
	// Archivable tells that the note shown can be put in the archive or taken
	// out, Archived that it is there; NotebookArchivable and
	// NotebookArchived the same of the notebook shown.
	Archivable         bool `json:"archivable"`
	Archived           bool `json:"archived"`
	NotebookArchivable bool `json:"notebookArchivable"`
	NotebookArchived   bool `json:"notebookArchived"`
}

// editMenu fills the edit menu of Windows with the items of the roles of Wails,
// which edit the page on a click, but without their shortcuts. Wails hands the
// keys of its menu on to WebView2 too, which handles them itself, so the page
// pasted, undid or redid twice. The keys are left to WebView2, and the labels
// show them as the shortcuts would.
func editMenu(edit *application.Menu) {
	add := func(role application.Role, key string) {
		edit.AddRole(role)
		item := edit.FindByRole(role)
		item.RemoveAccelerator()
		if key != "" {
			item.SetLabel(item.Label() + "\t" + key)
		}
	}
	add(application.Undo, "Ctrl+Z")
	add(application.Redo, "Ctrl+Y")
	edit.AddSeparator()
	add(application.Cut, "Ctrl+X")
	add(application.Copy, "Ctrl+C")
	add(application.Paste, "Ctrl+V")
	// The Delete key deletes after the caret, where the item deletes before it,
	// as Backspace does: the label shows no key.
	add(application.Delete, "")
	edit.AddSeparator()
	add(application.SelectAll, "Ctrl+A")
}

// noteMenu holds the items that depend on the state of the page.
type noteMenu struct {
	mu    sync.Mutex
	items []stateItem
	trash *application.MenuItem
	// archive and nbArchive put the note and the notebook shown in the
	// archive, or take them out.
	archive, nbArchive *application.MenuItem
	text               func(string) string
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
func archivable(p pageState) bool   { return p.Archivable && !p.Editing }
func nbArchivable(p pageState) bool { return p.NotebookArchivable && !p.Editing }

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
	n.archive.SetLabel(n.text(archiveLabel(p.Archived)))
	n.nbArchive.SetLabel(n.text(archiveLabel(p.NotebookArchived)))
}

// archiveLabel is the label of an item that puts in the archive, or takes out
// of it what is there.
func archiveLabel(in bool) string {
	if in {
		return "Take Out of Archive"
	}
	return "Put in Archive"
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
	n.archive.SetLabel(n.text(archiveLabel(false)))
	n.nbArchive.SetLabel(n.text(archiveLabel(false)))
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

// pageSettings opens the settings of the Web UI, whose first tab shows those
// of the app; a page without them, from a server of an earlier build, asks
// for the window of the app.
const pageSettings = `window.ebira?.settings ? window.ebira.settings() : ` + toApp + `.postMessage("ebira:appSettings");`

// openPageSettings opens the settings: those of the page, or without a page,
// as when no server answers, the window of the app's settings. The page asks
// for that window too when it cannot read the settings of its server.
func (s *shell) openPageSettings() {
	win := s.main()
	if win == nil {
		s.openSettings("")
		return
	}
	win.ExecJS(pageSettings)
}

// onPageMessage reads a message of tellApp in the Web UI.
func (s *shell) onPageMessage(data string) {
	var m struct {
		Type string `json:"type"`
		pageState
		Text string `json:"text"`
		URL  string `json:"url"`
		Dark bool   `json:"dark"` // theme: whether the page is dark
		// download: a part of a file to save
		ID   string `json:"id"`
		Name string `json:"name"`
		Data string `json:"data"`
		End  bool   `json:"end"`
	}
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		log.Print("a message of the page: ", err)
		return
	}
	switch m.Type {
	case "state":
		s.notes.update(m.pageState)
	case "theme": // the theme the page shows (tellTheme)
		s.pageTheme(m.Dark)
	case "openURL": // a link to open in the browser
		s.openURL(m.URL)
	case "restart": // the mark of an outdated server (markOutdated)
		go s.askRestart()
	case "appSettings": // Change… of the app tab of the settings
		go s.openSettings("")
	case "download": // a file of a note (downloadFile)
		s.download(m.ID, m.Name, m.Data, m.End)
	case "copy":
		if !s.app.Clipboard.SetText(m.Text) {
			log.Printf("the clipboard took no text of %d bytes", len(m.Text))
		}
	}
}

// issueURL is where requests and bug reports of ebira go (ISSUE_URL of the web
// UI).
const issueURL = "https://github.com/ikawaha/ebira/issues/new"

// openURL opens url in the browser. The schemes are limited because the link
// may come from a note, which AI can write too.
func (s *shell) openURL(url string) {
	scheme, _, _ := strings.Cut(url, ":")
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto":
		if err := s.app.Browser.OpenURL(url); err != nil {
			log.Print("open a link: ", err)
		}
	}
}

// appMenu is the menu bar: of the app on macOS, of the main window on Windows.
// It has no reload: WebKit would drop unsaved edits, since Wails does not ask
// about the page's beforeunload.
func (s *shell) appMenu() *application.Menu {
	n := &noteMenu{text: s.text}
	s.notes = n
	mac := runtime.GOOS == "darwin"
	on := func(f func()) func(*application.Context) {
		return func(*application.Context) { f() }
	}
	// key is the shortcut of an item: the one of macOS (⌘), or of Windows
	// (Ctrl), where some of macOS mean something else, as Ctrl+Y redoes and
	// Ctrl+Backspace deletes a word. Empty is none.
	key := func(item *application.MenuItem, macKey, windowsKey string) *application.MenuItem {
		k := windowsKey
		if mac {
			k = macKey
		}
		if k != "" {
			item.SetAccelerator(k)
		}
		return item
	}
	settingsItem := func(m *application.Menu) {
		// One place for the settings: those of the page, of its display and of
		// the server, with a tab of those of the app (openPageSettings).
		key(m.Add("Settings…"), "cmd+,", "ctrl+,").OnClick(on(s.openPageSettings))
		n.add(m.Add("Server Status…").OnClick(on(func() { s.call("serverStats") })), always)
	}
	about := func() { go s.showAbout() }

	menu := s.app.Menu.New()
	if mac {
		app := menu.AddSubmenu("ebira")
		app.Add("About ebira").OnClick(on(about))
		app.AddSeparator()
		settingsItem(app)
		app.AddSeparator()
		app.AddRole(application.ServicesMenu)
		app.AddSeparator()
		app.AddRole(application.Hide)
		app.AddRole(application.HideOthers)
		app.AddRole(application.UnHide)
		app.AddSeparator()
		app.AddRole(application.Quit)
	}

	file := menu.AddSubmenu("File")
	n.add(key(file.Add("New Note"), "cmd+n", "ctrl+n").OnClick(on(func() { s.call("create") })), always)
	n.add(key(file.Add("New Notebook…"), "cmd+shift+n", "ctrl+shift+n").OnClick(on(func() { s.call("newNotebook") })), notEditing)
	file.AddSeparator()
	n.add(file.Add("Import Markdown…").OnClick(on(func() { s.call("importFiles") })), notEditing)
	n.add(file.Add("Check Files…").OnClick(on(func() { s.call("checkFiles") })), always)
	n.add(file.Add("Candidates for the Archive…").OnClick(on(func() { s.call("archiveCandidates") })), notEditing)
	n.add(file.Add("Sync with Git").OnClick(on(func() { s.call("syncGit") })), inGit)
	file.AddSeparator()
	if mac {
		file.AddRole(application.CloseWindow)
	} else {
		settingsItem(file)
		file.AddSeparator()
		file.AddRole(application.Quit)
	}

	if mac {
		menu.AddRole(application.EditMenu)
	} else {
		editMenu(menu.AddSubmenu("Edit"))
	}

	note := menu.AddSubmenu("Note")
	n.add(key(note.Add("Edit Note"), "cmd+e", "ctrl+e").OnClick(on(func() { s.call("edit") })), shownOutside)
	n.add(key(note.Add("Save"), "cmd+s", "ctrl+s").OnClick(on(func() { s.call("save") })), editing)
	note.AddSeparator()
	n.add(key(note.Add("Find in Note"), "cmd+f", "ctrl+f").OnClick(on(func() { s.call("find") })), shown)
	// Ctrl+Alt is AltGr on many keyboards of Windows.
	n.add(key(note.Add("Search Notes"), "cmd+alt+f", "ctrl+shift+f").OnClick(on(func() { s.call("search") })), always)
	note.AddSeparator()
	n.add(note.Add("Mark…").OnClick(on(func() { s.call("mark") })), shownOutside)
	n.add(key(note.Add("History…"), "cmd+y", "ctrl+h").OnClick(on(func() { s.call("history") })), shown)
	note.AddSeparator()
	n.add(key(note.Add("Copy as Markdown"), "cmd+shift+c", "ctrl+shift+c").OnClick(on(func() { s.call("copy") })), shown)
	n.add(note.Add("Copy Link").OnClick(on(func() { s.call("copyLink", false) })), shown)
	n.add(note.Add("Copy Markdown Link").OnClick(on(func() { s.call("copyLink", true) })), shown)
	note.AddSeparator()
	n.archive = note.Add(archiveLabel(false)).OnClick(on(func() { s.call("archive") }))
	n.add(n.archive, archivable)
	// No shortcut: the menu would take ⌘⌫ (Ctrl+Backspace) from the text
	// fields of the page, which delete with it, and trash the note shown.
	n.trash = note.Add("Move to Trash").OnClick(on(func() { s.call("trash") }))
	n.add(n.trash, shown)
	n.add(note.Add("Restore").OnClick(on(func() { s.call("restore") })), shownInTrash)

	notebook := menu.AddSubmenu("Notebook")
	n.add(notebook.Add("Rename Notebook").OnClick(on(func() { s.call("renameNotebook") })), renamable)
	n.nbArchive = notebook.Add(archiveLabel(false)).OnClick(on(func() { s.call("archiveNotebook") }))
	n.add(n.nbArchive, nbArchivable)
	notebook.AddSeparator()
	n.add(key(notebook.Add("Empty Trash…"), "cmd+shift+backspace", "").OnClick(on(func() { s.call("emptyTrash") })), trashFull)

	view := menu.AddSubmenu("View")
	n.add(view.Add("Preview Style…").OnClick(on(func() { s.call("previewStyle") })), notEditing)
	view.AddSeparator()
	// The size of the text of notes, as the minus and plus of the settings
	// menu, rather than a zoom of the whole page.
	n.add(key(view.Add("Larger Text"), "cmd+plus", "ctrl+plus").OnClick(on(func() { s.call("textSize", 1) })), always)
	n.add(key(view.Add("Smaller Text"), "cmd+-", "ctrl+-").OnClick(on(func() { s.call("textSize", -1) })), always)
	n.add(key(view.Add("Default Text Size"), "cmd+0", "ctrl+0").OnClick(on(func() { s.call("textSize", 0) })), always)
	view.AddSeparator()
	view.AddRole(application.ToggleFullscreen)
	if mac {
		menu.AddRole(application.WindowMenu)
	}

	help := menu.AddSubmenu("Help")
	n.add(help.Add("ebira Guide").OnClick(on(func() { s.call("openGuide") })), always)
	n.add(help.Add("How to Search").OnClick(on(func() { s.call("searchHelp") })), always)
	n.add(help.Add("Add the Guide").OnClick(on(func() { s.call("addGuide") })), always)
	n.add(help.Add("Connect AI (MCP)…").OnClick(on(func() { s.call("connectAI") })), always)
	help.AddSeparator()
	help.Add("Requests and Bug Reports…").OnClick(on(func() {
		// The browser, as the web view opens no window for a page that the
		// user did not click.
		s.openURL(issueURL)
	}))
	if !mac {
		help.AddSeparator()
		help.Add("About ebira").OnClick(on(about))
	}
	return menu
}
