// Package index is the SQLite index built from the note YAML files.
// The index is derived data that can be rebuilt from the YAML files at any
// time, so it is not committed to git.
package index

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
	_ "modernc.org/sqlite"

	"github.com/ikawaha/ebira/internal/store"
)

// Mode is a search mode.
type Mode string

const (
	// ModeLike searches substrings with LIKE, without FTS.
	ModeLike Mode = "like"
	// ModeBigram indexes character bigrams in FTS5 and searches substrings.
	// Terms shorter than two characters are searched with LIKE.
	ModeBigram Mode = "bigram"
	// ModeJa indexes Japanese text split into morphemes (by kagome) in FTS5 and
	// searches by morpheme. Stop words and stop tags are dropped.
	ModeJa Mode = "ja"
)

// ParseMode parses s as a Mode.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeLike, ModeBigram, ModeJa:
		return m, nil
	}
	return "", fmt.Errorf("unknown search mode %q (like, bigram, ja)", s)
}

// Summary is an entry of a list or search result.
type Summary struct {
	ID       string
	Notebook string
	// TrashedNotebook is the notebook in the trash that holds the note, for a
	// note of a notebook deleted as a whole (see store.Note.TrashedNotebook).
	TrashedNotebook string
	Title           string
	Tags            []string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CreatedBy       string // see store.Note.CreatedBy
	Snippet         string
	// TitleHighlights and SnippetHighlights are the ranges of Title and
	// Snippet matched by the query of a search.
	TitleHighlights   []Span
	SnippetHighlights []Span
}

// Config configures the index.
type Config struct {
	Mode Mode
	// StopWords and StopTags configure the ja mode. StopTags are parts of
	// speech written as the IPA dictionary names of their levels joined by "-",
	// matched exactly (see parsePOS). Nil uses
	// kagome's defaults, which come from Lucene's Japanese analyzer.
	StopWords []string
	StopTags  []string
}

// Index is the SQLite index.
type Index struct {
	db   *sql.DB
	mode Mode
	an   *analyzer // nil in the like mode
	// fts lists the full-text tables: notes_fts for the search mode (none in
	// the like mode) and, in the ja mode, notes_bigram. Search merges
	// their results in this order.
	fts []ftsTable
}

// ftsTable is a full-text table and the analyzer that fills it.
type ftsTable struct {
	name string // table name
	key  string // key in meta of the analyzer fingerprint
	an   *analyzer
}

// bigramTable is kept besides the morpheme table of the ja mode, so that search also finds
// substrings that morphemes miss, such as a city name inside a longer
// administrative name that the dictionary takes as one word.
const bigramTable = "notes_bigram"

var (
	//go:embed sql/schema.sql
	schemaSQL string
	//go:embed sql/fts.sql
	ftsSQL string
)

// Open opens the SQLite database at path. If the search mode or the analyzer
// settings differ from the previous ones, the full-text tables are rebuilt.
func Open(ctx context.Context, path string, cfg Config) (*Index, error) {
	an, err := newAnalyzer(cfg)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ix := &Index{db: db, mode: cfg.Mode, an: an}
	if an != nil {
		ix.fts = append(ix.fts, ftsTable{name: "notes_fts", key: "fts", an: an})
	}
	if cfg.Mode == ModeJa {
		bi, err := newAnalyzer(Config{Mode: ModeBigram})
		if err != nil {
			db.Close()
			return nil, err
		}
		ix.fts = append(ix.fts, ftsTable{name: bigramTable, key: "bigram", an: bi})
	}
	if err := ix.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return ix, nil
}

// Close closes the database.
func (ix *Index) Close() error { return ix.db.Close() }

// Mode returns the search mode.
func (ix *Index) Mode() Mode { return ix.mode }

// schemaVersion is stored as the database's user_version. When it differs,
// all tables are dropped and rebuilt from the YAML files by the next Sync.
const schemaVersion = 6 // 3: notes.seq, contentless full-text tables; 4: notes.trashed_notebook; 5: notes.sort_key, title_key; 6: notes.created_by

