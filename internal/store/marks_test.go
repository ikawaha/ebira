package store

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCleanMarks(t *testing.T) {
	for _, tt := range []struct{ in, want []string }{
		{nil, nil},
		{[]string{"red"}, []string{"red"}},
		{[]string{"pink", "star", "red"}, []string{"star"}}, // unknown ones out, one at most
		{[]string{"star", "star"}, []string{"star"}},
	} {
		if got := CleanMarks(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("CleanMarks(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSaveMarkNames(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The other keys and the comments stay.
	if err := os.WriteFile(s.ConfigPath(), []byte("# 検索の方式\nsearch: ja\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMarkNames(map[string]string{"star": "あとで", "red": "重要", "blue": ""}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.ConfigPath())
	if !strings.Contains(string(b), "# 検索の方式\nsearch: ja\nmarks:\n  red: 重要\n  star: あとで\n") {
		t.Errorf("the file:\n%s", b)
	}
	c, err := s.Config()
	if err != nil || c.Search != "ja" || len(c.Marks) != 2 || c.Marks["red"] != "重要" {
		t.Errorf("Config() = %+v, %v", c, err)
	}
	// No names take the key away.
	if err := s.SaveMarkNames(nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.ConfigPath()); strings.Contains(string(b), "marks") {
		t.Errorf("the file:\n%s", b)
	}
}

func TestNoteMarks(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := &Note{ID: "a1", Notebook: Inbox, Title: "t", Marks: []string{"pin"}}
	if err := s.Create(n); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("a1")
	if err != nil || !slices.Equal(got.Marks, []string{"pin"}) {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	b, _ := os.ReadFile(s.path(Inbox, "a1"))
	if i, j := strings.Index(string(b), "marks: [pin]"), strings.Index(string(b), "created_at"); i < 0 || i > j {
		t.Errorf("marks before created_at:\n%s", b)
	}
	got.Marks = nil
	if err := s.Save(got); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.path(Inbox, "a1")); strings.Contains(string(b), "marks") {
		t.Errorf("no marks, no key:\n%s", b)
	}
}
