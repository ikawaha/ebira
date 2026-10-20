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

// TestDropNotebooks checks notebooks of the drop kind: AI clients may add
// notes to them but cannot see their notes, and notes created by AI are
// marked.
func TestDropNotebooks(t *testing.T) {
	ai := context.Background()
	person := AsPerson(ai)
	svc, dir := newTestService(t)
	// The inbox is of the drop kind by default, without a kind in its file.
	if b, err := os.ReadFile(filepath.Join(dir, store.Inbox, store.NotebookFile)); err == nil && strings.Contains(string(b), "kind") {
		t.Fatalf("inbox settings: %s", b)
	}
	mine, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("mine"), Title: "人のメモ", Body: ptr("ひまわり")})
	if err != nil {
		t.Fatal(err)
	}
	if mine.CreatedBy != nil {
		t.Errorf("a note of people has created_by %q", *mine.CreatedBy)
	}

	// AI clients see the inbox in the list, with no count, but not its notes.
	nbs, err := svc.Notebooks(ai, nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(nbs, func(nb *gennotes.Notebook) bool { return nb.ID == store.Inbox })
	if i < 0 || nbs[i].Kind != "drop" || nbs[i].Count != 0 {
		t.Fatalf("notebooks for AI = %+v, want inbox of the drop kind with count 0", nbs)
	}
	if ls, err := svc.List(ai, &gennotes.ListPayload{Limit: 50}); err != nil || len(ls.Notes) != 0 {
		t.Errorf("List for AI = %v, %v; want none", ls, err)
	}
	if sr, err := svc.Search(ai, &gennotes.SearchPayload{Q: "ひまわり", Limit: 20}); err != nil || sr.Total != 0 {
		t.Errorf("Search for AI = %+v, %v; want none", sr, err)
	}
	if _, err := svc.Get(ai, &gennotes.GetPayload{ID: "mine"}); errorName(err) != "not_found" {
		t.Errorf("Get for AI = %v, want not_found", err)
	}

	// They may add a note, with a random ID only, and then cannot touch it.
	if _, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{ID: ptr("mine"), Notebook: store.Inbox, Title: "x"}); errorName(err) != "bad_request" {
		t.Errorf("CreateAiNote with an ID in a drop notebook = %v, want bad_request", err)
	}
	added, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: store.Inbox, Title: "AI のメモ", Body: ptr("調べたこと")})
	if err != nil {
		t.Fatal(err)
	}
	if added.CreatedBy == nil || *added.CreatedBy != "ai" {
		t.Errorf("created_by = %v, want ai", added.CreatedBy)
	}
	if _, err := svc.UpdateAiNote(ai, &gennotes.UpdateAiNotePayload{ID: added.ID, Version: added.Version, Title: ptr("y")}); errorName(err) != "not_found" {
		t.Errorf("UpdateAiNote in a drop notebook = %v, want not_found", err)
	}
	if err := svc.DeleteAiNote(ai, &gennotes.DeleteAiNotePayload{ID: added.ID, Version: added.Version}); errorName(err) != "not_found" {
		t.Errorf("DeleteAiNote in a drop notebook = %v, want not_found", err)
	}

	// People see the mark in the file, the note and the list, and it stays
	// when they edit the note.
	b, err := os.ReadFile(filepath.Join(dir, store.Inbox, added.ID+store.Ext))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "created_by: ai\nbody:") {
		t.Errorf("file:\n%s\nwant created_by before body", b)
	}
	edited, err := svc.Update(person, &gennotes.UpdatePayload{ID: added.ID, Title: ptr("確認済み")})
	if err != nil {
		t.Fatal(err)
	}
	if edited.CreatedBy == nil {
		t.Error("created_by was lost by an edit of people")
	}
	ls, err := svc.List(person, &gennotes.ListPayload{Notebook: ptr(store.Inbox), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range ls.Notes {
		if got, want := n.CreatedBy != nil, n.ID == added.ID; got != want {
			t.Errorf("summary %s has created_by %v, want %v", n.ID, got, want)
		}
	}

	// Notes created by AI in a collab notebook are marked too.
	shared, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "共有"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{ID: ptr("chosen"), Notebook: shared.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if n.CreatedBy == nil {
		t.Error("a note created by AI in a collab notebook is not marked")
	}

	// The name of a drop notebook, which AI clients see, blocks theirs, and
	// they may make it private; only people can set the drop kind, which the
	// trash cannot have.
	if _, err := svc.CreateAiNotebook(ai, &gennotes.CreateAiNotebookPayload{Name: "Inbox"}); errorName(err) != "already_exists" {
		t.Errorf("CreateAiNotebook with the name of a drop notebook = %v, want already_exists", err)
	}
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: store.Trash, Kind: ptr("drop")}); errorName(err) != "bad_request" {
		t.Errorf("SaveNotebook(trash, drop) = %v, want bad_request", err)
	}
	// People may make the inbox default, which is then written in its file.
	nb, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: store.Inbox, Kind: ptr("default")})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, store.Inbox, store.NotebookFile)); nb.Kind != "default" || !strings.Contains(string(b), "kind: default") {
		t.Errorf("inbox made default: kind %s, file %q", nb.Kind, b)
	}
	if ls, err := svc.List(ai, &gennotes.ListPayload{Notebook: ptr(store.Inbox), Limit: 50}); err != nil || len(ls.Notes) != 2 || ls.Total != 2 {
		t.Errorf("List of the default inbox for AI = %+v, %v; want 2 notes", ls, err)
	}
	if err := svc.MakeNotebookPrivate(ai, &gennotes.MakeNotebookPrivatePayload{ID: store.Inbox}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAiNote(ai, &gennotes.CreateAiNotePayload{Notebook: store.Inbox, Title: "x"}); errorName(err) != "forbidden" {
		t.Errorf("CreateAiNote in a private inbox = %v, want forbidden", err)
	}
}
