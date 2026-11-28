package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSafe checks the values of the environment variable that turn the safe
// mode on.
func TestSafe(t *testing.T) {
	for v, want := range map[string]bool{"": false, "0": false, "1": true, "true": true} {
		t.Setenv(SafeEnv, v)
		if got := Safe(); got != want {
			t.Errorf("Safe() with %s=%q = %v, want %v", SafeEnv, v, got, want)
		}
	}
}

// TestSalvage checks that the safe mode takes notes_dir from a settings file
// that Load refuses, when its line reads, and nothing else.
func TestSalvage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	t.Setenv(Env, p)
	for name, tc := range map[string]struct{ file, want string }{
		"broken below":     {"notes_dir: ~/notes\nlang: [ja\n", filepath.Join(home, "notes")},
		"broken above":     {"lang: ja\n  theme: : dark\nnotes_dir: \"/data/my notes\" # mine\n", "/data/my notes"},
		"relative":         {"addr: [\nnotes_dir: notes\n", filepath.Join(dir, "notes")},
		"unknown key":      {"notes_dir: /n\nnotess: x\n", "/n"},
		"nested only":      {"zenn:\n  notes_dir: /n\nlang: [\n", ""},
		"unreadable value": {"notes_dir: [/n\n", ""},
		"none":             {"lang: [\n", ""},
	} {
		if err := os.WriteFile(p, []byte(tc.file), 0o644); err != nil {
			t.Fatal(err)
		}
		c := Salvage()
		if c.NotesDir != tc.want {
			t.Errorf("%s: notes_dir %q, want %q", name, c.NotesDir, tc.want)
		}
		c.NotesDir = ""
		if c != (Config{}) {
			t.Errorf("%s: salvaged more than notes_dir: %+v", name, c)
		}
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if c := Salvage(); c != (Config{}) {
		t.Errorf("no file: %+v", c)
	}
}
