package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestEditMenu(t *testing.T) {
	menu := application.NewMenu()
	editMenu(menu.AddSubmenu("Edit"))
	translateMenu(menu, map[string]string{"Edit": "E", "Paste": "P", "Delete": "D"})
	edit := menu.ItemAt(0).GetSubmenu()
	if got := menu.ItemAt(0).Label(); got != "E" {
		t.Errorf("menu = %q, want E", got)
	}
	var labels []string
	for i := 0; edit.ItemAt(i) != nil; i++ {
		item := edit.ItemAt(i)
		if item.IsSeparator() {
			continue
		}
		labels = append(labels, item.Label())
		if acc := item.GetAccelerator(); acc != "" {
			t.Errorf("%q has the shortcut %q", item.Label(), acc)
		}
	}
	want := []string{"Undo\tCtrl+Z", "Redo\tCtrl+Y", "Cut\tCtrl+X", "Copy\tCtrl+C", "P\tCtrl+V", "D", "Select All\tCtrl+A"}
	if len(labels) != len(want) {
		t.Fatalf("labels = %q, want %q", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("labels[%d] = %q, want %q", i, labels[i], want[i])
		}
	}
}

// The items of the archive follow the state of the page: their labels tell
// whether they put in the archive or take out, and they work only on what can
// go in and out.
func TestArchiveItems(t *testing.T) {
	menu := application.NewMenu()
	n := &noteMenu{text: func(s string) string { return s }}
	n.trash = menu.Add("Move to Trash")
	n.archive = menu.Add(archiveLabel(false))
	n.add(n.archive, archivable)
	n.nbArchive = menu.Add(archiveLabel(false))
	n.add(n.nbArchive, nbArchivable)
	n.filter = menu.AddCheckbox("Filter the List", false)
	n.update(pageState{Filter: true})
	if !n.filter.Checked() {
		t.Error("the filter item is not checked while the page filters")
	}
	for _, tt := range []struct {
		name         string
		p            pageState
		note, nb     string
		noteOn, nbOn bool
	}{
		{"nothing to put", pageState{}, "Put in Archive", "Put in Archive", false, false},
		{"a note and a notebook", pageState{Note: true, Archivable: true, NotebookArchivable: true}, "Put in Archive", "Put in Archive", true, true},
		{"in the archive", pageState{Note: true, Archivable: true, Archived: true, NotebookArchivable: true, NotebookArchived: true}, "Take Out of Archive", "Take Out of Archive", true, true},
		{"while editing", pageState{Note: true, Editing: true, Archivable: true, NotebookArchivable: true}, "Put in Archive", "Put in Archive", false, false},
	} {
		n.update(tt.p)
		if got := n.archive.Label(); got != tt.note {
			t.Errorf("%s: note item = %q, want %q", tt.name, got, tt.note)
		}
		if got := n.nbArchive.Label(); got != tt.nb {
			t.Errorf("%s: notebook item = %q, want %q", tt.name, got, tt.nb)
		}
		if n.archive.Enabled() != tt.noteOn || n.nbArchive.Enabled() != tt.nbOn {
			t.Errorf("%s: enabled = %v and %v, want %v and %v", tt.name, n.archive.Enabled(), n.nbArchive.Enabled(), tt.noteOn, tt.nbOn)
		}
	}
	n.reset()
	if n.archive.Label() != "Put in Archive" || !n.archive.Enabled() {
		t.Errorf("after reset: %q, enabled %v", n.archive.Label(), n.archive.Enabled())
	}
}
