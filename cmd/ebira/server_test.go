package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikawaha/ebira/internal/daemon"
)

// setStateHome points the XDG state directory at t's temporary directory.
func setStateHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func TestServerDir(t *testing.T) {
	setStateHome(t)
	dir := t.TempDir()
	sd, err := stateDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, running, err := serverDir(dir); err != nil || running || got != sd {
		t.Errorf("no server: %s, %v, %v", got, running, err)
	}
	if _, err := os.Stat(legacyDir(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("serverDir created %s", legacyDir(dir))
	}

	// A server of an earlier build holds the lock in the data directory.
	old, err := daemon.Lock(legacyDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got, running, err := serverDir(dir); err != nil || !running || got != legacyDir(dir) {
		t.Errorf("earlier build: %s, %v, %v", got, running, err)
	}
	if err := removeLegacyDir(dir); !errors.Is(err, daemon.ErrRunning) {
		t.Errorf("removeLegacyDir while it runs: %v", err)
	}
	old.Close()

	lock, err := daemon.Lock(sd)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if got, running, err := serverDir(dir); err != nil || !running || got != sd {
		t.Errorf("this build: %s, %v, %v", got, running, err)
	}
}

func TestRemoveLegacyDir(t *testing.T) {
	setStateHome(t)
	if err := removeLegacyDir(t.TempDir()); err != nil {
		t.Errorf("no directory: %v", err)
	}

	write := func(dir string, names ...string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	dir := t.TempDir()
	write(legacyDir(dir), legacyFiles...)
	if err := removeLegacyDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyDir(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("only files of earlier builds: kept (%v)", err)
	}

	dir = t.TempDir()
	write(legacyDir(dir), "index.sqlite", "mine.txt")
	if err := removeLegacyDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(legacyDir(dir), "index.sqlite")); err != nil {
		t.Errorf("with another file: %v", err)
	}
}
