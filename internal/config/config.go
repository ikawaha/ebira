// Package config reads the settings file of the ebira commands and computes
// where their files go, following the XDG Base Directory Specification on
// macOS as on Linux:
//
//   - the settings file: $XDG_CONFIG_HOME/ebira/config.yaml (~/.config), or
//     the file named by $EBIRA_CONFIG.
//   - the state of a notes directory (the server's lock, server.json and
//     log): $XDG_STATE_HOME/ebira/<name>-<hash>/ (~/.local/state).
//   - the cache of a notes directory (the index): $XDG_CACHE_HOME/ebira/index/<name>-<hash>/
//     (~/.cache).
//
// <name> is the base name of the notes directory and <hash> the first 12 hex
// digits of the SHA-256 of its absolute path with symbolic links resolved.
// Each of those directories has a file notes-dir with that path.
//
// The settings file holds the settings of this machine and user. The
// settings shared with the notes, such as the search mode, are in
// _ebira.yaml of the notes directory instead (see internal/store), and
// secrets in neither.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Env names the environment variable that replaces the path of the settings
// file.
const Env = "EBIRA_CONFIG"

// NotesDirFile is the file in a state or cache directory with the path of
// its notes directory.
const NotesDirFile = "notes-dir"

// LegacyDataDirFile is the file in a state or cache directory of earlier
// builds with the path of its data directory, the directory above the notes
// directory then.
const LegacyDataDirFile = "data-dir"

// Config is the settings file. Unset values are zero; the commands use them
// as the defaults of their flags.
type Config struct {
	// NotesDir is the default notes directory. ~ and paths relative to the
	// settings file are resolved by Load.
	NotesDir string `yaml:"notes_dir"`
	// Addr is the address the server listens on.
	Addr string `yaml:"addr"`
	// Watch tells whether the server watches the notes directory.
	Watch *bool `yaml:"watch"`
	// Provider and Model choose the model of the assistant.
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	// Lang is the language of the UI, the web UI and the TUI, such as en or ja.
	Lang string `yaml:"lang"`
	// Theme is the color theme of the UI: auto (the system's light or dark
	// mode), light or dark. The TUI, which cannot tell the background of the
	// terminal, takes light as -light and the others as dark.
	Theme string `yaml:"theme"`
	// GitSync is the interval of syncing the notes with git, such as 10m.
	GitSync string `yaml:"git_sync"`

	// LegacyDataDir is data_dir of earlier builds, the directory above the
	// notes directory; Load fails with it, telling what to write instead.
	LegacyDataDir string `yaml:"data_dir"`
}

// Path returns the path of the settings file.
func Path() (string, error) {
	if p := os.Getenv(Env); p != "" {
		return p, nil
	}
	d, err := base("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ebira", "config.yaml"), nil
}

// Load reads the settings file. A missing file is an empty Config; an unknown
// key is an error, so that a misspelled one is not ignored.
func Load() (Config, error) {
	var c Config
	p, err := Path()
	if err != nil {
		return c, err
	}
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
	if c.LegacyDataDir != "" {
		return c, fmt.Errorf("%s: data_dir is replaced by notes_dir, the notes directory itself: write notes_dir: %s", p, filepath.Join(c.LegacyDataDir, "notes"))
	}
	if c.NotesDir != "" {
		if c.NotesDir, err = resolve(c.NotesDir, filepath.Dir(p)); err != nil {
			return c, fmt.Errorf("%s: notes_dir: %w", p, err)
		}
	}
	return c, nil
}

// resolve expands a leading ~ of path and makes it absolute against dir.
func resolve(path, dir string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path), nil
}

// StateDir returns the state directory of the notes directory notesDir.
func StateDir(notesDir string) (string, error) {
	d, err := base("XDG_STATE_HOME", filepath.Join(".local", "state"))
	if err != nil {
		return "", err
	}
	return under(filepath.Join(d, "ebira"), notesDir)
}

// CacheDir returns the cache directory of the notes directory notesDir.
func CacheDir(notesDir string) (string, error) {
	d, err := base("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return "", err
	}
	return under(filepath.Join(d, "ebira", "index"), notesDir)
}

// Prepare creates dir, a directory of StateDir or CacheDir, and writes the
// path of its notes directory to NotesDirFile in it.
func Prepare(dir, notesDir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The same path may have been a data directory of an earlier build.
	if err := os.Remove(filepath.Join(dir, LegacyDataDirFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	p := filepath.Join(dir, NotesDirFile)
	want := notesDir + "\n"
	if b, err := os.ReadFile(p); err == nil && string(b) == want {
		return nil
	}
	return os.WriteFile(p, []byte(want), 0o644)
}

// under returns the directory of dir under root.
func under(root, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	// Two paths of the same directory share its server and index.
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	name := filepath.Base(abs)
	if name == string(filepath.Separator) {
		name = "root"
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(root, name+"-"+hex.EncodeToString(sum[:])[:12]), nil
}

// base returns the directory of the XDG variable env, or home/def when it is
// unset or not absolute, as the specification says.
func base(env, def string) (string, error) {
	if d := os.Getenv(env); filepath.IsAbs(d) {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, def), nil
}

// Save adds the set values of c, notes_dir, addr, lang and git_sync, to the
// settings file, creating the file when it does not exist. A notes directory under the home
// directory is written with ~. The keys are appended, so the rest of the
// file, with its comments, is kept; the file must not have them yet.
func Save(c Config) (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	var add []byte
	for _, kv := range [][2]string{{"notes_dir", homeRel(c.NotesDir)}, {"addr", c.Addr}, {"lang", c.Lang}, {"git_sync", c.GitSync}} {
		if kv[1] == "" {
			continue
		}
		v, err := yaml.Marshal(map[string]string{kv[0]: kv[1]})
		if err != nil {
			return "", err
		}
		add = append(add, v...)
	}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if len(b) > 0 && !bytes.HasSuffix(b, []byte("\n")) {
		b = append(b, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, append(b, add...), 0o644)
}

// homeRel returns path with the home directory written as ~.
func homeRel(path string) string {
	home, err := os.UserHomeDir()
	if path == "" || err != nil {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join("~", rel)
	}
	return path
}
