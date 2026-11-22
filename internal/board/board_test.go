package board

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "boards"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 21, 9, 0, 0, 123456789, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func open(t *testing.T, s *Store, title, source string) *Board {
	t.Helper()
	b, err := s.Create(Board{Title: title, Source: Source{From: FromText}}, source)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCreate(t *testing.T) {
	s, _ := newStore(t)
	b, err := s.Create(Board{
		Title:  "  PR #1  ",
		Source: Source{From: FromUpload, Format: Diff, NoteID: "ignored"},
		Origin: Origin{SessionID: "71f0d3f3-75c3-460e-b7ed-ce852dd93fb3", Cwd: "/work/ebira"},
	}, "diff --git a/x b/x\n")
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "PR #1" || b.Status != Open || b.Source.NoteID != "" || b.Source.Size != 19 {
		t.Errorf("board = %+v", b)
	}
	if want := time.Date(2026, 11, 21, 9, 0, 0, 123000000, time.UTC); !b.CreatedAt.Equal(want) {
		t.Errorf("created at %v, want %v (to the millisecond)", b.CreatedAt, want)
	}
	got, err := s.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin.Cwd != "/work/ebira" || got.Source.Format != Diff {
		t.Errorf("read back %+v", got)
	}
	src, err := s.Source(b.ID)
	if err != nil || src != "diff --git a/x b/x\n" {
		t.Errorf("source = %q, %v", src, err)
	}
	if fi, err := os.Stat(filepath.Join(s.dir, b.ID, boardFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("board file mode: %v %v", fi.Mode(), err)
	}

	// The format defaults to Markdown.
	if b := open(t, s, "notes", "# x"); b.Source.Format != Markdown {
		t.Errorf("format = %q", b.Source.Format)
	}
}

func TestCreateInvalid(t *testing.T) {
	s, _ := newStore(t)
	for name, c := range map[string]struct {
		b      Board
		source string
		err    error
	}{
		"no title":     {Board{Source: Source{From: FromText}}, "x", ErrInvalid},
		"long title":   {Board{Title: strings.Repeat("あ", MaxTitle+1), Source: Source{From: FromText}}, "x", ErrInvalid},
		"no source":    {Board{Title: "t", Source: Source{From: FromText}}, " \n", ErrInvalid},
		"not UTF-8":    {Board{Title: "t", Source: Source{From: FromText}}, "\xff", ErrInvalid},
		"large":        {Board{Title: "t", Source: Source{From: FromText}}, strings.Repeat("x", MaxSource+1), ErrTooLarge},
		"from":         {Board{Title: "t", Source: Source{From: "web"}}, "x", ErrInvalid},
		"format":       {Board{Title: "t", Source: Source{From: FromText, Format: "html"}}, "x", ErrInvalid},
		"lang":         {Board{Title: "t", Source: Source{From: FromText, Lang: "Go Lang"}}, "x", ErrInvalid},
		"note ID":      {Board{Title: "t", Source: Source{From: FromNote, NoteID: "../x"}}, "x", ErrInvalid},
		"session":      {Board{Title: "t", Source: Source{From: FromText}, Origin: Origin{SessionID: "../../x"}}, "x", ErrInvalid},
		"relative cwd": {Board{Title: "t", Source: Source{From: FromText}, Origin: Origin{Cwd: "work"}}, "x", ErrInvalid},
		"unclean cwd":  {Board{Title: "t", Source: Source{From: FromText}, Origin: Origin{Cwd: "/work/../etc"}}, "x", ErrInvalid},
	} {
		if _, err := s.Create(c.b, c.source); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, want %v", name, err, c.err)
		}
	}
	if boards, _ := s.List(); len(boards) != 0 {
		t.Errorf("%d boards left behind", len(boards))
	}
}

func TestMaxOpen(t *testing.T) {
	s, _ := newStore(t)
	var first *Board
	for i := range MaxOpen {
		b := open(t, s, "b", "x")
		if i == 0 {
			first = b
		}
	}
	if _, err := s.Create(Board{Title: "one more", Source: Source{From: FromText}}, "x"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("err = %v, want ErrTooMany", err)
	}
	// A finished board does not count.
	if _, err := s.Update(first.ID, func(b *Board) error { b.Status = Done; return nil }); err != nil {
		t.Fatal(err)
	}
	open(t, s, "one more", "x")
}

func TestStickies(t *testing.T) {
	s, now := newStore(t)
	b := open(t, s, "t", "the source text")
	var root, child, other string
	b, err := s.Update(b.ID, func(b *Board) error {
		st, err := b.Add(Sticky{
			Kind:     Assistant,
			Anchor:   Anchor{Exact: "source", Prefix: "the ", Suffix: " text"},
			Messages: []Message{{Role: Agent, Text: "What is this?"}},
		}, *now)
		if err != nil {
			return err
		}
		root = st.ID
		st, err = b.Add(Sticky{Parent: root, Kind: Memo, Anchor: Anchor{Exact: "answer"}}, *now)
		if err != nil {
			return err
		}
		child = st.ID
		st, err = b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "text"}, Messages: []Message{{Text: "memo"}}}, *now)
		if err != nil {
			return err
		}
		other = st.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st := b.Find(root)
	if st == nil || st.State != Idle || len(st.Messages) != 1 || st.Messages[0].Role != User {
		t.Fatalf("root = %+v (the first message is the person's whatever the role given)", st)
	}
	if !strings.HasPrefix(root, "s") || root == child {
		t.Errorf("IDs %q %q", root, child)
	}

	// Answers and follow-ups.
	*now = now.Add(time.Minute)
	b, err = s.Update(b.ID, func(b *Board) error {
		st := b.Find(root)
		if err := st.Say(Agent, "It is the source.", s.Now()); err != nil {
			return err
		}
		return st.Say(User, "Why?", s.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	if st := b.Find(root); len(st.Messages) != 3 || st.Messages[1].Role != Agent || !st.UpdatedAt.After(st.CreatedAt) {
		t.Errorf("after messages: %+v", st)
	}
	if !b.UpdatedAt.After(b.CreatedAt) {
		t.Errorf("board not stamped as updated")
	}

	// Removing a sticky note removes the ones on it.
	b, err = s.Update(b.ID, func(b *Board) error {
		if !b.Remove(root) {
			t.Error("Remove = false")
		}
		if b.Remove("nope") {
			t.Error("Remove of nothing = true")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Stickies) != 1 || b.Stickies[0].ID != other {
		t.Errorf("left %+v, want only %s", b.Stickies, other)
	}
}

func TestStickyInvalid(t *testing.T) {
	now := time.Now()
	b := &Board{Status: Open}
	for name, c := range map[string]struct {
		s   Sticky
		err error
	}{
		"kind":         {Sticky{Kind: "fork", Anchor: Anchor{Exact: "x"}}, ErrInvalid},
		"no passage":   {Sticky{Kind: Memo, Anchor: Anchor{Exact: " "}}, ErrInvalid},
		"long passage": {Sticky{Kind: Memo, Anchor: Anchor{Exact: strings.Repeat("x", MaxQuote+1)}}, ErrTooLarge},
		"long prefix":  {Sticky{Kind: Memo, Anchor: Anchor{Exact: "x", Prefix: strings.Repeat("あ", MaxContext+1)}}, ErrInvalid},
		"parent":       {Sticky{Kind: Memo, Parent: "s1", Anchor: Anchor{Exact: "x"}}, ErrInvalid},
		"empty":        {Sticky{Kind: Memo, Anchor: Anchor{Exact: "x"}, Messages: []Message{{Text: " "}}}, ErrInvalid},
		"long message": {Sticky{Kind: Memo, Anchor: Anchor{Exact: "x"}, Messages: []Message{{Text: strings.Repeat("x", MaxMessage+1)}}}, ErrTooLarge},
	} {
		if _, err := b.Add(c.s, now); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, want %v", name, err, c.err)
		}
	}
	if len(b.Stickies) != 0 {
		t.Errorf("added %d", len(b.Stickies))
	}

	// A finished board takes no sticky notes.
	done := &Board{Status: Done}
	if _, err := done.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "x"}}, now); !errors.Is(err, ErrDone) {
		t.Errorf("finished: err = %v", err)
	}

	// Limits on counts.
	full := &Board{Status: Open}
	for range MaxStickies {
		if _, err := full.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "x"}}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := full.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "x"}}, now); !errors.Is(err, ErrTooMany) {
		t.Errorf("sticky notes beyond the limit: err = %v", err)
	}
	st := &full.Stickies[0]
	for range MaxMessages {
		if err := st.Say(User, "x", now); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Say(User, "x", now); !errors.Is(err, ErrTooMany) {
		t.Errorf("messages beyond the limit: err = %v", err)
	}
}

