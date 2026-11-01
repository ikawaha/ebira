package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// TestSeparators checks that the list draws the separators between the notes,
// one line each and as wide as the rows, and that the cursor and the scroll
// go by the notes only.
func TestSeparators(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "ja")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = tm.(Model)
	m.items = []*gennotes.NoteSummary{
		{ID: "a", Notebook: "work", Title: "ノート A", UpdatedAt: "2026-10-30T00:00:00Z"},
		{ID: "b", Notebook: "work", Title: "ノート B", UpdatedAt: "2026-10-30T00:00:00Z"},
		{ID: "c", Notebook: "work", Title: "ノート C", UpdatedAt: "2026-10-30T00:00:00Z"},
	}
	m.seps = []*gennotes.Separator{
		{ID: "sep-1", Notebook: "work", Title: ptr("今週"), Date: ptr("2026-10-27"), Before: ptr("a")},
		{ID: "sep-2", Notebook: "work", Title: ptr("先週"), Color: ptr("blue"), Before: ptr("c")},
		{ID: "sep-3", Notebook: "work"},
	}
	rows := func(h int) []string {
		var rows []string
		for _, l := range strings.Split(ansi.Strip(m.renderList(40, h)), "\n") {
			if strings.HasPrefix(l, "│") && strings.TrimSpace(strings.Trim(l, "│")) != "" && !strings.Contains(l, "work ·") {
				rows = append(rows, strings.TrimSpace(strings.Trim(l, "│ ▌")))
			}
		}
		return rows
	}
	got := strings.Join(rows(20), " / ")
	if want := "今週 ──"; !strings.HasPrefix(got, want) || !strings.Contains(got, "─ 2026-10-27 / ノート A / ノート B / 先週 ─") || !strings.HasSuffix(got, "/ ノート C / ─────────────────────────────────────") {
		t.Errorf("rows: %s", got)
	}
	for _, l := range strings.Split(m.renderList(40, 20), "\n") {
		if w := ansi.StringWidth(l); w != 40 {
			t.Errorf("line %q is %d wide, want 40", ansi.Strip(l), w)
		}
	}

	// In 7 lines inside the box: the first separator and A and B, but not the
	// second separator and C, which do not fit whole.
	m.top, m.sel = 0, 0
	if got := strings.Join(rows(9), " / "); strings.Contains(got, "ノート C") || !strings.Contains(got, "ノート B") {
		t.Errorf("rows in a short box: %s", got)
	}
	// The cursor goes by the notes, and the list scrolls to show it.
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	m = tm.(Model)
	m.sel = 2
	m.scrollList()
	if m.top == 0 {
		t.Errorf("the list did not scroll to the last note: top %d", m.top)
	}
	if lines := m.listLines(m.top, m.computeLayout().paneH-2); !containsNote(lines, 2) {
		t.Errorf("the last note is not shown from top %d", m.top)
	}
}

func containsNote(lines []listLine, i int) bool {
	for _, l := range lines {
		if l.note == i {
			return true
		}
	}
	return false
}
