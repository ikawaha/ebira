package tui

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"
)

// draft is a note being edited in an external editor. The file has a YAML
// front matter with the ID, notebook, title and tags, followed by the Markdown
// body, like a Zenn article. The ID of a new note becomes its file name; when
// it is empty the server assigns a random one. Changing the notebook moves the
// note.
type draft struct {
	id       string // empty for a new note
	notebook string // notebook the note is in, or the default for a new note
	version  string // version the edit is based on
	path     string // temporary file
	source   bool   // from the source editor of the preview (see source.go)
	orig     []byte // file content before editing

	newID       string // ID written in the front matter
	newNotebook string // notebook written in the front matter
	title       string
	tags        []string
	body        string
}

type frontMatter struct {
	ID       string   `yaml:"id"`
	Notebook string   `yaml:"notebook"`
	Title    string   `yaml:"title"`
	Tags     []string `yaml:"tags,flow"`
}

// idPattern and notebookPattern mirror the patterns of note and notebook IDs
// in the API design (design/dsl.go), so that the UI can explain a bad ID
// instead of showing a validation error. A notebook ID is <parent>/<sub> for
// a sub notebook.
var (
	idPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	notebookPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}(/[a-z0-9][a-z0-9_-]{0,63})?$`)
)

// Reserved notebooks of the API.
const (
	inboxNotebook = "inbox"
	trashNotebook = "trash"
)

const fence = "---\n"

// formatDraft returns the file of a draft. hint is the comment on an empty
// ID, telling what it may be.
func formatDraft(id, notebook, title string, tags []string, body, hint string) ([]byte, error) {
	if tags == nil {
		tags = []string{}
	}
	var node yaml.Node
	if err := node.Encode(frontMatter{ID: id, Notebook: notebook, Title: title, Tags: tags}); err != nil {
		return nil, err
	}
	if id == "" {
		node.Content[1].LineComment = hint // the value node of "id"
	}
	fm, err := yaml.Marshal(&node)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(fence)
	b.Write(fm)
	b.WriteString(fence)
	b.WriteString(body)
	return b.Bytes(), nil
}

func parseDraft(b []byte) (fm frontMatter, body string, err error) {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(s, fence) {
		return fm, "", errMsg("draft.noFrontMatter")
	}
	rest := s[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
		} else {
			return fm, "", errMsg("draft.unclosed")
		}
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return fm, "", err
	}
	if i := end + len("\n"+fence); i <= len(rest) {
		body = rest[i:]
	}
	fm.ID = strings.TrimSpace(fm.ID)
	fm.Notebook = strings.TrimSpace(fm.Notebook)
	fm.Title = strings.TrimSpace(fm.Title)
	return fm, body, nil
}

// newDraft writes the note into a temporary file for editing, with hint on an
// empty ID (see formatDraft).
func newDraft(id, notebook, version, title string, tags []string, body, hint string) (*draft, error) {
	b, err := formatDraft(id, notebook, title, tags, body, hint)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "ebira-*.md")
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return &draft{id: id, notebook: notebook, version: version, path: f.Name(), orig: b}, nil
}

// editorDoneMsg is sent when the external editor exits.
type editorDoneMsg struct {
	d   *draft
	err error
}

// openEditor runs $VISUAL or $EDITOR (vi by default) on the draft.
func openEditor(d *draft) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	args := strings.Fields(editor)
	cmd := exec.Command(args[0], append(args[1:], d.path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{d: d, err: err} })
}

// load reads and checks the edited file. changed is false when the file was
// not modified.
func (d *draft) load() (changed bool, err error) {
	b, err := os.ReadFile(d.path)
	if err != nil {
		return false, err
	}
	if bytes.Equal(b, d.orig) {
		return false, nil
	}
	fm, body, err := parseDraft(b)
	if err != nil {
		return true, err
	}
	return true, d.apply(fm, body)
}

// apply checks the edited values and stores them in the draft.
func (d *draft) apply(fm frontMatter, body string) error {
	switch {
	case fm.Title == "":
		return errMsg("draft.noTitle")
	case d.id != "" && fm.ID != "" && fm.ID != d.id:
		return errMsg("draft.idChange")
	case fm.ID != "" && !idPattern.MatchString(fm.ID):
		return errMsg("draft.badID", "id", fm.ID)
	case fm.Notebook != "" && !notebookPattern.MatchString(fm.Notebook):
		return errMsg("draft.badNotebook", "notebook", fm.Notebook)
	case fm.Notebook == trashNotebook && d.notebook != trashNotebook:
		return errMsg("draft.useDelete")
	case d.id == "" && fm.Notebook == trashNotebook:
		return errMsg("draft.inTrash")
	}
	d.newID, d.title, d.tags, d.body = fm.ID, fm.Title, fm.Tags, body
	d.newNotebook = fm.Notebook
	if d.newNotebook == "" {
		d.newNotebook = d.notebook // an empty notebook leaves the note where it is
	}
	return nil
}

// remove deletes the temporary file, if any.
func (d *draft) remove() {
	if d.path != "" {
		os.Remove(d.path)
	}
}

// location describes where the draft is kept, for status messages.
func (d *draft) location() string {
	if d.path == "" {
		return "the editor"
	}
	return d.path
}
