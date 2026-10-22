package previewstyle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStyles(t *testing.T) {
	for _, name := range Names {
		css, ok := CSS(name)
		if !ok || !strings.HasPrefix(string(css), "/* ebira preview style: "+name+"\n") {
			t.Errorf("%s: no header naming it", name)
		}
		if st := stateOf(css); st != (State{Style: name}) {
			t.Errorf("%s: state %+v", name, st)
		}
	}
	if _, ok := CSS("nothing"); ok {
		t.Error("CSS(nothing) found one")
	}
}

func TestReadAndUse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf", "preview.css") // the directory is made
	read := func() State {
		t.Helper()
		st, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	if st := read(); st != (State{Style: Default}) {
		t.Errorf("no file: %+v", st)
	}

	// A built-in style is written, and replaced without a backup.
	if backedUp, err := Use(path, "band"); err != nil || backedUp {
		t.Fatalf("Use(band) = %v, %v", backedUp, err)
	}
	if st := read(); st != (State{Style: "band"}) {
		t.Errorf("band: %+v", st)
	}
	if backedUp, err := Use(path, "document"); err != nil || backedUp {
		t.Fatalf("Use(document) = %v, %v", backedUp, err)
	}
	if _, err := os.Stat(path + BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("a backup of a built-in style: %v", err)
	}

	// Changed, it is kept in the backup when replaced.
	css, _ := CSS("document")
	changed := string(css) + "h1 { color: red }\n"
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := read(); st != (State{Style: "document", Modified: true}) {
		t.Errorf("changed: %+v", st)
	}
	if backedUp, err := Use(path, "compact"); err != nil || !backedUp {
		t.Fatalf("Use(compact) = %v, %v", backedUp, err)
	}
	if b, _ := os.ReadFile(path + BackupSuffix); string(b) != changed {
		t.Errorf("backup = %q", b)
	}

	// The user's own CSS, and back to the default: the file goes, kept.
	own := "h1 { color: blue }\n"
	if err := os.WriteFile(path, []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := read(); st != (State{}) {
		t.Errorf("own: %+v", st)
	}
	if backedUp, err := Use(path, Default); err != nil || !backedUp {
		t.Fatalf("Use(default) = %v, %v", backedUp, err)
	}
	if st := read(); st != (State{Style: Default}) {
		t.Errorf("after default: %+v", st)
	}
	if b, _ := os.ReadFile(path + BackupSuffix); string(b) != own {
		t.Errorf("backup = %q", b)
	}
	// Default again does nothing.
	if backedUp, err := Use(path, Default); err != nil || backedUp {
		t.Errorf("Use(default) again = %v, %v", backedUp, err)
	}

	if _, err := Use(path, "nothing"); err == nil {
		t.Error("Use(nothing) did not fail")
	}
	// No temporary file is left.
	if es, _ := os.ReadDir(filepath.Dir(path)); len(es) != 1 {
		t.Errorf("files left: %v", es)
	}
}
