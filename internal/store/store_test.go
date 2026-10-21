package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSaveKeepsComments(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := `# 手で書いたノート
title: 古いタイトル # 行末コメント
tags: [a]
created_at: 2026-10-01T09:00:00Z
updated_at: 2026-10-01T09:00:00Z
body: |
  本文
extra: 残すキー
`
	if err := os.WriteFile(filepath.Join(dir, Inbox, "memo.yaml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := s.Load("memo")
	if err != nil {
		t.Fatal(err)
	}
	n.Title = "新しいタイトル"
	n.Body = "一行目\n二行目\n"
	n.UpdatedAt = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, Inbox, "memo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := `# 手で書いたノート
title: 新しいタイトル # 行末コメント
tags: [a]
created_at: 2026-10-01T09:00:00Z
updated_at: 2026-10-15T00:00:00Z
body: |
  一行目
  二行目
extra: 残すキー
`
	if got := string(b); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got, err := s.Load("memo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != n.Title || got.Body != n.Body || !got.UpdatedAt.Equal(n.UpdatedAt) {
		t.Errorf("round trip = %+v", got)
	}
}

func TestCreateExists(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := &Note{ID: "x", Notebook: Inbox, Title: "X"}
	if err := s.Create(n); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(n); err != ErrExists {
		t.Errorf("second Create = %v, want ErrExists", err)
	}
	if _, err := s.Load("../x"); err != ErrInvalidID {
		t.Errorf("Load(../x) = %v, want ErrInvalidID", err)
	}
}

func TestVersion(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := &Note{ID: "v", Notebook: Inbox, Title: "V"}
	if err := s.Create(n); err != nil {
		t.Fatal(err)
	}
	v1 := n.Version
	if got, err := s.Load("v"); err != nil || got.Version != v1 {
		t.Fatalf("Load version = %q, %v; want %q", got.Version, err, v1)
	}
	n.Body = "changed"
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	if n.Version == v1 {
		t.Error("version did not change after Save")
	}
	if got, err := s.Version("v"); err != nil || got != n.Version {
		t.Errorf("Version = %q, %v; want %q", got, err, n.Version)
	}
}

func TestMatchVersion(t *testing.T) {
	const v = `"abc"`
	tests := []struct {
		ifMatch string
		want    bool
	}{
		{`"abc"`, true},
		{`abc`, true},
		{`*`, true},
		{`"x", "abc"`, true},
		{`"x"`, false},
		{`W/"abc"`, false},
		{``, false},
	}
	for _, tt := range tests {
		if got := MatchVersion(tt.ifMatch, v); got != tt.want {
			t.Errorf("MatchVersion(%q) = %v, want %v", tt.ifMatch, got, tt.want)
		}
	}
}

func TestNotebooks(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(&Note{ID: "a", Notebook: "work", Title: "A"}); err != nil {
		t.Fatal(err)
	}
	// IDs are unique across notebooks.
	if err := s.Create(&Note{ID: "a", Notebook: Inbox, Title: "A2"}); err != ErrExists {
		t.Errorf("Create with an ID used in another notebook = %v, want ErrExists", err)
	}
	if err := s.Create(&Note{ID: "b", Notebook: "Bad Name", Title: "B"}); err != ErrInvalidNotebook {
		t.Errorf("Create in an invalid notebook = %v, want ErrInvalidNotebook", err)
	}
	nbs, err := s.Notebooks()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{Inbox, Trash, "work"}; !slices.Equal(nbs, want) {
		t.Errorf("Notebooks = %v, want %v", nbs, want)
	}

	if err := s.Move("a", "home"); err != nil {
		t.Fatal(err)
	}
	n, err := s.Load("a")
	if err != nil {
		t.Fatal(err)
	}
	if n.Notebook != "home" {
		t.Errorf("notebook after Move = %q, want home", n.Notebook)
	}

	// The trashed_from key is written only while set.
	n.TrashedFrom = "home"
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "home", "a.yaml")); !strings.Contains(string(b), "trashed_from: home") {
		t.Errorf("trashed_from not written:\n%s", b)
	}
	n.TrashedFrom = ""
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "home", "a.yaml")); strings.Contains(string(b), "trashed_from") {
		t.Errorf("trashed_from not removed:\n%s", b)
	}

	// A duplicate made outside the store is reported.
	if err := os.WriteFile(filepath.Join(dir, Inbox, "a.yaml"), []byte("title: dup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("a"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("Load of a duplicate = %v, want ErrDuplicate", err)
	}
}

func TestMigrateFlat(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"old.yaml", "Not-An-ID.yaml", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("title: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := s.MigrateFlat()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(moved, []string{"old"}) {
		t.Errorf("moved = %v, want [old]", moved)
	}
	if n, err := s.Load("old"); err != nil || n.Notebook != Inbox {
		t.Errorf("Load(old) = %+v, %v", n, err)
	}
}

func TestNotebookMeta(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m, err := s.NotebookMeta("work"); err != nil || m != (NotebookMeta{}) {
		t.Fatalf("NotebookMeta of a missing notebook = %+v, %v", m, err)
	}
	// Saving creates the notebook, which then is listed even without notes.
	if err := s.SaveNotebookMeta("work", NotebookMeta{Name: "仕事", Order: 10}); err != nil {
		t.Fatal(err)
	}
	if nbs, _ := s.Notebooks(); !slices.Contains(nbs, "work") {
		t.Errorf("Notebooks = %v, want work included", nbs)
	}
	if m, err := s.NotebookMeta("work"); err != nil || m != (NotebookMeta{Name: "仕事", Order: 10}) {
		t.Errorf("NotebookMeta = %+v, %v", m, err)
	}
	// Comments and unknown keys survive; zero values remove their keys.
	p := filepath.Join(dir, "work", NotebookFile)
	if err := os.WriteFile(p, []byte("# my notebook\nname: 仕事 # display\norder: 10\ncolor: red\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNotebookMeta("work", NotebookMeta{Name: "しごと"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if want := "# my notebook\nname: しごと # display\ncolor: red\n"; string(b) != want {
		t.Errorf("file = %q, want %q", b, want)
	}
	// A file emptied to "{}" goes back to the block style when keys are added.
	if err := s.SaveNotebookMeta("empty", NotebookMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNotebookMeta("empty", NotebookMeta{Order: 30}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "empty", NotebookFile)); string(b) != "order: 30\n" {
		t.Errorf("file = %q, want %q", b, "order: 30\n")
	}

	// The settings file is not a note.
	if files, _ := s.Scan(); len(files) != 0 {
		t.Errorf("Scan = %+v, want no notes", files)
	}
}

func TestNoteOrder(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := `title: メモ
tags: []
created_at: 2026-10-01T09:00:00Z
updated_at: 2026-10-01T09:00:00Z
body: |
  本文
extra: 残すキー
`
	p := filepath.Join(dir, Inbox, "memo.yaml")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := s.Load("memo")
	if err != nil {
		t.Fatal(err)
	}
	// Without an order, the key is the creation time in milliseconds.
	if want := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC).UnixMilli(); n.Order != nil || n.SortKey() != want {
		t.Fatalf("Order = %v, SortKey = %d, want nil, %d", n.Order, n.SortKey(), want)
	}
	// A new order goes before the body, not after it.
	order := int64(1234)
	n.Order = &order
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := `title: メモ
tags: []
created_at: 2026-10-01T09:00:00Z
updated_at: 2026-10-01T09:00:00Z
order: 1234
body: |
  本文
extra: 残すキー
`
	if string(b) != want {
		t.Errorf("got:\n%s\nwant:\n%s", b, want)
	}
	if got, _ := s.Load("memo"); got.Order == nil || *got.Order != 1234 || got.SortKey() != 1234 {
		t.Errorf("round trip Order = %v", got.Order)
	}
	// Clearing the order removes the key.
	n.Order = nil
	if err := s.Save(n); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "order:") {
		t.Errorf("order kept:\n%s", b)
	}
}

func TestNotebookSort(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNotebookMeta("work", NotebookMeta{Sort: SortTitle}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "work", NotebookFile)
	if b, _ := os.ReadFile(p); string(b) != "sort: title\n" {
		t.Errorf("file = %q", b)
	}
	if m, _ := s.NotebookMeta("work"); m.Sort != SortTitle {
		t.Errorf("Sort = %q, want %q", m.Sort, SortTitle)
	}
	// The default order is not written; an unknown sort reads as the default.
	if err := s.SaveNotebookMeta("work", NotebookMeta{Sort: SortOrder}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{}\n" {
		t.Errorf("file = %q, want {}", b)
	}
	if err := os.WriteFile(p, []byte("sort: random\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.NotebookMeta("work"); m.Sort != "" {
		t.Errorf("Sort of an unknown value = %q, want empty", m.Sort)
	}
}

func TestDropKind(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Inbox may be of the drop kind; the trash keeps the default.
	for nb, want := range map[string]string{Inbox: KindDrop, "work": KindDrop, Trash: ""} {
		if err := os.MkdirAll(filepath.Join(dir, nb), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, nb, NotebookFile), []byte("kind: drop\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if m, _ := s.NotebookMeta(nb); m.Kind != want {
			t.Errorf("kind of %s = %q, want %q", nb, m.Kind, want)
		}
	}
	// CreatedBy goes before the body, and survives a round trip.
	n := &Note{ID: "n1", Notebook: Inbox, Title: "t", Body: "b\n", CreatedBy: CreatedByAI}
	if err := s.Create(n); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, Inbox, "n1"+Ext))
	if !strings.Contains(string(b), "created_by: ai\nbody: |\n") {
		t.Errorf("file:\n%s", b)
	}
	got, err := s.Load("n1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedBy != CreatedByAI {
		t.Errorf("CreatedBy = %q", got.CreatedBy)
	}
}

func TestInboxDefaultKind(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, Inbox, NotebookFile)
	os.Remove(p)
	if m, _ := s.NotebookMeta(Inbox); m.Kind != KindDrop {
		t.Errorf("kind of the inbox without settings = %q, want drop", m.Kind)
	}
	// Made default, the inbox says so in its file; drop is not written.
	if err := s.SaveNotebookMeta(Inbox, NotebookMeta{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "kind: default\n" {
		t.Errorf("file = %q", b)
	}
	if m, _ := s.NotebookMeta(Inbox); m.Kind != "" {
		t.Errorf("kind = %q, want the default", m.Kind)
	}
	if err := s.SaveNotebookMeta(Inbox, NotebookMeta{Kind: KindDrop}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{}\n" {
		t.Errorf("file = %q, want no kind", b)
	}
	// Other notebooks stay default without a kind; collab is not for the inbox,
	// which falls back to drop.
	if m, _ := s.NotebookMeta("work"); m.Kind != "" {
		t.Errorf("kind of work = %q", m.Kind)
	}
	if err := os.WriteFile(p, []byte("kind: collab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.NotebookMeta(Inbox); m.Kind != KindDrop {
		t.Errorf("kind of a collab inbox = %q, want drop", m.Kind)
	}
}

func TestCreateWithExtra(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	var fm yaml.Node
	if err := yaml.Unmarshal([]byte("author: me # comment\ncreated_by: person\nbody: x\naliases: [a]\n"), &fm); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	n := &Note{ID: "imp", Notebook: Inbox, Title: "取り込み", CreatedAt: at, UpdatedAt: at, Body: "本文\n",
		ImportedFrom: "memo.md", ImportHash: "sha256:abc", Extra: fm.Content[0].Content}
	if err := s.Create(n); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, Inbox, "imp.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// The keys of the note itself in Extra are ignored.
	want := `title: 取り込み
tags: []
created_at: 2026-04-01T00:00:00Z
updated_at: 2026-04-01T00:00:00Z
author: me # comment
aliases: [a]
imported_from: memo.md
import_hash: sha256:abc
body: |
  本文
`
	if got := string(b); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got, err := s.Load("imp")
	if err != nil || got.ImportedFrom != "memo.md" || got.ImportHash != "sha256:abc" || got.CreatedBy != "" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	// Saving keeps the keys.
	got.Body = "変更\n"
	if err := s.Save(got); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, Inbox, "imp.yaml"))
	for _, k := range []string{"author: me # comment", "imported_from: memo.md", "import_hash: sha256:abc"} {
		if !strings.Contains(string(b), k) {
			t.Errorf("after Save, %q is missing:\n%s", k, b)
		}
	}
}

func TestConflictMarkers(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	merged := "title: A\nbody: |\n<<<<<<< HEAD\n  mine\n=======\n  theirs\n>>>>>>> origin/main\n"
	if err := os.WriteFile(filepath.Join(dir, Inbox, "a.yaml"), []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("a"); !errors.Is(err, ErrConflict) {
		t.Errorf("err = %v", err)
	}
	// Markers in a body are indented, and not conflicts.
	body := "title: B\nbody: |\n  <<<<<<< HEAD\n  >>>>>>> x\n"
	if err := os.WriteFile(filepath.Join(dir, Inbox, "b.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Load("b"); err != nil || !strings.Contains(n.Body, "<<<<<<<") {
		t.Errorf("b: %+v, %v", n, err)
	}
}

func TestIsNotePath(t *testing.T) {
	for p, want := range map[string]bool{
		"inbox/a.yaml":         true,
		"trash/a.yaml":         true,
		"trash/dev/a.yaml":     true,
		"dev/_notebook.yaml":   false,
		"_ebira.yaml":          false,
		"a.yaml":               false,
		"dev/a.assets/x.yaml":  false,
		"dev/sub/a.yaml":       true, // a sub notebook
		"Dev/a.yaml":           false,
		"dev/a.yml":            false,
		"trash/dev/sub/a.yaml": true,
		"trash/dev~sub/a.yaml": true, // a sub notebook deleted on its own
		"dev/sub/x/a.yaml":     false,
		"inbox/sub/a.yaml":     false,
		"dev~sub/a.yaml":       false,
		"trash/dev~/a.yaml":    false,
	} {
		if got := IsNotePath(p); got != want {
			t.Errorf("IsNotePath(%q) = %v", p, got)
		}
	}
}

func TestConflictCopy(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	theirs := "# kept\ntitle: Plan\ntags: [a]\ncreated_at: 2026-10-18T10:00:00Z\nupdated_at: 2026-10-18T11:00:00Z\nmine: 1\nbody: |\n  theirs\n"
	p, err := s.ConflictCopy("dev/x.yaml", []byte(theirs), "conflict 2026-10-18 20:00")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSuffix(filepath.Base(p), Ext)
	if filepath.Dir(p) != "dev" || !ValidID(id) || id == "x" {
		t.Fatalf("path %s", p)
	}
	n, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Plan (conflict 2026-10-18 20:00)" || n.Body != "theirs\n" || n.Notebook != "dev" || !slices.Equal(n.Tags, []string{"a"}) {
		t.Errorf("copy: %+v", n)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "dev", id+Ext))
	if !strings.Contains(string(b), "# kept") || !strings.Contains(string(b), "mine: 1") {
		t.Errorf("comments or keys lost:\n%s", b)
	}
	if _, err := s.ConflictCopy("dev/_notebook.yaml", []byte(theirs), "c"); err == nil {
		t.Error("not a note: no error")
	}
}
