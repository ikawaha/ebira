package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeNote(t *testing.T, dir, rel, s string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func TestRepairCopies(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	orig := "title: Plan\nupdated_at: 2026-10-18T10:00:00Z\nbody: |\n  mine\n"
	other := "title: Plan\nupdated_at: 2026-10-18T10:05:00Z\nbody: |\n  theirs\n"
	writeNote(t, dir, "dev/abc.yaml", orig)
	writeNote(t, dir, "dev/abc (Mac's conflicted copy 2026-10-18).yaml", other) // Dropbox
	writeNote(t, dir, "dev/abc 2.yaml", orig)                                   // iCloud Drive, the same
	writeNote(t, dir, "dev/abc-MacBook.yaml", orig)                             // OneDrive, the same
	writeNote(t, dir, "dev/abc.sync-conflict-20261018-100500-XYZ.yaml", other)  // Syncthing
	writeNote(t, dir, "dev/Readme.yaml", "not: a note\n")
	writeNote(t, dir, "dev/half (1).yaml", "title: [broken\n")
	writeNote(t, dir, "dev/.abc.yaml.tmp.yaml", other)
	writeNote(t, dir, "dev/My Plan.yaml", "title: Mine\n") // put by hand

	res, err := s.Repair()
	if err != nil {
		t.Fatal(err)
	}
	made := map[string]string{}
	removed := []string{}
	for _, r := range res {
		if r.Note == "" {
			removed = append(removed, r.Path)
		} else {
			made[r.Path] = r.Note
		}
	}
	slices.Sort(removed)
	if !slices.Equal(removed, []string{"dev/abc 2.yaml", "dev/abc-MacBook.yaml"}) {
		t.Errorf("removed: %v", removed)
	}
	if len(made) != 3 {
		t.Fatalf("made: %v", made)
	}
	if n, err := s.Load(made["dev/My Plan.yaml"]); err != nil || n.Title != "Mine" {
		t.Errorf("by hand: %+v, %v", n, err)
	}
	delete(made, "dev/My Plan.yaml")
	for p, id := range made {
		n, err := s.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		if n.Title != "Plan (conflict copy)" || n.Body != "theirs\n" || n.Notebook != "dev" {
			t.Errorf("%s: %+v", p, n)
		}
		if exists(dir, p) {
			t.Errorf("%s is left", p)
		}
	}
	for _, p := range []string{"dev/abc.yaml", "dev/Readme.yaml", "dev/half (1).yaml", "dev/.abc.yaml.tmp.yaml"} {
		if !exists(dir, p) {
			t.Errorf("%s was removed", p)
		}
	}

	// Another machine repairing the same copy makes the same note.
	writeNote(t, dir, "dev/abc (Mac's conflicted copy 2026-10-18).yaml", other)
	again, err := s.Repair()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Note != made["dev/abc (Mac's conflicted copy 2026-10-18).yaml"] {
		t.Errorf("again: %+v, made %v", again, made)
	}
	files, _ := s.Scan()
	if len(files) != 4 {
		t.Errorf("notes: %+v", files)
	}
}

func TestRepairDuplicates(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Moved to work on a machine, changed in dev on another: the change wins
	// the ID, and the moved one becomes a copy in work.
	writeNote(t, dir, "work/x.yaml", "title: X\nupdated_at: 2026-10-18T10:00:00Z\nbody: old\n")
	writeNote(t, dir, "dev/x.yaml", "title: X\nupdated_at: 2026-10-18T11:00:00Z\nbody: new\n")
	// The same in the trash and in a notebook: the one in the trash goes.
	same := "title: Y\nupdated_at: 2026-10-18T10:00:00Z\nbody: y\n"
	writeNote(t, dir, "trash/y.yaml", same)
	writeNote(t, dir, "dev/y.yaml", same)

	res, err := s.Repair()
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Path != "work/x.yaml" || res[0].Note == "" || res[1].Path != "trash/y.yaml" || res[1].Note != "" {
		t.Fatalf("repairs: %+v", res)
	}
	if n, err := s.Load("x"); err != nil || n.Notebook != "dev" || n.Body != "new" {
		t.Errorf("x: %+v, %v", n, err)
	}
	cp, err := s.Load(res[0].Note)
	if err != nil || cp.Notebook != "work" || cp.Body != "old" || !strings.HasSuffix(cp.Title, "(conflict copy)") {
		t.Errorf("copy: %+v, %v", cp, err)
	}
	if n, err := s.Load("y"); err != nil || n.Notebook != "dev" {
		t.Errorf("y: %+v, %v", n, err)
	}
	if again, err := s.Repair(); err != nil || len(again) != 0 {
		t.Errorf("again: %+v, %v", again, err)
	}
}

func TestCopiedIDs(t *testing.T) {
	for name, want := range map[string][]string{
		"abc (Mac's conflicted copy 2026-10-18)": {"abc"},
		"abc 2":                                  {"abc"},
		"abc-MacBook":                            {"abc-", "abc"},
		"abc.sync-conflict-20261018-100500-XYZ":  {"abc"},
		"Plan":                                   nil,
	} {
		if got := copiedIDs(name); !slices.Equal(got, want) {
			t.Errorf("%q: %q", name, got)
		}
	}
}
