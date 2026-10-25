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
