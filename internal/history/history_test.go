package history

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, &now
}

func TestAddListGet(t *testing.T) {
	s, now := newStore(t)
	for i := range 3 {
		v := Version{Title: fmt.Sprintf("title %d", i), Tags: []string{"a"}, Body: fmt.Sprintf("line 1\n本文 %d\n", i), UpdatedAt: now.Add(-time.Hour)}
		if err := s.Add("n1", v); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute)
	}
	vs, err := s.List("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 || vs[0].Title != "title 2" || vs[2].Title != "title 0" {
		t.Fatalf("List = %+v, want newest first", vs)
	}
	v, err := s.Get("n1", vs[1].Rev)
	if err != nil {
		t.Fatal(err)
	}
	if v.Body != "line 1\n本文 1\n" || v.Tags[0] != "a" || !v.ReplacedAt.Equal(time.Date(2026, 10, 19, 9, 1, 0, 0, time.UTC)) {
		t.Errorf("Get = %+v", v)
	}
	if _, err := s.Get("n1", "1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get of a missing rev: %v, want ErrNotFound", err)
	}
	if _, err := s.Get("n1", "../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get of a bad rev: %v, want ErrNotFound", err)
	}
	if _, err := s.List("../x"); err == nil {
		t.Error("List of a bad ID: no error")
	}
	if vs, err := s.List("none"); err != nil || len(vs) != 0 {
		t.Errorf("List of a note without versions = %v, %v", vs, err)
	}
}

func TestSameTime(t *testing.T) {
	s, _ := newStore(t)
	for range 2 {
		if err := s.Add("n1", Version{Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if vs, _ := s.List("n1"); len(vs) != 2 || vs[0].Rev == vs[1].Rev {
		t.Errorf("two versions at the same time: %+v", vs)
	}
}

func TestLimits(t *testing.T) {
	s, now := newStore(t)
	for i := range MaxVersions + 3 {
		if err := s.Add("n1", Version{Title: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute)
	}
	vs, _ := s.List("n1")
	if len(vs) != MaxVersions || vs[len(vs)-1].Title != "3" {
		t.Fatalf("kept %d versions, the oldest %q; want %d from 3", len(vs), vs[len(vs)-1].Title, MaxVersions)
	}
	// After MaxAge, only the MinKeep newest are kept, counting the new one.
	*now = now.Add(MaxAge + time.Hour)
	if err := s.Add("n1", Version{Title: "new"}); err != nil {
		t.Fatal(err)
	}
	vs, _ = s.List("n1")
	if len(vs) != MinKeep || vs[0].Title != "new" {
		t.Errorf("after MaxAge: %d versions, the newest %q; want %d", len(vs), vs[0].Title, MinKeep)
	}
}

func TestRemoveAndSweep(t *testing.T) {
	s, _ := newStore(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Add(id, Version{Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Sweep(func(id string) bool { return id != "b" }); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"a": 0, "b": 0, "c": 1} {
		if vs, _ := s.List(id); len(vs) != want {
			t.Errorf("%s: %d versions, want %d", id, len(vs), want)
		}
	}
}

// Sweep applies MaxAge to a note that got no version since, which Add alone
// would not.
func TestSweepAge(t *testing.T) {
	s, now := newStore(t)
	for i := range MinKeep + 3 {
		if err := s.Add("n1", Version{Title: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute)
	}
	if err := s.Sweep(func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if vs, _ := s.List("n1"); len(vs) != MinKeep+3 {
		t.Fatalf("recent versions swept: %d left, want %d", len(vs), MinKeep+3)
	}
	*now = now.Add(MaxAge + time.Hour)
	if err := s.Sweep(func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	vs, _ := s.List("n1")
	if len(vs) != MinKeep || vs[0].Title != fmt.Sprint(MinKeep+2) {
		t.Errorf("after MaxAge: %d versions, the newest %q; want the %d newest", len(vs), vs[0].Title, MinKeep)
	}
}
