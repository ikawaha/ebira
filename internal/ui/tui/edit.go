package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// Fields of the inline editor, in tab order.
const (
	fieldID = iota
	fieldNotebook
	fieldTitle
	fieldTags
	fieldBody
	numFields
)

// editor edits a note inside the UI, replacing the preview pane.
type editor struct {
	id, version string // empty for a new note
	notebook    string // notebook the note is in, or the default for a new note
	inputs      [fieldBody]textinput.Model
	body        textarea.Model
	field       int
	orig        string // snapshot to detect unsaved changes
	outside     bool   // the note was changed outside while editing
	idFixed     string // shown after the ID of an existing note
	// undo and redo hold earlier and undone states of the body, for ctrl+z
	// and ctrl+y; typing is the kind of the last change, so that the letters
	// of a word typed in a row are undone together.
	undo, redo []bodyState
	typing     bool
	// saving holds the snapshot being saved by the editor of the source
	// mode, which becomes orig once the save succeeds.
	saving string
}

// bodyState is the body of the editor with its cursor.
type bodyState struct {
	value     string
	line, col int
}

// maxUndo is the number of changes of the body that can be undone.
const maxUndo = 200

// newEditor edits n, or a new note in notebook when n is nil, with the
// messages of tr.
func newEditor(th theme, tr *catalog, n *gennotes.Note, notebook string) *editor {
	e := &editor{notebook: notebook, idFixed: tr.t("edit.idFixed")}
	for i, ph := range []string{tr.t("edit.idPlaceholder"), tr.t("edit.notebookPlaceholder"), tr.t("edit.titlePlaceholder"), tr.t("edit.tagsPlaceholder")} {
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = ph
		in.CharLimit = 200
		e.inputs[i] = in
	}
	e.body = textarea.New()
	e.body.ShowLineNumbers = false
	e.body.Prompt = ""
	e.body.MaxHeight = 0
	e.body.MaxWidth = 0
	st := textarea.DefaultStyles(th.dark)
	st.Focused.CursorLine = st.Focused.CursorLine.Background(th.selBg)
	e.body.SetStyles(st)
	if n != nil {
		e.id, e.version, e.notebook = n.ID, n.Version, n.Notebook
		e.inputs[fieldID].SetValue(n.ID)
		e.inputs[fieldTitle].SetValue(n.Title)
		e.inputs[fieldTags].SetValue(strings.Join(n.Tags, ", "))
		e.body.SetValue(n.Body)
		e.body.MoveToBegin()
		e.field = fieldBody
	} else {
		e.field = fieldTitle
	}
	e.inputs[fieldNotebook].SetValue(e.notebook)
	e.orig = e.snapshot()
	return e
}

func (e *editor) snapshot() string {
	parts := []string{e.body.Value()}
	for _, in := range e.inputs {
		parts = append(parts, in.Value())
	}
	return strings.Join(parts, "\x00")
}

func (e *editor) dirty() bool { return e.snapshot() != e.orig }

// focus moves the focus to field f. The ID of an existing note cannot be
// edited, so that field is skipped.
func (e *editor) focus(f int) tea.Cmd {
	if e.id != "" && f == fieldID {
		f = fieldTitle
	}
	f = max(0, min(f, numFields-1))
	e.field = f
	for i := range e.inputs {
		e.inputs[i].Blur()
	}
	e.body.Blur()
	if f == fieldBody {
		return e.body.Focus()
	}
	return e.inputs[f].Focus()
}

func (e *editor) resize(w, h int) {
	for i := range e.inputs {
		e.inputs[i].SetWidth(max(10, w-12))
	}
	e.body.SetWidth(max(10, w-2))
	e.body.SetHeight(max(1, h-len(e.inputs)-2))
}

// newSourceEditor edits n in the document pane, in the source mode. The
// field and the cursor of prev are kept when it is given, as when the note
// changed outside.
func newSourceEditor(th theme, tr *catalog, n *gennotes.Note, prev *editor) *editor {
	e := newEditor(th, tr, n, n.Notebook)
	if prev != nil {
		st := prev.bodyState()
		e.setBody(bodyState{value: n.Body, line: st.line, col: st.col})
		e.field = prev.field
	}
	return e
}

// moveCursor moves the cursor of the body to line and col, in runes, and has
// the body show it.
func (e *editor) moveCursor(line, col int) {
	e.body.MoveToBegin()
	for i := 0; e.body.Line() < line && i <= len(e.body.Value()); i++ {
		e.body.CursorDown()
	}
	e.body.SetCursorColumn(col)
	e.field = fieldBody
	e.typing = false
}

// focused reports whether a field of the editor has the focus.
func (e *editor) focused() bool {
	for _, in := range e.inputs {
		if in.Focused() {
			return true
		}
	}
	return e.body.Focused()
}

// blur takes the focus away from the fields.
func (e *editor) blur() {
	for i := range e.inputs {
		e.inputs[i].Blur()
	}
	e.body.Blur()
}

