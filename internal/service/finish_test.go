package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	genboards "github.com/ikawaha/ebira/gen/boards"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/board"
)

// TestFinishBoardOnce checks that closing a board twice at once makes its
// minutes, and the link to them in the note, once.
func TestFinishBoardOnce(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	person := AsPerson(WithBaseURL(context.Background(), "http://localhost:13579"))
	nb, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "work", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.Create(person, &gennotes.CreatePayload{Notebook: &nb.ID, Title: "Plan", Body: ptr("Use a trie.\n")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why?")}); err != nil {
		t.Fatal(err)
	}

	const tries = 4
	var wg sync.WaitGroup
	errs := make([]error, tries)
	for i := range tries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: b.ID, Text: "- a point", Notebook: &nb.ID, Lang: "en", AppendToNote: ptr(true)})
		}()
	}
	wg.Wait()
	done := 0
	for _, err := range errs {
		switch {
		case err == nil:
			done++
		case errorName(err) != "bad_request":
			t.Errorf("finishing at once = %v", err)
		}
	}
	if done != 1 {
		t.Errorf("finished %d times", done)
	}
	list, err := svc.List(person, &gennotes.ListPayload{Notebook: &nb.ID, Tag: ptr(board.Tag), Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Notes) != 1 {
		t.Errorf("%d minutes made", len(list.Notes))
	}
	got, err := svc.Get(person, &gennotes.GetPayload{ID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(got.Body, "](/#note/"); c != 1 {
		t.Errorf("%d links to the minutes in the note:\n%s", c, got.Body)
	}
}

// TestFinishingBoardTakesNoChange checks that a board being closed takes
// no other change, such as a sticky note starting to wait for an answer.
func TestFinishingBoardTakesNoChange(t *testing.T) {
	svc, _ := newBoardService(t, newFakeAsker().ask)
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	person := AsPerson(ai)
	opened, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "Plan", Format: "markdown", Text: ptr("Use a trie.")})
	if err != nil {
		t.Fatal(err)
	}
	svc.boards.setClosing(opened.ID, true)
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: "assistant", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why?")}); errorName(err) != "bad_request" {
		t.Errorf("asking on a board being closed = %v", err)
	}
	svc.boards.setClosing(opened.ID, false)
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why?")}); err != nil {
		t.Errorf("a sticky note once closing failed = %v", err)
	}
}

// TestTrackKeepsTheNewerRun checks that a run that ends does not forget a
// newer run under the same key, which deleting the sticky note must stop.
func TestTrackKeepsTheNewerRun(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	bs := svc.boards
	key := runKey("b1", "s1")
	_, doneOld := bs.track(key)
	ctxNew, doneNew := bs.track(key)
	defer doneNew()
	doneOld()
	bs.cancel("b1", []board.Sticky{{ID: "s1"}})
	if ctxNew.Err() == nil {
		t.Error("the newer run was not stopped")
	}
}
