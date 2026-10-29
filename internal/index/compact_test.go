package index

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/store"
)

func TestCompact(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := Open(ctx, filepath.Join(dir, "index.sqlite"), Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	// A small index is left as it is.
	if freed, err := ix.Compact(ctx); err != nil || freed != 0 {
		t.Fatalf("Compact of an empty index = %d, %v", freed, err)
	}

	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	var ids []string
	for i := range 40 {
		id := fmt.Sprintf("n%02d", i)
		ids = append(ids, id)
		body := strings.Repeat(fmt.Sprintf("東京の天気 %d は雨。", i), 1000)
		if err := st.Create(&store.Note{ID: id, Notebook: store.Inbox, Title: id, Body: body, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[1:] {
		if err := st.Delete(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	before, err := ix.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.FreePages*before.PageSize < compactMin {
		t.Fatalf("free pages = %d of %d, want enough to compact", before.FreePages, before.Pages)
	}

	freed, err := ix.Compact(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ix.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if freed != (before.Pages-after.Pages)*after.PageSize || freed < compactMin {
		t.Errorf("freed %d bytes, pages %d -> %d", freed, before.Pages, after.Pages)
	}
	if after.FreePages != 0 || after.Files["wal"] != 0 || after.Files["db"] != after.Pages*after.PageSize {
		t.Errorf("after Compact: %d free pages, files %v, %d pages", after.FreePages, after.Files, after.Pages)
	}
	// The index still finds the note left.
	if _, total, err := ix.Search(ctx, "天気", "", "", 10); err != nil || total != 1 {
		t.Errorf("search after Compact: total %d, %v, want 1", total, err)
	}
	if freed, err := ix.Compact(ctx); err != nil || freed != 0 {
		t.Errorf("second Compact = %d, %v, want nothing to do", freed, err)
	}
}
