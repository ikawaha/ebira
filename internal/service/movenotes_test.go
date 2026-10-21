package service

import (
	"context"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

func TestMoveNotes(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	where := func(id string) (notebook, trashedFrom string) {
		t.Helper()
		n, err := svc.Get(person, &gennotes.GetPayload{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return n.Notebook, deref(n.TrashedFrom)
	}

	// Nothing moves when a note is missing.
	if err := svc.MoveNotes(person, &gennotes.MoveNotesPayload{Ids: []string{"a", "nothing"}, Notebook: "work"}); errorName(err) != "not_found" {
		t.Errorf("move with a missing note = %v, want not_found", err)
	}
	if nb, _ := where("a"); nb != store.Inbox {
		t.Errorf("a moved to %s although a note was missing", nb)
	}
	if err := svc.MoveNotes(person, &gennotes.MoveNotesPayload{Ids: []string{"a"}, Notebook: store.Trash}); errorName(err) != "bad_request" {
		t.Errorf("move to the trash = %v, want bad_request", err)
	}

	// Notes in the trash come out of it; a repeated ID is moved once.
	if err := svc.MoveNotes(person, &gennotes.MoveNotesPayload{Ids: []string{"a", "b", "c", "a"}, Notebook: "work"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if nb, from := where(id); nb != "work" || from != "" {
			t.Errorf("%s in %s (trashed from %q), want work", id, nb, from)
		}
	}

	if err := svc.TrashNotes(person, &gennotes.TrashNotesPayload{Ids: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if nb, from := where(id); nb != store.Trash || from != "work" {
			t.Errorf("%s in %s (trashed from %q), want the trash from work", id, nb, from)
		}
	}
	// Nothing moves when a note is in the trash already.
	if err := svc.TrashNotes(person, &gennotes.TrashNotesPayload{Ids: []string{"c", "a"}}); errorName(err) != "bad_request" {
		t.Errorf("trash with a note in the trash = %v, want bad_request", err)
	}
	if nb, _ := where("c"); nb != "work" {
		t.Errorf("c moved to %s although a note was in the trash", nb)
	}
}
