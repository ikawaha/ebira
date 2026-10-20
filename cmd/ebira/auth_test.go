package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikawaha/ebira/internal/auth"
)

// TestAuthReset checks that ebira auth reset removes the passkeys, and asks
// for -yes without a terminal.
func TestAuthReset(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	notes := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	sd, err := stateDir(notes)
	if err != nil {
		t.Fatal(err)
	}
	dir := auth.Dir(sd)
	if _, err := auth.NewRegisterToken(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "passkeys.json"), []byte(`{"credentials":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := authReset([]string{"-notes", notes}, strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "-yes") {
		t.Errorf("without -yes and a terminal: %v, want to ask for -yes", err)
	}
	if err := authReset([]string{"-notes", notes, "-yes"}, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"passkeys.json", "register.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("%s is left: %v", f, err)
		}
	}
}
