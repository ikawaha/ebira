package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the name of the settings file in the notes directory. It is
// synced with the notes, so every machine uses the same settings. Its name
// starts with "_" like NotebookFile, so it is never taken for a note.
const ConfigFile = "_ebira" + Ext

// Config holds the settings in ConfigFile.
type Config struct {
	// Search is the search mode: like, bigram or ja. Empty for the default.
	Search string `yaml:"search,omitempty"`
	// StopWords and StopTags replace the lists of kagome's ja filter, which
	// come from Lucene's Japanese analyzer, in the ja mode. Leave them out
	// to use those defaults; an empty list ([]) uses none. StopTags are parts
	// of speech, written as the IPA dictionary names of their levels joined by
	// "-".
	StopWords []string `yaml:"stop_words"`
	StopTags  []string `yaml:"stop_tags"`
	// Marks are the names of the marks, by their IDs (see MarkIDs), such as
	// "red: important", which say what they mean. A mark without a name is
	// shown by what it looks like.
	Marks map[string]string `yaml:"marks,omitempty"`
}

// Config reads ConfigFile. A missing file gives the zero value, and an unknown
// key is an error so that a misspelled setting is not silently ignored.
func (s *Store) Config() (Config, error) {
	var c Config
	p := s.ConfigPath()
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) { // io.EOF: an empty file
		return c, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

// ConfigPath returns the path of ConfigFile.
func (s *Store) ConfigPath() string { return filepath.Join(s.dir, ConfigFile) }

// SaveSearch writes the search settings of c to ConfigFile, its search mode
// and stop lists, and keeps the rest of the file, such as the names of the
// marks and comments. An empty mode and a nil list are left out, for the
// defaults; an empty list is written as [], for none.
func (s *Store) SaveSearch(c Config) error {
	p := s.ConfigPath()
	old, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(old)) > 0 {
		if err := yaml.Unmarshal(old, &doc); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	m := doc.Content[0]
	if c.Search != "" {
		set(m, "search", str(c.Search, 0))
	} else {
		unset(m, "search")
	}
	for _, l := range []struct {
		key   string
		words []string
	}{{"stop_words", c.StopWords}, {"stop_tags", c.StopTags}} {
		if l.words == nil {
			unset(m, l.key)
			continue
		}
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if len(l.words) == 0 {
			seq.Style = yaml.FlowStyle // []
		}
		for _, w := range l.words {
			seq.Content = append(seq.Content, str(w, 0))
		}
		set(m, l.key, seq)
	}
	var buf bytes.Buffer
	if len(m.Content) > 0 {
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(&doc); err != nil {
			return err
		}
		if err := enc.Close(); err != nil {
			return err
		}
	}
	return writeFileAtomic(p, buf.Bytes())
}
