package service

import (
	"context"
	"path/filepath"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/history"
)

func newHistoryService(t *testing.T) *Notes {
	t.Helper()
	svc, notes := newTestService(t)
	h, err := history.New(filepath.Join(filepath.Dir(notes), "history"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetHistory(h)
	return svc
}

func TestHistory(t *testing.T) {
	ctx := AsPerson(context.Background())
	svc := newHistoryService(t)
	if _, err := svc.Create(ctx, &gennotes.CreatePayload{ID: ptr("a"), Title: "A", Body: ptr("一行目")}); err != nil {
		t.Fatal(err)
	}
	vs, err := svc.History(ctx, &gennotes.HistoryPayload{ID: "a"})
	if err != nil || len(vs) != 0 {
		t.Fatalf("History of a new note = %v, %v; want none", vs, err)
	}
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Body: ptr("二行目")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Title: ptr("B")}); err != nil {
		t.Fatal(err)
	}
	// Moving the note keeps its content, so it adds no version.
	if _, err := svc.Update(ctx, &gennotes.UpdatePayload{ID: "a", Notebook: ptr("work")}); err != nil {
		t.Fatal(err)
	}
	vs, err = svc.History(ctx, &gennotes.HistoryPayload{ID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].Title != "A" || vs[1].Title != "A" {
		t.Fatalf("History = %+v, want the 2 versions before the saves", vs)
	}
	v, err := svc.HistoryVersion(ctx, &gennotes.HistoryVersionPayload{ID: "a", Rev: vs[1].Rev})
	if err != nil {
		t.Fatal(err)
	}
	if v.Body != "一行目" || v.Title != "A" {
		t.Errorf("oldest version = %+v", v)
	}
	if _, err := svc.HistoryVersion(ctx, &gennotes.HistoryVersionPayload{ID: "a", Rev: "1"}); errorName(err) != "not_found" {
		t.Errorf("missing version: %v, want not_found", err)
	}

	// AI clients see no history, of any note.
	if _, err := svc.History(context.Background(), &gennotes.HistoryPayload{ID: "a"}); errorName(err) != "not_found" {
		t.Errorf("History for AI: %v, want not_found", err)
	}
	if _, err := svc.HistoryVersion(context.Background(), &gennotes.HistoryVersionPayload{ID: "a", Rev: vs[0].Rev}); errorName(err) != "not_found" {
		t.Errorf("HistoryVersion for AI: %v, want not_found", err)
	}

	// Deleting the note for good drops its history.
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, &gennotes.DeletePayload{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if vs, err := svc.history.List("a"); err != nil || len(vs) != 0 {
		t.Errorf("history after the note was deleted = %v, %v", vs, err)
	}
	if _, err := svc.History(ctx, &gennotes.HistoryPayload{ID: "a"}); errorName(err) != "not_found" {
		t.Errorf("History of a deleted note: %v, want not_found", err)
	}
}
