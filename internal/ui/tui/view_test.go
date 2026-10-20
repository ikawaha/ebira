package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestPadHighlight(t *testing.T) {
	st, hl := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true)
	tests := []struct {
		s     string
		spans []*gennotes.TextRange
		w     int
		want  string
	}{
		{"東京都の天気", []*gennotes.TextRange{{Start: 1, End: 3}}, 14, "東京都の天気  "},
		{"東京都の天気", []*gennotes.TextRange{{Start: 4, End: 6}}, 8, "東京都… "},
		{"abc", nil, 5, "abc  "},
	}
	for _, tt := range tests {
		got := padHighlight(tt.s, tt.spans, st, hl, tt.w)
		if plain := ansi.Strip(got); plain != tt.want {
			t.Errorf("padHighlight(%q, %d) = %q, want %q", tt.s, tt.w, plain, tt.want)
		}
	}
}

func TestShiftRanges(t *testing.T) {
	got := shiftRanges([]*gennotes.TextRange{{Start: 0, End: 2}}, "受信 · ")
	if got[0].Start != 5 || got[0].End != 7 {
		t.Errorf("shiftRanges = %+v, want {5 7}", *got[0])
	}
}

func TestNotebookIcons(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	tm, _ = m.Update(notebooksMsg{
		notebooks: []*gennotes.Notebook{
			{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
			{ID: "work", Name: "Work", Kind: "default", Sort: "order"},
			{ID: "shared", Name: "Shared", Kind: "collab", Sort: "order"},
			{ID: trashNotebook, Name: "Trash", Kind: "default", Sort: "order", Reserved: true},
		},
		trashed: []*gennotes.TrashedNotebook{{ID: "old", Name: "Old", Kind: "private", Count: 1}},
	})
	m = tm.(Model)
	lines := strings.Split(ansi.Strip(m.renderNotebooks(30, 9)), "\n")
	// The names start in one column, with or without an icon; the notebook in
	// the trash is one column to the right, after the arrow.
	col := func(name string) int {
		for _, l := range lines {
			if i := strings.Index(l, " "+name+" "); i >= 0 {
				return ansi.StringWidth(l[:i])
			}
		}
		t.Fatalf("no row of %s in\n%s", name, strings.Join(lines, "\n"))
		return 0
	}
	want := col("All")
	for _, name := range []string{"Inbox", "Work", "Shared", "Trash"} {
		if got := col(name); got != want {
			t.Errorf("%s starts at %d, want %d\n%s", name, got, want, strings.Join(lines, "\n"))
		}
	}
	if got := col("Old"); got != want+2 {
		t.Errorf("Old starts at %d, want %d\n%s", got, want+2, strings.Join(lines, "\n"))
	}
	for _, s := range []string{"📥 Inbox", "👥 Shared", trashIcon + " Trash", "↳ 🔒 Old"} {
		if !strings.Contains(strings.Join(lines, "\n"), s) {
			t.Errorf("no %q in\n%s", s, strings.Join(lines, "\n"))
		}
	}
}
