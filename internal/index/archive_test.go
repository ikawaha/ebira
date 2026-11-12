package index

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/store"
)

// The archive is left out unless asked for: the notes put there on their own
// and those of the notebooks there, which the index is given.
func TestArchive(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for _, n := range []*store.Note{
		{ID: "live", Notebook: "work", Title: "りんご", Tags: []string{"fruit"}, CreatedAt: old, UpdatedAt: old},
		{ID: "put", Notebook: "work", Title: "りんご", Tags: []string{"fruit"}, CreatedAt: now, UpdatedAt: now, ArchivedAt: &now},
		{ID: "past", Notebook: "past", Title: "りんご", Tags: []string{"fruit"}, CreatedAt: old, UpdatedAt: old},
		{ID: "gone", Notebook: store.Trash, Title: "りんご", CreatedAt: old, UpdatedAt: old, ArchivedAt: &now},
	} {
		if err := st.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := Open(ctx, filepath.Join(dir, "index.sqlite"), Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	ids := func(ss []*Summary) []string {
		var r []string
		for _, s := range ss {
			r = append(r, s.ID)
		}
		slices.Sort(r)
		return r
	}
	past := []string{"past"}
	for _, tt := range []struct {
		mode     string
		notebook string
		want     []string
	}{
		{ArchiveExclude, "", []string{"live"}},
		{"", "", []string{"live"}}, // the default
		{ArchiveInclude, "", []string{"live", "past", "put"}},
		{ArchiveOnly, "", []string{"past", "put"}},
		{ArchiveExclude, "work", []string{"live"}},
		{ArchiveOnly, "work", []string{"put"}},
		{ArchiveInclude, "past", []string{"past"}},
		{ArchiveExclude, "past", nil},
		{ArchiveExclude, store.Trash, []string{"gone"}}, // the trash is not in the archive
	} {
		a := Archive{Mode: tt.mode, Notebooks: past}
		list, total, err := ix.List(ctx, tt.notebook, "", "", a, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(list); !slices.Equal(got, tt.want) || total != len(tt.want) {
			t.Errorf("List(%q, %s) = %v (%d), want %v", tt.notebook, tt.mode, got, total, tt.want)
		}
		if tt.notebook == store.Trash {
			continue
		}
		found, _, err := ix.Search(ctx, "りんご", tt.notebook, "", a, 10)
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(found); !slices.Equal(got, tt.want) {
			t.Errorf("Search(%q, %s) = %v, want %v", tt.notebook, tt.mode, got, tt.want)
		}
	}
	// in:archive asks for the archive only, with terms or alone.
	for _, q := range []string{"りんご in:archive", "IN:ARCHIVE", "in:archive tag:fruit"} {
		found, _, err := ix.Search(ctx, q, "", "", Archive{Notebooks: past}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(found); !slices.Equal(got, []string{"past", "put"}) {
			t.Errorf("Search(%q) = %v", q, got)
		}
	}
	if found, _, _ := ix.Search(ctx, "りんご -in:archive", "", "", Archive{Notebooks: past}, 10); !slices.Equal(ids(found), []string{"live"}) {
		t.Errorf("-in:archive = %v", ids(found))
	}
	tags, err := ix.Tags(ctx, "", Archive{Notebooks: past})
	if err != nil || len(tags) != 1 || tags[0].Count != 1 {
		t.Errorf("Tags = %v, %v", tags, err)
	}
	counts, err := ix.Counts(ctx)
	if err != nil || counts["work"] != (Count{Notes: 1, Archived: 1}) || counts["past"] != (Count{Notes: 1}) {
		t.Errorf("Counts = %v, %v", counts, err)
	}
	// Notes not updated since a time, but those in the archive and the trash.
	stale, total, err := ix.Stale(ctx, now, Archive{Notebooks: past}, 10, 0)
	if err != nil || !slices.Equal(ids(stale), []string{"live"}) || total != 1 {
		t.Errorf("Stale = %v (%d), %v", ids(stale), total, err)
	}
	if list, _, _ := ix.List(ctx, "work", "", "", Archive{Mode: ArchiveOnly}, 10, 0); len(list) != 1 || !list[0].Archived {
		t.Errorf("Archived is not set: %+v", list)
	}
}
