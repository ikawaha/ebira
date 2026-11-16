package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidNotebook(t *testing.T) {
	for name, want := range map[string]bool{
		"dev":          true,
		"dev/design":   true,
		"dev/design/":  false,
		"dev/a/b":      false,
		"/design":      false,
		"inbox/a":      false,
		"trash/a":      false,
		"dev/Design":   false,
		"dev~design":   false,
		"dev/a.assets": false,
	} {
		if got := ValidNotebook(name); got != want {
			t.Errorf("ValidNotebook(%q) = %v", name, got)
		}
	}
}

func TestSubNotebooks(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNotebookMeta("dev", NotebookMeta{Name: "Dev", Kind: KindPrivate}); err != nil {
		t.Fatal(err)
	}
	// The kind of a sub notebook is not written: it takes its parent's.
	if err := s.SaveNotebookMeta("dev/design", NotebookMeta{Name: "Design", Order: 10, Kind: KindCollab}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "dev", "design", NotebookFile)); strings.Contains(string(b), "kind") {
		t.Errorf("kind written for a sub notebook:\n%s", b)
	}
	m, err := s.NotebookMeta("dev/design")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "Design" || m.Order != 10 || m.Kind != KindPrivate {
		t.Errorf("NotebookMeta(dev/design) = %+v, want Design, 10 and the parent's kind", m)
	}
	if err := s.Create(&Note{ID: "a", Notebook: "dev/design", Title: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(&Note{ID: "b", Notebook: "dev", Title: "B"}); err != nil {
		t.Fatal(err)
	}
	// Directories in a sub notebook, in the inbox, and the files of notes are
	// not notebooks.
	for _, d := range []string{"dev/design/deeper", "inbox/sub", "dev/b.assets"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nbs, err := s.Notebooks()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"dev", "dev/design", Inbox, Trash}; !slices.Equal(nbs, want) {
		t.Errorf("Notebooks = %v, want %v", nbs, want)
	}
	files, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Notebook+":"+f.ID)
	}
	if want := []string{"dev:b", "dev/design:a"}; !slices.Equal(got, want) {
		t.Errorf("Scan = %v, want %v", got, want)
	}
	n, err := s.Load("a")
	if err != nil || n.Notebook != "dev/design" {
		t.Fatalf("Load(a) = %+v, %v", n, err)
	}
	if err := s.Move("b", "dev/design"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Load("b"); err != nil || n.Notebook != "dev/design" {
		t.Errorf("Load(b) after Move = %+v, %v", n, err)
	}
	if got := NotebookName("dev/xn--4dkc0654c57za"); got != "開発メモ" {
		t.Errorf("NotebookName of a sub notebook = %q", got)
	}
}

func TestTrashSubNotebook(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNotebookMeta("dev", NotebookMeta{Kind: KindCollab}); err != nil {
		t.Fatal(err)
	}
	for id, nb := range map[string]string{"a": "dev/design", "b": "dev/ops"} {
		if err := s.Create(&Note{ID: id, Notebook: nb, Title: id}); err != nil {
			t.Fatal(err)
		}
	}

	// A sub notebook deleted on its own is told from a notebook in the trash.
	if _, err := s.TrashNotebook("dev/design"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, Trash, "dev~design", "a.yaml")); err != nil {
		t.Errorf("the sub notebook is not in the trash: %v", err)
	}
	n, err := s.Load("a")
	if err != nil || n.Notebook != Trash || n.TrashedNotebook != "dev~design" {
		t.Errorf("Load(a) = %+v, %v", n, err)
	}
	if m, err := s.TrashedNotebookMeta("dev~design"); err != nil || m.Kind != KindCollab {
		t.Errorf("TrashedNotebookMeta(dev~design) = %+v, %v, want the kind of the parent", m, err)
	}

	// The parent goes with its other sub notebooks.
	if _, err := s.TrashNotebook("dev"); err != nil {
		t.Fatal(err)
	}
	trashed, err := s.TrashedNotebooks()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"dev", "dev/ops", "dev~design"}; !slices.Equal(trashed, want) {
		t.Errorf("TrashedNotebooks = %v, want %v", trashed, want)
	}
	if n, err := s.Load("b"); err != nil || n.TrashedNotebook != "dev/ops" {
		t.Errorf("Load(b) = %+v, %v", n, err)
	}
	if m, err := s.TrashedNotebookMeta("dev/ops"); err != nil || m.Kind != KindCollab {
		t.Errorf("TrashedNotebookMeta(dev/ops) = %+v, %v, want the kind of the parent in the trash", m, err)
	}
	if m, err := s.TrashedNotebookMeta("dev~design"); err != nil || m.Kind != "" {
		t.Errorf("TrashedNotebookMeta(dev~design) without its parent = %+v, %v, want the default kind", m, err)
	}

	// A sub notebook goes back only into its parent.
	if _, err := s.RestoreNotebook("dev~design"); !errors.Is(err, ErrNoParent) {
		t.Errorf("RestoreNotebook without the parent = %v, want ErrNoParent", err)
	}
	if nb, err := s.RestoreNotebook("dev"); err != nil || nb != "dev" {
		t.Fatalf("RestoreNotebook(dev) = %q, %v", nb, err)
	}
	if nb, err := s.RestoreNotebook("dev~design"); err != nil || nb != "dev/design" {
		t.Fatalf("RestoreNotebook(dev~design) = %q, %v", nb, err)
	}
	if n, err := s.Load("a"); err != nil || n.Notebook != "dev/design" || n.TrashedNotebook != "" {
		t.Errorf("Load(a) after restoring = %+v, %v", n, err)
	}
	if n, err := s.Load("b"); err != nil || n.Notebook != "dev/ops" {
		t.Errorf("Load(b) after restoring = %+v, %v", n, err)
	}

	// Emptying the trash removes the sub notebooks deleted on their own.
	if _, err := s.TrashNotebook("dev/ops"); err != nil {
		t.Fatal(err)
	}
	if err := s.EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	if trashed, err := s.TrashedNotebooks(); err != nil || len(trashed) != 0 {
		t.Errorf("TrashedNotebooks after EmptyTrash = %v, %v", trashed, err)
	}
}

