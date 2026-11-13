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
	"regexp"
	"slices"
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
	// Auth is how the REST API authenticates people: none or passkey.
	Auth string `yaml:"auth"`
	// GitSync is the interval of syncing the notes with git, such as 10m.
	GitSync string `yaml:"git_sync"`
	// TUIPreview is how the preview of the TUI shows a note: rendered (the
	// default) or source, the Markdown itself, which can be edited at once.
	// The TUI writes it with Set when it is switched.
	TUIPreview string `yaml:"tui_preview"`
	// TUISearch is what the search box of the TUI does: search (the default),
	// listing the notes found by relevance, or filter, narrowing the list in
	// its order. The TUI writes it with Set when it is switched.
	TUISearch string `yaml:"tui_search"`

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
	return Parse(b, p)
}

// Parse reads b as the settings file at path p, as Load does.
func Parse(b []byte, p string) (Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) { // io.EOF: an empty file
		return c, fmt.Errorf("%s: %w", p, plainError(err))
	}
	if c.LegacyDataDir != "" {
		return c, fmt.Errorf("%s: data_dir is replaced by notes_dir, the notes directory itself: write notes_dir: %s", p, filepath.Join(c.LegacyDataDir, "notes"))
	}
	if c.NotesDir != "" {
		var err error
		if c.NotesDir, err = resolve(c.NotesDir, filepath.Dir(p)); err != nil {
			return c, fmt.Errorf("%s: notes_dir: %w", p, err)
		}
	}
	return c, nil
}

// notesKeys are keys of the settings shared with the notes, in _ebira.yaml
// of the notes directory (see internal/store), which a settings file may be
// given by mistake.
var notesKeys = []string{"search", "stop_words", "stop_tags", "marks"}

// unknownField matches an error of yaml for an unknown key, which names the
// Go type.
var unknownField = regexp.MustCompile(`^line (\d+): field (\S+) not found in type \S+$`)

// plainError says the errors of yaml for unknown keys without the Go type,
// telling where a key of the settings shared with the notes goes.
func plainError(err error) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return err
	}
	msgs := make([]string, len(te.Errors))
	for i, e := range te.Errors {
		m := unknownField.FindStringSubmatch(e)
		switch {
		case m == nil:
			msgs[i] = e
		case slices.Contains(notesKeys, m[2]):
			msgs[i] = fmt.Sprintf("line %s: %s is not a key of the settings file; write it in _ebira.yaml of the notes directory, shared with the notes", m[1], m[2])
		default:
			msgs[i] = fmt.Sprintf("line %s: unknown key %s", m[1], m[2])
		}
	}
	return errors.New(strings.Join(msgs, "; "))
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

// Write replaces the settings file with b, creating the file when it does
// not exist. The file is replaced at once, so that a command reading it
// meanwhile reads the old or the new content.
func Write(b []byte) (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	// A link, such as to a repository of dotfiles, stays a link.
	target := p
	if t, err := filepath.EvalSymlinks(p); err == nil {
		target = t
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name()) // after a failure; renamed otherwise
	if _, err := f.Write(b); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return p, os.Rename(f.Name(), target)
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

// Set writes key: value to the settings file, replacing the value of key when
// the file has it, else appending it, so that the rest of the file, with its
// comments, is kept. The file is created when it does not exist.
func Set(key, value string) (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	v, err := yaml.Marshal(map[string]string{key: value})
	if err != nil {
		return "", err
	}
	line := strings.TrimSuffix(string(v), "\n")
	lines := strings.Split(string(b), "\n")
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", p, err)
	}
	replaced := false
	if len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		m := doc.Content[0].Content
		for i := 0; i+1 < len(m); i += 2 {
			k, val := m[i], m[i+1]
			// Only a value on the line of its key is replaced; another one,
			// such as a block, is left to the user.
			if k.Value != key || val.Kind != yaml.ScalarNode || val.Line != k.Line {
				continue
			}
			if val.LineComment != "" {
				line += " " + val.LineComment
			}
			lines[k.Line-1] = line
			replaced = true
		}
	}
	out := strings.Join(lines, "\n")
	if !replaced {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += line + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, []byte(out), 0o644)
}

// UpdatedKeys are the keys of the settings file that Update writes, those
// the web UI shows to change: not notes_dir and addr, which a restart of the
// server keeps.
var UpdatedKeys = []string{"lang", "theme", "auth", "git_sync", "watch", "provider", "model", "tui_preview", "tui_search"}

// Update writes the values of c for UpdatedKeys to the settings file, and
// keeps the rest of it, with its comments. A zero value is left out, for the
// default. The file is created when it does not exist.
func Update(c Config) (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	old, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(old)) > 0 {
		if err := yaml.Unmarshal(old, &doc); err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	SetKeys(doc.Content[0], c, UpdatedKeys)
	m := doc.Content[0]
	var buf bytes.Buffer
	if len(m.Content) > 0 {
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(&doc); err != nil {
			return "", err
		}
		if err := enc.Close(); err != nil {
			return "", err
		}
	}
	return Write(buf.Bytes())
}

// SetKeys sets keys of the mapping node m, of a settings file or of the
// defaults in _ebira.yaml, to their values in c, keeping the comments of the
// old values; a zero value removes its key, for the default. keys are of
// those of the settings file other than notes_dir.
func SetKeys(m *yaml.Node, c Config, keys []string) {
	values := map[string]string{"addr": c.Addr, "lang": c.Lang, "theme": c.Theme, "auth": c.Auth, "git_sync": c.GitSync, "provider": c.Provider, "model": c.Model, "tui_preview": c.TUIPreview, "tui_search": c.TUISearch}
	for _, k := range keys {
		var v *yaml.Node
		switch {
		case k == "watch":
			if c.Watch != nil {
				v = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(*c.Watch)}
			}
		case values[k] != "":
			v = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: values[k]}
		}
		setKey(m, k, v)
	}
}

// setKey sets key of the mapping node m to v, keeping the comments of the old
// value, or removes it for nil.
func setKey(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		if v == nil {
			removePair(m, i)
			return
		}
		old := m.Content[i+1]
		v.HeadComment, v.LineComment, v.FootComment = old.HeadComment, old.LineComment, old.FootComment
		m.Content[i+1] = v
		return
	}
	if v != nil {
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	}
}

// removePair removes the pair at i of the mapping node m, keeping its
// comments, such as lines commented out after it: they go before the next
// key, or to the end of m.
func removePair(m *yaml.Node, i int) {
	k, v := m.Content[i], m.Content[i+1]
	var kept []string
	for _, c := range []string{k.HeadComment, v.HeadComment, v.FootComment, k.FootComment} {
		if c != "" {
			kept = append(kept, c)
		}
	}
	m.Content = append(m.Content[:i], m.Content[i+2:]...)
	if len(kept) == 0 {
		return
	}
	c := strings.Join(kept, "\n")
	if i < len(m.Content) {
		next := m.Content[i]
		if next.HeadComment != "" {
			c += "\n" + next.HeadComment
		}
		next.HeadComment = c
		return
	}
	if m.FootComment != "" {
		c = m.FootComment + "\n" + c
	}
	m.FootComment = c
}
