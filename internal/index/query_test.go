package index

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/store"
)

func TestParseQuery(t *testing.T) {
	for _, tt := range []struct {
		q     string
		terms []string
		tags  []string
		marks []string
	}{
		{q: "東京 天気", terms: []string{"東京", "天気"}},
		{q: `"New York" pizza`, terms: []string{"New York", "pizza"}},
		{q: `"error handling`, terms: []string{"error handling"}}, // still being typed
		{q: `""  "  "`, terms: nil},
		{q: "tag:todo 雨", terms: []string{"雨"}, tags: []string{"todo"}},
		{q: `TAG:"two words" tag:go`, tags: []string{"two words", "go"}},
		{q: "tag: 雨", terms: []string{"雨"}}, // a tag being typed
		{q: "a:b tags:x", terms: []string{"a:b", "tags:x"}},
		{q: `say"hi"`, terms: []string{`say"hi"`}}, // quotes only start a phrase
		{q: `mark:red Mark:"あとで" 雨 tag:x`, terms: []string{"雨"}, tags: []string{"x"}, marks: []string{"red", "あとで"}},
	} {
		got := ParseQuery(tt.q)
		if !reflect.DeepEqual(got.Terms, tt.terms) || !reflect.DeepEqual(got.Tags, tt.tags) || !reflect.DeepEqual(got.Marks, tt.marks) {
			t.Errorf("ParseQuery(%q) = %q, %q, %q; want %q, %q, %q", tt.q, got.Terms, got.Tags, got.Marks, tt.terms, tt.tags, tt.marks)
		}
	}
}

func TestSearchPhraseAndTags(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for i, n := range []*store.Note{
		{ID: "ny", Title: "Trip", Tags: []string{"Travel", "todo"}, Body: "Pizza in New York."},
		{ID: "apart", Title: "Words", Tags: []string{"todo"}, Body: "York is old, and New Delhi is new."},
		{ID: "ja", Title: "東京の天気", Tags: []string{"ｔｒａｖｅｌ"}, Body: "東京 タワーに登った。"},
	} {
		n.Notebook, n.CreatedAt, n.UpdatedAt = store.Inbox, now, now.Add(time.Duration(i)*time.Hour)
		if err := st.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		q    string
		want []string
	}{
		{q: "new york", want: []string{"apart", "ny"}},
		{q: `"new york"`, want: []string{"ny"}}, // in that order, next to each other
		{q: `"東京 タワー"`, want: []string{"ja"}},   // a phrase with a space
		{q: "tag:todo york", want: []string{"apart", "ny"}},
		{q: `tag:todo "new york"`, want: []string{"ny"}},
		{q: "tag:todo tag:travel", want: []string{"ny"}}, // tags alone, all of them
		{q: "tag:TRAVEL", want: []string{"ja", "ny"}},    // case and width do not matter
		{q: "tag:nothing york", want: nil},
	}
	db := filepath.Join(dir, "index.sqlite")
	for _, mode := range []Mode{ModeLike, ModeBigram, ModeTrigram, ModeEn, ModeJa} {
		t.Run(string(mode), func(t *testing.T) {
			ix, err := Open(ctx, db, Config{Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			defer ix.Close()
			if _, err := ix.Sync(ctx, st); err != nil {
				t.Fatal(err)
			}
			for _, tt := range tests {
				res, total, err := ix.Search(ctx, tt.q, "", "", 10)
				if err != nil {
					t.Fatalf("Search(%q): %v", tt.q, err)
				}
				var got []string
				for _, s := range res {
					got = append(got, s.ID)
				}
				slices.Sort(got)
				if !slices.Equal(got, tt.want) || total != len(tt.want) {
					t.Errorf("Search(%q) = %v (total %d), want %v", tt.q, got, total, tt.want)
				}
			}
			// Tags alone list the notes last updated first.
			res, _, err := ix.Search(ctx, "tag:travel", "", "", 10)
			if err != nil || len(res) != 2 || res[0].ID != "ja" {
				t.Errorf("tags alone: %v, %v; want ja first", res, err)
			}
			// The phrase is highlighted as one; the tag is not looked for in
			// the text.
			_, body := ix.Highlight(`"new york" tag:todo`, "", "Pizza in New York. todo")
			if len(body) != 1 || body[0] != (Span{9, 17}) {
				t.Errorf("Highlight = %v, want New York only", body)
			}
		})
	}
}

func TestSearchSubNotebooks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	// axb would match a_b in LIKE, where _ is a wildcard.
	for _, n := range []*store.Note{
		{ID: "top", Notebook: "a_b", Tags: []string{"log"}, Body: "雨"},
		{ID: "sub", Notebook: "a_b/s", Tags: []string{"log", "done"}, Body: "雨"},
		{ID: "hid", Notebook: "a_b/h", Tags: []string{"log", "secret"}, Body: "雨"},
		{ID: "other", Notebook: "axb", Tags: []string{"log"}, Body: "雨"},
	} {
		n.CreatedAt, n.UpdatedAt = now, now
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
	for _, tt := range []struct {
		q, notebook string
		exclude     []string
		want        []string
	}{
		{q: "tag:log", notebook: "a_b", want: []string{"hid", "sub", "top"}},
		{q: "雨", notebook: "a_b", want: []string{"hid", "sub", "top"}},
		{q: "tag:log", notebook: "a_b", exclude: []string{"trash", "a_b/h"}, want: []string{"sub", "top"}},
		{q: "tag:log", notebook: "a_b/s", want: []string{"sub"}},
	} {
		res, total, err := ix.Search(ctx, tt.q, tt.notebook, "", 10, tt.exclude...)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range res {
			got = append(got, s.ID)
		}
		slices.Sort(got)
		if !slices.Equal(got, tt.want) || total != len(tt.want) {
			t.Errorf("Search(%q, %q, exclude %v) = %v (total %d), want %v", tt.q, tt.notebook, tt.exclude, got, total, tt.want)
		}
	}
	tags, err := ix.Tags(ctx, "a_b", "trash", "a_b/h")
	if err != nil {
		t.Fatal(err)
	}
	want := []TagCount{{"done", 1}, {"log", 2}}
	if !slices.Equal(tags, want) {
		t.Errorf("Tags(a_b) = %v, want %v", tags, want)
	}
}
