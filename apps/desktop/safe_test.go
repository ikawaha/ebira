package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// TestBrokenSettings checks that the app asks its ebira for the status in the
// safe mode, which tells the error of the settings file, and runs the
// commands that start the server in it only once it is on.
func TestBrokenSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ebira of the test is a shell script")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv(safeEnv, "")
	script := `#!/bin/sh
if [ "$` + safeEnv + `" != 1 ]; then echo "config.yaml: yaml: line 2: mapping values are not allowed" >&2; exit 1; fi
echo '{"notes_dir":"/n","configured":true,"running":false,"settings_error":"config.yaml: yaml: line 2","settings_file":"/c/config.yaml"}'
`
	if err := os.WriteFile(filepath.Join(bin, "ebira"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	st := brokenSettings()
	if st == nil || st.SettingsError != "config.yaml: yaml: line 2" || st.SettingsFile != "/c/config.yaml" {
		t.Fatalf("brokenSettings = %+v, want the error and the file", st)
	}
	has := func(cmd *exec.Cmd) bool { return slices.Contains(cmd.Env, safeEnv+"=1") }
	if has(ebiraCommand("ebira", false, "server", "start")) {
		t.Error("the server starts in the safe mode before it is asked for")
	}
	safeMode.Store(true)
	defer safeMode.Store(false)
	if !has(ebiraCommand("ebira", false, "server", "start")) {
		t.Error("the server starts outside the safe mode once it is asked for")
	}
	if st := brokenSettings(); st != nil {
		t.Errorf("brokenSettings in the safe mode = %+v, want nil", st)
	}
	if n, err := configuredNotes(); err != nil || n != "/n" {
		t.Errorf("configuredNotes = %q, %v; want notes_dir taken from the broken file", n, err)
	}

	// Not even notes_dir reads: no first start, and no folder to start in.
	unread := `#!/bin/sh
echo '{"notes_dir":"/cwd/notes","configured":false,"running":false,"settings_error":"config.yaml: yaml: line 1","settings_file":"/c/config.yaml"}'
`
	if err := os.WriteFile(filepath.Join(bin, "ebira"), []byte(unread), 0o755); err != nil {
		t.Fatal(err)
	}
	if firstStart(target{Addr: "localhost:13579"}) {
		t.Error("a first start with a config file that has an error")
	}
	if n, err := configuredNotes(); err == nil {
		t.Errorf("configuredNotes = %q, want an error to choose the folder", n)
	}
}
