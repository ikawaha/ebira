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
}

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
	if key == "shift+tab" || (key == "up" && e.body.Line() == 0) {
		return e.focus(fieldTags)
	}
	var cmd tea.Cmd
	e.body, cmd = e.body.Update(msg)
	return cmd
}

// updateOther passes a non-key message, such as a cursor blink or a paste,
// to the focused field.
func (e *editor) updateOther(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if e.field == fieldBody {
		e.body, cmd = e.body.Update(msg)
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
