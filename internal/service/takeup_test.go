package service

import (
	"context"
	"strings"
	"testing"
	"time"

	genboards "github.com/ikawaha/ebira/gen/boards"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/board"
)

// waitClaude waits until Claude replied to the next points of board id.
func waitClaude(t *testing.T, svc *Notes, id string) *genboards.BoardClaudeReply {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := boardsOf(svc).Get(AsPerson(context.Background()), &genboards.GetPayload{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if c := b.Result.Claude; c != nil && c.State != board.Waiting {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Claude still works on board %s", id)
	return nil
}

func TestTakeUpOnClose(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	f := newFakeForker()
	svc.SetForker(f)
	person := signedInPerson()
	collab, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "with AI", Kind: "collab"})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "mine", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	note := func(nb, id string) *gennotes.Note {
		n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr(id), Notebook: &nb, Title: "Plan", Body: ptr("# Plan\n\nUse a trie.\n")})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	open := func(n *gennotes.Note) *genboards.Board {
		b, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("a trie keeps prefixes")}); err != nil {
			t.Fatal(err)
		}
		return b
	}
	finish := func(id, text string) (*genboards.Board, error) {
		return boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: id, Text: text, AskClaude: ptr(true)})
	}

	// Claude changes only notes it may change, and needs points.
	other := open(note(mine.ID, "mine"))
	if _, err := finish(other.ID, "- add it"); errorName(err) != "bad_request" {
		t.Errorf("asking Claude on a note of the default kind = %v", err)
	}
	n := note(collab.ID, "plan")
	b := open(n)
	if _, err := finish(b.ID, " "); errorName(err) != "bad_request" {
		t.Errorf("asking Claude without points = %v", err)
	}
	// A board an AI opened on the note goes back to its session instead.
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	byAI, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "Plan", Format: "markdown", NoteID: &n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finish(byAI.ID, "- add it"); errorName(err) != "bad_request" {
		t.Errorf("asking Claude on a board of an AI = %v", err)
	}

	f.mu.Lock()
	f.reply = func(a ForkAsk) string {
		if !strings.Contains(a.Question, "take up the next points") {
			return "read"
		}
		return "Added the summary.\n\n```" + board.NoteFence + "\n" +
			`{"edits": [{"old": "Use a trie.", "new": "Use a trie, as it keeps prefixes."}], "append": "## Summary\n\nA trie."}` + "\n```\n"
	}
	f.mu.Unlock()
	// With the minutes kept, linked from the note: what Claude adds goes
	// before that section.
	done, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: b.ID, Text: "- add what we discussed to the note", AskClaude: ptr(true), Notebook: &collab.ID, AppendToNote: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if done.Result.Claude == nil || done.Result.Claude.State != board.Waiting {
		t.Fatalf("closed = %+v", done.Result)
	}
	c := waitClaude(t, svc, b.ID)
	if c.State != board.Idle || !c.Edited || deref(c.Text) != "Added the summary." || c.Error != nil || c.Reason != nil {
		t.Errorf("reply = %+v", c)
	}
	if a := f.lastAsk(); !strings.Contains(a.Question, "add what we discussed") || !strings.Contains(a.Question, "a trie keeps prefixes") || !strings.Contains(a.Question, n.ID) {
		t.Errorf("question = %s", a.Question)
	}
	got, _ := svc.Get(person, &gennotes.GetPayload{ID: n.ID})
	if !strings.HasPrefix(got.Body, "# Plan\n\nUse a trie, as it keeps prefixes.\n\n## Summary\n\nA trie.\n\n## Appendix: discussions\n\n- ") {
		t.Errorf("note = %q", got.Body)
	}

	// A note changed after the board was closed is left as it is: the
	// changes are shown.
	n2 := note(collab.ID, "plan2")
	b2 := open(n2)
	f.mu.Lock()
	f.reply = func(a ForkAsk) string {
		if !strings.Contains(a.Question, "take up the next points") {
			return "read"
		}
		cur, _ := svc.Get(person, &gennotes.GetPayload{ID: n2.ID})
		if _, err := svc.Update(person, &gennotes.UpdatePayload{ID: n2.ID, Body: ptr("changed\n"), IfMatch: &cur.Version}); err != nil {
			t.Error(err)
		}
		return "Done.\n\n```" + board.NoteFence + "\n{\"append\": \"more\"}\n```"
	}
	f.mu.Unlock()
	if _, err := finish(b2.ID, "- add more"); err != nil {
		t.Fatal(err)
	}
	c = waitClaude(t, svc, b2.ID)
	if c.Edited || c.Error == nil || deref(c.Reason) != board.ReasonChanged || !strings.Contains(deref(c.Text), "more") {
		t.Errorf("reply on a changed note = %+v", c)
	}
	if got, _ := svc.Get(person, &gennotes.GetPayload{ID: n2.ID}); got.Body != "changed\n" {
		t.Errorf("changed note = %q", got.Body)
	}

	// Changes that do not fit the note, or are not valid, are not made.
	for _, c := range []struct{ block, reason string }{
		{`{"edits": [{"old": "not in the note", "new": "x"}]}`, board.ReasonNotMade},
		{"not json", board.ReasonInvalid},
	} {
		n := note(collab.ID, "plan-"+c.reason)
		b := open(n)
		f.mu.Lock()
		f.reply = func(a ForkAsk) string {
			if !strings.Contains(a.Question, "take up the next points") {
				return "read"
			}
			return "Done.\n\n```" + board.NoteFence + "\n" + c.block + "\n```"
		}
		f.mu.Unlock()
		if _, err := finish(b.ID, "- change it"); err != nil {
			t.Fatal(err)
		}
		r := waitClaude(t, svc, b.ID)
		if r.State != board.Idle || r.Edited || deref(r.Reason) != c.reason || r.Error == nil {
			t.Errorf("reply with %s = %+v", c.block, r)
		}
		if got, _ := svc.Get(person, &gennotes.GetPayload{ID: n.ID}); got.Body != n.Body {
			t.Errorf("note with %s = %q", c.block, got.Body)
		}
	}
}