// TestTrashSameNotebook checks that a notebook deleted while the trash holds
// one with the same ID gets a number there, and goes back to its ID.
func TestTrashSameNotebook(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mk := func(id, nb string) {
		t.Helper()
		if err := s.Create(&Note{ID: id, Notebook: nb, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	trash := func(nb, want string) {
		t.Helper()
		if id, err := s.TrashNotebook(nb); err != nil || id != want {
			t.Fatalf("TrashNotebook(%s) = %q, %v, want %q", nb, id, err, want)
		}
	}
	mk("a", "dev")
	trash("dev", "dev")
	mk("b", "dev")
	mk("c", "dev/ops")
	trash("dev", "dev@2")
	mk("d", "dev/ops")
	trash("dev/ops", "dev~ops")
	mk("e", "dev/ops")
	trash("dev/ops", "dev~ops@2")

	trashed, err := s.TrashedNotebooks()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"dev", "dev@2", "dev@2/ops", "dev~ops", "dev~ops@2"}; !slices.Equal(trashed, want) {
		t.Errorf("TrashedNotebooks = %v, want %v", trashed, want)
	}
	for id, want := range map[string]string{"a": "dev", "b": "dev@2", "c": "dev@2/ops", "d": "dev~ops", "e": "dev~ops@2"} {
		if n, err := s.Load(id); err != nil || n.Notebook != Trash || n.TrashedNotebook != want {
			t.Errorf("Load(%s) = %+v, %v, want in %s", id, n, err, want)
		}
	}
	for id, want := range map[string]string{"dev@2": "dev", "dev@2/ops": "dev/ops", "dev~ops@2": "dev/ops", "dev@12": "dev"} {
		if got := RestoredNotebook(id); got != want {
			t.Errorf("RestoredNotebook(%s) = %q, want %q", id, got, want)
		}
	}
	for _, id := range []string{"dev@1", "dev@02", "dev@", "dev@2@3", "dev@x", "dev~ops@2/x", "dev/ops@2"} {
		if validTrashed(id) {
			t.Errorf("validTrashed(%q) = true", id)
		}
	}

	// Over a notebook with its ID (renamed, say, which keeps the ID), it
	// takes another, as a new notebook would, keeping its name; its sub
	// notebook goes with it.
	if err := s.SaveNotebookMeta("dev", NotebookMeta{Name: "Old dev"}); err != nil {
		t.Fatal(err)
	}
	if nb, err := s.RestoreNotebook("dev@2"); err != nil || nb != "dev-2" {
		t.Fatalf("RestoreNotebook(dev@2) over dev = %q, %v, want dev-2", nb, err)
	}
	if m, err := s.NotebookMeta("dev-2"); err != nil || m.Name != "dev" {
		t.Errorf("NotebookMeta(dev-2) = %+v, %v, want the name it had", m, err)
	}
	if n, err := s.Load("c"); err != nil || n.Notebook != "dev-2/ops" || n.TrashedNotebook != "" {
		t.Errorf("Load(c) after restoring = %+v, %v", n, err)
	}
	// A sub notebook likewise, in its parent.
	if nb, err := s.RestoreNotebook("dev~ops"); err != nil || nb != "dev/ops" {
		t.Fatalf("RestoreNotebook(dev~ops) = %q, %v", nb, err)
	}
	if nb, err := s.RestoreNotebook("dev~ops@2"); err != nil || nb != "dev/ops-2" {
		t.Fatalf("RestoreNotebook(dev~ops@2) over dev/ops = %q, %v, want dev/ops-2", nb, err)
	}
	if n, err := s.Load("e"); err != nil || n.Notebook != "dev/ops-2" {
		t.Errorf("Load(e) after restoring = %+v, %v", n, err)
	}
	trash("dev/ops-2", "dev~ops-2")
	if err := s.EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	if trashed, err := s.TrashedNotebooks(); err != nil || len(trashed) != 0 {
		t.Errorf("TrashedNotebooks after EmptyTrash = %v, %v", trashed, err)
	}
}
