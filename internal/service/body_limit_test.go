package service

import (
	"context"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// A body gets no larger than maxBody through the API, also when append or
// edits make it grow.
func TestBodyLimit(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, _ := newTestService(t)
	full, over := strings.Repeat("a", maxBody), strings.Repeat("a", maxBody+1)

	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("big"), Title: "big", Body: &over}); errorName(err) != "bad_request" {
		t.Errorf("create with a body past the limit = %v, want bad_request", err)
	}
	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("full"), Title: "full", Body: &full})
	if err != nil {
		t.Fatalf("create with a body at the limit: %v", err)
	}
	if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: n.ID, Body: &over}); errorName(err) != "bad_request" {
		t.Errorf("update with a body past the limit = %v, want bad_request", err)
	}

	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "big", Body: &over}); errorName(err) != "bad_request" {
		t.Errorf("create_ai_note with a body past the limit = %v, want bad_request", err)
	}
	m, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: shared.ID, Title: "log", Body: &full})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: m.ID, Version: m.Version, Append: ptr("more")}); errorName(err) != "bad_request" {
		t.Errorf("append past the limit = %v, want bad_request", err)
	}
	if got, err := svc.Get(ai, &gennotes.GetPayload{ID: m.ID}); err != nil || len(got.Body) != maxBody {
		t.Errorf("after the refused append: %d bytes, %v", len(got.Body), err)
	}
}
