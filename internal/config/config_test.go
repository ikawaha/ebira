package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setHome points the home directory and the XDG variables at t's temporary
// directory, with the XDG variables unset.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, env := range []string{Env, "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(env, "")
	}
	return home
}

func TestPath(t *testing.T) {
	home := setHome(t)
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "ebira", "config.yaml"); p != want {
		t.Errorf("default: %s, want %s", p, want)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if p, _ := Path(); p != filepath.Join(home, "xdg", "ebira", "config.yaml") {
		t.Errorf("XDG_CONFIG_HOME: %s", p)
	}
	// A relative XDG path is ignored, as the specification says.
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if p, _ := Path(); p != filepath.Join(home, ".config", "ebira", "config.yaml") {
		t.Errorf("relative XDG_CONFIG_HOME: %s", p)
	}

	t.Setenv(Env, filepath.Join(home, "other.yaml"))
	if p, _ := Path(); p != filepath.Join(home, "other.yaml") {
		t.Errorf("%s: %s", Env, p)
	}
}

func TestLoad(t *testing.T) {
	home := setHome(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if c != (Config{}) {
		t.Errorf("missing file: %+v", c)
	}

	dir := filepath.Join(home, "conf")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.yaml")
	t.Setenv(Env, p)
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("")
	if c, err := Load(); err != nil || c != (Config{}) {
		t.Errorf("empty file: %+v, %v", c, err)
	}

	write("notes_dir: ~/notes\naddr: localhost:9090\nwatch: false\nprovider: gemini\nmodel: m\ngit_sync: 10m\n")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.NotesDir != filepath.Join(home, "notes") || c.Addr != "localhost:9090" || c.Watch == nil || *c.Watch || c.Provider != "gemini" || c.Model != "m" || c.GitSync != "10m" {
		t.Errorf("got %+v", c)
	}

	write("notes_dir: data\n")
	if c, _ := Load(); c.NotesDir != filepath.Join(dir, "data") {
		t.Errorf("relative notes_dir: %s", c.NotesDir)
	}

	// data_dir of earlier builds tells what to write instead.
	write("data_dir: ~/ebira\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "notes_dir: ~/ebira/notes") {
		t.Errorf("data_dir: %v", err)
	}

	write("serach: ja\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), p) {
		t.Errorf("unknown key: %v", err)
	}
}

func TestStateAndCacheDir(t *testing.T) {
	home := setHome(t)
	data := filepath.Join(home, "My Notes")
	if err := os.Mkdir(data, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(data, link); err != nil {
		t.Fatal(err)
	}

	sd, err := StateDir(data)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(sd) != filepath.Join(home, ".local", "state", "ebira") || !strings.HasPrefix(filepath.Base(sd), "My Notes-") {
		t.Errorf("state dir: %s", sd)
	}
	cd, err := CacheDir(data)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(cd) != filepath.Join(home, ".cache", "ebira", "index") || filepath.Base(cd) != filepath.Base(sd) {
		t.Errorf("cache dir: %s", cd)
	}

	// The same directory through a symbolic link shares the directories.
	if l, _ := StateDir(link); l != sd {
		t.Errorf("through a link: %s, want %s", l, sd)
	}
	other, _ := StateDir(filepath.Join(home, "other", "My Notes"))
	if other == sd {
		t.Errorf("another notes directory has the same state dir %s", sd)
	}

	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "st"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "ca"))
	if d, _ := StateDir(data); d != filepath.Join(home, "st", "ebira", filepath.Base(sd)) {
		t.Errorf("XDG_STATE_HOME: %s", d)
	}
	if d, _ := CacheDir(data); d != filepath.Join(home, "ca", "ebira", "index", filepath.Base(sd)) {
		t.Errorf("XDG_CACHE_HOME: %s", d)
	}
}

