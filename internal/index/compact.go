package index

import "context"

// Compact gives the free pages of the database back to the file system with
// VACUUM, when they are at least compactRatio of the pages and compactMin
// bytes: SQLite keeps the pages that rebuilding a full-text table or deleting
// notes leaves free, and never shrinks the file by itself. VACUUM holds the
// only connection while it runs, so the server calls it on start, before it
// listens. It returns the number of bytes given back, 0 when it did nothing.
func (ix *Index) Compact(ctx context.Context) (int64, error) {
	pages, free, size, err := ix.pages(ctx)
	if err != nil {
		return 0, err
	}
	if free*size < compactMin || float64(free) < compactRatio*float64(pages) {
		return 0, nil
	}
	if _, err := ix.db.ExecContext(ctx, `VACUUM`); err != nil {
		return 0, err
	}
	// VACUUM writes the new pages through the WAL; move them into the
	// database file and empty the WAL.
	if _, err := ix.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return 0, err
	}
	after, _, _, err := ix.pages(ctx)
	if err != nil {
		return 0, err
	}
	return (pages - after) * size, nil
}

const (
	compactRatio = 0.25
	compactMin   = 1 << 20
)
