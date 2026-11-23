package service

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	genboards "github.com/ikawaha/ebira/gen/boards"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/board"
)

const originSession = "11111111-2222-4333-8444-555555555555"

// fakeForker keeps sessions in a map: a fork makes the session it forks
// into. With block, Ask waits for its context to end.
type fakeForker struct {
	mu       sync.Mutex
	sessions map[string]bool
	asks     []ForkAsk
	removed  []string
	block    bool
	// reply, if set, gives the reply to a question instead of the usual.
	reply func(ForkAsk) string
}

func newFakeForker() *fakeForker {
	return &fakeForker{sessions: map[string]bool{originSession: true}}
}

func (f *fakeForker) Ask(ctx context.Context, a ForkAsk) (string, error) {
	f.mu.Lock()
	f.asks = append(f.asks, a)
	if a.Session != "" {
		f.sessions[a.Session] = true
	}
	block, reply := f.block, f.reply
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if reply != nil {
		return reply(a), nil
	}
	return "answered in " + a.Resume, nil
}

func (f *fakeForker) Has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[id]
}

func (f *fakeForker) Remove(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeForker) setBlock(b bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.block = b
}

func (f *fakeForker) lastAsk() ForkAsk {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asks[len(f.asks)-1]
}

// signedInPerson returns a request from a person signed in with a passkey.
func signedInPerson() context.Context {
	return context.WithValue(AsPerson(WithBaseURL(context.Background(), "http://localhost:13579")), verifiedKey{}, false)
}

