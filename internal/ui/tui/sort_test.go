package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestCycleSort(t *testing.T) {
	m := New(context.Background(), &Clients{}, false)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	tm, _ = m.Update(notebooksMsg{notebooks: []*gennotes.Notebook{
		{ID: inboxNotebook, Name: "Inbox", Kind: "default", Sort: "order", Reserved: true},
		{ID: "work", Name: "Work", Kind: "default", Sort: "order"},
	}})
	m = tm.(Model)
	m.notebook = "work"
	if !m.canReorder() {
		t.Fatal("a notebook in the manual order cannot be reordered")
	}

	// Two quick presses: the sort cycles at once, and only the last is saved.
	m, first := press(t, m, text("s"))
	m, last := press(t, m, text("s"))
	if got := m.currentSort(); got != "updated" {
		t.Errorf("sort = %q, want updated", got)
	}
	if m.canReorder() {
		t.Error("notes sorted by update time can be reordered")
	}
	if cmd := tick(t, m, first); cmd != nil {
		t.Error("the first press was saved")
	}
	if cmd := tick(t, m, last); cmd == nil {
		t.Error("the last press was not saved")
	}

	// All notes keep their sort in the TUI only.
	m.notebook = ""
	m, _ = press(t, m, text("s"))
	if m.allSort != "created" || m.currentSort() != "created" {
		t.Errorf("all notes sort = %q", m.allSort)
	}
	if m.canReorder() {
		t.Error("all notes can be reordered")
	}
}

// tick runs the sort tick of cmd and returns the command of handling it.
func tick(t *testing.T, m Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	msg, ok := cmd().(sortTickMsg)
	if !ok {
		t.Fatalf("command does not give a sortTickMsg")
	}
	_, next := m.Update(msg)
	return next
}
