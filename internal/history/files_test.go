package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeepFile(t *testing.T) {
	s, now := newStore(t)
	src := filepath.Join(t.TempDir(), "a.md")
	for i := range 3 {
		if err := os.WriteFile(src, []byte(fmt.Sprintf("version %d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := s.KeepFile("n1", "a.md", src); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute)
	}
	fs, err := s.Files("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 3 || fs[0].Name != "a.md" || fs[0].Size != int64(len("version 2")) || !fs[0].ReplacedAt.After(fs[2].ReplacedAt) {
		t.Fatalf("Files = %+v", fs)
	}
	// The files do not show among the versions of the note.
	if vs, err := s.List("n1"); err != nil || len(vs) != 0 {
		t.Errorf("List = %+v, %v", vs, err)
	}
	if err := s.KeepFile("n1", "../a.md", src); err == nil {
		t.Error("KeepFile of a bad name: no error")
	}

	var got string
	err = s.TakeFile("n1", fs[1].Rev, func(f *File, path string) error {
		b, err := os.ReadFile(path)
		got = f.Name + ": " + string(b)
		return err
	})
	if err != nil || got != "a.md: version 1" {
		t.Fatalf("TakeFile = %q, %v", got, err)
	}
	if err := s.TakeFile("n1", fs[1].Rev, func(*File, string) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Errorf("TakeFile of a taken file: %v", err)
	}
	// A failing put keeps the file.
	if err := s.TakeFile("n1", fs[0].Rev, func(*File, string) error { return errors.New("failed") }); err == nil {
		t.Error("TakeFile with a failing put: no error")
	}
	if fs, _ := s.Files("n1"); len(fs) != 2 {
		t.Errorf("Files after taking one = %+v", fs)
	}
}

func TestFileLimits(t *testing.T) {
	s, now := newStore(t)
	src := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	for range MaxFileVersions + 3 {
		if err := s.KeepFile("n1", "a.png", src); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute)
	}
	if fs, _ := s.Files("n1"); len(fs) != MaxFileVersions {
		t.Errorf("%d files kept, want %d", len(fs), MaxFileVersions)
	}
	// Old files go but the MinKeep newest, when the history is swept.
	*now = now.Add(MaxAge + time.Hour)
	if err := s.Sweep(func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if fs, _ := s.Files("n1"); len(fs) != MinKeep {
		t.Errorf("%d files kept after the sweep, want %d", len(fs), MinKeep)
	}
	// The files of a note that is gone go with it.
	if err := s.Sweep(func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if fs, _ := s.Files("n1"); len(fs) != 0 {
		t.Errorf("Files of a note gone = %+v", fs)
	}
}
