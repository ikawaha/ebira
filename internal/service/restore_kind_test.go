package service

import (
	"context"
	"regexp"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

// A note deleted on its own from a private notebook does not come back into
// a notebook AI clients can read, whatever became of its notebook.
func TestRestoreKeepsPrivate(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &diary.ID, Title: "secret", Body: ptr("private text")}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: diary.ID}); err != nil {
		t.Fatal(err)
	}

	// While its notebook is in the trash, the note waits for it.
	if _, err := svc.Restore(person, &gennotes.RestorePayload{ID: "secret"}); errorName(err) != "bad_request" {
		t.Errorf("restore while the notebook is in the trash = %v, want bad_request", err)
	}
	// AI clients cannot make a notebook with its ID meanwhile.
	nb, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "Diary"})
	if err != nil {
		t.Fatal(err)
	}
	if nb.ID == diary.ID {
		t.Errorf("AI notebook took the ID %s of a notebook in the trash", nb.ID)
	}

	// Deleted for good, the notebook comes back private.
	if err := svc.DeleteTrashedNotebook(person, &gennotes.DeleteTrashedNotebookPayload{ID: diary.ID}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Restore(person, &gennotes.RestorePayload{ID: "secret"})
	if err != nil || n.Notebook != diary.ID {
		t.Fatalf("restore after the notebook is gone = %+v, %v", n, err)
	}
	if m, err := svc.store.NotebookMeta(diary.ID); err != nil || m.Kind != store.KindPrivate {
		t.Errorf("recreated notebook = %+v, %v, want private", m, err)
	}
	if _, err := svc.Get(ai, &gennotes.GetPayload{ID: "secret"}); errorName(err) != "not_found" {
		t.Errorf("AI get of the restored note = %v, want not_found", err)
	}
}

// The same holds for a note of a private sub notebook: the sub notebook, or
// its parent when that is gone too, comes back private, and neither is
// recreated while it waits in the trash.
func TestRestoreSubKeepsPrivate(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	work, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Work", Kind: "collab"})
	if err != nil {
		t.Fatal(err)
	}
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Diary", Parent: &work.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: diary.ID, Kind: ptr("private")}); err != nil {
		t.Fatal(err)
	}
	trash := func(id string) {
		t.Helper()
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Notebook: &diary.ID, Title: id, Body: ptr("private text")}); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(person, &gennotes.DeletePayload{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	purge := func(nb string) {
		t.Helper()
		trashed, err := svc.store.TrashedNotebooks()
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range trashed {
			if store.RestoredNotebook(id) == nb {
				if err := svc.DeleteTrashedNotebook(person, &gennotes.DeleteTrashedNotebookPayload{ID: id}); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("%s is not in the trash: %v", nb, trashed)
	}
	restore := func(id string) {
		t.Helper()
		n, err := svc.Restore(person, &gennotes.RestorePayload{ID: id})
		if err != nil || n.Notebook != diary.ID {
			t.Fatalf("restore %s = %+v, %v", id, n, err)
		}
		if _, err := svc.Get(ai, &gennotes.GetPayload{ID: id}); errorName(err) != "not_found" {
			t.Errorf("AI get of %s = %v, want not_found", id, err)
		}
	}

	// The sub notebook is gone; its parent stays.
	trash("one")
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: diary.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Restore(person, &gennotes.RestorePayload{ID: "one"}); errorName(err) != "bad_request" {
		t.Errorf("restore while the sub notebook is in the trash = %v, want bad_request", err)
	}
	purge(diary.ID)
	restore("one")
	if m, err := svc.store.NotebookMeta(diary.ID); err != nil || m.OwnKind == nil || *m.OwnKind != store.KindPrivate {
		t.Errorf("recreated sub notebook = %+v, %v, want its own kind private", m, err)
	}

	// The parent is gone with it.
	trash("two")
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: work.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Restore(person, &gennotes.RestorePayload{ID: "two"}); errorName(err) != "bad_request" {
		t.Errorf("restore while the parent is in the trash = %v, want bad_request", err)
	}
	purge(work.ID)
	restore("two")
	if m, err := svc.store.NotebookMeta(work.ID); err != nil || m.Kind != store.KindPrivate {
		t.Errorf("recreated parent = %+v, %v, want private", m, err)
	}
}

// The ID of a notebook made by AI clients does not tell whether a private
// notebook of the same name exists.
func TestAINotebookIDHidesPrivate(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	if _, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Medical", Kind: "private"}); err != nil {
		t.Fatal(err)
	}
	shape := regexp.MustCompile(`^[a-z]+-[0-9a-f]{6}$`)
	for _, name := range []string{"Medical", "Holiday"} {
		nb, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if !shape.MatchString(nb.ID) {
			t.Errorf("AI notebook %s = %s, want <name>-<random>", name, nb.ID)
		}
	}
}
