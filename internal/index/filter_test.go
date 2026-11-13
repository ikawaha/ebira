package index

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/store"
)

// A filter lists the notes matching a query as a search finds them, but in the
// order of the list, and only those of the notebook, not of its sub notebooks.
func TestFilter(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []Mode{ModeLike, ModeBigram, ModeJa, ModeEn} {
		t.Run(string(mode), func(t *testing.T) {
			dir := t.TempDir()
			st, err := store.New(filepath.Join(dir, "notes"))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
			order := int64(100)
			for _, n := range []*store.Note{
				{ID: "a", Notebook: "work", Title: "雨の日の予定", Tags: []string{"renovate"}},
				{ID: "b", Notebook: "work", Title: "晴れの日の予定", Tags: []string{"todo"}, Marks: []string{"red"}},
				{ID: "c", Notebook: "work", Title: "雨漏りを直す", Tags: []string{"renovate", "todo"}},
				{ID: "d", Notebook: "work", Title: "pizza dinner"},
				{ID: "e", Notebook: "work", Title: "雨の買い物", ArchivedAt: &now},
				{ID: "s", Notebook: "work/sub", Title: "雨の日の予定"},
			} {
				n.CreatedAt, n.UpdatedAt = now, now
				order -= 10
				o := order
				n.Order = &o
				if err := st.Create(n); err != nil {
					t.Fatal(err)
				}
			}
			ix, err := Open(ctx, filepath.Join(dir, "index.sqlite"), Config{Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			defer ix.Close()
			if _, err := ix.Sync(ctx, st); err != nil {
				t.Fatal(err)
			}
			for _, tt := range []struct {
				q    string
				a    Archive
				want []string
			}{
				{"", Archive{}, []string{"a", "b", "c", "d"}},
				{"-tag:renovate", Archive{}, []string{"b", "d"}},
				{"tag:todo", Archive{}, []string{"b", "c"}},
				{"予定", Archive{}, []string{"a", "b"}},
				{"雨 -tag:todo", Archive{}, []string{"a"}},
				{"mark:red OR pizza", Archive{}, []string{"b", "d"}},
				{"-mark:red", Archive{}, []string{"a", "c", "d"}},
				{"雨", Archive{Mode: ArchiveInclude}, []string{"a", "c", "e"}},
				{"in:archive", Archive{}, []string{"e"}},
				{"none", Archive{}, nil},
			} {
				res, total, err := ix.Filter(ctx, tt.q, "work", "", store.SortOrder, tt.a, 10, 0)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, s := range res {
					got = append(got, s.ID)
				}
				// The order of the list, not of relevance.
				if !slices.Equal(got, tt.want) || total != len(tt.want) {
					t.Errorf("Filter(%q) = %v (total %d), want %v", tt.q, got, total, tt.want)
				}
				m, err := ix.Matching(ctx, tt.q, "work", tt.a)
				if err != nil {
					t.Fatal(err)
				}
				if len(m) != len(tt.want) {
					t.Errorf("Matching(%q) = %v, want %v", tt.q, m, tt.want)
				}
			}
		})
	}
}
