package index

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/store"
)

func TestSearch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for _, n := range []*store.Note{
		{ID: "tokyo", Notebook: store.Inbox, Title: "東京都の天気", Tags: []string{"weather"}, Body: "今日は雨が降りました。", CreatedAt: now, UpdatedAt: now},
		{ID: "go", Notebook: "dev", Title: "Go のヒント", Tags: []string{"go"}, Body: "goroutine を京都で書いた。", CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	} {
		if err := st.Create(n); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		q    string
		tag  string
		want map[Mode][]string
	}{
		{q: "東京都", want: map[Mode][]string{ModeLike: {"tokyo"}, ModeBigram: {"tokyo"}, ModeJa: {"tokyo"}, ModeTrigram: {"tokyo"}, ModeEn: {"tokyo"}}},
		// Substring search finds 京都 inside 東京都. Morphemes do not, but the
		// ja mode also searches its bigram table.
		{q: "京都", want: map[Mode][]string{ModeLike: {"go", "tokyo"}, ModeBigram: {"go", "tokyo"}, ModeJa: {"go", "tokyo"}, ModeTrigram: {"go", "tokyo"}, ModeEn: {"go", "tokyo"}}},
		{q: "GOROUTINE", want: map[Mode][]string{ModeLike: {"go"}, ModeBigram: {"go"}, ModeJa: {"go"}, ModeTrigram: {"go"}, ModeEn: {"go"}}},
		{q: "雨 天気", want: map[Mode][]string{ModeLike: {"tokyo"}, ModeBigram: {"tokyo"}, ModeJa: {"tokyo"}, ModeTrigram: {"tokyo"}, ModeEn: {"tokyo"}}},
		{q: "雨", want: map[Mode][]string{ModeLike: {"tokyo"}, ModeBigram: {"tokyo"}, ModeJa: {"tokyo"}, ModeTrigram: {"tokyo"}, ModeEn: {"tokyo"}}},
		{q: "rout", want: map[Mode][]string{ModeLike: {"go"}, ModeBigram: {"go"}, ModeJa: {"go"}, ModeTrigram: {"go"}, ModeEn: {"go"}}},
		// NFKC: full-width terms match half-width text.
		{q: "ＧＯＲＯＵＴＩＮＥ", want: map[Mode][]string{ModeLike: {}, ModeBigram: {"go"}, ModeJa: {"go"}, ModeTrigram: {"go"}, ModeEn: {"go"}}},
		// Base form: 書く matches 書いた.
		{q: "書く", want: map[Mode][]string{ModeLike: {}, ModeBigram: {}, ModeJa: {"go"}, ModeTrigram: {}, ModeEn: {}}},
		// A term made only of stop words is searched with LIKE instead of matching everything.
		{q: "の", want: map[Mode][]string{ModeLike: {"go", "tokyo"}, ModeBigram: {"go", "tokyo"}, ModeJa: {"go", "tokyo"}, ModeTrigram: {"go", "tokyo"}, ModeEn: {"go", "tokyo"}}},
		{q: "の 雨", want: map[Mode][]string{ModeLike: {"tokyo"}, ModeBigram: {"tokyo"}, ModeJa: {"tokyo"}, ModeTrigram: {"tokyo"}, ModeEn: {"tokyo"}}},
		{q: "京都", tag: "weather", want: map[Mode][]string{ModeLike: {"tokyo"}, ModeBigram: {"tokyo"}, ModeJa: {"tokyo"}, ModeTrigram: {"tokyo"}, ModeEn: {"tokyo"}}},
	}
	// Reopen the same database with each mode to check that the full-text table is rebuilt.
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
				res, total, err := ix.Search(ctx, tt.q, "", tt.tag, 10)
				if err != nil {
					t.Fatalf("Search(%q): %v", tt.q, err)
				}
				var got []string
				for _, s := range res {
					got = append(got, s.ID)
				}
				slices.Sort(got)
				want := tt.want[mode]
				if !slices.Equal(got, want) || total != len(want) {
					t.Errorf("Search(%q, tag=%q) = %v (total %d), want %v", tt.q, tt.tag, got, total, want)
				}
			}
		})
	}
}