func TestForkStickies(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	f := newFakeForker()
	svc.SetForker(f)
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	cwd := t.TempDir()
	opened, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "Plan", Format: "markdown", Text: ptr("Use a trie for the index."), SessionID: ptr(originSession), Cwd: ptr(cwd)})
	if err != nil {
		t.Fatal(err)
	}
	person := signedInPerson()
	add := func(ctx context.Context, parent *string, exact, text string) (*genboards.BoardSticky, error) {
		return boardsOf(svc).AddSticky(ctx, &genboards.AddStickyPayload{ID: opened.ID, Parent: parent, Kind: board.Fork, Anchor: &genboards.BoardAnchor{Exact: exact}, Text: ptr(text)})
	}

	// Without passkeys, no fork from another machine: anyone who reaches the
	// server could run Claude Code on its machine.
	none := AsPerson(context.Background())
	if b, err := boardsOf(svc).Get(none, &genboards.GetPayload{ID: opened.ID}); err != nil || b.Fork.Available || deref(b.Fork.Reason) != "auth_none" || deref(b.Fork.Cwd) != cwd {
		t.Errorf("fork from another machine without passkeys = %+v, %v", b.Fork, err)
	}
	if _, err := add(none, nil, "trie", "why?"); errorName(err) != "unavailable" {
		t.Errorf("fork from another machine without passkeys = %v", err)
	}
	// From this machine, it is as signed in.
	local := FromThisMachine(AsPerson(WithBaseURL(context.Background(), "http://localhost:13579")))
	if b, err := boardsOf(svc).Get(local, &genboards.GetPayload{ID: opened.ID}); err != nil || !b.Fork.Available {
		t.Errorf("fork from this machine without passkeys = %+v, %v", b.Fork, err)
	}
	if b, err := boardsOf(svc).Get(person, &genboards.GetPayload{ID: opened.ID}); err != nil || !b.Fork.Available {
		t.Fatalf("fork signed in = %+v, %v", b.Fork, err)
	}
	if _, err := add(person, nil, "trie", " "); errorName(err) != "bad_request" {
		t.Errorf("fork without a question = %v", err)
	}

	// The first question forks the session of the board.
	q, err := add(person, nil, "trie", "why a trie?")
	if err != nil || q.State != board.Waiting || q.Session == nil {
		t.Fatalf("fork sticky = %+v, %v", q, err)
	}
	if got := waitSticky(t, svc, opened.ID, q.ID); got.State != board.Idle || got.Messages[1].Text != "answered in "+originSession {
		t.Fatalf("answer = %+v", got)
	}
	if a := f.lastAsk(); a.Resume != originSession || a.Session != *q.Session || a.Cwd != cwd || !strings.Contains(a.Question, "fork of this session") || !strings.Contains(a.Question, "why a trie?") {
		t.Errorf("first ask = %+v", a)
	}

	// The next one goes on in the session of the sticky note.
	if _, err := boardsOf(svc).AskSticky(person, &genboards.AskStickyPayload{ID: opened.ID, Sticky: q.ID, Text: ptr("and memory?")}); err != nil {
		t.Fatal(err)
	}
	waitSticky(t, svc, opened.ID, q.ID)
	if a := f.lastAsk(); a.Resume != *q.Session || a.Session != "" || a.Question != "The person asks again on the same sticky note: and memory?" {
		t.Errorf("ask again = %+v", a)
	}

	// One on its answer forks its session.
	child, err := add(person, &q.ID, "answered", "answered how?")
	if err != nil {
		t.Fatal(err)
	}
	waitSticky(t, svc, opened.ID, child.ID)
	if a := f.lastAsk(); a.Resume != *q.Session || a.Session != *child.Session {
		t.Errorf("child ask = %+v", a)
	}

	// Deleting a sticky note deletes the sessions of it and those on it,
	// and never the session of the board.
	if err := boardsOf(svc).DeleteSticky(person, &genboards.DeleteStickyPayload{ID: opened.ID, Sticky: q.ID}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	removedOrigin := slices.Contains(f.removed, originSession)
	f.mu.Unlock()
	if f.Has(*q.Session) || f.Has(*child.Session) || !f.Has(originSession) || removedOrigin {
		t.Errorf("after deleting: the sessions of the sticky notes are left, or that of the board is gone")
	}

	// Deleting a sticky note waiting for a fork stops it.
	f.setBlock(true)
	w, err := add(person, nil, "index", "how big?")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !f.Has(*w.Session) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := boardsOf(svc).DeleteSticky(person, &genboards.DeleteStickyPayload{ID: opened.ID, Sticky: w.ID}); err != nil {
		t.Fatal(err)
	}
	for f.Has(*w.Session) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.Has(*w.Session) {
		t.Error("the session of a sticky note deleted while waiting is left")
	}
	f.setBlock(false)

	// Deleting the board deletes the sessions of its sticky notes.
	last, err := add(person, nil, "index", "how big?")
	if err != nil {
		t.Fatal(err)
	}
	waitSticky(t, svc, opened.ID, last.ID)
	if err := boardsOf(svc).Delete(person, &genboards.DeletePayload{ID: opened.ID}); err != nil {
		t.Fatal(err)
	}
	if f.Has(*last.Session) || !f.Has(originSession) {
		t.Error("after deleting the board: the session of a sticky note is left, or that of the board is gone")
	}
}

func TestForkUnavailable(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	person := signedInPerson()
	reason := func(id string) string {
		t.Helper()
		b, err := boardsOf(svc).Get(person, &genboards.GetPayload{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return deref(b.Fork.Reason)
	}
	plain, _ := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "t", Format: "text", Text: ptr("x")})
	withSession, _ := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "t", Format: "text", Text: ptr("x"), SessionID: ptr(originSession), Cwd: ptr(t.TempDir())})
	gone, _ := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "t", Format: "text", Text: ptr("x"), SessionID: ptr("99999999-2222-4333-8444-555555555555"), Cwd: ptr(t.TempDir())})

	if r := reason(plain.ID); r != "not_found" {
		t.Errorf("without claude, a board without a session: %q", r)
	}
	if r := reason(withSession.ID); r != "not_found" {
		t.Errorf("without claude: %q", r)
	}
	svc.SetForker(newFakeForker())
	if r := reason(gone.ID); r != "session_missing" {
		t.Errorf("session gone: %q", r)
	}
	if r := reason(withSession.ID); r != "" {
		t.Errorf("available: %q", r)
	}
	// A board without a session gets one when asked.
	if r := reason(plain.ID); r != "" {
		t.Errorf("board without a session: %q", r)
	}
	if _, err := boardsOf(svc).Finish(person, &genboards.FinishPayload{ID: withSession.ID}); err != nil {
		t.Fatal(err)
	}
	if r := reason(withSession.ID); r != "done" {
		t.Errorf("finished: %q", r)
	}
}

