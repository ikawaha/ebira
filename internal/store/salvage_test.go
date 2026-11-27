package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSalvageSearch checks that the safe mode takes the search mode from
// _ebira.yaml that cannot be read, when its line reads, and LoadConfig, which
// tells the error, on a folder that is not there.
func TestSalvageSearch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ConfigFile)
	for name, tc := range map[string]struct{ file, want string }{
		"broken below":     {"search: ja\ndefaults:\n  lang: [ja\n", "ja"},
		"broken above":     {"marks: [\nsearch: \"bigram\" # two characters\n", "bigram"},
		"conflict":         {"<<<<<<< HEAD\nsearch: ja\n=======\nsearch: en\n>>>>>>> theirs\n", "ja"},
		"nested only":      {"defaults:\n  search: ja\nmarks: [\n", ""},
		"unreadable value": {"search: [ja\n", ""},
		"none":             {"marks: [\n", ""},
	} {
		if err := os.WriteFile(p, []byte(tc.file), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(dir); err == nil {
			t.Errorf("%s: LoadConfig read a broken file", name)
		}
		if got := SalvageSearch(dir); got != tc.want {
			t.Errorf("%s: search %q, want %q", name, got, tc.want)
		}
	}
	missing := filepath.Join(dir, "missing")
	if c, err := LoadConfig(missing); err != nil || c.Search != "" {
		t.Errorf("LoadConfig of a missing folder = %+v, %v; want none", c, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("LoadConfig made the folder: %v", err)
	}
	if got := SalvageSearch(missing); got != "" {
		t.Errorf("SalvageSearch of a missing folder = %q", got)
	}
}
