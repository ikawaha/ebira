package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// TestLinks checks which symbolic links in the notes directory the store
// follows: the folder of a notebook, and nothing in it.
func TestLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need rights on Windows")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "notes")
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	create := func(id, nb string) {
		t.Helper()
		if err := s.Create(&Note{ID: id, Notebook: nb, Title: id, CreatedAt: at, UpdatedAt: at, Body: "x\n"}); err != nil {
			t.Fatal(err)
		}
	}
	create("a", Inbox)

	// A notebook whose folder is a link, as to another disk, is a notebook.
	far := filepath.Join(base, "far")
	if err := os.MkdirAll(far, 0o755); err != nil {
		t.Fatal(err)
	}
	link(far, filepath.Join(dir, "linked"))
	if nbs, err := s.Notebooks(); err != nil || !slices.Contains(nbs, "linked") {
		t.Errorf("Notebooks = %v, %v, want linked", nbs, err)
	}
	create("c", "linked")
	if n, err := s.Load("c"); err != nil || n.Notebook != "linked" {
		t.Errorf("Load of a note in a linked notebook = %+v, %v", n, err)
	}

	// A note that is a link is no note.
	outside := filepath.Join(base, "outside.yaml")
	b, err := os.ReadFile(filepath.Join(dir, Inbox, "a.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, b, 0o644); err != nil {
		t.Fatal(err)
	}
	link(outside, filepath.Join(dir, Inbox, "b.yaml"))
	if _, err := s.Load("b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load of a linked note = %v, want ErrNotFound", err)
	}
	files, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.ID == "b" {
			t.Error("Scan lists a linked note")
		}
	}
	if _, err := readFile(filepath.Join(dir, Inbox, "b.yaml")); !errors.Is(err, ErrLink) {
		t.Errorf("readFile of a link = %v, want ErrLink", err)
	}

	// Nor are the settings of a notebook.
	if err := os.WriteFile(filepath.Join(base, "meta.yaml"), []byte("kind: collab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	link(filepath.Join(base, "meta.yaml"), filepath.Join(dir, "work", NotebookFile))
	if _, err := s.NotebookMeta("work"); !errors.Is(err, ErrLink) {
		t.Errorf("NotebookMeta through a link = %v, want ErrLink", err)
	}

	// Nor the files of a note: a file that is a link, or a linked folder.
	if _, err := s.AddFile("a", "kept.txt", []byte("kept")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.File("a", "kept.txt"); err != nil {
		t.Errorf("File of a regular file: %v", err)
	}
	secret := filepath.Join(base, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link(secret, filepath.Join(dir, Inbox, "a.assets", "secret.txt"))
	if _, err := s.File("a", "secret.txt"); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("File of a link = %v, want ErrFileNotFound", err)
	}
	create("d", Inbox)
	assets := filepath.Join(base, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link(assets, filepath.Join(dir, Inbox, "d.assets"))
	if _, err := s.File("d", "x.txt"); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("File in a linked folder = %v, want ErrFileNotFound", err)
	}
}