func TestForkSeeds(t *testing.T) {
	svc, _ := newBoardService(t, nil)
	f := newFakeForker()
	svc.SetForker(f)
	person := signedInPerson()
	n, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("plan"), Title: "Plan", Body: ptr("Use a trie for the index.\n")})
	if err != nil {
		t.Fatal(err)
	}

	// Opened on a note, the board gets a session that read the note.
	b, err := boardsOf(svc).Open(person, &genboards.OpenPayload{NoteID: n.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Fork.Available {
		t.Fatalf("fork on a board from a note = %+v", b.Fork)
	}
	var seeded *genboards.Board
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if seeded, _ = boardsOf(svc).Get(person, &genboards.GetPayload{ID: b.ID}); seeded.Fork.Cwd != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if seeded.Fork.Cwd == nil {
		t.Fatal("the board got no session")
	}
	seed := f.lastAsk()
	if seed.Resume != "" || seed.Session == "" || !strings.Contains(seed.Question, "Use a trie for the index.") || !strings.Contains(seed.Question, "reply only: ready") || seed.Cwd != *seeded.Fork.Cwd {
		t.Errorf("seed = %+v", seed)
	}

	// The sticky notes fork it.
	q, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: b.ID, Kind: board.Fork, Anchor: &genboards.BoardAnchor{Exact: "trie"}, Text: ptr("why?")})
	if err != nil {
		t.Fatal(err)
	}
	waitSticky(t, svc, b.ID, q.ID)
	if a := f.lastAsk(); a.Resume != seed.Session || a.Session != *q.Session {
		t.Errorf("fork of the seed = %+v", a)
	}

	// A draft is made in a fork that goes.
	draft, err := boardsOf(svc).Draft(person, &genboards.DraftPayload{ID: b.ID, Stickies: []string{q.ID}})
	if err != nil || draft.Text != "answered in "+seed.Session {
		t.Fatalf("draft = %+v, %v", draft, err)
	}
	if a := f.lastAsk(); a.Resume != seed.Session || a.Session == "" || f.Has(a.Session) || !strings.Contains(a.Question, "Draft") {
		t.Errorf("draft fork = %+v, left: %v", a, f.Has(a.Session))
	}

	// Deleting the board deletes the session ebira made for it too.
	if err := boardsOf(svc).Delete(person, &genboards.DeletePayload{ID: b.ID}); err != nil {
		t.Fatal(err)
	}
	if f.Has(seed.Session) || f.Has(*q.Session) || !f.Has(originSession) {
		t.Error("after deleting the board: a session ebira made is left, or another is gone")
	}

	// A board an AI opened without a session gets one at the first question.
	ai := WithBaseURL(context.Background(), "http://localhost:13579")
	opened, err := svc.OpenAiBoard(ai, &gennotes.OpenAiBoardPayload{Title: "Diff", Format: "diff", Text: ptr("+x\n")})
	if err != nil {
		t.Fatal(err)
	}
	asks := len(f.asks)
	q2, err := boardsOf(svc).AddSticky(person, &genboards.AddStickyPayload{ID: opened.ID, Kind: board.Fork, Anchor: &genboards.BoardAnchor{Exact: "x"}, Text: ptr("what?")})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitSticky(t, svc, opened.ID, q2.ID); got.State != board.Idle {
		t.Fatalf("answer = %+v", got)
	}
	f.mu.Lock()
	newAsks := append([]ForkAsk{}, f.asks[asks:]...)
	f.mu.Unlock()
	if len(newAsks) != 2 || newAsks[0].Resume != "" || newAsks[1].Resume != newAsks[0].Session {
		t.Errorf("asks for a board without a session = %+v", newAsks)
	}
}
