package service

import (
	"context"
	"strings"
	"testing"
	"time"

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
	if deref(b.Source.NoteState) != "ok" || deref(b.Source.NoteTitle) != "Plan" || deref(b.Source.NoteNotebook) != n.Notebook {
		t.Errorf("note of the board = %v, %v, %v", deref(b.Source.NoteState), deref(b.Source.NoteTitle), deref(b.Source.NoteNotebook))
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

	// The note keeps its body, with the change made meanwhile, and gets a
	// line linking to the minutes in its section of discussions; the points
	// and the sticky notes are in the minutes.
	got, _ := svc.Get(person, &gennotes.GetPayload{ID: n.ID})
	day := time.Now().Format(time.DateOnly)
	if want := "Use a trie for the index.\nMind memory.\n\n## Appendix: discussions\n\n- " + day + " [Plan](/#note/" + *done.Result.NoteID + ")\n"; got.Body != want {
		t.Errorf("note:\n%s\nwant:\n%s", got.Body, want)
	}
	// The minutes link to the note, and have the points.
	minutes, _ := svc.Get(person, &gennotes.GetPayload{ID: *done.Result.NoteID})
	if !strings.HasPrefix(minutes.Body, "From the note: [Plan](/#note/plan)\n") || !strings.Contains(minutes.Body, "- compare with a map") {
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

	// Nor without the minutes to link to.
	kept, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("kept"), Title: "Kept", Body: ptr("x")})
	if err != nil {
		t.Fatal(err)
	}
	kb, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: kept.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: kb.ID, Text: "- a point", AppendToNote: ptr(true)}); errorName(err) != "bad_request" {
		t.Errorf("adding without the minutes = %v", err)
	}

	// With no sticky notes and no points, nothing is kept nor added, even
	// when asked.
	cur, _ := svc.Get(person, &gennotes.GetPayload{ID: kept.ID})
	nb, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "minutes", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: kb.ID, Notebook: &nb.ID, AppendToNote: ptr(true)})
	if err != nil || done.Result.NoteID != nil || done.Result.Appended {
		t.Errorf("closing an empty board = %+v, %v", done, err)
	}
	if after, _ := svc.Get(person, &gennotes.GetPayload{ID: kept.ID}); after.Body != cur.Body {
		t.Errorf("the note changed:\n%s", after.Body)
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

func TestAIReadsBoardsOnNotes(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	person := AsPerson(context.Background())
	ai := WithBaseURL(context.Background(), "http://localhost:13579")

	work, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "work", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("plan"), Notebook: &work.ID, Title: "Plan", Body: ptr("Use a trie.\n")})
	if err != nil {
		t.Fatal(err)
	}
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &diary.ID, Title: "Secret", Body: ptr("Not for AI.\n")})
	if err != nil {
		t.Fatal(err)
	}
	onShared, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: shared.ID})
	if err != nil {
		t.Fatal(err)
	}
	onSecret, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: secret.ID})
	if err != nil {
		t.Fatal(err)
	}
	byAI, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "Diff", Format: "text", Text: ptr("x")})
	if err != nil {
		t.Fatal(err)
	}

	// AI clients list the boards they opened and those on notes they see,
	// not one on a private note, which holds a copy of it.
	list, err := svc.ListAiBoards(ai)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]*gennotes.AIBoardSummary{}
	for _, b := range list {
		seen[b.ID] = b
	}
	if b := seen[onShared.ID]; b == nil || b.OpenedBy != "note" || deref(b.FromNote) != shared.ID {
		t.Errorf("board on a shared note = %+v", b)
	}
	if b := seen[byAI.ID]; b == nil || b.OpenedBy != "ai" || b.FromNote != nil {
		t.Errorf("board an AI opened = %+v", b)
	}
	if seen[onSecret.ID] != nil || len(list) != 2 {
		t.Errorf("AI lists %d boards, with the private one: %v", len(list), seen[onSecret.ID] != nil)
	}
	if _, err := svc.GetAiBoard(ai, &gennotes.GetAiBoardPayload{ID: onSecret.ID}); errorName(err) != "not_found" {
		t.Errorf("board on a private note for AI = %v", err)
	}

	// Closed, the board on the shared note gives AI clients what was given
	// back, with the note it was opened from.
	q, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: onShared.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why not a map?")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: onShared.ID, Text: "- compare", Stickies: []string{q.ID}, Notebook: &work.ID, AppendToNote: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetAiBoard(ai, &gennotes.GetAiBoardPayload{ID: onShared.ID})
	if err != nil || got.Status != "done" || deref(got.FromNote) != shared.ID || deref(got.Result) != "- compare" || len(got.Stickies) != 1 {
		t.Errorf("closed board on a note for AI = %+v, %v", got, err)
	}

	// Made private later, the note hides its board too.
	if _, err := svc.SaveNotebook(person, &gennotes.SaveNotebookPayload{ID: shared.Notebook, Kind: ptr("private")}); err != nil {
		t.Skipf("making the notebook private: %v", err)
	}
	if _, err := svc.GetAiBoard(ai, &gennotes.GetAiBoardPayload{ID: onShared.ID}); errorName(err) != "not_found" {
		t.Errorf("board on a note made private = %v", err)
	}
}

func TestOneBoardPerNote(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	person := AsPerson(context.Background())
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	work, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "work", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("plan"), Notebook: &work.ID, Title: "Plan", Body: ptr("Use a trie.\n")})
	if err != nil {
		t.Fatal(err)
	}
	// A board an AI opened on the note is of its conversation: not reused.
	byAI, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "Plan", Format: "markdown", NoteID: &n.ID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == byAI.ID {
		t.Error("opening the note reused the board of an AI")
	}
	if !first.Unchanged {
		t.Error("a new board is not unchanged")
	}
	if got, _ := boardsOf(svc).Get(ai, &genboards.GetPayload{ID: byAI.ID}); got != nil && got.Unchanged {
		t.Error("the board of an AI is unchanged")
	}
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: first.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why?")}); err != nil {
		t.Fatal(err)
	}

	// Opening the note again, even changed, shows the board open on it.
	cur, _ := svc.Get(person, &gennotes.GetPayload{ID: n.ID})
	if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: n.ID, Body: ptr("Use a map.\n"), IfMatch: &cur.Version}); err != nil {
		t.Fatal(err)
	}
	again, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || len(again.Stickies) != 1 || deref(again.Source.NoteState) != "changed" {
		t.Errorf("opened again = %s (%d sticky notes, %s), want %s", again.ID, len(again.Stickies), deref(again.Source.NoteState), first.ID)
	}
	// The sticky note was there when it was opened again: nothing changed
	// since, until another one comes.
	if !again.Unchanged {
		t.Error("the board opened again is not unchanged")
	}
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: first.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "map"}, Text: ptr("why not?")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := boardsOf(svc).Get(person, &genboards.GetPayload{ID: first.ID}); got.Unchanged {
		t.Error("the board is unchanged after a sticky note was added")
	}

	// Once it is closed, a new one opens, on the note as it is now.
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: first.ID}); err != nil {
		t.Fatal(err)
	}
	next, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID || next.Text != "Use a map.\n" {
		t.Errorf("opened after closing = %s %q", next.ID, next.Text)
	}
}