func TestPrepare(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	for range 2 {
		if err := Prepare(dir, "/data/notes"); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, NotesDirFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "/data/notes\n" {
		t.Errorf("%s: %q", NotesDirFile, b)
	}

	// The file of an earlier build, of the same path as a data directory, goes.
	if err := os.WriteFile(filepath.Join(dir, LegacyDataDirFile), []byte("/data/notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(dir, "/data/notes"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, LegacyDataDirFile)); !os.IsNotExist(err) {
		t.Errorf("%s is left: %v", LegacyDataDirFile, err)
	}
}

func TestSave(t *testing.T) {
	home := setHome(t)
	p, err := Save(Config{NotesDir: filepath.Join(home, "notes dir"), Addr: "localhost:13579", Lang: "ja"})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "notes_dir: ~/notes dir\naddr: localhost:13579\nlang: ja\n" {
		t.Errorf("new file: %q", b)
	}
	if c, err := Load(); err != nil || c.NotesDir != filepath.Join(home, "notes dir") || c.Addr != "localhost:13579" || c.Lang != "ja" {
		t.Errorf("new file: %+v, %v", c, err)
	}

	// Other keys and comments are kept; unset values are not written.
	if err := os.WriteFile(p, []byte("# mine\naddr: localhost:9090"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(Config{NotesDir: "/srv/notes"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "# mine\naddr: localhost:9090\nnotes_dir: /srv/notes\n" {
		t.Errorf("appended: %q", b)
	}
}

func TestSet(t *testing.T) {
	setHome(t)
	p, err := Set("tui_preview", "source")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "tui_preview: source\n" {
		t.Errorf("new file: %q", b)
	}

	// The value is replaced in place; other lines and comments are kept.
	if err := os.WriteFile(p, []byte("# mine\ntui_preview: source # how the TUI shows notes\nlang: ja"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Set("tui_preview", "rendered"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "# mine\ntui_preview: rendered # how the TUI shows notes\nlang: ja" {
		t.Errorf("replaced: %q", b)
	}
	if _, err := Set("theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "# mine\ntui_preview: rendered # how the TUI shows notes\nlang: ja\ntheme: dark\n" {
		t.Errorf("appended: %q", b)
	}
	if c, err := Load(); err != nil || c.TUIPreview != "rendered" || c.Theme != "dark" {
		t.Errorf("loaded: %+v, %v", c, err)
	}
}

func TestWrite(t *testing.T) {
	home := setHome(t)
	p, err := Write([]byte("lang: ja\n"))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "lang: ja\n" {
		t.Errorf("new file: %q", b)
	}

	// The mode is kept.
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Write([]byte("lang: en\n")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}

	// A link stays a link, and its target is written.
	target := filepath.Join(home, "dotfiles", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("lang: en\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
	if _, err := Write([]byte("theme: dark\n")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "theme: dark\n" {
		t.Errorf("target: %q", b)
	}
	// No temporary file is left.
	if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
		t.Errorf("files: %v", es)
	}
}

func TestParseUnknownKeys(t *testing.T) {
	_, err := Parse([]byte("lang: ja\nsearch: ja\nlng: en\n"), "/c/config.yaml")
	if err == nil {
		t.Fatal("unknown keys passed")
	}
	want := "/c/config.yaml: line 2: search is not a key of the settings file; write it in _ebira.yaml of the notes directory, shared with the notes; line 3: unknown key lng"
	if err.Error() != want {
		t.Errorf("err = %q\nwant %q", err, want)
	}
}

func TestUpdate(t *testing.T) {
	setHome(t)
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	file := "# mine\nnotes_dir: ~/notes\nlang: en # the UI\ntheme: dark\n# auth: passkey\n"
	if err := os.WriteFile(p, []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := Update(Config{Lang: "ja", GitSync: "10m", Watch: &off}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{"# mine", "notes_dir: ~/notes", "lang: ja # the UI", "git_sync: 10m", "watch: false", "# auth: passkey"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "theme") {
		t.Errorf("theme, set to the default, is kept:\n%s", b)
	}
	c, err := Load()
	if err != nil || c.Lang != "ja" || c.GitSync != "10m" || c.Watch == nil || *c.Watch || c.Theme != "" {
		t.Errorf("read back = %+v, %v", c, err)
	}
}

func TestDefaults(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	if d, w, err := LoadDefaults(dir); err != nil || d != (Config{}) || w != nil {
		t.Errorf("without _ebira.yaml: %+v, %v, %v", d, w, err)
	}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "_ebira.yaml"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("search: ja\n")
	if d, w, err := LoadDefaults(dir); err != nil || d != (Config{}) || w != nil {
		t.Errorf("without defaults: %+v, %v, %v", d, w, err)
	}
	// The keys of a machine alone are left out, with a warning each.
	write("search: ja\ndefaults:\n  lang: ja\n  git_sync: 10m\n  auth: none\n  notes_dir: ~/x\n  addr: 0.0.0.0:13579\n")
	d, w, err := LoadDefaults(dir)
	if err != nil || d.Lang != "ja" || d.GitSync != "10m" || d.Auth != "" || d.NotesDir != "" || d.Addr != "" || len(w) != 3 {
		t.Errorf("left out: %+v, %v, %v", d, w, err)
	}
	for i, key := range []string{"auth", "notes_dir", "addr 0.0.0.0:13579"} { // in the order of the file
		if i < len(w) && !strings.Contains(w[i], "defaults: "+key) {
			t.Errorf("warning %d: %s", i, w[i])
		}
	}
	// They are left out whatever their form, without a type error.
	write("defaults:\n  auth: [none]\n  notes_dir: {a: b}\n  lang: en\n")
	if d, w, err := LoadDefaults(dir); err != nil || d.Lang != "en" || len(w) != 2 {
		t.Errorf("left out in any form: %+v, %v, %v", d, w, err)
	}
	// An unknown key is an error, as in the settings file.
	write("defaults:\n  lnag: ja\n")
	if _, _, err := LoadDefaults(dir); err == nil || !strings.Contains(err.Error(), "unknown key lnag") {
		t.Errorf("unknown key: %v", err)
	}
	write("defaults: ja\n")
	if _, _, err := LoadDefaults(dir); err == nil {
		t.Error("defaults not a mapping: no error")
	}
}

func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"localhost:13579": true, "127.0.0.1:13579": true, "127.0.0.2:1": true, "[::1]:13579": true,
		"[::ffff:127.0.0.1]:13579": true, "localhost.:13579": false, "LOCALHOST:13579": false,
		":13579": false, "0.0.0.0:13579": false, "[::]:13579": false, "example.com:80": false, "192.168.1.2:13579": false, "localhost": false,
	} {
		if got := Loopback(addr); got != want {
			t.Errorf("Loopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestMerge(t *testing.T) {
	no, yes := false, true
	c := Merge(Config{Lang: "en", Watch: &no}, Config{Lang: "ja", Theme: "dark", Watch: &yes, Model: "m"})
	if c.Lang != "en" || c.Theme != "dark" || c.Watch != &no || c.Model != "m" {
		t.Errorf("Merge: %+v", c)
	}
}