// busy reports whether the body has changes that are not saved yet, or being
// saved.
func (e *editor) busy() bool { return e.saving != "" || e.dirty() }

// draft returns the edited values as a draft to save.
func (e *editor) draft() (*draft, error) {
	d := &draft{id: e.id, notebook: e.notebook, version: e.version}
	var tags []string
	for _, t := range strings.Split(e.inputs[fieldTags].Value(), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	fm := frontMatter{
		ID:       strings.TrimSpace(e.inputs[fieldID].Value()),
		Notebook: strings.TrimSpace(e.inputs[fieldNotebook].Value()),
		Title:    strings.TrimSpace(e.inputs[fieldTitle].Value()),
		Tags:     tags,
	}
	return d, d.apply(fm, e.body.Value())
}

// update handles a key for the focused field.
func (e *editor) update(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if e.field != fieldBody {
		switch key {
		case "enter", "down", "tab":
			return e.focus(e.field + 1)
		case "up", "shift+tab":
			return e.focus(e.field - 1)
		}
		var cmd tea.Cmd
		e.inputs[e.field], cmd = e.inputs[e.field].Update(msg)
		return cmd
	}
	switch key {
	case "shift+tab":
		return e.focus(fieldTags)
	case "up":
		if e.body.Line() == 0 {
			return e.focus(fieldTags)
		}
	case "ctrl+z":
		e.step(&e.undo, &e.redo)
		return nil
	case "ctrl+y", "ctrl+shift+z":
		e.step(&e.redo, &e.undo)
		return nil
	}
	// A letter typed after a letter joins its change; a space, an enter, a
	// deletion or a move of the cursor starts a new one.
	typing := msg.Text != "" && strings.TrimSpace(msg.Text) == msg.Text
	return e.updateBody(msg, typing, true)
}

// updateBody passes msg to the body, keeping the state before it for undo if
// it changes the body. A change of the typing kind right after another one is
// undone together with it. A key that leaves the body as it is, such as a
// move of the cursor, ends the word being typed; other messages, such as the
// blinks of the cursor, do not.
func (e *editor) updateBody(msg tea.Msg, typing, key bool) tea.Cmd {
	before := e.bodyState()
	var cmd tea.Cmd
	e.body, cmd = e.body.Update(msg)
	if e.body.Value() == before.value {
		if key {
			e.typing = false
		}
		return cmd
	}
	if !typing || !e.typing || len(e.undo) == 0 {
		e.undo = append(e.undo, before)
		if len(e.undo) > maxUndo {
			e.undo = e.undo[len(e.undo)-maxUndo:]
		}
	}
	e.redo = nil
	e.typing = typing
	return cmd
}

func (e *editor) bodyState() bodyState {
	return bodyState{value: e.body.Value(), line: e.body.Line(), col: e.body.Column()}
}

// step moves the body back to the last state of from, keeping the current
// one in to: an undo with from = undo, a redo with from = redo.
func (e *editor) step(from, to *[]bodyState) {
	if len(*from) == 0 {
		return
	}
	st := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, e.bodyState())
	e.setBody(st)
	e.typing = false
}

// setBody sets the body and puts the cursor back where it was. The textarea
// moves the cursor by lines on the screen, so it goes down until it reaches
// the line.
func (e *editor) setBody(st bodyState) {
	e.body.SetValue(st.value)
	e.body.MoveToBegin()
	for i := 0; e.body.Line() < st.line && i <= len(st.value); i++ {
		e.body.CursorDown()
	}
	e.body.SetCursorColumn(st.col)
}

// updateOther passes a non-key message, such as a cursor blink or a paste,
// to the focused field.
func (e *editor) updateOther(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if e.field == fieldBody {
		cmd = e.updateBody(msg, false, false) // a paste is a change of its own
	} else {
		e.inputs[e.field], cmd = e.inputs[e.field].Update(msg)
	}
	return cmd
}

func (e *editor) view(th theme, w int) []string {
	label := func(i int, name string) string {
		st := th.style(th.dim)
		if i == e.field {
			st = lipgloss.NewStyle().Bold(true).Foreground(th.accent)
		}
		return " " + st.Render(pad(name, 8)) + " "
	}
	idView := e.inputs[fieldID].View()
	if e.id != "" {
		idView = th.style(th.dim).Render(e.id + "  " + e.idFixed)
	}
	lines := []string{
		label(fieldID, "id") + idView,
		label(fieldNotebook, "notebook") + e.inputs[fieldNotebook].View(),
		label(fieldTitle, "title") + e.inputs[fieldTitle].View(),
		label(fieldTags, "tags") + e.inputs[fieldTags].View(),
		" " + th.style(th.border).Render(strings.Repeat("─", max(0, w-2))),
	}
	for _, l := range strings.Split(e.body.View(), "\n") {
		lines = append(lines, " "+l)
	}
	return lines
}
