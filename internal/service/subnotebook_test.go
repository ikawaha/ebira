package service

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

func TestSubNotebooks(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	dev, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Dev", Kind: "collab"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Zoo"}); err != nil {
		t.Fatal(err)
	}
	// A sub notebook takes the kind of its parent, whatever is asked.
	design, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Design", Kind: "private", Parent: &dev.ID})
	if err != nil {
		t.Fatal(err)
	}
	if design.ID != "dev/design" || design.Parent == nil || *design.Parent != "dev" || design.Kind != "collab" {
		t.Errorf("sub notebook = %+v, want dev/design in dev, of the collab kind", design)
	}
	// Names are told apart among the notebooks of the same parent only.
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "design", Parent: &dev.ID}); errorName(err) != "already_exists" {
		t.Errorf("sub notebook with a taken name = %v, want already_exists", err)
	}
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Design"}); err != nil {
		t.Errorf("notebook with the name of a sub notebook: %v", err)
	}
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Ops", Parent: ptr("dev")}); err != nil {
		t.Fatal(err)
	}
	for parent, want := range map[string]string{"dev/design": "bad_request", store.Inbox: "bad_request", "nothing": "not_found"} {
		if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "X", Parent: &parent}); errorName(err) != want {
			t.Errorf("sub notebook in %s = %v, want %s", parent, err, want)
		}
	}
	nbs, err := svc.Notebooks(person, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, nb := range nbs {
		ids = append(ids, nb.ID)
	}
	if want := []string{store.Inbox, "dev", "dev/design", "dev/ops", "zoo", "design", store.Trash}; !slices.Equal(ids, want) {
		t.Errorf("notebooks = %v, want %v", ids, want)
	}

	// The kind of a sub notebook is that of its parent.
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: design.ID, Kind: ptr("default")}); errorName(err) != "bad_request" {
		t.Errorf("kind of a sub notebook = %v, want bad_request", err)
	}
	if err := svc.MakeNotebookPrivate(ai, &gennotes.MakeNotebookPrivatePayload{ID: design.ID}); errorName(err) != "bad_request" {
		t.Errorf("make_notebook_private of a sub notebook = %v, want bad_request", err)
	}
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: design.ID, Name: ptr("Design 2")}); err != nil {
		t.Errorf("name of a sub notebook: %v", err)
	}
	// AI clients write in the sub notebooks of a collab notebook.
	if _, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{ID: ptr("a"), Notebook: design.ID, Title: "A", Body: ptr("ひまわり")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("b"), Notebook: &dev.ID, Title: "B", Body: ptr("ひまわり")}); err != nil {
		t.Fatal(err)
	}
	// The notes of a sub notebook are not among those of its parent.
	list := func(ctx context.Context, nb string) (res []string) {
		ls, err := svc.List(ctx, &gennotes.ListPayload{Notebook: &nb, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range ls.Notes {
			res = append(res, n.ID)
		}
		return res
	}
	if got := list(person, dev.ID); !slices.Equal(got, []string{"b"}) {
		t.Errorf("notes of dev = %v, want [b]", got)
	}
	if got := list(person, design.ID); !slices.Equal(got, []string{"a"}) {
		t.Errorf("notes of dev/design = %v, want [a]", got)
	}

	// Made private, the parent hides its sub notebooks from AI clients too.
	if err := svc.MakeNotebookPrivate(ai, &gennotes.MakeNotebookPrivatePayload{ID: dev.ID}); err != nil {
		t.Fatal(err)
	}
	if nbs, err := svc.Notebooks(ai, nil); err != nil || slices.ContainsFunc(nbs, func(nb *gennotes.Notebook) bool { return nb.ID == design.ID }) {
		t.Errorf("notebooks for AI = %v, %v, want no dev/design", nbs, err)
	}
	if got := list(ai, design.ID); len(got) != 0 {
		t.Errorf("notes of dev/design for AI = %v, want none", got)
	}
	if sr, err := svc.Search(ai, &gennotes.SearchPayload{Q: "ひまわり", Limit: 20}); err != nil || len(sr.Notes) != 0 {
		t.Errorf("search for AI = %+v, %v, want nothing", sr, err)
	}
	if _, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: design.ID, Title: "C"}); errorName(err) != "forbidden" {
		t.Errorf("AI note in a sub notebook of a private notebook = %v, want forbidden", err)
	}
}

