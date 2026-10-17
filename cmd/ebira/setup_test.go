package main

import (
	"errors"
	"flag"
	"os"
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
	old, oldFlags := settings, setupFlags
	settings, setupFlags = config.Config{}, nil
	t.Cleanup(func() { settings, setupFlags = old, oldFlags })
	return home
}

// dataDirFlags returns a flag set with -data-dir parsed from args.
func dataDirFlags(t *testing.T, args ...string) (*flag.FlagSet, *string) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	d := fs.String("data-dir", ".", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs, d
}

func TestDataDirOf(t *testing.T) {
	setupEnv(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	// No notes here, and nothing given: an error when not interactive.
	fs, d := dataDirFlags(t)
	if _, err := dataDirOf(fs, *d, false, nil, nil); !errors.Is(err, errNoNotes) {
		t.Errorf("no notes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "notes")); !errors.Is(err, os.ErrNotExist) {
		t.Error("notes/ was made")
	}

	// A data directory given by the flag is used as is.
	other := t.TempDir()
	fs, d = dataDirFlags(t, "-data-dir", other)
	if dir, err := dataDirOf(fs, *d, false, nil, nil); err != nil || dir != other {
		t.Errorf("-data-dir: %s, %v", dir, err)
	}

	// So is one of the settings file.
	settings.DataDir = other
	fs, d = dataDirFlags(t)
	*d = other
	if dir, err := dataDirOf(fs, *d, false, nil, nil); err != nil || dir != other {
		t.Errorf("data_dir: %s, %v", dir, err)
	}
	settings.DataDir = ""

	// The current directory with notes.
	if err := os.Mkdir(filepath.Join(cwd, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs, d = dataDirFlags(t)
	if dir, err := dataDirOf(fs, *d, false, nil, nil); err != nil || dir != cwd {
		t.Errorf("notes here: %s, %v", dir, err)
	}
}

func TestSetup(t *testing.T) {
	home := setupEnv(t)
	cwd := t.TempDir()
	var out strings.Builder

	// All defaults: ~/ebira, ja search, ja for $LANG, saved.
	dir, err := setup(cwd, strings.NewReader("\n\n\n\n\n"), &out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if want := filepath.Join(home, "ebira"); dir != want {
		t.Errorf("dir: %s, want %s", dir, want)
	}
	b, err := os.ReadFile(filepath.Join(dir, "notes", "_ebira.yaml"))
	if err != nil || string(b) != "search: ja\n" {
		t.Errorf("_ebira.yaml: %q, %v", b, err)
	}
	b, err = os.ReadFile(filepath.Join(home, ".config", "ebira", "config.yaml"))
	if err != nil || string(b) != "data_dir: ~/ebira\nlang: ja\n" {
		t.Errorf("config.yaml: %q, %v", b, err)
	}
	if c, err := config.Load(); err != nil || c.DataDir != dir {
		t.Errorf("Load: %+v, %v", c, err)
	}
	if settings.DataDir != dir || settings.Lang != "ja" || setupFlags != nil {
		t.Errorf("settings: %+v, flags %v", settings, setupFlags)
	}
}

func TestSetupAnswers(t *testing.T) {
	home := setupEnv(t)
	cwd := t.TempDir()
	var out strings.Builder

	// A relative directory, bigram, en, not saved; a wrong answer is asked
	// again.
	dir, err := setup(cwd, strings.NewReader("my notes\nmaybe\nn\nfr\nEN\nno\ny\n"), &out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if want := filepath.Join(cwd, "my notes"); dir != want {
		t.Errorf("dir: %s, want %s", dir, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes", "_ebira.yaml")); string(b) != "search: bigram\n" {
		t.Errorf("_ebira.yaml: %q", b)
	}
	if !slices.Equal(setupFlags, []string{"-lang", "en"}) {
		t.Errorf("flags of the server: %v", setupFlags)
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
	again, err := setup(cwd, strings.NewReader(dir+"\nn\n\n"), &out)
	if err != nil || again != dir {
		t.Fatalf("existing: %s, %v\n%s", again, err, out.String())
	}
	if strings.Contains(out.String(), "Use ja") || strings.Contains(out.String(), "Language") {
		t.Errorf("asked the search mode:\n%s", out.String())
	}

	// Canceled at the end: nothing is made.
	out.Reset()
	if _, err := setup(cwd, strings.NewReader("other\n\n\nn\n"), &out); err == nil {
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
