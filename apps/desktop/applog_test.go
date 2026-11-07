package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func size(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

func TestLogFileLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "desktop.log")
	l, err := openLog(path)
	if err != nil {
		t.Fatal(err)
	}
	line := append(bytes.Repeat([]byte("x"), 999), '\n')
	// Three times the limit: the log turns over twice, and one older file stays.
	for range 3 * maxLog / len(line) {
		if _, err := l.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if got := size(t, path); got > maxLog {
		t.Errorf("the log has %d bytes, more than %d", got, maxLog)
	}
	if got := size(t, path+".1"); got > maxLog || got < maxLog-int64(len(line)) {
		t.Errorf("the older log has %d bytes, want about %d", got, maxLog)
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Errorf("a second older log: %v", err)
	}
}

func TestOpenLogFull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxLog), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := openLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("start\n")); err != nil {
		t.Fatal(err)
	}
	if got := size(t, path); got != 6 {
		t.Errorf("the log has %d bytes, want the new line alone", got)
	}
	if got := size(t, path+".1"); got != maxLog {
		t.Errorf("the older log has %d bytes, want %d", got, maxLog)
	}

	// A log with room is written on.
	l, err = openLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("again\n")); err != nil {
		t.Fatal(err)
	}
	if got := size(t, path); got != 12 {
		t.Errorf("the log has %d bytes, want both lines", got)
	}
}
