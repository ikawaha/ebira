-- Full-text tables. The application stores text already split into
-- space-separated tokens (bigrams or morphemes), so the unicode61 tokenizer only
-- has to split on spaces. {{table}} is notes_fts for the search mode and, in the
-- kagome mode, also notes_bigram, which is always kept with bigrams so that
-- search can fall back to substrings. A table is recreated when its analyzer
-- settings change.
--
-- The tables are contentless: they keep only the index, not a copy of the
-- tokens, since titles and bodies are always read from notes. A row's rowid is
-- notes.seq, and contentless_delete lets a row be deleted by its rowid.

CREATE VIRTUAL TABLE {{table}} USING fts5(
	title,
	body,
	tokenize = 'unicode61',
	content = '',
	contentless_delete = 1
);