func TestUpdateFails(t *testing.T) {
	s, now := newStore(t)
	b := open(t, s, "t", "x")
	*now = now.Add(time.Hour)
	boom := errors.New("boom")
	if _, err := s.Update(b.ID, func(b *Board) error { b.Title = "changed"; return boom }); err != boom {
		t.Fatalf("err = %v", err)
	}
	got, _ := s.Get(b.ID)
	if got.Title != "t" || !got.UpdatedAt.Equal(b.UpdatedAt) {
		t.Errorf("a failed change was written: %+v", got)
	}
}

func TestNotFound(t *testing.T) {
	s, _ := newStore(t)
	for _, id := range []string{"0123456789abcd", "../boards", "", "A"} {
		if _, err := s.Get(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v", id, err)
		}
		if _, err := s.Source(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Source(%q) = %v", id, err)
		}
		if err := s.Delete(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete(%q) = %v", id, err)
		}
		if _, err := s.Update(id, func(*Board) error { return nil }); !errors.Is(err, ErrNotFound) {
			t.Errorf("Update(%q) = %v", id, err)
		}
	}
}

func TestListAndDelete(t *testing.T) {
	s, now := newStore(t)
	a := open(t, s, "a", "x")
	*now = now.Add(time.Second)
	b := open(t, s, "b", "x")
	// A broken board and stray files are not listed.
	os.Mkdir(filepath.Join(s.dir, "broken"), 0o700)
	os.WriteFile(filepath.Join(s.dir, "broken", boardFile), []byte("{"), 0o600)
	os.WriteFile(filepath.Join(s.dir, ".tmp-1"), nil, 0o600)

	boards, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 2 || boards[0].ID != b.ID || boards[1].ID != a.ID {
		t.Fatalf("list = %v, want b then a", boards)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted board: %v", err)
	}
}

