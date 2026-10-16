package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfig(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Config()
	if err != nil || c.Search != "" || c.StopWords != nil || c.StopTags != nil {
		t.Fatalf("Config without a file = %+v, %v", c, err)
	}
	tests := []struct {
		file, search, err string
	}{
		{"# shared settings\nsearch: ja  # like, bigram or ja\n", "ja", ""},
		{"", "", ""},
		{"# only a comment\n", "", ""},
		{"serach: ja\n", "", "field serach not found"},
		{"search: [ja]\n", "", "cannot unmarshal"},
	}
	for _, tt := range tests {
		if err := os.WriteFile(s.ConfigPath(), []byte(tt.file), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := s.Config()
		switch {
		case tt.err == "" && err != nil:
			t.Errorf("Config(%q): %v", tt.file, err)
		case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
			t.Errorf("Config(%q) error = %v, want %q", tt.file, err, tt.err)
		case tt.err == "" && c.Search != tt.search:
			t.Errorf("Config(%q).Search = %q, want %q", tt.file, c.Search, tt.search)
		}
	}

	// Leaving the lists out keeps the defaults (nil); [] means none.
	lists := []struct {
		file              string
		words, tags       []string
		wordsNil, tagsNil bool
	}{
		{"search: ja\n", nil, nil, true, true},
		{"stop_words: []\nstop_tags: [助詞-格助詞, 記号]\n", []string{}, []string{"助詞-格助詞", "記号"}, false, false},
		{"stop_words:\n  - の\n  - です\n", []string{"の", "です"}, nil, false, true},
		{"stop_words:\n", nil, nil, true, true}, // null: the same as leaving it out
	}
	for _, tt := range lists {
		if err := os.WriteFile(s.ConfigPath(), []byte(tt.file), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := s.Config()
		if err != nil {
			t.Fatalf("Config(%q): %v", tt.file, err)
		}
		if (c.StopWords == nil) != tt.wordsNil || !slices.Equal(c.StopWords, tt.words) ||
			(c.StopTags == nil) != tt.tagsNil || !slices.Equal(c.StopTags, tt.tags) {
			t.Errorf("Config(%q) = words %#v, tags %#v", tt.file, c.StopWords, c.StopTags)
		}
	}

	// The settings file is never taken for an old flat note.
	if err := os.WriteFile(filepath.Join(s.Dir(), "flat.yaml"), []byte("title: flat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved, err := s.MigrateFlat()
	if err != nil || len(moved) != 1 || moved[0] != "flat" {
		t.Errorf("MigrateFlat = %v, %v", moved, err)
	}
	if _, err := os.Stat(s.ConfigPath()); err != nil {
		t.Errorf("the settings file was moved: %v", err)
	}
}
