-- Tables of the SQLite index. Applied on every start, so each statement must be idempotent.
-- Bump schemaVersion in index.go when changing a table; the index is then rebuilt.

CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notes (
	seq        INTEGER PRIMARY KEY,  -- rowid of the note in the full-text tables
	id         TEXT NOT NULL UNIQUE, -- unique across notebooks
	notebook   TEXT NOT NULL,    -- directory under notes/ (trash for any note in the trash)
	trashed_notebook TEXT NOT NULL DEFAULT '', -- notebook deleted as a whole that holds the note, as trash/<id>
	title      TEXT NOT NULL,
	tags       TEXT NOT NULL, -- JSON array
	marks      TEXT NOT NULL DEFAULT '[]', -- JSON array of store.MarkIDs
	body       TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	sort_key   INTEGER NOT NULL, -- key of the manual order (store.Note.SortKey), highest first
	title_key  TEXT NOT NULL,    -- title for sorting by title: NFKC, lower-cased
	created_by TEXT NOT NULL DEFAULT '', -- ai for a note created by an AI client (store.Note.CreatedBy)
	import_hash TEXT NOT NULL DEFAULT '', -- hash of the file the note was imported from (store.Note.ImportHash)
	file_mtime INTEGER NOT NULL, -- used by Sync to skip unchanged files
	file_size  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS notes_updated_at ON notes (updated_at);
CREATE INDEX IF NOT EXISTS notes_created_at ON notes (created_at);
CREATE INDEX IF NOT EXISTS notes_notebook ON notes (notebook, sort_key);
CREATE INDEX IF NOT EXISTS notes_import_hash ON notes (import_hash) WHERE import_hash != '';

CREATE TABLE IF NOT EXISTS note_tags (
	id  TEXT NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
	tag TEXT NOT NULL,
	PRIMARY KEY (id, tag)
);
CREATE INDEX IF NOT EXISTS note_tags_tag ON note_tags (tag);
