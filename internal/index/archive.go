package index

import (
	"context"
	"strings"
	"time"

	"github.com/ikawaha/ebira/internal/store"
)

// Archive tells which notes a list, a search or the tags take by the archive:
// the notes put there on their own (store.Note.ArchivedAt) and those of the
// notebooks there (store.NotebookMeta.ArchivedAt), which the index does not
// know, so they are given.
type Archive struct {
	// Mode is ArchiveExclude, ArchiveInclude or ArchiveOnly.
	Mode string
	// Notebooks are the notebooks in the archive, with the sub notebooks of
	// those there.
	Notebooks []string
}

// Modes of an Archive.
const (
	ArchiveExclude = "exclude" // leave the archive out; the default
	ArchiveInclude = "include" // take the archive in with the rest
	ArchiveOnly    = "only"    // only the archive
)

// cond returns the condition on the notes of notes n for a, "" for none. The
// trash is not in the archive, so a note in it is always taken in.
func (a Archive) cond() (string, []any) {
	var in string
	var args []any
	if len(a.Notebooks) > 0 {
		in = `n.notebook IN (?` + strings.Repeat(", ?", len(a.Notebooks)-1) + `)`
		for _, nb := range a.Notebooks {
			args = append(args, nb)
		}
	}
	switch a.Mode {
	case ArchiveInclude:
		return "", nil
	case ArchiveOnly:
		if in == "" {
			return `n.archived = 1 AND n.notebook <> ?`, []any{store.Trash}
		}
		return `(n.archived = 1 OR ` + in + `) AND n.notebook <> ?`, append(args, store.Trash)
	}
	if in == "" {
		return `(n.archived = 0 OR n.notebook = ?)`, []any{store.Trash}
	}
	return `((n.archived = 0 AND NOT ` + in + `) OR n.notebook = ?)`, append(args, store.Trash)
}

// and appends the condition of a to cond and args.
func (a Archive) and(cond string, args []any) (string, []any) {
	c, cargs := a.cond()
	if c == "" {
		return cond, args
	}
	return cond + ` AND ` + c, append(args, cargs...)
}

// Stale returns the notes last updated before t, oldest first, but those in
// the trash, in the archive (by a, whose mode is ignored) and in exclude, and
// the number of all of them: those that may be put in the archive.
func (ix *Index) Stale(ctx context.Context, before time.Time, a Archive, limit, offset int, exclude ...string) ([]*Summary, int, error) {
	cond, args := notebookCond("", exclude)
	cond, args = Archive{Mode: ArchiveExclude, Notebooks: a.Notebooks}.and(cond, args)
	cond += ` AND n.updated_at < ?`
	args = append(args, formatTime(before))
	var total int
	if err := ix.db.QueryRowContext(ctx, `SELECT count(*) FROM notes n WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	res, err := ix.query(ctx, `SELECT `+summaryColumns+`, '' FROM notes n WHERE `+cond+` ORDER BY n.updated_at, n.id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	return res, total, nil
}

// ArchivedIn returns the notes of notebook put in the archive on their own.
func (ix *Index) ArchivedIn(ctx context.Context, notebook string) (map[string]bool, error) {
	rows, err := ix.db.QueryContext(ctx, `SELECT id FROM notes WHERE notebook = ? AND archived = 1`, notebook)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		res[id] = true
	}
	return res, rows.Err()
}
