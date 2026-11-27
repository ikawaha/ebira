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
	st := brokenSettings("")
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
	if st := brokenSettings(""); st != nil {
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

// TestBrokenNotesSettings checks that the app learns of an error of the
// settings shared with the notes of the folder it is to show, and offers
// that file, after the settings file of ebira when both have one.
func TestBrokenNotesSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ebira of the test is a shell script")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	ebira := func(out string) {
		t.Helper()
		// The folder asked for is told back as $notes, so that the test sees
		// it given; PATH has no other commands.
		script := "#!/bin/sh\nnotes=\"\"\nwhile [ $# -gt 0 ]; do [ \"$1\" = -notes ] && notes=\"$2\"; shift; done\necho \"" + out + "\"\n"
		if err := os.WriteFile(filepath.Join(bin, "ebira"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ebira(`{\"notes_dir\":\"$notes\",\"configured\":true,\"running\":false,\"notes_settings_error\":\"$notes/_ebira.yaml: yaml: line 3\",\"notes_settings_file\":\"$notes/_ebira.yaml\"}`)
	st := brokenSettings("/my/notes")
	if st == nil {
		t.Fatal("brokenSettings = nil, want the error of _ebira.yaml")
	}
	if file, err := st.broken(); file != "/my/notes/_ebira.yaml" || err != "/my/notes/_ebira.yaml: yaml: line 3" {
		t.Errorf("broken = %q, %q; want _ebira.yaml of the folder asked for", file, err)
	}
	ebira(`{\"notes_dir\":\"/n\",\"configured\":true,\"running\":false,\"settings_error\":\"config.yaml: line 1\",\"settings_file\":\"/c/config.yaml\",\"notes_settings_error\":\"_ebira.yaml: line 3\",\"notes_settings_file\":\"/n/_ebira.yaml\"}`)
	if file, _ := brokenSettings("").broken(); file != "/c/config.yaml" {
		t.Errorf("broken file = %q, want the settings file of ebira first", file)
	}
	ebira(`{\"notes_dir\":\"/n\",\"configured\":true,\"running\":false}`)
	if st := brokenSettings(""); st != nil {
		t.Errorf("brokenSettings with no error = %+v, want nil", st)
	}
}