func (ix *Index) migrate(ctx context.Context) error {
	var v int
	if err := ix.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v != schemaVersion {
		for _, t := range []string{"notes_fts", bigramTable, "note_tags", "notes", "meta"} {
			if _, err := ix.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+t); err != nil {
				return err
			}
		}
		if _, err := ix.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
			return err
		}
	}
	if _, err := ix.db.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	main := ftsTable{name: "notes_fts", key: "fts"} // no table in the like mode
	mainFP := string(ModeLike)
	if ix.an != nil {
		main.an, mainFP = ix.an, ix.an.fingerprint
	}
	if err := ix.migrateFTS(ctx, main, mainFP); err != nil {
		return err
	}
	bigram := ftsTable{name: bigramTable, key: "bigram"} // only in the ja mode
	bigramFP := ""
	if len(ix.fts) > 1 {
		bigram = ix.fts[1]
		bigramFP = bigram.an.fingerprint
	}
	return ix.migrateFTS(ctx, bigram, bigramFP)
}

// migrateFTS rebuilds the full-text table t when the fingerprint stored for it
// differs from cur. A table without an analyzer is dropped.
func (ix *Index) migrateFTS(ctx context.Context, t ftsTable, cur string) error {
	var prev string
	err := ix.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, t.key).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if prev == cur {
		return nil
	}
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+t.name); err != nil {
		return err
	}
	if t.an != nil {
		if _, err := tx.ExecContext(ctx, strings.ReplaceAll(ftsSQL, "{{table}}", t.name)); err != nil {
			return fmt.Errorf("create %s: %w", t.name, err)
		}
		rows, err := tx.QueryContext(ctx, `SELECT seq, title, body FROM notes`)
		if err != nil {
			return err
		}
		type row struct {
			seq         int64
			title, body string
		}
		var all []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.seq, &r.title, &r.body); err != nil {
				rows.Close()
				return err
			}
			all = append(all, r)
		}
		rows.Close()
		for _, r := range all {
			if err := insertFTS(ctx, tx, t, r.seq, r.title, r.body); err != nil {
				return err
			}
		}
	}
	if cur == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, t.key)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, t.key, cur)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// insertFTS adds a note, whose notes.seq is seq, to the full-text table t.
func insertFTS(ctx context.Context, tx *sql.Tx, t ftsTable, seq int64, title, body string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO `+t.name+` (rowid, title, body) VALUES (?, ?, ?)`, seq, t.an.analyze(title), t.an.analyze(body))
	return err
}

// Upsert puts the note into the index. f is the note's file information, used
// by later Syncs to detect changes.
func (ix *Index) Upsert(ctx context.Context, n *store.Note, f store.File) error {
	tags, err := json.Marshal(n.Tags)
	if err != nil {
		return err
	}
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// An update keeps the note's seq, and so its rowid in the full-text tables.
	var seq int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO notes (id, notebook, trashed_notebook, title, tags, body, created_at, updated_at, sort_key, title_key, created_by, file_mtime, file_size)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	notebook = excluded.notebook, trashed_notebook = excluded.trashed_notebook, title = excluded.title, tags = excluded.tags, body = excluded.body,
	created_at = excluded.created_at, updated_at = excluded.updated_at, sort_key = excluded.sort_key, title_key = excluded.title_key,
	created_by = excluded.created_by, file_mtime = excluded.file_mtime, file_size = excluded.file_size
RETURNING seq`,
		n.ID, n.Notebook, n.TrashedNotebook, n.Title, string(tags), n.Body, formatTime(n.CreatedAt), formatTime(n.UpdatedAt), n.SortKey(), titleKey(n.Title), n.CreatedBy, f.ModTime.UnixNano(), f.Size,
	).Scan(&seq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE id = ?`, n.ID); err != nil {
		return err
	}
	for _, t := range n.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_tags (id, tag) VALUES (?, ?)`, n.ID, t); err != nil {
			return err
		}
	}
	for _, t := range ix.fts {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t.name+` WHERE rowid = ?`, seq); err != nil {
			return err
		}
		if err := insertFTS(ctx, tx, t, seq, n.Title, n.Body); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete removes the note from the index.
func (ix *Index) Delete(ctx context.Context, id string) error {
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var seq int64
	err = tx.QueryRowContext(ctx, `DELETE FROM notes WHERE id = ? RETURNING seq`, id).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // not in the index
	}
	if err != nil {
		return err
	}
	for _, t := range ix.fts {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t.name+` WHERE rowid = ?`, seq); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SyncResult is the result of Sync. Added, Updated and Removed hold note IDs.
