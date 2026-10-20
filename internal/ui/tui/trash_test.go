package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestEmptyTrash(t *testing.T) {
	emptied := 0
	api := &Clients{Notes: &gennotes.Client{EmptyTrashEndpoint: func(context.Context, any) (any, error) {
		emptied++
		return nil, nil
	}}}
	m := New(context.Background(), api, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 20})
	m = tm.(Model)
	load := func(trash int, trashed ...*gennotes.TrashedNotebook) {
		tm, _ := m.Update(notebooksMsg{
			notebooks: []*gennotes.Notebook{
				{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
				{ID: trashNotebook, Name: "Trash", Kind: "default", Sort: "order", Reserved: true, Count: trash},
			},
			trashed: trashed,
		})
		m = tm.(Model)
		m.focus = focusNotebooks
		m.selectNotebook(2) // all notes, inbox, trash
	}

	load(1, &gennotes.TrashedNotebook{ID: "old", Name: "Old", Count: 2})
	if !strings.Contains(ansi.Strip(m.renderFooter()), "empty trash") {
		t.Errorf("the footer does not tell d on the trash: %q", ansi.Strip(m.renderFooter()))
	}
	m, _ = press(t, m, text("d"))
	if m.prompt != promptEmptyTrash {
		t.Fatalf("d on the trash: prompt = %v, want the confirmation", m.prompt)
	}
	if got := ansi.Strip(m.renderFooter()); !strings.Contains(got, "3 notes and 1 notebooks") {
		t.Errorf("confirmation = %q, want the counts", got)
	}
	// Anything but y keeps the trash.
	m, cmd := press(t, m, text("n"))
	if m.prompt != promptNone || cmd != nil {
		t.Fatalf("n: prompt = %v, cmd = %v", m.prompt, cmd)
	}
	m, _ = press(t, m, text("d"))
	m, cmd = press(t, m, text("y"))
	if cmd == nil {
		t.Fatal("y: no command")
	}
	if msg, ok := cmd().(notebookOpMsg); !ok || msg.err != nil || msg.status != "status.trashEmptied" || emptied != 1 {
		t.Fatalf("y: msg = %+v, emptied %d times", msg, emptied)
	}

	// An empty trash asks nothing.
	load(0)
	m, _ = press(t, m, text("d"))
	if m.prompt != promptNone || m.status != m.tr.t("status.trashAlreadyEmpty") {
		t.Errorf("d on an empty trash: prompt = %v, status = %q", m.prompt, m.status)
	}
}
