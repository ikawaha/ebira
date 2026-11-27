//go:build !windows

package apikey

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRun(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name, command, key, err string
	}{
		{name: "key", command: `printf '  sk-test-123\n'`, key: "sk-test-123"},
		{name: "no interaction", command: `printf '%s' "$BW_NOINTERACTION"`, key: "true"},
		{name: "failed", command: `echo 'not signed in' >&2; exit 1`, err: "not signed in"},
		{name: "failed silently", command: `exit 3`, err: "exit status 3"},
		{name: "empty", command: `echo 'locked' >&2`, err: "printed no key: locked"},
		{name: "lines", command: `printf 'a\nb\n'`, err: "more than one line"},
		{name: "no input", command: `read x || echo none`, key: "none"},
		{name: "blank", command: "  ", err: "no command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := Run(ctx, tt.command)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("got %q, %v; want an error with %q", key, err, tt.err)
				}
				return
			}
			if err != nil || key != tt.key {
				t.Fatalf("got %q, %v; want %q", key, err, tt.key)
			}
		})
	}
}

// TestRunShellPath runs a command found only in the PATH of the login
// shell, as a server started from the Finder must.
func TestRunShellPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ebira-test-key"), []byte("#!/bin/sh\necho sk-from-shell\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(dir, "shell")
	// A login shell whose profile prints a greeting before the PATH.
	if err := os.WriteFile(shell, []byte("#!/bin/sh\necho hello\nPATH="+bin+":/usr/bin:/bin\nexport PATH\nexec /bin/sh \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", "/usr/bin:/bin")
	loginPathOnce, loginPath = sync.Once{}, ""
	key, err := Run(context.Background(), "ebira-test-key")
	if err != nil || key != "sk-from-shell" {
		t.Fatalf("got %q, %v", key, err)
	}
}
