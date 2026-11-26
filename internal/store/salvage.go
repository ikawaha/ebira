package store

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SalvageSearch returns the search mode of ConfigFile of the notes directory
// dir that cannot be read, from its own line at the top level, when that
// line reads, or "". The safe mode starts the server with it, so that the
// index, made in that mode, is kept rather than made again.
func SalvageSearch(dir string) string {
	b, err := readFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "search:") {
			continue
		}
		var v struct {
			Search string `yaml:"search"`
		}
		if yaml.Unmarshal([]byte(line), &v) == nil && v.Search != "" {
			return v.Search
		}
	}
	return ""
}