func TestTrashSubNotebook(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	dev, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Dev", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	design, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Design", Parent: &dev.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("a"), Notebook: &design.ID, Title: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: design.ID}); err != nil {
		t.Fatal(err)
	}
	trashed, err := svc.TrashedNotebooks(person, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 || trashed[0].ID != "dev~design" || trashed[0].Name != "Design" || trashed[0].Kind != "private" || trashed[0].Count != 1 {
		t.Fatalf("trashed notebooks = %+v, want dev~design", trashed)
	}
	n, err := svc.Get(person, &gennotes.GetPayload{ID: "a"})
	if err != nil || n.TrashedNotebook == nil || *n.TrashedNotebook != "dev~design" {
		t.Errorf("note in the trashed sub notebook = %+v, %v", n, err)
	}

	// A sub notebook goes back only into its parent, with the same kind.
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: dev.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RestoreNotebook(person, &gennotes.RestoreNotebookPayload{ID: "dev~design"}); errorName(err) != "bad_request" {
		t.Errorf("restore without the parent = %v, want bad_request", err)
	}
	if _, err := svc.Restore(person, &gennotes.RestorePayload{ID: "a"}); errorName(err) != "bad_request" {
		t.Errorf("restore of a note without the parent = %v, want bad_request", err)
	}
	if _, err := svc.RestoreNotebook(person, &gennotes.RestoreNotebookPayload{ID: dev.ID}); err != nil {
		t.Fatal(err)
	}
	nb, err := svc.RestoreNotebook(person, &gennotes.RestoreNotebookPayload{ID: "dev~design"})
	if err != nil {
		t.Fatal(err)
	}
	if nb.ID != design.ID || nb.Kind != "private" {
		t.Errorf("restored sub notebook = %+v", nb)
	}
	if n, err := svc.Get(person, &gennotes.GetPayload{ID: "a"}); err != nil || n.Notebook != design.ID {
		t.Errorf("note after restoring = %+v, %v", n, err)
	}

	// Deleted with its parent, a sub notebook is listed under it.
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: dev.ID}); err != nil {
		t.Fatal(err)
	}
	trashed, err = svc.TrashedNotebooks(person, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 2 || trashed[1].ID != "dev/design" || trashed[1].Parent == nil || *trashed[1].Parent != "dev" {
		t.Errorf("trashed notebooks = %+v, want dev and dev/design in it", trashed)
	}
	ls, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr(store.Trash), TrashedNotebook: ptr("dev/design"), Limit: 50})
	if err != nil || len(ls.Notes) != 1 || ls.Notes[0].ID != "a" {
		t.Errorf("notes of dev/design in the trash = %+v, %v", ls, err)
	}
	// A note of it goes back into the sub notebook, recreating it in its
	// parent.
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Dev", Kind: "private"}); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.Restore(person, &gennotes.RestorePayload{ID: "a"}); err != nil || n.Notebook != design.ID {
		t.Errorf("restore of a note of a sub notebook in the trash = %+v, %v", n, err)
	}
}

func TestMoveNotebook(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	work, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Work", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Dev", Kind: "collab"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Design", Parent: &dev.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("w"), Notebook: &work.ID, Title: "W"}); err != nil {
		t.Fatal(err)
	}
	move := func(id string, parent *string) (*gennotes.Notebook, error) {
		return svc.MoveNotebook(person, &gennotes.MoveNotebookPayload{ID: id, Parent: parent})
	}

	for _, c := range []struct {
		id     string
		parent *string
		want   string
	}{
		{dev.ID, &work.ID, "bad_request"},          // it has a sub notebook
		{store.Inbox, &dev.ID, "bad_request"},      // reserved
		{work.ID, ptr(store.Inbox), "bad_request"}, // the inbox holds none
		{work.ID, ptr("dev/design"), "bad_request"},
		{work.ID, ptr("nothing"), "not_found"},
		{"nothing", &dev.ID, "not_found"},
	} {
		if _, err := move(c.id, c.parent); errorName(err) != c.want {
			t.Errorf("move %s into %v = %v, want %s", c.id, deref(c.parent), err, c.want)
		}
	}

	// Into a notebook: the ID, the kind and the notes follow.
	nb, err := move(work.ID, &dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nb.ID != "dev/work" || deref(nb.Parent) != "dev" || nb.Kind != "collab" || nb.Name != "Work" || nb.Count != 1 {
		t.Errorf("moved notebook = %+v", nb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "dev", "work", store.NotebookFile)); strings.Contains(string(b), "kind") {
		t.Errorf("the sub notebook keeps a kind:\n%s", b)
	}
	if n, err := svc.Get(person, &gennotes.GetPayload{ID: "w"}); err != nil || n.Notebook != "dev/work" {
		t.Errorf("note after the move = %+v, %v", n, err)
	}
	if ls, err := svc.List(ai, &gennotes.ListPayload{Notebook: ptr("dev/work"), Limit: 50}); err != nil || len(ls.Notes) != 1 {
		t.Errorf("notes of dev/work for AI = %+v, %v, want the note, now in a collab notebook", ls, err)
	}

	// A name taken among the new siblings is refused.
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Design"}); err != nil {
		t.Fatal(err)
	}
	if _, err := move("design", &dev.ID); errorName(err) != "already_exists" {
		t.Errorf("move with a taken name = %v, want already_exists", err)
	}

	// To the top: the kind it had in its parent stays.
	nb, err = move("dev/work", nil)
	if err != nil {
		t.Fatal(err)
	}
	if nb.ID != "work" || nb.Parent != nil || nb.Kind != "collab" {
		t.Errorf("notebook moved to the top = %+v", nb)
	}
	// From one parent into another.
	nb, err = move("dev/design", ptr("work"))
	if err != nil {
		t.Fatal(err)
	}
	if nb.ID != "work/design" || nb.Kind != "collab" {
		t.Errorf("sub notebook moved into another notebook = %+v", nb)
	}
}
