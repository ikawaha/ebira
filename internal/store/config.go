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
