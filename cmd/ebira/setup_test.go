package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ikawaha/ebira/internal/config"
)

// setupEnv points the home directory and the settings file at t's temporary
// directories, with no settings, and returns the home directory.
func setupEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(config.Env, "")
	t.Setenv("LANG", "ja_JP.UTF-8")
	old, oldFlags, oldLang := settings, setupFlags, setupLang
	settings, setupFlags, setupLang = config.Config{}, nil, ""
	t.Cleanup(func() { settings, setupFlags, setupLang = old, oldFlags, oldLang })
	return home
}

// notesDirFlags returns a flag set with -notes parsed from args.
func notesDirFlags(t *testing.T, args ...string) (*flag.FlagSet, *string) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	d := fs.String("notes", "notes", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs, d
}

func TestNotesDirOf(t *testing.T) {
	setupEnv(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	// No notes here, and nothing given: an error when not interactive.
	fs, d := notesDirFlags(t)
	if _, err := notesDirOf(fs, *d, false, nil, nil); !errors.Is(err, errNoNotes) {
		t.Errorf("no notes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "notes")); !errors.Is(err, os.ErrNotExist) {
		t.Error("notes/ was made")
	}

	// A notes directory given by the flag is used as is.
	other := t.TempDir()
	fs, d = notesDirFlags(t, "-notes", other)
	if dir, err := notesDirOf(fs, *d, false, nil, nil); err != nil || dir != other {
		t.Errorf("-notes: %s, %v", dir, err)
	}

	// So is one of the settings file.
	settings.NotesDir = other
	fs, d = notesDirFlags(t)
	*d = other
	if dir, err := notesDirOf(fs, *d, false, nil, nil); err != nil || dir != other {
		t.Errorf("notes_dir: %s, %v", dir, err)
	}
	settings.NotesDir = ""

	// notes/ in the current directory.
	if err := os.Mkdir(filepath.Join(cwd, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs, d = notesDirFlags(t)
	if dir, err := notesDirOf(fs, *d, false, nil, nil); err != nil || dir != filepath.Join(cwd, "notes") {
		t.Errorf("notes here: %s, %v", dir, err)
	}
}

func TestSetup(t *testing.T) {
	home := setupEnv(t)
	cwd := t.TempDir()
	var out strings.Builder

	// All defaults: ~/notes, no git, ja search, ja for $LANG, saved.
	dir, err := setup(cwd, strings.NewReader("\n\n\n\n\n\n"), &out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if want := filepath.Join(home, "notes"); dir != want {
		t.Errorf("dir: %s, want %s", dir, want)
	}
	b, err := os.ReadFile(filepath.Join(dir, "_ebira.yaml"))
	if err != nil || string(b) != "search: ja\n" {
		t.Errorf("_ebira.yaml: %q, %v", b, err)
	}
	b, err = os.ReadFile(filepath.Join(home, ".config", "ebira", "config.yaml"))
	if err != nil || string(b) != "notes_dir: ~/notes\naddr: "+fallbackAddr+"\nlang: ja\n" {
		t.Errorf("config.yaml: %q, %v", b, err)
	}
	if c, err := config.Load(); err != nil || c.NotesDir != dir {
		t.Errorf("Load: %+v, %v", c, err)
	}
	if settings.NotesDir != dir || settings.Addr != fallbackAddr || settings.Lang != "ja" || setupFlags != nil {
		t.Errorf("settings: %+v, flags %v", settings, setupFlags)
	}
}

func TestSetupAnswers(t *testing.T) {
	home := setupEnv(t)
	cwd := t.TempDir()
	var out strings.Builder

	// A relative directory, bigram, en, not saved; a wrong answer is asked
	// again.
	dir, err := setup(cwd, strings.NewReader("my notes\n\nmaybe\nbigram\nfr\nEN\nmaybe\nno\ny\n"), &out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if want := filepath.Join(cwd, "my notes"); dir != want {
		t.Errorf("dir: %s, want %s", dir, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "_ebira.yaml")); string(b) != "search: bigram\n" {
		t.Errorf("_ebira.yaml: %q", b)
	}
	if !slices.Equal(setupFlags, []string{"-lang", "en"}) {
		t.Errorf("flags of the server: %v", setupFlags)
	}
	if setupLang != "en" {
		t.Errorf("language of the TUI: %q", setupLang)
	}
	if !strings.Contains(out.String(), "Answer y or n.") || !strings.Contains(out.String(), "Answer one of en, ja.") {
		t.Errorf("no retry:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ebira", "config.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("config.yaml was written: %v", err)
	}

	// Existing notes: no question about the search mode, and their settings
	// are kept. With lang in the settings file, no question about it.
	out.Reset()
	settings.Lang = "en"
	again, err := setup(cwd, strings.NewReader(dir+"\n\nn\n\n"), &out)
	if err != nil || again != dir {
		t.Fatalf("existing: %s, %v\n%s", again, err, out.String())
	}
	if strings.Contains(out.String(), "Search mode (") || strings.Contains(out.String(), "Language") {
		t.Errorf("asked the search mode:\n%s", out.String())
	}

	// A directory with files that are not notes of ebira is confirmed.
	out.Reset()
	foreign := filepath.Join(cwd, "foreign")
	if err := os.MkdirAll(filepath.Join(foreign, "memo"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := setup(cwd, strings.NewReader("foreign\n\nforeign\ny\n\nbigram\nn\n\n"), &out)
	if err != nil || got != foreign {
		t.Fatalf("foreign: %s, %v\n%s", got, err, out.String())
	}
	if strings.Count(out.String(), "has files, but no notes of ebira") != 2 || !strings.Contains(out.String(), "Search mode (") {
		t.Errorf("foreign:\n%s", out.String())
	}

	// Canceled at the end: nothing is made.
	out.Reset()
	if _, err := setup(cwd, strings.NewReader("other\n\n\n\nn\n"), &out); err == nil {
		t.Error("canceled: no error")
	}
	if _, err := os.Stat(filepath.Join(cwd, "other")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("canceled: made %v", err)
	}

	// End of input: canceled.
	if _, err := setup(cwd, strings.NewReader(""), &out); err == nil {
		t.Error("EOF: no error")
	}
}

func TestSetupGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "test")
		t.Setenv(k+"_EMAIL", "test@example.com")
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--quiet", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	// The first machine: an empty repository, after a wrong URL; the notes
	// are pushed, and every answer saved.
	home := setupEnv(t)
	cwd := t.TempDir()
	var out strings.Builder
	in := "first\n" + filepath.Join(cwd, "none.git") + "\n" + remote + "\n\n\n\n\n\n"
	dir, err := setup(cwd, strings.NewReader(in), &out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Cannot read the repository") || !strings.Contains(out.String(), "Git:          push to "+remote) {
		t.Errorf("output:\n%s", out.String())
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "ebira", "config.yaml"))
	if err != nil || !strings.HasSuffix(string(b), "lang: ja\ngit_sync: 10m\n") {
		t.Errorf("config.yaml: %q, %v", b, err)
	}
	if log, err := exec.Command("git", "-C", remote, "log", "--format=%s", "--all").CombinedOutput(); err != nil || !strings.Contains(string(log), "Update notes") {
		t.Errorf("not pushed: %s, %v", log, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("the notes are not a repository: %v", err)
	}

	// The second machine clones them, with their search mode; not saved,
	// so the server gets the interval and the language.
	setupEnv(t)
	out.Reset()
	dir, err = setup(cwd, strings.NewReader("second\n"+remote+"\n5m\nen\nn\n\n"), &out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "Search mode (") || !strings.Contains(out.String(), "Git:          clone "+remote) {
		t.Errorf("output:\n%s", out.String())
	}
	if b, err := os.ReadFile(filepath.Join(dir, "_ebira.yaml")); err != nil || string(b) != "search: ja\n" {
		t.Errorf("cloned _ebira.yaml: %q, %v", b, err)
	}
	if !slices.Equal(setupFlags, []string{"-lang", "en", "-git-sync", "5m"}) {
		t.Errorf("flags of the server: %v", setupFlags)
	}
}

func TestSyncedFolder(t *testing.T) {
	home := setupEnv(t)
	cloud := filepath.Join(home, "Library", "CloudStorage", "Dropbox")
	if err := os.MkdirAll(cloud, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cloud, filepath.Join(home, "Dropbox")); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]string{
		filepath.Join(home, "Dropbox", "ebira"):                                                "Dropbox", // through the link, not made yet
		filepath.Join(home, "Library", "CloudStorage", "GoogleDrive-you@example.com", "ebira"): "Google Drive",
		filepath.Join(home, "Library", "CloudStorage", "OneDrive-Personal", "ebira"):           "OneDrive",
		filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs", "ebira"):     "iCloud Drive",
		filepath.Join(home, "ebira"):                                                           "",
		t.TempDir():                                                                            "",
	} {
		if got := syncedFolder(dir); got != want {
			t.Errorf("%s: %q, want %q", dir, got, want)
		}
	}

	// Setup does not ask for git there.
	var out strings.Builder
	dir := filepath.Join(home, "Dropbox", "ebira")
	got, err := setup(t.TempDir(), strings.NewReader(dir+"\n\n\n\n\n"), &out)
	if err != nil || got != dir {
		t.Fatalf("%s, %v\n%s", got, err, out.String())
	}
	if !strings.Contains(out.String(), "The notes are in Dropbox") || strings.Contains(out.String(), "URL of the repository") {
		t.Errorf("output:\n%s", out.String())
	}
}
