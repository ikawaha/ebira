package service

import (
	"context"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestAddGuide(t *testing.T) {
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	nb, err := svc.AddGuide(person, &gennotes.AddGuidePayload{Lang: ptr("ja")})
	if err != nil {
		t.Fatal(err)
	}
	if nb.Count == 0 || nb.Kind != "default" {
		t.Errorf("guide = %+v", nb)
	}
	// The notes are indexed, so that a search finds them.
	res, err := svc.Search(person, &gennotes.SearchPayload{Q: "tag:guide", Notebook: &nb.ID, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total == 0 {
		t.Error("no notes of the guide found")
	}
	// The notes of the guide do not count as imported, so that importing the
	// same file is not skipped.
	again, err := svc.AddGuide(person, &gennotes.AddGuidePayload{Lang: ptr("ja")})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == nb.ID || again.Name != nb.Name+" (2)" {
		t.Errorf("second guide = %+v", again)
	}
}
