package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestTagRows(t *testing.T) {
	m := New(context.Background(), &Clients{}, false)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = tm.(Model)
	msg := notebooksMsg{
		notebooks: []*gennotes.Notebook{
			{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
			{ID: "work", Name: "Work", Kind: "default", Sort: "order"},
			{ID: trashNotebook, Name: "Trash", Kind: "default", Sort: "order", Reserved: true},
		},
		tags: []*gennotes.TagCount{{Name: "go", Count: 3}, {Name: "検索", Count: 1}},
	}
	tm, _ = m.Update(msg)
	m = tm.(Model)
	ids := func() []string {
		var ids []string
		for _, r := range m.notebookRows() {
			ids = append(ids, r.id)
		}
		return ids
	}
	if got, want := ids(), []string{"", inboxNotebook, "work", trashNotebook}; !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}

	// T switches the pane to the tags, keeping the list.
	m.selectNotebook(2)
	m, _ = press(t, m, text("T"))
	if got, want := ids(), []string{"", "#go", "#検索"}; !slices.Equal(got, want) || m.notebook != "work" || m.nbSel != 0 {
		t.Fatalf("tag rows = %q, notebook = %q, cursor = %d; want %q, work, 0", got, m.notebook, m.nbSel, want)
	}

	// Choosing a tag lists the notes with it in all notebooks.
	m.selectNotebook(1)
	if m.notebook != "#go" || !m.acrossNotebooks() || m.canReorder() {
		t.Errorf("notebook = %q, across = %v, reorder = %v", m.notebook, m.acrossNotebooks(), m.canReorder())
	}
	if nb, tags := m.defaultNotebook(), m.defaultTags(); nb != inboxNotebook || !slices.Equal(tags, []string{"go"}) {
		t.Errorf("a new note goes to %q with %q, want inbox with [go]", nb, tags)
	}

	// The selected tag stays when no note has it any more.
	msg.tags = msg.tags[1:]
	tm, _ = m.Update(msg)
	m = tm.(Model)
	if r := m.selectedRow(); r.id != "#go" || r.count != 0 {
		t.Errorf("selected row = %+v, want #go with no notes", r)
	}

	// The pane scrolls to keep the selected row in view.
	if top := m.notebookTop(2); top != 1 {
		t.Errorf("top of 2 rows = %d, want 1", top)
	}
	if v := m.renderNotebooks(30, 4); !strings.Contains(v, "# go") {
		t.Errorf("the selected tag is out of view:\n%s", v)
	}

	// Back to the notebooks, where no row is the tag: the cursor is on all
	// notes, and the list stays.
	m, _ = press(t, m, text("T"))
	if m.sideTags || m.notebook != "#go" || m.nbSel != 0 {
		t.Errorf("tags = %v, notebook = %q, cursor = %d; want the notebooks, #go, 0", m.sideTags, m.notebook, m.nbSel)
	}
}