func TestSync(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes")
	st, err := store.New(notes)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := Open(ctx, filepath.Join(dir, "index.sqlite"), Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	write := func(name, body string, mtime time.Time) {
		p := filepath.Join(notes, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now().Add(-time.Hour)
	write("inbox/a.yaml", "title: A\nupdated_at: 2026-10-01T00:00:00Z\nbody: りんご\n", t0)
	write("work/b.yaml", "title: B\nupdated_at: 2026-10-01T00:00:00Z\nbody: みかん\n", t0)
	write("inbox/broken.yaml", "title: [\n", t0)
	write("inbox/Not-An-ID.yaml", "title: X\n", t0)
	write("top.yaml", "title: not in a notebook\n", t0)

	r, err := ix.Sync(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 2 || len(r.Updated) != 0 || len(r.Removed) != 0 || len(r.Errors) != 1 {
		t.Fatalf("first sync = %+v", r)
	}

	// Simulate a git pull that modifies a and deletes b.
	write("inbox/a.yaml", "title: A\nupdated_at: 2026-10-02T00:00:00Z\nbody: ぶどう\n", t0.Add(time.Minute))
	if err := os.Remove(filepath.Join(notes, "work", "b.yaml")); err != nil {
		t.Fatal(err)
	}
	r, err = ix.Sync(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Added) != 0 || !slices.Equal(r.Updated, []string{"a"}) || !slices.Equal(r.Removed, []string{"b"}) {
		t.Fatalf("second sync = %+v", r)
	}
	if _, total, _ := ix.Search(ctx, "ぶどう", "", "", 10); total != 1 {
		t.Errorf("updated note not found")
	}
	if _, total, _ := ix.Search(ctx, "みかん", "", "", 10); total != 0 {
		t.Errorf("removed note still found")
	}

	// Moving a note keeps its mtime and size; the notebook change is still detected.
	if err := os.MkdirAll(filepath.Join(notes, "fruit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(notes, "inbox", "a.yaml"), filepath.Join(notes, "fruit", "a.yaml")); err != nil {
		t.Fatal(err)
	}
	if r, err = ix.Sync(ctx, st); err != nil || !slices.Equal(r.Updated, []string{"a"}) {
		t.Fatalf("sync after move = %+v, %v", r, err)
	}
	if res, _, _ := ix.Search(ctx, "ぶどう", "fruit", "", 10); len(res) != 1 || res[0].Notebook != "fruit" {
		t.Errorf("moved note not found in its new notebook: %+v", res)
	}

	// The same ID in two notebooks is reported and the index is left alone.
	write("work/a.yaml", "title: dup\n", t0)
	if r, err = ix.Sync(ctx, st); err != nil || len(r.Errors) != 2 || len(r.Removed) != 0 {
		t.Fatalf("sync with a duplicate = %+v, %v", r, err)
	}
	if res, _, _ := ix.Search(ctx, "ぶどう", "", "", 10); len(res) != 1 {
		t.Errorf("note with a duplicate ID dropped from the index")
	}
}

func TestNotebookFilter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for _, n := range []*store.Note{
		{ID: "a", Notebook: store.Inbox, Title: "りんご", CreatedAt: now, UpdatedAt: now},
		{ID: "b", Notebook: "work", Title: "りんご", CreatedAt: now, UpdatedAt: now},
		{ID: "c", Notebook: store.Trash, Title: "りんご", CreatedAt: now, UpdatedAt: now},
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
	for _, tt := range []struct {
		notebook string
		want     []string
	}{
		{"", []string{"a", "b"}}, // all but the trash
		{"work", []string{"b"}},
		{store.Trash, []string{"c"}},
	} {
		list, _, err := ix.List(ctx, tt.notebook, "", "", 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		found, _, err := ix.Search(ctx, "りんご", tt.notebook, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ids(list), tt.want) || !slices.Equal(ids(found), tt.want) {
			t.Errorf("notebook %q: List = %v, Search = %v, want %v", tt.notebook, ids(list), ids(found), tt.want)
		}
	}
	counts, err := ix.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[store.Inbox] != 1 || counts["work"] != 1 || counts[store.Trash] != 1 {
		t.Errorf("Counts = %v", counts)
	}
}

// TestJaWithBigram checks that the ja mode keeps a bigram table and
// lists the notes found by morphemes before those found only by bigrams.
func TestJaWithBigram(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, n := range []*store.Note{
		// Found only by bigrams, though the match in the title would rank it first by bigrams.
		{ID: "tokyo", Notebook: store.Inbox, Title: "東京都の天気", Body: "晴れ", CreatedAt: now, UpdatedAt: now},
		{ID: "kyoto", Notebook: store.Inbox, Title: "旅行", Body: "京都へ行った。", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(dir, "index.sqlite")
	tables := func(ix *Index) []string {
		rows, err := ix.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('notes_fts', 'notes_bigram') ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			names = append(names, n)
		}
		return names
	}

	ix, err := Open(ctx, db, Config{Mode: ModeJa})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	if got := tables(ix); !slices.Equal(got, []string{"notes_bigram", "notes_fts"}) {
		t.Errorf("ja tables = %v", got)
	}
	res, total, err := ix.Search(ctx, "京都", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range res {
		got = append(got, s.ID)
	}
	if !slices.Equal(got, []string{"kyoto", "tokyo"}) || total != 2 {
		t.Errorf("Search = %v (total %d), want [kyoto tokyo]", got, total)
	}
	// The bigram match is highlighted too.
	if hl := res[1].TitleHighlights; len(hl) != 1 || hl[0] != (Span{1, 3}) {
		t.Errorf("title highlights of tokyo = %v, want [{1 3}]", hl)
	}
	// limit cuts the merged list.
	if res, total, err = ix.Search(ctx, "京都", "", "", 1); err != nil || len(res) != 1 || res[0].ID != "kyoto" || total != 2 {
		t.Errorf("Search with limit 1 = %v, %d, %v", res, total, err)
	}
	// Updates and deletes reach the bigram table.
	n, err := st.Load("tokyo")
	if err != nil {
		t.Fatal(err)
	}
	n.Title = "大阪の天気"
	if err := st.Save(n); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	if res, _, _ = ix.Search(ctx, "京都", "", "", 10); len(res) != 1 {
		t.Errorf("after the update, Search = %d notes, want 1", len(res))
	}
	ix.Close()

	// Another mode drops the bigram table, and ja brings it back.
	ix, err = Open(ctx, db, Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	if got := tables(ix); !slices.Equal(got, []string{"notes_fts"}) {
		t.Errorf("bigram tables = %v", got)
	}
	ix.Close()
	ix, err = Open(ctx, db, Config{Mode: ModeJa})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if got := tables(ix); !slices.Equal(got, []string{"notes_bigram", "notes_fts"}) {
		t.Errorf("ja tables again = %v", got)
	}
	if res, _, _ = ix.Search(ctx, "大阪", "", "", 10); len(res) != 1 {
		t.Errorf("the rebuilt tables lost notes: %v", res)
	}
}

// TestEnWithTrigram checks that the en mode finds notes by the stems of words
// first, and then by trigrams, such as a word inside a longer one, and that it
// keeps the trigram table instead of the bigram table of the ja mode.
func TestEnWithTrigram(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for _, n := range []*store.Note{
		{ID: "airplane", Notebook: store.Inbox, Title: "Airplane", Body: "Seats by the window", CreatedAt: now, UpdatedAt: now},
		{ID: "trip", Notebook: store.Inbox, Title: "Trip", Body: "Planning the trip to 京都", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	db := filepath.Join(dir, "index.sqlite")
	tables := func(ix *Index) []string {
		t.Helper()
		rows, err := ix.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('notes_fts', 'notes_bigram', 'notes_trigram') ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			names = append(names, n)
		}
		return names
	}
	// From the ja mode, the bigram table goes and the trigram table comes.
	ja, err := Open(ctx, db, Config{Mode: ModeJa})
	if err != nil {
		t.Fatal(err)
	}
	if got := tables(ja); !slices.Equal(got, []string{"notes_bigram", "notes_fts"}) {
		t.Errorf("ja tables = %v", got)
	}
	ja.Close()
	ix, err := Open(ctx, db, Config{Mode: ModeEn})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if got := tables(ix); !slices.Equal(got, []string{"notes_fts", "notes_trigram"}) {
		t.Errorf("en tables = %v", got)
	}
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	search := func(q string) []string {
		t.Helper()
		res, _, err := ix.Search(ctx, q, "", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, s := range res {
			ids = append(ids, s.ID)
		}
		return ids
	}
	// plan finds planning by its stem first, then airplane by trigrams.
	if got := search("plan"); !slices.Equal(got, []string{"trip", "airplane"}) {
		t.Errorf("plan: %v", got)
	}
	// plans finds planning by its stem; trigrams find the characters of the
	// term itself, which airplane does not have.
	if got := search("plans"); !slices.Equal(got, []string{"trip"}) {
		t.Errorf("plans: %v", got)
	}
	// Japanese in an English note is found by the bigrams of the en table.
	if got := search("京都"); !slices.Equal(got, []string{"trip"}) {
		t.Errorf("京都: %v", got)
	}
	// A word being typed is found by trigrams.
	if got := search("planni"); !slices.Equal(got, []string{"trip"}) {
		t.Errorf("planni: %v", got)
	}
}

// TestContentless checks that the full-text tables keep no copy of the tokens,
// and that rows follow updates and deletes by notes.seq.
func TestContentless(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b"} {
		if err := st.Create(&store.Note{ID: id, Notebook: store.Inbox, Title: "京都の" + id, Body: "本文", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := Open(ctx, filepath.Join(dir, "index.sqlite"), Config{Mode: ModeJa})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := ix.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name IN ('notes_fts_content', 'notes_bigram_content')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d content tables exist, want none", n)
	}
	count := func(q string) int {
		_, total, err := ix.Search(ctx, q, "", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		return total
	}
	if got := count("京都"); got != 2 {
		t.Fatalf("Search(京都) = %d, want 2", got)
	}
	if err := ix.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Delete(ctx, "missing"); err != nil {
		t.Errorf("Delete of a note not in the index: %v", err)
	}
	if got := count("京都"); got != 1 {
		t.Errorf("after deleting a, Search(京都) = %d, want 1", got)
	}
	// Re-adding a note gets a new seq; the old rows must not come back.
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	if got := count("京都"); got != 2 {
		t.Errorf("after re-adding a, Search(京都) = %d, want 2", got)
	}
}

// TestSchemaUpgrade checks that an index of an older schema is rebuilt.
func TestSchemaUpgrade(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := filepath.Join(dir, "index.sqlite")
	ix, err := Open(ctx, db, Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	// Make it look like schema 2: notes keyed by id, with a stale row.
	for _, q := range []string{
		`DROP TABLE notes_fts`, `DROP TABLE note_tags`, `DROP TABLE notes`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, notebook TEXT, title TEXT, tags TEXT, body TEXT, created_at TEXT, updated_at TEXT, file_mtime INTEGER, file_size INTEGER)`,
		`INSERT INTO notes VALUES ('old', 'inbox', 't', '[]', 'b', '', '', 0, 0)`,
		`PRAGMA user_version = 2`,
	} {
		if _, err := ix.db.ExecContext(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}
	ix.Close()
	ix, err = Open(ctx, db, Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	var n int
	if err := ix.db.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&n); err != nil {
		t.Fatalf("notes after upgrade: %v", err)
	}
	if n != 0 {
		t.Errorf("notes has %d rows after the upgrade, want 0 (rebuilt by the next Sync)", n)
	}
	if _, err := ix.db.ExecContext(ctx, `SELECT seq FROM notes`); err != nil {
		t.Errorf("notes has no seq: %v", err)
	}
}

func TestImportedNote(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for _, n := range []*store.Note{
		{ID: "a", Notebook: "work", Title: "a", CreatedAt: now, UpdatedAt: now, ImportHash: "sha256:1"},
		{ID: "b", Notebook: store.Trash, Title: "b", CreatedAt: now, UpdatedAt: now, ImportHash: "sha256:2"},
		{ID: "c", Notebook: "secret", Title: "c", CreatedAt: now, UpdatedAt: now, ImportHash: "sha256:3"},
		{ID: "d", Notebook: "work", Title: "d", CreatedAt: now, UpdatedAt: now},
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
	tests := []struct {
		hash    string
		exclude []string
		want    string
	}{
		{"sha256:1", nil, "a"},
		{"sha256:2", nil, "b"}, // the trash counts
		{"sha256:2", []string{store.Trash}, ""},
		{"sha256:3", []string{store.Trash, "secret"}, ""},
		{"sha256:9", nil, ""},
		{"", nil, ""}, // notes not imported have no hash
	}
	for _, tt := range tests {
		id, ok, err := ix.ImportedNote(ctx, tt.hash, tt.exclude...)
		if err != nil || id != tt.want || ok != (tt.want != "") {
			t.Errorf("ImportedNote(%q, %v) = %q, %v, %v; want %q", tt.hash, tt.exclude, id, ok, err, tt.want)
		}
	}
}

func TestOnReplace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes")
	st, err := store.New(notes)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := Open(ctx, filepath.Join(dir, "index.sqlite"), Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	var got []Replaced
	ix.OnReplace(func(r Replaced) { got = append(got, r) })

	p := filepath.Join(notes, "inbox", "a.yaml")
	write := func(body string, mtime time.Time) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now().Add(-time.Hour)
	write("title: A\ntags: [x]\nupdated_at: 2026-10-01T00:00:00Z\nbody: りんご\n", t0)
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a new note was reported replaced: %+v", got)
	}
	// A change of the file found by Sync reports the content before it.
	write("title: A\ntags: [x]\nupdated_at: 2026-10-02T00:00:00Z\nbody: みかん\n", t0.Add(time.Minute))
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" || got[0].Body != "りんご" || got[0].Title != "A" || len(got[0].Tags) != 1 || !got[0].UpdatedAt.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("replaced = %+v", got)
	}
	// The same content in a new file, such as after a move or a reorder, is
	// not a new version.
	write("title: A\ntags: [x]\norder: 5\nupdated_at: 2026-10-02T00:00:00Z\nbody: みかん\n", t0.Add(2*time.Minute))
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("an unchanged content was reported replaced: %+v", got[1:])
	}
}
