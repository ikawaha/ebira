package index

import (
	"context"
	"os"
	"time"
)

// Stats describes the state of the database, for GET /debug/stats.
type Stats struct {
	Path string `json:"path"`
	// Files are the sizes in bytes of the database file (db) and of its WAL
	// (wal) and shared memory (shm) files.
	Files map[string]int64 `json:"files"`
	// PageSize is in bytes. FreePages are the pages that VACUUM would give
	// back.
	PageSize  int64 `json:"page_size"`
	Pages     int64 `json:"pages"`
	FreePages int64 `json:"free_pages"`
	// CacheSize is PRAGMA cache_size of the connection: pages when positive,
	// KiB when negative.
	CacheSize     int64        `json:"cache_size"`
	JournalMode   string       `json:"journal_mode"`
	SchemaVersion int          `json:"schema_version"`
	Mode          Mode         `json:"mode"`
	Notes         int          `json:"notes"`
	Tables        []TableStats `json:"tables"`
	Pool          PoolStats    `json:"pool"`
}

// TableStats is the space that a table or an index takes in the database
// file, from the dbstat table. Full-text tables are listed by their shadow
// tables, such as notes_fts_data.
type TableStats struct {
	Name  string `json:"name"`
	Pages int64  `json:"pages"`
	Bytes int64  `json:"bytes"`
}

// PoolStats are the statistics of the pool of connections (there is one).
type PoolStats struct {
	Open         int           `json:"open"`
	InUse        int           `json:"in_use"`
	WaitCount    int64         `json:"wait_count"`
	WaitDuration time.Duration `json:"wait_duration_ns"`
}

// Stats reads the statistics of the database. Tables are listed largest
// first.
func (ix *Index) Stats(ctx context.Context) (Stats, error) {
	s := Stats{Path: ix.path, Files: map[string]int64{}, Mode: ix.mode}
	for name, suffix := range map[string]string{"db": "", "wal": "-wal", "shm": "-shm"} {
		if fi, err := os.Stat(ix.path + suffix); err == nil {
			s.Files[name] = fi.Size()
		}
	}
	var err error
	if s.Pages, s.FreePages, s.PageSize, err = ix.pages(ctx); err != nil {
		return s, err
	}
	for _, p := range []struct {
		pragma string
		dst    any
	}{
		{"cache_size", &s.CacheSize},
		{"journal_mode", &s.JournalMode},
		{"user_version", &s.SchemaVersion},
	} {
		if err := ix.db.QueryRowContext(ctx, `PRAGMA `+p.pragma).Scan(p.dst); err != nil {
			return s, err
		}
	}
	if err := ix.db.QueryRowContext(ctx, `SELECT count(*) FROM notes`).Scan(&s.Notes); err != nil {
		return s, err
	}
	rows, err := ix.db.QueryContext(ctx, `SELECT name, count(*), sum(pgsize) FROM dbstat GROUP BY name ORDER BY sum(pgsize) DESC, name`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var t TableStats
		if err := rows.Scan(&t.Name, &t.Pages, &t.Bytes); err != nil {
			return s, err
		}
		s.Tables = append(s.Tables, t)
	}
	if err := rows.Err(); err != nil {
		return s, err
	}
	p := ix.db.Stats()
	s.Pool = PoolStats{Open: p.OpenConnections, InUse: p.InUse, WaitCount: p.WaitCount, WaitDuration: p.WaitDuration}
	return s, nil
}

// pages returns the number of pages of the database, how many of them are
// free and the page size in bytes.
func (ix *Index) pages(ctx context.Context) (pages, free, size int64, err error) {
	for _, p := range []struct {
		pragma string
		dst    *int64
	}{
		{"page_count", &pages},
		{"freelist_count", &free},
		{"page_size", &size},
	} {
		if err = ix.db.QueryRowContext(ctx, `PRAGMA `+p.pragma).Scan(p.dst); err != nil {
			return 0, 0, 0, err
		}
	}
	return pages, free, size, nil
}
