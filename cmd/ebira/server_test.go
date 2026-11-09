package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/daemon"
)

// setStateHome points the XDG state directory at t's temporary directory.
func setStateHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// legacyState makes the state directory that an earlier build kept for the
// notes directory dir, keyed by the directory above it, and returns it.
func legacyState(t *testing.T, dir string) string {
	t.Helper()
	sd, err := config.StateDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, config.LegacyDataDirFile), []byte(filepath.Dir(dir)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return sd
}

// TestLoopback checks which listening hosts are reached from this computer
// only, below which the server does not warn about auth: none.
func TestLoopback(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":   true,
		"127.0.0.1":   true,
		"127.0.0.2":   true,
		"::1":         true,
		"":            false, // every address
		"0.0.0.0":     false,
		"::":          false,
		"192.168.0.2": false,
		"example.com": false,
	} {
		if got := loopback(host); got != want {
			t.Errorf("loopback(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestServerDir(t *testing.T) {
	setStateHome(t)
	dir := filepath.Join(t.TempDir(), "notes")
	sd, err := stateDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, running, err := serverDir(dir); err != nil || running || got != sd {
		t.Errorf("no server: %s, %v, %v", got, running, err)
	}

	// A server of an earlier build holds the lock in the state directory of
	// the directory above.
	old, err := daemon.Lock(legacyState(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if got, running, err := serverDir(dir); err != nil || !running || got == sd {
		t.Errorf("earlier build: %s, %v, %v", got, running, err)
	}
	if err := removeLegacyDirs(dir); !errors.Is(err, daemon.ErrRunning) {
		t.Errorf("removeLegacyDirs while it runs: %v", err)
	}
	old.Close()

	// A server of this build for the directory above, as a notes directory
	// with a notebook named notes, is not of dir.
	other, err := stateDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Prepare(other, filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	above, err := daemon.Lock(other)
	if err != nil {
		t.Fatal(err)
	}
	if got, running, err := serverDir(dir); err != nil || running || got != sd {
		t.Errorf("server of the directory above: %s, %v, %v", got, running, err)
	}
	above.Close()

	lock, err := daemon.Lock(sd)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if got, running, err := serverDir(dir); err != nil || !running || got != sd {
		t.Errorf("this build: %s, %v, %v", got, running, err)
	}
}

func TestRemoveLegacyDirs(t *testing.T) {
	setStateHome(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := removeLegacyDirs(filepath.Join(t.TempDir(), "notes")); err != nil {
		t.Errorf("no directory: %v", err)
	}

	write := func(dir string, names ...string) {
		t.Helper()
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	gone := func(dir string) bool {
		_, err := os.Stat(dir)
		return errors.Is(err, os.ErrNotExist)
	}

	dir := filepath.Join(t.TempDir(), "notes")
	sd := legacyState(t, dir)
	write(sd, daemon.LockFile, daemon.StateFile, daemon.LogFile)
	cd, err := config.CacheDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cd, config.LegacyDataDirFile), []byte(filepath.Dir(dir)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(cd, "index.sqlite", "mine.txt")
	if err := removeLegacyDirs(dir); err != nil {
		t.Fatal(err)
	}
	if !gone(sd) {
		t.Error("only files of earlier builds: kept")
	}
	if gone(cd) {
		t.Error("with another file: removed")
	}
}

func TestServerStatusJSON(t *testing.T) {
	setStateHome(t)
	dir := t.TempDir()
	var out strings.Builder
	if err := serverStatusJSON(dir, &out); err != nil {
		t.Fatal(err)
	}
	var got statusJSON
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("%v in %q", err, out.String())
	}
	if got.NotesDir != dir || got.Running || got.State != nil {
		t.Errorf("serverStatusJSON(%q) = %+v, want the directory, not running, without a state", dir, got)
	}
}
