package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// zoomModel returns a model of width wide showing a note, with the focus on
// the preview.
func zoomModel(t *testing.T, width int) Model {
	t.Helper()
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	m = tm.(Model)
	m.note = &gennotes.Note{ID: "plan", Title: "Plan", Body: "body", Notebook: "inbox", UpdatedAt: "2026-10-19T10:00:00Z"}
	m.focus = focusPreview
	m.layout()
	return m
}

// onlyPreview reports whether the layout shows the preview alone, over the
// whole width.
func onlyPreview(m Model) bool {
	l := m.computeLayout()
	return l.showPreview && !l.showList && !l.showNotebooks && l.previewW == m.width
}

func TestZoom(t *testing.T) {
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}
	m := zoomModel(t, 120)
	if onlyPreview(m) {
		t.Fatal("zoomed before z")
	}

	m, _ = press(t, m, text("z"))
	if !m.zoom || !onlyPreview(m) || m.vp.Width() != 118 {
		t.Fatalf("z: zoom %v, layout %+v, preview width %d", m.zoom, m.computeLayout(), m.vp.Width())
	}

	// The editor opens over the whole width, and closing it goes back to
	// the zoomed preview.
	m, _ = press(t, m, text("e"))
	if m.focus != focusEdit || !onlyPreview(m) {
		t.Fatalf("e: focus %v, layout %+v", m.focus, m.computeLayout())
	}
	m, _ = press(t, m, esc)
	if m.focus != focusPreview || !m.zoom || !onlyPreview(m) {
		t.Fatalf("esc in the editor: focus %v, zoom %v", m.focus, m.zoom)
	}

	// esc goes back to the three panes, with the focus on the preview.
	m, _ = press(t, m, esc)
	if m.zoom || onlyPreview(m) || m.focus != focusPreview {
		t.Errorf("esc: zoom %v, focus %v", m.zoom, m.focus)
	}

	// So does z again.
	m, _ = press(t, m, text("z"), text("z"))
	if m.zoom || onlyPreview(m) {
		t.Errorf("z twice: zoom %v", m.zoom)
	}

	// h goes back to the list.
	m, _ = press(t, m, text("z"), text("h"))
	if m.zoom || m.focus != focusList || onlyPreview(m) {
		t.Errorf("h: zoom %v, focus %v", m.zoom, m.focus)
	}

	// Leaving the preview otherwise ends the zoom too, so that it does not
	// come back when the focus returns.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}, text("z"), tea.KeyPressMsg{Code: tea.KeyTab})
	if m.zoom || m.focus == focusPreview {
		t.Errorf("tab: zoom %v, focus %v", m.zoom, m.focus)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.focus != focusPreview || onlyPreview(m) {
		t.Errorf("back to the preview: focus %v, layout %+v", m.focus, m.computeLayout())
	}
}

func TestZoomAnswer(t *testing.T) {
	m := zoomModel(t, 120)
	m.answer, m.answerOn = &answer{question: "q", text: "a"}, true
	m, _ = press(t, m, text("z"), tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.answerOn || !m.zoom {
		t.Fatalf("first esc: answer %v, zoom %v", m.answerOn, m.zoom)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.zoom {
		t.Error("second esc: still zoomed")
	}
}

func TestZoomNarrow(t *testing.T) {
	m := zoomModel(t, 60)
	m, _ = press(t, m, text("z"))
	if m.zoom {
		t.Error("zoomed in a narrow terminal, which shows one pane anyway")
	}
}

func TestZoomSource(t *testing.T) {
	var kept []bool
	n := &gennotes.Note{ID: "plan", Version: "v1", Title: "Plan", Body: "body", Notebook: "inbox", UpdatedAt: "2026-10-25T10:00:00Z"}
	m := sourceModel(t, n, &kept)

	// z in the list opens the note over the whole width.
	m, _ = press(t, m, text("z"))
	if !m.zoom || m.focus != focusPreview || !onlyPreview(m) || !m.showsSource() {
		t.Fatalf("z in the list: zoom %v, focus %v", m.zoom, m.focus)
	}
	// While editing, z is a letter, and ctrl+l zooms.
	m, _ = press(t, m, text("z"))
	if !m.zoom || m.src.body.Value() != "zbody" {
		t.Fatalf("z while editing: zoom %v, body %q", m.zoom, m.src.body.Value())
	}
	ctrlL := tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
	m, _ = press(t, m, ctrlL)
	if m.zoom || onlyPreview(m) {
		t.Fatal("ctrl+l: still zoomed")
	}
	m, _ = press(t, m, ctrlL)
	if !m.zoom || !onlyPreview(m) {
		t.Fatal("ctrl+l again: not zoomed")
	}
	// The peek keeps the zoom, and z there ends it.
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !m.peeking() || !m.zoom {
		t.Fatalf("ctrl+r: peek %v, zoom %v", m.peek, m.zoom)
	}
	m, _ = press(t, m, text("z"), tea.KeyPressMsg{Code: tea.KeyEscape}, ctrlL)
	if !m.showsSource() || !m.zoom {
		t.Fatalf("back to the source: peek %v, zoom %v", m.peek, m.zoom)
	}
	// esc ends the zoom first, then goes back to the list.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.zoom || m.focus != focusPreview {
		t.Fatalf("esc: zoom %v, focus %v", m.zoom, m.focus)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.focus != focusList {
		t.Errorf("second esc: focus %v", m.focus)
	}
}

func TestTopAndBottom(t *testing.T) {
	m := zoomModel(t, 120)
	m.note.Body = strings.Repeat("line\n\n", 100)
	m.renderPreview(false)
	m, _ = press(t, m, text(">"))
	if !m.vp.AtBottom() {
		t.Error("> does not go to the end")
	}
	m, _ = press(t, m, text("<"))
	if !m.vp.AtTop() {
		t.Error("< does not go to the start")
	}

	// The same while peeking in the source mode.
	var kept []bool
	n := &gennotes.Note{ID: "plan", Version: "v1", Title: "Plan", Body: strings.Repeat("line\n\n", 100), Notebook: "inbox", UpdatedAt: "2026-10-25T10:00:00Z"}
	m = sourceModel(t, n, &kept)
	m, _ = press(t, m, text("p"), tea.KeyPressMsg{Code: tea.KeyEnter}, text(">"))
	if !m.peeking() || !m.vp.AtBottom() {
		t.Error("> does not go to the end while peeking")
	}
	m, _ = press(t, m, text("<"))
	if !m.vp.AtTop() || m.src.body.Value() != n.Body {
		t.Error("< does not go to the start while peeking")
	}
}
