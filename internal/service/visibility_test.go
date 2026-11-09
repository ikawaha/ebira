package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// Notes the index still has under a notebook that is no longer on disk, such
// as a private notebook renamed outside ebira before the index syncs, stay
// hidden from AI clients.
func TestStaleIndexHidesFromAI(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	vault, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "vault", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &vault.ID, Title: "secret", Body: ptr("quokka"), Tags: []string{"hush"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, vault.ID), filepath.Join(dir, "vault2")); err != nil {
		t.Fatal(err)
	}

	// The index has not synced yet.
	if res, err := svc.Search(ai, &gennotes.SearchPayload{Q: "quokka", Limit: 50}); err != nil || res.Total != 0 {
		t.Errorf("AI search = %+v, %v, want nothing", res, err)
	}
	if res, err := svc.List(ai, &gennotes.ListPayload{Limit: 50}); err != nil || res.Total != 0 {
		t.Errorf("AI list = %+v, %v, want nothing", res, err)
	}
	tags, err := svc.Tags(ai, &gennotes.TagsPayload{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range tags {
		if tg.Name == "hush" {
			t.Errorf("AI tags = %+v, want no tag of the hidden note", tags)
		}
	}
	// People still find it.
	if res, err := svc.Search(person, &gennotes.SearchPayload{Q: "quokka", Limit: 50}); err != nil || res.Total != 1 {
		t.Errorf("person search = %+v, %v", res, err)
	}
}
