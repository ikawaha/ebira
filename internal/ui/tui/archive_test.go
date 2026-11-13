package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestArchiveRows(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	yes, since := true, "2026-11-12T00:00:00.000Z"
	all := []*gennotes.Notebook{
		{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
		{ID: "work", Name: "Work", Kind: "default", Sort: "order", Count: 2, ArchivedCount: 1},
		{ID: "past", Name: "Past", Kind: "collab", Sort: "order", Count: 3, ArchivedAt: &since, Archived: &yes},
		{ID: "past/old", Name: "Old", Kind: "collab", Sort: "order", Count: 1, Parent: ptr("past"), Archived: &yes},
		{ID: trashNotebook, Name: "Trash", Kind: "default", Sort: "order", Reserved: true},
	}
	rest, archive := splitArchived(all)
	if len(rest) != 3 || len(archive) != 2 {
		t.Fatalf("split into %d and %d notebooks, want 3 and 2", len(rest), len(archive))
	}
	tm, _ = m.Update(notebooksMsg{notebooks: rest, archived: archive})
	m = tm.(Model)
	var ids []string
	for _, r := range m.notebookRows() {
		ids = append(ids, r.id)
	}
	if want := []string{"", inboxNotebook, "work", "@", "@past", "@past/old", trashNotebook}; !slices.Equal(ids, want) {
		t.Fatalf("rows = %q, want %q", ids, want)
	}
	// The archive counts the note of work put there on its own and all those
	// of the notebooks there; all notes, the rest.
	if r := m.notebookRows()[3]; r.count != 5 {
		t.Errorf("archive count = %d, want 5", r.count)
	}
	if r := m.notebookRows()[0]; r.count != 2 {
		t.Errorf("all count = %d, want 2", r.count)
	}

	v := m.renderNotebooks(40, 12)
	for _, s := range []string{archiveIcon + " Archive", "↳ 👥 Past", "  ↳ 👥 Old"} {
		if !strings.Contains(v, s) {
			t.Errorf("notebook pane lacks %q:\n%s", s, v)
		}
	}
	// A note in a notebook in the archive is named after it.
	if got := m.notebookName("past/old"); got != "Past / Old" {
		t.Errorf("name of past/old = %q", got)
	}

	// The archive lists the notes put there on their own; a notebook there
	// all its notes, and a new note goes into it.
	for _, c := range []struct{ filter, param, nb string }{
		{"@", "only", inboxNotebook},
		{"@past", "include", "past"},
		{"work", "exclude", "work"},
	} {
		m.notebook = c.filter
		if p := archiveParam(c.filter); p != c.param {
			t.Errorf("archived of %q = %q, want %q", c.filter, p, c.param)
		}
		if nb := m.defaultNotebook(); nb != c.nb {
			t.Errorf("new note in %q goes to %q, want %q", c.filter, nb, c.nb)
		}
	}
	m.notebook = "@past"
	if m.canReorder() {
		t.Error("a notebook in the archive can be reordered")
	}

	// Once the notebook leaves the archive, its own list is shown; with
	// nothing left there, the archive has no row.
	tm, _ = m.Update(notebooksMsg{notebooks: []*gennotes.Notebook{
		rest[0],
		{ID: "work", Name: "Work", Sort: "order", Count: 3},
		{ID: "past", Name: "Past", Kind: "collab", Sort: "order", Count: 3},
		rest[2],
	}})
	m = tm.(Model)
	if m.notebook != "past" {
		t.Errorf("notebook = %q after leaving the archive, want past", m.notebook)
	}
	for _, r := range m.notebookRows() {
		if r.id == archivePrefix {
			t.Error("the archive has a row with nothing in it")
		}
	}
}

func TestArchiveKeys(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	yes, since := true, "2026-11-12T00:00:00.000Z"
	tm, _ = m.Update(notebooksMsg{
		notebooks: []*gennotes.Notebook{{ID: inboxNotebook, Name: "Inbox", Reserved: true}, {ID: trashNotebook, Name: "Trash", Reserved: true}},
		archived: []*gennotes.Notebook{
			{ID: "past", Name: "Past", ArchivedAt: &since, Archived: &yes},
			{ID: "past/old", Name: "Old", Parent: ptr("past"), Archived: &yes},
		},
	})
	m = tm.(Model)

	// Notes in the trash, in the inbox and in a notebook in the archive stay.
	for _, c := range []struct{ notebook, status string }{
		{trashNotebook, "status.archiveTrash"},
		{inboxNotebook, "status.archiveInbox"},
		{"past/old", "status.archivedWithNotebook"},
	} {
		m.note = &gennotes.Note{ID: "n1", Notebook: c.notebook}
		if cmd := m.toggleNoteArchive(); cmd != nil || m.status != m.tr.t(c.status) {
			t.Errorf("x on a note in %s: status %q", c.notebook, m.status)
		}
	}
	// A sub notebook there with its parent goes out with it.
	m.focus = focusNotebooks
	m.nbSel = slices.IndexFunc(m.notebookRows(), func(r notebookRow) bool { return r.id == "@past/old" })
	if cmd := m.toggleNotebookArchive(); cmd != nil || m.status != m.tr.t("status.archivedWithParent") {
		t.Errorf("x on a sub notebook there with its parent: status %q", m.status)
	}
	// inbox stays out.
	m.nbSel = 1
	if cmd := m.toggleNotebookArchive(); cmd != nil || m.status != m.tr.t("status.archiveReserved") {
		t.Errorf("x on inbox: status %q", m.status)
	}
	// The notebook put there itself can be taken out.
	m.nbSel = slices.IndexFunc(m.notebookRows(), func(r notebookRow) bool { return r.id == "@past" })
	if cmd := m.toggleNotebookArchive(); cmd == nil {
		t.Error("x on a notebook in the archive does nothing")
	}
}

// A search tells how many notes in the archive match too: in the title of the
// list, or in place of the list when nothing else matches.
func TestArchiveHits(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = tm.(Model)
	m.query = "apple"
	m.listSeq = 1
	tm, _ = m.Update(listMsg{seq: 1, archived: 2})
	m = tm.(Model)
	if v := ansi.Strip(m.renderList(60, 10)); !strings.Contains(v, "The archive has 2") {
		t.Errorf("no note found, and the archive not told:\n%s", v)
	}
	tm, _ = m.Update(listMsg{seq: 1, items: []*gennotes.NoteSummary{{ID: "a", Notebook: "work", Title: "apple pie"}}, total: 1, archived: 2})
	m = tm.(Model)
	if v := ansi.Strip(m.renderList(60, 10)); !strings.Contains(v, "2 more in the archive") {
		t.Errorf("the title does not tell the archive:\n%s", v)
	}
	tm, _ = m.Update(listMsg{seq: 1, items: []*gennotes.NoteSummary{{ID: "a", Notebook: "work", Title: "apple pie"}}, total: 1})
	m = tm.(Model)
	if v := ansi.Strip(m.renderList(60, 10)); strings.Contains(v, "archive") {
		t.Errorf("the archive is told with nothing there:\n%s", v)
	}
}
