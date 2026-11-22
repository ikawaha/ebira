package service

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	genboards "github.com/ikawaha/ebira/gen/boards"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/board"
	"github.com/ikawaha/ebira/internal/store"
)

// fakeAsker answers questions on whiteboards with what the test tells it.
type fakeAsker struct {
	mu        sync.Mutex
	questions []string
	answers   chan result
}

type result struct {
	text string
	err  error
}

func newFakeAsker() *fakeAsker { return &fakeAsker{answers: make(chan result, 8)} }

func (f *fakeAsker) ask(ctx context.Context, q string) (string, error) {
	f.mu.Lock()
	f.questions = append(f.questions, q)
	f.mu.Unlock()
	select {
	case r := <-f.answers:
		return r.text, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (f *fakeAsker) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.questions) == 0 {
		return ""
	}
	return f.questions[len(f.questions)-1]
}

// boardsOf returns the boards service of svc, as the REST API serves it.
func boardsOf(svc *Notes) Boards { return Boards{REST{Notes: svc}} }

func newBoardService(t *testing.T, ask Asker) (*Notes, *board.Store) {
	t.Helper()
	svc, dir := newTestService(t)
	st, err := board.New(filepath.Join(filepath.Dir(dir), "boards"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetBoards(st, ask); err != nil {
		t.Fatal(err)
	}
	return svc, st
}

// waitSticky waits until sticky note sid of board id is no longer waiting.
func waitSticky(t *testing.T, svc *Notes, id, sid string) *genboards.BoardSticky {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := boardsOf(svc).Get(AsPerson(context.Background()), &genboards.GetPayload{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range b.Stickies {
			if s.ID == sid && s.State != board.Waiting {
				return s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("sticky note %s still waits", sid)
	return nil
}

func TestOpenAiBoard(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	person := AsPerson(ai)

	opened, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{
		Title: "PR", Format: "diff", Text: ptr("+added\n"),
		SessionID: ptr("71f0d3f3-75c3-460e-b7ed-ce852dd93fb3"), Cwd: ptr("/work"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened.URL != "http://localhost:13579/#board/"+opened.ID {
		t.Errorf("url = %q", opened.URL)
	}
	b, err := boardsOf(svc).Get(person, &genboards.GetPayload{ID: opened.ID})
	if err != nil {
		t.Fatal(err)
	}
	if b.Text != "+added\n" || b.Source.Format != "diff" || b.Source.From != "text" || !b.FromSession {
		t.Errorf("board = %+v", b)
	}

	// From a note visible to AI clients, but not from a private one.
	// The inbox is of the drop kind, hidden from AI clients.
	shared, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "shared", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("pub"), Notebook: &shared.ID, Title: "Public", Body: ptr("# Body")})
	if err != nil {
		t.Fatal(err)
	}
	opened, err = svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "From a note", Format: "markdown", NoteID: &pub.ID})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := boardsOf(svc).Get(person, &genboards.GetPayload{ID: opened.ID}); b.Text != "# Body" || deref(b.Source.NoteID) != "pub" {
		t.Errorf("board from a note = %+v", b)
	}
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &diary.ID, Title: "s", Body: ptr("secret")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "x", Format: "markdown", NoteID: ptr("secret")}); errorName(err) != "not_found" {
		t.Errorf("board from a private note = %v, want not_found", err)
	}

	for name, p := range map[string]*gennotes.OpenAiBoardPayload{
		"neither": {Title: "x", Format: "markdown"},
		"both":    {Title: "x", Format: "markdown", Text: ptr("x"), NoteID: &pub.ID},
		"empty":   {Title: "x", Format: "markdown", Text: ptr("  ")},
		"session": {Title: "x", Format: "markdown", Text: ptr("x"), Cwd: ptr("relative")},
	} {
		if _, err := svc.OpenAiBoard(ai, p); errorName(err) != "bad_request" {
			t.Errorf("%s: err = %v, want bad_request", name, err)
		}
	}
}

func TestStartAiBoard(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	up, err := svc.StartAiBoard(ai, &gennotes.StartAiBoardPayload{Title: "diff", Format: "diff"})
	if err != nil {
		t.Fatal(err)
	}
	prefix := "http://localhost:13579" + BoardUploadsPath
	if !strings.HasPrefix(up.URL, prefix) || up.MaxSize != board.MaxSource {
		t.Fatalf("upload = %+v", up)
	}
	token := strings.TrimPrefix(up.URL, prefix)

	// A token for a file of a note is not one for a board, and back.
	if _, err := svc.UploadAiFile(ai, &gennotes.UploadAiFilePayload{Token: token}, io.NopCloser(strings.NewReader("x"))); errorName(err) != "not_found" {
		t.Errorf("board token used for a file = %v", err)
	}
	up, _ = svc.StartAiBoard(ai, &gennotes.StartAiBoardPayload{Title: "diff", Format: "diff"})
	token = strings.TrimPrefix(up.URL, prefix)

	opened, err := svc.UploadAiBoard(ai, &gennotes.UploadAiBoardPayload{Token: token}, io.NopCloser(strings.NewReader("-gone\n+came\n")))
	if err != nil {
		t.Fatal(err)
	}
	b, err := boardsOf(svc).Get(AsPerson(ai), &genboards.GetPayload{ID: opened.ID})
	if err != nil {
		t.Fatal(err)
	}
	if b.Text != "-gone\n+came\n" || b.Source.From != "upload" || b.Title != "diff" {
		t.Errorf("board = %+v", b)
	}
	// Once only.
	if _, err := svc.UploadAiBoard(ai, &gennotes.UploadAiBoardPayload{Token: token}, io.NopCloser(strings.NewReader("x"))); errorName(err) != "not_found" {
		t.Errorf("second upload = %v", err)
	}

	up, _ = svc.StartAiBoard(ai, &gennotes.StartAiBoardPayload{Title: "big", Format: "text"})
	big := strings.Repeat("x", board.MaxSource+1)
	if _, err := svc.UploadAiBoard(ai, &gennotes.UploadAiBoardPayload{Token: strings.TrimPrefix(up.URL, prefix)}, io.NopCloser(strings.NewReader(big))); errorName(err) != "too_large" {
		t.Errorf("large upload = %v", err)
	}
	if _, err := svc.StartAiBoard(ai, &gennotes.StartAiBoardPayload{Title: " ", Format: "text"}); errorName(err) != "bad_request" {
		t.Errorf("empty title = %v", err)
	}
}

func TestBoardStickies(t *testing.T) {
	asker := newFakeAsker()
	svc, _ := newBoardService(t, asker.ask)
	person := AsPerson(context.Background())
	events, cancel := svc.events.subscribe()
	defer cancel()

	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("doc"), Title: "Doc", Body: ptr("The bigram index splits text into pairs.")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Doc" || b.FromSession {
		t.Errorf("board = %+v", b)
	}

	memo, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "pairs"}, Text: ptr("check later")})
	if err != nil {
		t.Fatal(err)
	}
	if memo.State != "idle" || len(memo.Messages) != 1 {
		t.Errorf("memo = %+v", memo)
	}

	q, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{
		ID: b.ID, Kind: "assistant", Text: ptr("What is a bigram?"),
		Anchor: &genboards.BoardAnchor{Exact: "bigram", Prefix: ptr("The "), Suffix: ptr(" index")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.State != "waiting" {
		t.Fatalf("question = %+v, want waiting", q)
	}
	if _, err := boardsOf(svc).AskSticky(person, &genboards.AskStickyPayload{ID: b.ID, Sticky: q.ID, Text: ptr("more")}); errorName(err) != "bad_request" {
		t.Errorf("asking a waiting sticky note = %v", err)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: b.ID}); errorName(err) != "bad_request" {
		t.Errorf("finishing with a waiting sticky note = %v", err)
	}
	asker.answers <- result{text: "Two characters."}
	got := waitSticky(t, svc, b.ID, q.ID)
	if got.State != "idle" || len(got.Messages) != 2 || got.Messages[1].Role != "assistant" || got.Messages[1].Text != "Two characters." {
		t.Errorf("answered = %+v", got)
	}
	if qs := asker.last(); !strings.Contains(qs, "<<<bigram>>>") || !strings.Contains(qs, "What is a bigram?") {
		t.Errorf("question asked:\n%s", qs)
	}
	sawBoard := false
	for !sawBoard {
		select {
		case ev := <-events:
			sawBoard = ev.Type == eventBoard && deref(ev.ID) == b.ID
		case <-time.After(time.Second):
			t.Fatal("no board event")
		}
	}

	// A failed answer, then asking again without text asks the same question.
	if _, err := boardsOf(svc).AskSticky(person, &genboards.AskStickyPayload{ID: b.ID, Sticky: q.ID, Text: ptr("Why pairs?")}); err != nil {
		t.Fatal(err)
	}
	asker.answers <- result{err: errors.New("model down")}
	if got := waitSticky(t, svc, b.ID, q.ID); got.State != "error" || deref(got.Error) != "model down" {
		t.Errorf("failed = %+v", got)
	}
	if _, err := boardsOf(svc).AskSticky(person, &genboards.AskStickyPayload{ID: b.ID, Sticky: q.ID}); err != nil {
		t.Fatal(err)
	}
	asker.answers <- result{text: "To match any part."}
	got = waitSticky(t, svc, b.ID, q.ID)
	if len(got.Messages) != 4 || got.Messages[2].Text != "Why pairs?" || got.Messages[3].Text != "To match any part." {
		t.Errorf("after retry: %+v", got.Messages)
	}

	// A sticky note on the answer, then deleting the parent deletes it.
	child, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Parent: &q.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "any part"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Parent: ptr("s00000000"), Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "x"}}); errorName(err) != "not_found" {
		t.Errorf("sticky note on a missing parent = %v", err)
	}
	if _, err := boardsOf(svc).SaveSticky(person, &genboards.SaveStickyPayload{ID: b.ID, Sticky: q.ID, Text: ptr("x")}); errorName(err) != "bad_request" {
		t.Errorf("text of an answered sticky note = %v", err)
	}
	if m, err := boardsOf(svc).SaveSticky(person, &genboards.SaveStickyPayload{ID: b.ID, Sticky: memo.ID, Text: ptr("checked")}); err != nil || m.Messages[0].Text != "checked" {
		t.Errorf("memo saved = %+v, %v", m, err)
	}

	// The draft is from the sticky notes chosen, which exist.
	for _, ids := range [][]string{nil, {q.ID, "s00000000"}} {
		if _, err := boardsOf(svc).Draft(person, &genboards.DraftPayload{ID: b.ID, Stickies: ids}); errorName(err) != "bad_request" {
			t.Errorf("draft from %v = %v", ids, err)
		}
	}
	asker.answers <- result{text: "- bigrams are pairs"}
	draft, err := boardsOf(svc).Draft(person, &genboards.DraftPayload{ID: b.ID, Stickies: []string{q.ID}})
	if err != nil || draft.Text != "- bigrams are pairs" {
		t.Fatalf("draft = %+v, %v", draft, err)
	}
	if qs := asker.last(); !strings.Contains(qs, "To match any part.") || strings.Contains(qs, "checked") {
		t.Errorf("draft question:\n%s", qs)
	}

	if err := boardsOf(svc).DeleteSticky(person, &genboards.DeleteStickyPayload{ID: b.ID, Sticky: child.ID}); err != nil {
		t.Fatal(err)
	}
	if err := boardsOf(svc).DeleteSticky(person, &genboards.DeleteStickyPayload{ID: b.ID, Sticky: child.ID}); errorName(err) != "not_found" {
		t.Errorf("deleting again = %v", err)
	}
}