func TestSplitReply(t *testing.T) {
	text, c, err := board.SplitReply("Hi.\n\n```ebira-note\n{\"append\": \"x\"}\n```\n")
	if err != nil || text != "Hi." || c == nil || c.Append != "x" {
		t.Errorf("split = %q %+v %v", text, c, err)
	}
	if text, c, err := board.SplitReply("Only words."); err != nil || text != "Only words." || c != nil {
		t.Errorf("no block = %q %+v %v", text, c, err)
	}
	if _, c, err := board.SplitReply("x\n```ebira-note\nnot json\n```"); err == nil || c != nil {
		t.Errorf("bad block = %+v %v", c, err)
	}
}

func TestAddBeforeDiscussions(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{"Text.\n", "Text.\n\nMore.\n"},
		{"", "More.\n"},
		{"Text.\n\n## 補足：議論\n\n- a\n", "Text.\n\nMore.\n\n## 補足：議論\n\n- a\n"},
		{"Text.\n\n## Appendix: discussions\n\n- a\n\n## Mine\n\nx\n", "Text.\n\n## Appendix: discussions\n\n- a\n\n## Mine\n\nx\n\nMore.\n"},
		// A heading in a code block is text: the section still runs to the end.
		{"Text.\n\n## 補足：議論\n\n- a\n\n```\n## not a heading\n```\n", "Text.\n\nMore.\n\n## 補足：議論\n\n- a\n\n```\n## not a heading\n```\n"},
		{"```md\n## 補足：議論\n```\n", "```md\n## 補足：議論\n```\n\nMore.\n"},
	} {
		if got := board.AddBeforeDiscussions(c.body, "More.\n"); got != c.want {
			t.Errorf("AddBeforeDiscussions(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

func TestProposed(t *testing.T) {
	c := &board.NoteChange{Append: "Run:\n\n```sh\nebira serve\n```"}
	c.Edits = append(c.Edits, struct {
		Old string `json:"old"`
		New string `json:"new"`
	}{"a", "b"})
	got := proposed(c)
	want := "````diff\n- a\n+ b\n````\n\n````markdown\nRun:\n\n```sh\nebira serve\n```\n````"
	if got != want {
		t.Errorf("proposed = %q, want %q", got, want)
	}
}
