package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSeparators(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.Dir(), "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if seps, err := s.Separators("work"); err != nil || len(seps) != 0 {
		t.Fatalf("Separators of a notebook without the file = %v, %v", seps, err)
	}
	if err := s.SaveSeparators("missing", []Separator{{ID: "sep-1", Order: 1}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("SaveSeparators of a missing notebook = %v, want ErrNotFound", err)
	}
	if !ValidSeparatorID(NewSeparatorID()) || !ValidID(NewSeparatorID()) {
		t.Error("NewSeparatorID gives an invalid ID")
	}
	in := []Separator{
		{ID: "sep-b", Title: "2026 年 9 月", Date: "2026-09-01", Order: 100},
		{ID: "sep-a", Title: " 10 月 ", Date: "2026-10-01", Color: "blue", Order: 200},
		{ID: "sep-c", Order: 50},
	}
	if err := s.SaveSeparators("work", in); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.Dir(), "work", SeparatorsFile)
	b, _ := os.ReadFile(p)
	want := "- id: sep-a\n  title: 10 月\n  date: 2026-10-01\n  color: blue\n  order: 200\n" +
		"- id: sep-b\n  title: 2026 年 9 月\n  date: 2026-09-01\n  order: 100\n" +
		"- id: sep-c\n  order: 50\n"
	if string(b) != want {
		t.Errorf("the file:\n%s\nwant:\n%s", b, want)
	}
	got, err := s.Separators("work")
	if err != nil {
		t.Fatal(err)
	}
	ids := func(seps []Separator) []string {
		var r []string
		for _, sep := range seps {
			r = append(r, sep.ID)
		}
		return r
	}
	if !slices.Equal(ids(got), []string{"sep-a", "sep-b", "sep-c"}) || got[0].Title != "10 月" || got[0].Date != "2026-10-01" || got[0].Color != "blue" || got[2].Date != "" {
		t.Errorf("Separators = %+v", got)
	}

	// Comments and unknown keys of the entries stay; broken entries are left out.
	if err := os.WriteFile(p, []byte("# 区切り線\n- id: sep-a # 先頭\n  memo: x\n  order: 200\n- id: bad\n  order: 1\n- id: sep-a\n  order: 3\n- id: sep-d\n  date: someday\n  color: pink\n  order: 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = s.Separators("work")
	if err != nil || !slices.Equal(ids(got), []string{"sep-a", "sep-d"}) || got[1].Date != "" || got[1].Color != "" {
		t.Fatalf("Separators of a hand-edited file = %+v, %v", got, err)
	}
	got[0].Title = "見出し"
	got[0].Color = "red"
	got[0].Order = 5
	if err := s.SaveSeparators("work", got); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	// A comment goes with the entry it is on.
	if string(b) != "- id: sep-d\n  order: 10\n# 区切り線\n- id: sep-a # 先頭\n  memo: x\n  title: 見出し\n  color: red\n  order: 5\n" {
		t.Errorf("the file after saving:\n%s", b)
	}

	// No separators remove the file.
	if err := s.SaveSeparators("work", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file is left: %v", err)
	}
}

// The separators move with their notebook.
func TestSeparatorsMoveWithNotebook(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNotebookMeta("work", NotebookMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSeparators("work", []Separator{{ID: "sep-a", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNotebookMeta("home", NotebookMeta{}); err != nil {
		t.Fatal(err)
	}
	moved, err := s.MoveNotebook("work", "home")
	if err != nil {
		t.Fatal(err)
	}
	if seps, err := s.Separators(moved); err != nil || len(seps) != 1 {
		t.Errorf("Separators of the moved notebook = %v, %v", seps, err)
	}
	if err := s.TrashNotebook(moved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), Trash, "home~work", SeparatorsFile)); err != nil {
		t.Errorf("the separators did not go to the trash: %v", err)
	}
}