type SyncResult struct {
	Added, Updated, Removed []string
	Errors                  []string
}

// Sync brings the index in line with the notes directory. Notes whose
// notebook, file modification time and size are unchanged are not read again.
// An ID used in more than one notebook is reported and left as it is in the
// index until the duplicate is resolved.
func (ix *Index) Sync(ctx context.Context, s *store.Store) (*SyncResult, error) {
	files, err := s.Scan()
	if err != nil {
		return nil, err
	}
	type state struct {
		notebook, trashed string
		mtime, size       int64
	}
	known := map[string]state{}
	rows, err := ix.db.QueryContext(ctx, `SELECT id, notebook, trashed_notebook, file_mtime, file_size FROM notes`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var st state
		if err := rows.Scan(&id, &st.notebook, &st.trashed, &st.mtime, &st.size); err != nil {
			rows.Close()
			return nil, err
		}
		known[id] = st
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	where := map[string][]string{} // ID -> notebooks
	for _, f := range files {
		where[f.ID] = append(where[f.ID], f.Notebook)
	}
	res := &SyncResult{Errors: []string{}}
	for _, f := range files {
		k, ok := known[f.ID]
		if nbs := where[f.ID]; len(nbs) > 1 {
			if f.Notebook == nbs[0] {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %s in %s", store.ErrDuplicate, f.ID, strings.Join(nbs, ", ")))
			}
			delete(known, f.ID)
			continue
		}
		delete(known, f.ID)
		if ok && k == (state{f.Notebook, f.TrashedNotebook, f.ModTime.UnixNano(), f.Size}) {
			continue
		}
		n, err := s.LoadFile(f) // a duplicate ID was skipped above
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		if err := ix.Upsert(ctx, n, f); err != nil {
			return nil, err
		}
		if ok {
			res.Updated = append(res.Updated, f.ID)
		} else {
			res.Added = append(res.Added, f.ID)
		}
	}
	for id := range known {
		if err := ix.Delete(ctx, id); err != nil {
			return nil, err
		}
		res.Removed = append(res.Removed, id)
	}
	return res, nil
}

// notebookCond returns the condition for the notebook filter: an empty
// notebook means all notebooks except the trash and those in exclude. The
// trash means the notes deleted on their own, not those of the notebooks in
// the trash (see ListTrashedNotebook).
func notebookCond(notebook string, exclude []string) (string, []any) {
	if notebook == store.Trash {
		return `n.notebook = ? AND n.trashed_notebook = ''`, []any{notebook}
	}
	if notebook != "" {
		return `n.notebook = ?`, []any{notebook}
	}
	args := []any{store.Trash}
	for _, nb := range exclude {
		args = append(args, nb)
	}
	return `n.notebook NOT IN (?` + strings.Repeat(", ?", len(args)-1) + `)`, args
}

// Counts returns the number of notes per notebook. The count of the trash is
// of the notes deleted on their own (see TrashedNotebookCounts).
func (ix *Index) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT notebook, count(*) FROM notes WHERE trashed_notebook = '' GROUP BY notebook`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var nb string
		var c int
		if err := rows.Scan(&nb, &c); err != nil {
			return nil, err
		}
		counts[nb] = c
	}
	return counts, rows.Err()
}

// TagCount is a tag with the number of notes that have it.
type TagCount struct {
	Name  string
	Count int
}

// Tags returns the tags of the notes in notebook (all but the trash and
// exclude if empty) with their note counts, sorted by name as titles are.
func (ix *Index) Tags(ctx context.Context, notebook string, exclude ...string) ([]TagCount, error) {
	cond, args := notebookCond(notebook, exclude)
	rows, err := ix.db.QueryContext(ctx, `SELECT t.tag, count(*) FROM note_tags t JOIN notes n ON n.id = t.id WHERE `+cond+` GROUP BY t.tag`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Name, &t.Count); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(tags, func(a, b TagCount) int {
		return cmp.Or(strings.Compare(titleKey(a.Name), titleKey(b.Name)), strings.Compare(a.Name, b.Name))
	})
	return tags, nil
}

// summaryColumns are the columns that query scans.
const summaryColumns = `n.id, n.notebook, n.trashed_notebook, n.title, n.tags, n.created_at, n.updated_at, n.created_by`

// orderBy returns the ORDER BY clause of a list in the order sort (one of
// store.Sorts; empty for store.SortOrder). The ID breaks ties, so that the
// order is stable.
func orderBy(sort string) (string, error) {
	switch sort {
	case "", store.SortOrder:
		return ` ORDER BY n.sort_key DESC, n.id`, nil
	case store.SortCreated:
		return ` ORDER BY n.created_at DESC, n.id`, nil
	case store.SortUpdated:
		return ` ORDER BY n.updated_at DESC, n.id`, nil
	case store.SortTitle:
		return ` ORDER BY n.title_key, n.id`, nil
	}
	return "", fmt.Errorf("unknown sort %q", sort)
}

// titleKey returns the key of title for sorting by title: NFKC, so that
// full-width and half-width forms sort together, and lower-cased.
func titleKey(title string) string { return strings.ToLower(norm.NFKC.String(title)) }

// List returns notes in notebook (all but the trash and exclude if empty) in
// the order sort, and the number of all of them.
func (ix *Index) List(ctx context.Context, notebook, tag, sort string, limit, offset int, exclude ...string) ([]*Summary, int, error) {
	cond, args := notebookCond(notebook, exclude)
	return ix.list(ctx, cond, args, tag, sort, limit, offset)
}

// ListTrashedNotebook returns the notes of notebook nb in the trash in the
// order sort, and the number of all of them.
func (ix *Index) ListTrashedNotebook(ctx context.Context, nb, tag, sort string, limit, offset int) ([]*Summary, int, error) {
	return ix.list(ctx, `n.notebook = ? AND n.trashed_notebook = ?`, []any{store.Trash, nb}, tag, sort, limit, offset)
}

func (ix *Index) list(ctx context.Context, cond string, args []any, tag, sort string, limit, offset int) ([]*Summary, int, error) {
	order, err := orderBy(sort)
	if err != nil {
		return nil, 0, err
	}
	if tag != "" {
		cond += ` AND n.id IN (SELECT id FROM note_tags WHERE tag = ?)`
		args = append(args, tag)
	}
	var total int
	if err := ix.db.QueryRowContext(ctx, `SELECT count(*) FROM notes n WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	res, err := ix.query(ctx, `SELECT `+summaryColumns+`, '' FROM notes n WHERE `+cond+order+` LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	return res, total, nil
}

// SortKey is a note's key in the manual order (store.Note.SortKey).
type SortKey struct {
	ID  string
	Key int64
}

// SortKeys returns the keys of the notes in notebook (the notes deleted on
// their own for the trash) in the manual order, highest first.
func (ix *Index) SortKeys(ctx context.Context, notebook string) ([]SortKey, error) {
	cond, args := notebookCond(notebook, nil)
	rows, err := ix.db.QueryContext(ctx, `SELECT n.id, n.sort_key FROM notes n WHERE `+cond+` ORDER BY n.sort_key DESC, n.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []SortKey
	for rows.Next() {
		var k SortKey
		if err := rows.Scan(&k.ID, &k.Key); err != nil {
			return nil, err
		}
		res = append(res, k)
	}
	return res, rows.Err()
}

// TrashedNotebookCounts returns the number of notes per notebook in the trash.
func (ix *Index) TrashedNotebookCounts(ctx context.Context) (map[string]int, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT trashed_notebook, count(*) FROM notes WHERE notebook = ? AND trashed_notebook <> '' GROUP BY trashed_notebook`, store.Trash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var nb string
		var c int
		if err := rows.Scan(&nb, &c); err != nil {
			return nil, err
		}
		counts[nb] = c
	}
	return counts, rows.Err()
}

// Search finds notes in notebook (all but the trash and exclude if empty) matching q,
// whose space-separated terms are combined with AND. total is the number of
// matched notes. In the ja mode, notes found only by bigrams (substrings
// that morphemes miss) follow the notes found by morphemes.
func (ix *Index) Search(ctx context.Context, q, notebook, tag string, limit int, exclude ...string) (res []*Summary, total int, err error) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return []*Summary{}, 0, nil
	}
	var ids []string
	seen := map[string]bool{}
	tables := ix.fts
	if len(tables) == 0 {
		tables = []ftsTable{{}} // the like mode
	}
	for _, t := range tables {
		found, err := ix.searchIDs(ctx, t, terms, notebook, tag, exclude)
		if err != nil {
			return nil, 0, err
		}
		for _, id := range found {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	total = len(ids)
	if res, err = ix.summaries(ctx, ids[:min(limit, len(ids))]); err != nil {
		return nil, 0, err
	}
	h := ix.highlighter(terms)
	for _, s := range res {
		s.TitleHighlights = h.find(s.Title)
		s.Snippet, s.SnippetHighlights = snippet(s.Snippet, h.findInBody(s.Snippet, true))
	}
	return res, total, nil
}

// searchIDs returns the IDs of the notes matching terms in the full-text
// table t, best first, or with LIKE by update time when t has no analyzer.
func (ix *Index) searchIDs(ctx context.Context, t ftsTable, terms []string, notebook, tag string, exclude []string) ([]string, error) {
	var (
		from  = `notes n`
		where []string
		args  []any
		order = `n.updated_at DESC`
		match []string
	)
	for _, term := range terms {
		// A term with no tokens (a single character in the bigram mode, only
		// stop words in the ja mode, or only symbols) is searched with LIKE
		// so that it still narrows the result.
		if t.an != nil {
			if tokens := t.an.analyze(term); tokens != "" {
				match = append(match, phrase(tokens))
				continue
			}
		}
		where = append(where, `(n.title LIKE ? ESCAPE '\' OR n.body LIKE ? ESCAPE '\')`)
		p := "%" + escapeLike(term) + "%"
		args = append(args, p, p)
	}
	if len(match) > 0 {
		from = t.name + ` f JOIN notes n ON n.seq = f.rowid`
		where = append(where, t.name+` MATCH ?`)
		args = append(args, strings.Join(match, " AND "))
		order = `bm25(` + t.name + `, 0.0, 5.0, 1.0)` // weight title matches higher
	}
	nbCond, nbArgs := notebookCond(notebook, exclude)
	where = append(where, nbCond)
	args = append(args, nbArgs...)
	if tag != "" {
		where = append(where, `n.id IN (SELECT id FROM note_tags WHERE tag = ?)`)
		args = append(args, tag)
	}
	rows, err := ix.db.QueryContext(ctx, `SELECT n.id FROM `+from+` WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+`, n.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// summaries returns the summaries of the notes ids in that order, with the
// body in Snippet.
func (ix *Index) summaries(ctx context.Context, ids []string) ([]*Summary, error) {
	if len(ids) == 0 {
		return []*Summary{}, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	got, err := ix.query(ctx, `SELECT `+summaryColumns+`, n.body FROM notes n WHERE n.id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*Summary, len(got))
	for _, s := range got {
		byID[s.ID] = s
	}
	res := make([]*Summary, 0, len(ids))
	for _, id := range ids {
		if s := byID[id]; s != nil {
			res = append(res, s)
		}
	}
	return res, nil
}

// highlighter returns the highlighter of the search: the matches of every
// full-text table, merged.
func (ix *Index) highlighter(terms []string) highlighters {
	if len(ix.fts) == 0 {
		return highlighters{newHighlighter(nil, terms)}
	}
	var hs highlighters
	for _, t := range ix.fts {
		hs = append(hs, newHighlighter(t.an, terms))
	}
	return hs
}

// Highlight returns the ranges of title and body matched by the query q, as
// Search matches them.
func (ix *Index) Highlight(q, title, body string) (titleSpans, bodySpans []Span) {
	h := ix.highlighter(strings.Fields(q))
	return h.find(title), h.findInBody(body, false)
}

// query runs q, which must select summaryColumns and the body (stored in
// Snippet).
func (ix *Index) query(ctx context.Context, q string, args ...any) ([]*Summary, error) {
	rows, err := ix.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []*Summary{}
	for rows.Next() {
		var s Summary
		var tags, created, updated string
		if err := rows.Scan(&s.ID, &s.Notebook, &s.TrashedNotebook, &s.Title, &tags, &created, &updated, &s.CreatedBy, &s.Snippet); err != nil {
			return nil, err
		}
		if s.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tags), &s.Tags); err != nil {
			return nil, err
		}
		if s.UpdatedAt, err = time.Parse(time.RFC3339, updated); err != nil {
			return nil, err
		}
		res = append(res, &s)
	}
	return res, rows.Err()
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// phrase quotes s as an FTS5 phrase ("...").
func phrase(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
