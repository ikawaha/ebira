package service

import (
	"context"
	"strings"
	"testing"

	genboards "github.com/ikawaha/ebira/gen/boards"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestBoardAppendsToNote(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	person := AsPerson(context.Background())
	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("plan"), Title: "Plan", Body: ptr("Use a trie for the index.\n")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if deref(b.Source.NoteState) != "ok" || deref(b.Source.NoteTitle) != "Plan" {
		t.Errorf("note of the board = %v, %v", deref(b.Source.NoteState), deref(b.Source.NoteTitle))
	}
	q, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why not a map?")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "index"}, Text: ptr("only for me")}); err != nil {
		t.Fatal(err)
	}

	// The note changes while the board is open: closing keeps the change.
	cur, _ := svc.Get(person, &gennotes.GetPayload{ID: n.ID})
	if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: n.ID, Body: ptr(cur.Body + "Mind memory.\n"), IfMatch: &cur.Version}); err != nil {
		t.Fatal(err)
	}
	if b, _ := boardsOf(svc).Get(person, &genboards.GetPayload{ID: b.ID}); deref(b.Source.NoteState) != "changed" {
		t.Errorf("note changed: state %v", deref(b.Source.NoteState))
	}

	nb, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "boards", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{
		ID: b.ID, Text: "- compare with a map", Stickies: []string{q.ID}, Notebook: &nb.ID, Lang: "en", AppendToNote: ptr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !done.Result.Appended || done.Result.NoteID == nil {
		t.Fatalf("result = %+v", done.Result)
	}

	// The note keeps its body, with the change made meanwhile, and gets the
	// next points, the sticky note given back and a link to the minutes.
	got, _ := svc.Get(person, &gennotes.GetPayload{ID: n.ID})
	for _, want := range []string{
		"Use a trie for the index.\nMind memory.\n\n## Discussed on a whiteboard (",
		"### Next points\n\n- compare with a map\n",
		"**Memo** why not a map?",
		"Minutes: [Plan](/#note/" + *done.Result.NoteID + ")",
	} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("note has no %q:\n%s", want, got.Body)
		}
	}
	if strings.Contains(got.Body, "only for me") {
		t.Errorf("note has a sticky note not given back:\n%s", got.Body)
	}
	// The minutes link to the note.
	minutes, _ := svc.Get(person, &gennotes.GetPayload{ID: *done.Result.NoteID})
	if !strings.HasPrefix(minutes.Body, "From the note: [Plan](/#note/plan)\n") {
		t.Errorf("minutes:\n%s", minutes.Body)
	}
}

func TestBoardAppendRefused(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	person := AsPerson(context.Background())
	ai := WithBaseURL(context.Background(), "http://localhost:13579")

	// A board not opened from a note has no note to add to.
	opened, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "t", Format: "text", Text: ptr("x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: opened.ID, AppendToNote: ptr(true)}); errorName(err) != "bad_request" {
		t.Errorf("adding without a note = %v", err)
	}

	// Nor one whose note is in the trash.
	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("gone"), Title: "Gone", Body: ptr("x")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(person, &gennotes.DeletePayload{ID: n.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := boardsOf(svc).Get(person, &genboards.GetPayload{ID: b.ID}); deref(got.Source.NoteState) != "trash" {
		t.Errorf("note in the trash: state %v", deref(got.Source.NoteState))
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: b.ID, AppendToNote: ptr(true)}); errorName(err) != "bad_request" {
		t.Errorf("adding to a note in the trash = %v", err)
	}
	// Closing without adding still works.
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: b.ID}); err != nil {
		t.Errorf("closing = %v", err)
	}
}
