package service

import (
	"context"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

// A notebook that stops being private as its files change outside, as by a
// sync, is told to the web UI; one people make visible through the API is
// not, nor is one moved to the trash.
func TestPrivateLost(t *testing.T) {
	ctx := context.Background()
	person := AsPerson(ctx)
	svc, _ := newTestService(t)
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	plans, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "Plans", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	events, cancel := svc.events.subscribe()
	defer cancel()
	lost := func() (nbs []string) {
		t.Helper()
		for {
			select {
			case ev := <-events:
				if ev.Type == eventPrivateLost {
					nbs = append(nbs, *ev.Notebook)
				}
			case <-time.After(100 * time.Millisecond):
				return nbs
			}
		}
	}

	// Changed outside: the settings file says another kind.
	if err := svc.store.SaveNotebookMeta(diary.ID, store.NotebookMeta{Name: "Diary", Kind: store.KindCollab}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := lost(); len(got) != 1 || got[0] != diary.ID {
		t.Errorf("private_lost after a change outside = %v, want [%s]", got, diary.ID)
	}
	// Told once: the next sync does not tell it again.
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := lost(); len(got) != 0 {
		t.Errorf("private_lost again = %v", got)
	}

	// Made visible by people: no event.
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: plans.ID, Kind: ptr("default")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := lost(); len(got) != 0 {
		t.Errorf("private_lost after a change by people = %v, want none", got)
	}

	// Moved to the trash: gone, not visible.
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: plans.ID, Kind: ptr("private")}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteNotebook(person, &gennotes.DeleteNotebookPayload{ID: plans.ID}); err != nil {
		t.Fatal(err)
	}
	if got := lost(); len(got) != 0 {
		t.Errorf("private_lost after moving to the trash = %v, want none", got)
	}
}
