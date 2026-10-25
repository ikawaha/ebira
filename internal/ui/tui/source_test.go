package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// sourceModel returns a model in the source mode showing n, with the focus on
// the list; kept records the modes saved by v.
func sourceModel(t *testing.T, n *gennotes.Note, kept *[]bool) Model {
	t.Helper()
	m := New(context.Background(), &Clients{}, false, "en").SourcePreview(true, func(source bool) error {
		*kept = append(*kept, source)
		return nil
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	m.items = []*gennotes.NoteSummary{{ID: n.ID, Title: n.Title, Notebook: n.Notebook}}
	m.note = n
	tm, _ = m.Update(nil) // any message lets the source editor follow the note
	return tm.(Model)
}

func TestSourcePreview(t *testing.T) {
	var kept []bool
	n := &gennotes.Note{ID: "plan", Version: "v1", Title: "Plan", Tags: []string{"work"}, Body: "# Plan\nfirst", Notebook: "inbox", UpdatedAt: "2026-10-25T10:00:00Z"}
	m := sourceModel(t, n, &kept)
	if m.src == nil || m.src.focused() {
		t.Fatalf("source editor: %+v", m.src)
	}
	if v := ansi.Strip(m.render()); !strings.Contains(v, "# Plan") || !strings.Contains(v, "title") || !strings.Contains(v, "Plan") {
		t.Error("the pane does not show the note as the editor of e does")
	}

	// In the preview, the keys, even the commands of the list, go to the body.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.focus != focusPreview || !m.src.body.Focused() {
		t.Fatalf("enter: focus %v", m.focus)
	}
	m, _ = press(t, m, text("d"), text("v"), text("q"))
	if got := m.src.body.Value(); got != "dvq# Plan\nfirst" || !m.src.dirty() || m.prompt != promptNone {
		t.Fatalf("typed: body %q, dirty %v, prompt %v", got, m.src.dirty(), m.prompt)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.src.body.Value(); got != "# Plan\nfirst" {
		t.Errorf("ctrl+z: body %q", got)
	}
	m, _ = press(t, m, text("x"))

	// shift+tab goes to the fields above the body, as in the editor of e.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyEnd}, text("!"), tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != focusPreview || m.src.field != fieldBody || m.src.inputs[fieldTitle].Value() != "Plan!" {
		t.Fatalf("fields: focus %v, field %d, title %q", m.focus, m.src.field, m.src.inputs[fieldTitle].Value())
	}

	// Leaving the preview saves the changes, with the title and tags kept.
	m, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.focus != focusList || m.src.saving == "" || cmd == nil || m.src.focused() {
		t.Fatalf("esc: focus %v, saving %q", m.focus, m.src.saving)
	}
	d, err := m.src.draft()
	if err != nil || d.title != "Plan!" || strings.Join(d.tags, ",") != "work" || d.body != "x# Plan\nfirst" || d.version != "v1" {
		t.Fatalf("draft: %+v, %v", d, err)
	}
	d.source = true
	saved := &gennotes.Note{ID: "plan", Version: "v2", Title: "Plan!", Tags: []string{"work"}, Body: d.body, Notebook: "inbox"}
	tm, _ := m.Update(savedMsg{d: d, note: saved})
	m = tm.(Model)
	if m.src == nil || m.src.dirty() || m.src.saving != "" || m.src.version != "v2" || m.note.Version != "v2" {
		t.Fatalf("saved: %+v", m.src)
	}

	// A change made outside is followed while there are no changes here.
	m.note = &gennotes.Note{ID: "plan", Version: "v3", Title: "Plan", Body: "outside", Notebook: "inbox"}
	tm, _ = m.Update(nil)
	m = tm.(Model)
	if m.src.version != "v3" || m.src.body.Value() != "outside" {
		t.Errorf("outside: %q at %s", m.src.body.Value(), m.src.version)
	}

	// v switches to the rendered preview from the list, and keeps it.
	m, _ = press(t, m, text("v"))
	if m.source || m.src != nil || len(kept) != 1 || kept[0] {
		t.Errorf("v: source %v, kept %v", m.source, kept)
	}
	m, _ = press(t, m, text("v"))
	if !m.source || m.src == nil || len(kept) != 2 || !kept[1] {
		t.Errorf("v again: source %v, kept %v", m.source, kept)
	}
}

func TestSourcePreviewTrash(t *testing.T) {
	var kept []bool
	n := &gennotes.Note{ID: "old", Version: "v1", Title: "Old", Body: "# Old\ntext", Notebook: trashNotebook, UpdatedAt: "2026-10-25T10:00:00Z"}
	m := sourceModel(t, n, &kept)
	if m.src != nil {
		t.Fatal("a note in the trash is edited")
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}, text("x"))
	if m.src != nil || !strings.Contains(ansi.Strip(m.render()), "# Old") {
		t.Errorf("the note in the trash is not shown read only as Markdown")
	}
}

func TestSourcePeek(t *testing.T) {
	var kept []bool
	n := &gennotes.Note{ID: "plan", Version: "v1", Title: "Plan", Body: "# Plan\nfirst", Notebook: "inbox", UpdatedAt: "2026-10-25T10:00:00Z"}
	m := sourceModel(t, n, &kept)
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}, text("x"))

	// ctrl+r renders the body being edited, unsaved changes included.
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !m.peeking() || m.showsSource() || m.src.focused() {
		t.Fatalf("ctrl+r: peek %v", m.peek)
	}
	if v := ansi.Strip(m.vp.View()); !strings.Contains(v, "x# Plan") || strings.Contains(v, "x# Plan\nfirst") {
		t.Errorf("rendered: %q", v)
	}
	// Letters do nothing there, not even the commands of the list.
	m, _ = press(t, m, text("d"), text("y"), text("v"))
	if !m.peeking() || m.prompt != promptNone || m.src.body.Value() != "x# Plan\nfirst" || len(kept) != 0 {
		t.Fatalf("letters: peek %v, prompt %v, body %q", m.peek, m.prompt, m.src.body.Value())
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.showsSource() || !m.src.body.Focused() || m.focus != focusPreview {
		t.Fatalf("esc: peek %v, focus %v", m.peek, m.focus)
	}

	// p in the list peeks too, without saving the mode.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape}, text("p"))
	if !m.peeking() || m.focus != focusList || len(kept) != 0 {
		t.Fatalf("p: peek %v, focus %v", m.peek, m.focus)
	}
	m, _ = press(t, m, text("p"))
	if m.peek {
		t.Error("p again does not go back to the source")
	}

	// Another note opens in the source.
	m, _ = press(t, m, text("p"))
	m.note = &gennotes.Note{ID: "other", Version: "v1", Title: "Other", Body: "other", Notebook: "inbox"}
	tm, _ := m.Update(nil)
	if m = tm.(Model); m.peek || !m.showsSource() {
		t.Errorf("another note: peek %v", m.peek)
	}
}
