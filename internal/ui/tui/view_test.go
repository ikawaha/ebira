package tui

import (
	"context"
	"slices"
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

// TestSubNotebookRows checks that the sub notebooks follow their parents, one
// column to the right after an arrow and without an icon, as they take the
// kind of their parents, and that their names tell the parents elsewhere.
func TestSubNotebookRows(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	dev := "dev"
	tm, _ = m.Update(notebooksMsg{
		notebooks: []*gennotes.Notebook{
			{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
			{ID: "dev", Name: "Dev", Kind: "collab", Sort: "order"},
			{ID: "dev/design", Name: "Design", Kind: "collab", Sort: "order", Parent: &dev},
			{ID: trashNotebook, Name: "Trash", Kind: "default", Sort: "order", Reserved: true},
		},
		trashed: []*gennotes.TrashedNotebook{
			{ID: "old", Name: "Old", Kind: "private", Count: 1},
			{ID: "old/sub", Name: "Sub", Kind: "private", Count: 1, Parent: ptr("old")},
		},
	})
	m = tm.(Model)
	lines := strings.Split(ansi.Strip(m.renderNotebooks(30, 10)), "\n")
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
	if got := col("Design"); got != want+2 {
		t.Errorf("Design starts at %d, want %d\n%s", got, want+2, strings.Join(lines, "\n"))
	}
	if got := col("Sub"); got != want+4 {
		t.Errorf("Sub starts at %d, want %d\n%s", got, want+4, strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), "👥 Design") {
		t.Errorf("a sub notebook has the icon of its kind\n%s", strings.Join(lines, "\n"))
	}
	if got := m.notebookName("dev/design"); got != "Dev / Design" {
		t.Errorf("notebookName(dev/design) = %q", got)
	}
	if got := m.notebookName(trashedPrefix + "old/sub"); got != "↳ Old / Sub" {
		t.Errorf("notebookName of a sub notebook in the trash = %q", got)
	}
	if got := inTrash(&gennotes.Notebook{ID: "dev/design", Parent: &dev}); got != "dev~design" || restoredAs(got) != "dev/design" {
		t.Errorf("inTrash = %q, restoredAs = %q", got, restoredAs(got))
	}
}

// TestMoveNotebook checks choosing where to move a notebook with m: a
// notebook other than the inbox, the trash and sub notebooks, or All for the
// top, asking first when a private notebook would become visible to AI.
func TestMoveNotebook(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	dev := "dev"
	tm, _ = m.Update(notebooksMsg{
		notebooks: []*gennotes.Notebook{
			{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
			{ID: "dev", Name: "Dev", Kind: "collab", Sort: "order"},
			{ID: "dev/design", Name: "Design", Kind: "collab", Sort: "order", Parent: &dev},
			{ID: "work", Name: "Work", Kind: "private", Sort: "order"},
			{ID: trashNotebook, Name: "Trash", Kind: "default", Sort: "order", Reserved: true},
		},
	})
	m = tm.(Model)
	m.focus = focusNotebooks
	row := func(id string) int {
		return slices.IndexFunc(m.notebookRows(), func(r notebookRow) bool { return r.id == id })
	}

	// A notebook with sub notebooks cannot go into another.
	m.nbSel = row("dev")
	m, _ = press(t, m, text("m"))
	if m.nbToMove != nil {
		t.Fatal("moving a notebook with sub notebooks")
	}
	// Into the inbox, a sub notebook or itself: refused.
	m.nbSel = row("work")
	m, _ = press(t, m, text("m"))
	if m.nbToMove == nil || m.nbToMove.ID != "work" {
		t.Fatalf("nbToMove = %+v", m.nbToMove)
	}
	for _, id := range []string{inboxNotebook, "dev/design", "work", trashNotebook} {
		m.nbSel = row(id)
		if m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}); m.prompt != promptNone || m.nbToMove == nil {
			t.Errorf("enter on %s: prompt %v, moving %v", id, m.prompt, m.nbToMove)
		}
	}
	// Into a collab notebook: AI would read the private notebook, so asked.
	m.nbSel = row("dev")
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.prompt != promptMoveNotebook || m.nbParent != "dev" {
		t.Errorf("prompt %v, parent %q, want the question about AI", m.prompt, m.nbParent)
	}
	m, _ = press(t, m, text("n"))
	if m.prompt != promptNone || m.nbToMove == nil {
		t.Errorf("after n: prompt %v, moving %v, want still choosing", m.prompt, m.nbToMove)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.nbToMove != nil {
		t.Error("esc did not cancel")
	}
	// A sub notebook goes to the top on All.
	m.nbSel = row("dev/design")
	m, _ = press(t, m, text("m"))
	m.nbSel = row("")
	if m, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}); m.nbToMove != nil || cmd == nil {
		t.Errorf("moving to the top: moving %v, cmd %v", m.nbToMove, cmd)
	}
}
