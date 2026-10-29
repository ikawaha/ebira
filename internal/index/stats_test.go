package index

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/store"
)

func TestStats(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b"} {
		if err := st.Create(&store.Note{ID: id, Notebook: store.Inbox, Title: "東京の天気", Body: "雨", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "index.sqlite")
	ix, err := Open(ctx, path, Config{Mode: ModeJa})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}

	s, err := ix.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Path != path || s.Mode != ModeJa || s.Notes != 2 || s.SchemaVersion != schemaVersion || s.JournalMode != "wal" {
		t.Errorf("stats = %+v", s)
	}
	// The database file is behind the WAL until a checkpoint, so its size
	// is not the pages.
	if s.PageSize <= 0 || s.Pages <= 0 || s.Files["db"] <= 0 {
		t.Errorf("pages: size %d, count %d, files %v", s.PageSize, s.Pages, s.Files)
	}
	if _, ok := s.Files["wal"]; !ok {
		t.Errorf("files = %v, want the WAL file", s.Files)
	}
	names := map[string]bool{}
	for i, tb := range s.Tables {
		names[tb.Name] = true
		if i > 0 && tb.Bytes > s.Tables[i-1].Bytes {
			t.Errorf("tables not largest first: %v", s.Tables)
		}
	}
	for _, n := range []string{"notes", "notes_fts_data", "notes_bigram_data"} {
		if !names[n] {
			t.Errorf("tables = %v, want %s", s.Tables, n)
		}
	}
	if s.Pool.Open != 1 {
		t.Errorf("pool = %+v", s.Pool)
	}
}
