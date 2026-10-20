package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestSyncRepairsCopies(t *testing.T) {
	svc, dir := newTestService(t)
	ctx := AsPerson(context.Background())
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("plan"), Title: "Plan", Body: ptr("mine")}); err != nil {
		t.Fatal(err)
	}
	// Dropbox leaves the other machine's version as a conflicted copy.
	other := "title: Plan\nupdated_at: 2026-10-18T10:00:00Z\nbody: theirs\n"
	if err := os.WriteFile(filepath.Join(dir, "inbox", "plan (Mac's conflicted copy 2026-10-18).yaml"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(ctx, nil); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Search(ctx, &gennotes.SearchPayload{Q: "theirs", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || len(res.Notes) != 1 || res.Notes[0].Title != "Plan (conflict copy)" || res.Notes[0].ID == "plan" {
		t.Errorf("search: %+v", res.Notes)
	}
	if got, err := svc.Get(ctx, &gennotes.GetPayload{ID: "plan"}); err != nil || got.Body != "mine" {
		t.Errorf("plan: %+v, %v", got, err)
	}
}