func TestFinishBoard(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	person := AsPerson(ai)
	opened, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "Plan", Format: "markdown", Text: ptr("Use a trie.")})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := svc.GetAiBoard(ai, &gennotes.GetAiBoardPayload{ID: opened.ID}); err != nil || res.Status != "open" || res.Result != nil {
		t.Errorf("open board for AI = %+v, %v", res, err)
	}
	given, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why not a map?")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "Use"}, Text: ptr("only for me")}); err != nil {
		t.Fatal(err)
	}
	// Without the assistant.
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: "assistant", Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("?")}); errorName(err) != "unavailable" {
		t.Errorf("asking without the assistant = %v", err)
	}

	nb, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "boards", Kind: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: opened.ID, Text: "x", Stickies: []string{given.ID, "s00000000"}}); errorName(err) != "bad_request" {
		t.Errorf("finishing with a missing sticky note = %v", err)
	}
	b, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: opened.ID, Text: "Use a map instead.", Stickies: []string{given.ID, given.ID}, Notebook: &nb.ID})
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != "done" || b.Result == nil || b.Result.NoteID == nil || len(b.Result.Stickies) != 1 || b.Result.Stickies[0] != given.ID {
		t.Fatalf("finished = %+v", b)
	}
	n, err := svc.Get(person, &gennotes.GetPayload{ID: *b.Result.NoteID})
	if err != nil {
		t.Fatal(err)
	}
	if n.Notebook != nb.ID || n.Title != "Plan" || len(n.Tags) != 1 || n.Tags[0] != board.Tag ||
		!strings.HasPrefix(n.Body, "Use a map instead.") || !strings.Contains(n.Body, "why not a map?") || !strings.Contains(n.Body, "Use a trie.") {
		t.Errorf("note = %+v", n)
	}

	res, err := svc.GetAiBoard(ai, &gennotes.GetAiBoardPayload{ID: opened.ID})
	if err != nil || res.Status != "done" || deref(res.Result) != "Use a map instead." || deref(res.NoteID) != n.ID {
		t.Errorf("finished board for AI = %+v, %v", res, err)
	}
	// Only the sticky note chosen is given back, once.
	if len(res.Stickies) != 1 {
		t.Fatalf("sticky notes given back = %d", len(res.Stickies))
	}
	if st := res.Stickies[0]; st.ID != given.ID || st.Passage != "trie" || st.Parent != nil ||
		len(st.Messages) != 1 || st.Messages[0].Role != "user" || st.Messages[0].Text != "why not a map?" {
		t.Errorf("sticky note given back = %+v", st)
	}
	if _, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: "memo", Anchor: &genboards.BoardAnchor{Exact: "trie"}}); errorName(err) != "bad_request" {
		t.Errorf("sticky note on a finished board = %v", err)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: opened.ID}); errorName(err) != "bad_request" {
		t.Errorf("finishing again = %v", err)
	}

	list, err := boardsOf(svc).List(person, &genboards.ListPayload{})
	if err != nil || len(list) != 1 || list[0].Stickies != 2 || list[0].Status != "done" {
		t.Errorf("list = %+v, %v", list, err)
	}
	if err := boardsOf(svc).Delete(person, &genboards.DeletePayload{ID: opened.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetAiBoard(ai, &gennotes.GetAiBoardPayload{ID: opened.ID}); errorName(err) != "not_found" {
		t.Errorf("deleted board for AI = %v", err)
	}
	// The note stays.
	if _, err := svc.Get(person, &gennotes.GetPayload{ID: n.ID}); err != nil {
		t.Errorf("note of a deleted board: %v", err)
	}
}

func TestFinishBoardWithoutNote(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	person := AsPerson(context.Background())
	opened, err := svc.OpenAiBoard(context.Background(), &gennotes.OpenAiBoardPayload{Title: "x", Format: "text", Text: ptr("x")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: opened.ID, Text: ""})
	if err != nil || b.Result == nil || b.Result.NoteID != nil {
		t.Errorf("finished = %+v, %v", b, err)
	}
	// No note was made.
	if r, _ := svc.Search(person, &gennotes.SearchPayload{Q: "tag:" + board.Tag}); r != nil && len(r.Notes) != 0 {
		t.Errorf("notes = %+v", r.Notes)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: opened.ID, Notebook: ptr(store.Trash)}); errorName(err) != "bad_request" {
		t.Errorf("finished twice = %v", err)
	}
}

// TestBoardInterrupted checks that a sticky note left waiting by a server
// that stopped is marked as failed when the next server starts.
func TestBoardInterrupted(t *testing.T) {
	asker := newFakeAsker()
	svc, st := newBoardService(t, asker.ask)
	person := AsPerson(context.Background())
	opened, err := svc.OpenAiBoard(context.Background(), &gennotes.OpenAiBoardPayload{Title: "x", Format: "text", Text: ptr("abc")})
	if err != nil {
		t.Fatal(err)
	}
	q, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: "assistant", Anchor: &genboards.BoardAnchor{Exact: "b"}, Text: ptr("?")})
	if err != nil {
		t.Fatal(err)
	}
	svc.Close() // the answer is never written
	next, _ := newTestService(t)
	if err := next.SetBoards(st, nil); err != nil {
		t.Fatal(err)
	}
	b, err := boardsOf(next).Get(person, &genboards.GetPayload{ID: opened.ID})
	if err != nil {
		t.Fatal(err)
	}
	if s := b.Stickies[0]; s.ID != q.ID || s.State != "error" || s.Error == nil {
		t.Errorf("sticky note after a restart = %+v", s)
	}
}
