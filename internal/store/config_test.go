package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
		{"serach: ja\n", "", "unknown key serach (misspelled, or written by a newer ebira: update ebira)"},
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

func TestSaveSearch(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := "# shared\nsearch: bigram # the mode\nmarks:\n  red: important\n"
	if err := os.WriteFile(s.ConfigPath(), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSearch(Config{Search: "ja", StopWords: []string{"の", "は"}, StopTags: []string{}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.ConfigPath())
	for _, want := range []string{"# shared", "search: ja # the mode", "red: important", "stop_tags: []", "- の"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file lacks %q:\n%s", want, b)
		}
	}
	c, err := s.Config()
	if err != nil || c.Search != "ja" || !slices.Equal(c.StopWords, []string{"の", "は"}) || c.StopTags == nil || len(c.StopTags) != 0 || c.Marks["red"] != "important" {
		t.Errorf("read back = %+v, %v", c, err)
	}

	// The defaults leave the keys out, and keep the rest.
	if err := s.SaveSearch(Config{}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(s.ConfigPath())
	if strings.Contains(string(b), "search") || strings.Contains(string(b), "stop_") || !strings.Contains(string(b), "red: important") {
		t.Errorf("defaults:\n%s", b)
	}
}

func TestSaveDefaults(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := "# shared settings\nsearch: ja # mode\ndefaults:\n  # the language\n  lang: ja\nmarks:\n  red: important\n"
	if err := os.WriteFile(s.ConfigPath(), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	set := func(key, value string) func(m *yaml.Node) {
		return func(m *yaml.Node) {
			for i := 0; i+1 < len(m.Content); i += 2 {
				if m.Content[i].Value == key {
					if value == "" {
						m.Content = append(m.Content[:i], m.Content[i+2:]...)
					} else {
						m.Content[i+1].Value = value
					}
					return
				}
			}
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
		}
	}
	if err := s.SaveDefaults(set("lang", "en")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDefaults(set("theme", "dark")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.ConfigPath())
	want := "# shared settings\nsearch: ja # mode\ndefaults:\n  # the language\n  lang: en\n  theme: dark\nmarks:\n  red: important\n"
	if string(b) != want {
		t.Errorf("got:\n%s\nwant:\n%s", b, want)
	}
	c, err := s.Config()
	if err != nil || c.Search != "ja" || c.Defaults.Kind != yaml.MappingNode {
		t.Errorf("Config: %+v, %v", c, err)
	}
	// The defaults left empty are removed.
	if err := s.SaveDefaults(set("lang", "")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDefaults(set("theme", "")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.ConfigPath()); strings.Contains(string(b), "defaults") {
		t.Errorf("empty defaults kept:\n%s", b)
	}
}

// Writing the search settings or the names of the marks keeps the defaults,
// with their comments.
func TestSaveKeepsDefaults(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := "search: ja\ndefaults:\n  # shared\n  lang: ja\n"
	if err := os.WriteFile(s.ConfigPath(), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSearch(Config{Search: "bigram"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMarkNames(map[string]string{"red": "important"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.ConfigPath())
	if want := "search: bigram\ndefaults:\n  # shared\n  lang: ja\nmarks:\n  red: important\n"; string(b) != want {
		t.Errorf("got:\n%s\nwant:\n%s", b, want)
	}
}