func TestInterrupt(t *testing.T) {
	s, now := newStore(t)
	b := open(t, s, "t", "x")
	b, err := s.Update(b.ID, func(b *Board) error {
		st, err := b.Add(Sticky{Kind: Assistant, Anchor: Anchor{Exact: "x"}, Messages: []Message{{Text: "q"}}}, *now)
		if err != nil {
			return err
		}
		st.State = Waiting
		_, err = b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "x"}}, *now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Interrupt("the server stopped"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(b.ID)
	if st := got.Stickies[0]; st.State != Failed || st.Error != "the server stopped" {
		t.Errorf("waiting sticky note: %+v", st)
	}
	if st := got.Stickies[1]; st.State != Idle {
		t.Errorf("idle sticky note: %+v", st)
	}
}

func TestSweep(t *testing.T) {
	s, now := newStore(t)
	start := *now
	open(t, s, "idle", "x")
	busy := open(t, s, "busy", "x")
	done := open(t, s, "done", "x")
	recent := open(t, s, "recent", "x")
	finish := func(id string) {
		t.Helper()
		if _, err := s.Update(id, func(b *Board) error {
			b.Status = Done
			b.Result = &Result{Text: "back", FinishedAt: s.Now()}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	finish(done.ID)

	*now = start.Add(IdleAge - DoneAge + time.Hour)
	finish(recent.ID)
	*now = start.Add(IdleAge - time.Hour)
	if _, err := s.Update(busy.ID, func(*Board) error { return nil }); err != nil {
		t.Fatal(err)
	}

	*now = start.Add(IdleAge + time.Minute)
	gone, err := s.Sweep()
	if err != nil {
		t.Fatal(err)
	}
	left, _ := s.List()
	names := map[string]bool{}
	for _, b := range left {
		names[b.Title] = true
	}
	if len(gone) != 2 || names["idle"] || names["done"] || !names["busy"] || !names["recent"] {
		t.Errorf("gone %v, left %v", gone, names)
	}
}
