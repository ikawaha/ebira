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
		// Minus leaves notes out: terms, phrases, tags and marks.
		{q: "york -pizza", want: []string{"apart"}},
		{q: `york -"new york"`, want: []string{"apart"}},
		{q: "-york", want: []string{"ja"}}, // left out alone, all the others
		{q: "-東京", want: []string{"apart", "ny"}},
		{q: "-tag:TRAVEL", want: []string{"apart"}},
		{q: "tag:todo -tag:travel", want: []string{"apart"}},
		{q: "york -tag:nothing -mark:red", want: []string{"apart", "ny"}}, // nothing to leave out
		{q: "york -", want: []string{"apart", "ny"}},                      // a minus alone, as while typing
		// OR joins two, closer than the AND of spaces.
		{q: "pizza OR delhi", want: []string{"apart", "ny"}},
		{q: `new pizza OR 東京`, want: []string{"ny"}},
		{q: `"new york" OR 東京`, want: []string{"ja", "ny"}},
		{q: "tag:travel OR delhi", want: []string{"apart", "ja", "ny"}},
		{q: "tag:nothing OR mark:red OR 東京", want: []string{"ja"}},
		{q: "york OR 京", want: []string{"apart", "ja", "ny"}}, // a term with no tokens, with LIKE
		{q: "york OR 東京 -pizza", want: []string{"apart", "ja"}},
		{q: "pizza OR -york", want: nil}, // not with a minus: pizza and not york
		{q: "OR pizza OR", want: []string{"ny"}},
		{q: "pizza or delhi", want: nil}, // or in lower case is a word
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
			// Either of those joined with OR is highlighted.
			_, body = ix.Highlight("pizza OR delhi", "", "Pizza or Delhi")
			if len(body) != 2 {
				t.Errorf("Highlight with OR = %v, want both", body)
			}
		})
	}
}

func TestSearchShortPart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	for _, n := range []*store.Note{
		{ID: "no", Title: "a", Body: "ノートの編集をする"},
		{ID: "space", Title: "b", Body: "ノート 編集"},
		{ID: "joined", Title: "c", Body: "ノート編集"},
		{ID: "write", Title: "d", Body: "ノートを書く"},
		{ID: "c", Title: "e", Body: "C 言語の本"},
		{ID: "go", Title: "f", Body: "Go 言語の本"},
	} {
		n.Notebook, n.CreatedAt, n.UpdatedAt = store.Inbox, now, now
		if err := st.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	// A phrase with a part too short for an n-gram, such as a word of two
	// characters in the trigram mode, is searched with LIKE, not without the
	// part. The ja mode drops particles, so it finds the phrase around them.
	for _, tt := range []struct {
		mode Mode
		want map[string][]string
	}{
		{ModeLike, map[string][]string{`"ノート 編集"`: {"space"}, `"c 言語"`: {"c"}}},
		{ModeBigram, map[string][]string{`"ノート 編集"`: {"space"}, `"c 言語"`: {"c"}}},
		{ModeTrigram, map[string][]string{`"ノート 編集"`: {"space"}, `"c 言語"`: {"c"}}},
		{ModeEn, map[string][]string{`"ノート 編集"`: {"space"}, `"c 言語"`: {"c"}, `"ノート 書"`: nil}},
		{ModeJa, map[string][]string{`"ノート 編集"`: {"joined", "no", "space"}}},
	} {
		t.Run(string(tt.mode), func(t *testing.T) {
			ix, err := Open(ctx, filepath.Join(dir, string(tt.mode)+".sqlite"), Config{Mode: tt.mode})
			if err != nil {
				t.Fatal(err)
			}
			defer ix.Close()
			if _, err := ix.Sync(ctx, st); err != nil {
				t.Fatal(err)
			}
			for q, want := range tt.want {
				res, _, err := ix.Search(ctx, q, "", "", 10)
				if err != nil {
					t.Fatalf("Search(%q): %v", q, err)
				}
				var got []string
				for _, s := range res {
					got = append(got, s.ID)
				}
				slices.Sort(got)
				if !slices.Equal(got, want) {
					t.Errorf("Search(%q) = %v, want %v", q, got, want)
				}
			}
			if tt.mode == ModeJa {
				return
			}
			_, body := ix.Highlight(`"ノート 編集"`, "", "ノートを書く。ノート 編集")
			if len(body) != 1 || body[0] != (Span{7, 13}) {
				t.Errorf("Highlight = %v, want the phrase only", body)
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

func TestParseQueryNot(t *testing.T) {
	got := ParseQuery(`a -b -"c d" -tag:e -TAG:"f g" -mark:red - x-y -`)
	want := Query{
		Terms:    []string{"a", "x-y"},
		NotTerms: []string{"b", "c d"},
		NotTags:  []string{"e", "f g"},
		NotMarks: []string{"red"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseQuery = %+v, want %+v", got, want)
	}
}

func TestParseQueryOr(t *testing.T) {
	for _, tt := range []struct {
		q    string
		want Query
	}{
		{q: `a OR "b c" OR tag:d OR mark:red e`, want: Query{
			Terms: []string{"e"},
			Or:    [][]Alt{{{AltTerm, "a"}, {AltTerm, "b c"}, {AltTag, "d"}, {AltMark, "red"}}},
		}},
		{q: "a OR b c OR d", want: Query{Or: [][]Alt{{{AltTerm, "a"}, {AltTerm, "b"}}, {{AltTerm, "c"}, {AltTerm, "d"}}}}},
		// An OR at an end, after another or next to a minus is dropped.
		{q: "OR a OR OR b OR", want: Query{Or: [][]Alt{{{AltTerm, "a"}, {AltTerm, "b"}}}}},
		{q: "a OR -b OR c", want: Query{Terms: []string{"a", "c"}, NotTerms: []string{"b"}}},
		{q: `a or b "OR" -OR`, want: Query{Terms: []string{"a", "or", "b", "OR"}, NotTerms: []string{"OR"}}},
		{q: "a OR tag:", want: Query{Terms: []string{"a"}}}, // a tag being typed
	} {
		if got := ParseQuery(tt.q); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseQuery(%q) = %+v, want %+v", tt.q, got, tt.want)
		}
	}
}
