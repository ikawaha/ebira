package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// TestMarks checks that the list draws the mark of a note before its title,
// keeping the width of the rows, and that M chooses a mark with the arrows.
func TestMarks(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "ja")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	later := "あとで"
	tm, _ = m.Update(notebooksMsg{
		notebooks: []*gennotes.Notebook{
			{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
			{ID: trashNotebook, Name: "Trash", Kind: "default", Sort: "order", Reserved: true},
		},
		marks: []*gennotes.MarkInfo{{ID: "red", Kind: "color"}, {ID: "green", Kind: "color"}, {ID: "star", Kind: "symbol", Name: &later}},
	})
	m = tm.(Model)
	m.items = []*gennotes.NoteSummary{
		{ID: "a", Notebook: inboxNotebook, Title: "赤いノート", Marks: []string{"red"}, UpdatedAt: "2026-10-23T00:00:00Z"},
		{ID: "b", Notebook: inboxNotebook, Title: "星のノート", Marks: []string{"star"}, UpdatedAt: "2026-10-23T00:00:00Z"},
		{ID: "c", Notebook: inboxNotebook, Title: "印なし", UpdatedAt: "2026-10-23T00:00:00Z"},
	}
	out := m.renderList(40, 12)
	lines := strings.Split(ansi.Strip(out), "\n")
	var rows []string
	for _, l := range lines {
		if strings.HasPrefix(l, "│") && (strings.Contains(l, "ノート") || strings.Contains(l, "印なし")) {
			rows = append(rows, l)
		}
	}
	if len(rows) != 3 || !strings.Contains(rows[0], "● 赤いノート") || !strings.Contains(rows[1], "⭐ 星のノート") {
		t.Fatalf("rows:\n%s", strings.Join(rows, "\n"))
	}
	for _, r := range rows {
		if w := ansi.StringWidth(r); w != 40 {
			t.Errorf("row %q is %d wide, want 40", r, w)
		}
	}

	// M on the note in the preview: the arrows choose, enter sets.
	m.note = &gennotes.Note{ID: "b", Notebook: inboxNotebook, Title: "星のノート", Marks: []string{"star"}}
	m.focus = focusList
	m, _ = press(t, m, text("M"))
	if m.prompt != promptMark || m.markChoices()[m.markSel] != "star" {
		t.Fatalf("prompt %v at %q, want at star", m.prompt, m.markChoices()[m.markSel])
	}
	if s := ansi.Strip(m.renderMarkPrompt()); !strings.Contains(s, "[⭐]") || !strings.Contains(s, "あとで") {
		t.Errorf("prompt: %q", s)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.markChoices()[m.markSel]; got != "" {
		t.Errorf("after the last mark: %q, want none", got)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.markChoices()[m.markSel]; got != "red" {
		t.Errorf("round: %q, want red", got)
	}
	m, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.prompt != promptNone || cmd == nil {
		t.Errorf("enter: prompt %v, cmd %v", m.prompt, cmd)
	}
	// Not for a note in the trash.
	m.note = &gennotes.Note{ID: "t", Notebook: trashNotebook}
	if m, _ = press(t, m, text("M")); m.prompt != promptNone {
		t.Errorf("M in the trash: prompt %v", m.prompt)
	}
}

func TestMarkWindow(t *testing.T) {
	widths := []int{3, 3, 4, 4, 4, 4}
	for _, tt := range []struct {
		sel, room   int
		first, last int
	}{
		{sel: 0, room: 100, first: 0, last: 6},
		{sel: 0, room: 7, first: 0, last: 2},
		{sel: 5, room: 8, first: 4, last: 6},
		{sel: 3, room: 12, first: 2, last: 5},
		{sel: 3, room: 1, first: 3, last: 4}, // the choice shows, even if it does not fit
		{sel: 9, room: 4, first: 5, last: 6},
	} {
		first, last := markWindow(widths, tt.sel, tt.room)
		if first != tt.first || last != tt.last {
			t.Errorf("markWindow(%d, %d) = %d, %d, want %d, %d", tt.sel, tt.room, first, last, tt.first, tt.last)
		}
	}
}
