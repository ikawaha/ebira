package previewstyle

import (
	"bytes"
	"io/fs"
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
	if backedUp, err := Use(path, "plain"); err != nil || backedUp {
		t.Fatalf("Use(plain) = %v, %v", backedUp, err)
	}
	if st := read(); st != (State{Style: "plain"}) {
		t.Errorf("plain: %+v", st)
	}
	// The default style removes the file, as default, its earlier name, does.
	for _, name := range []string{Default, NoFile} {
		if backedUp, err := Use(path, name); err != nil || backedUp {
			t.Fatalf("Use(%s) = %v, %v", name, backedUp, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("Use(%s) left the file: %v", name, err)
		}
		if err := writeFile(path, mustCSS(t, "plain")); err != nil {
			t.Fatal(err)
		}
	}
	// The default style, written by an earlier web UI, is that style.
	if err := writeFile(path, mustCSS(t, Default)); err != nil {
		t.Fatal(err)
	}
	if st := read(); st != (State{Style: Default}) {
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

func TestEarlier(t *testing.T) {
	files, err := fs.Glob(earlier, "earlier/*.css")
	if err != nil || len(files) == 0 {
		t.Fatalf("no earlier versions: %v", err)
	}
	for _, f := range files {
		old, _ := earlier.ReadFile(f)
		name := earlierOf(old)
		now, ok := CSS(name)
		if !ok {
			t.Errorf("%s: not of a built-in style (%q)", f, name)
			continue
		}
		// As chosen then, it is that style, served as it is now, and
		// replaced without a backup.
		if st := stateOf(old); st != (State{Style: name}) {
			t.Errorf("%s: state %+v", f, st)
		}
		if got := Current(old); !bytes.Equal(got, now) {
			t.Errorf("%s: not served as the style is now", f)
		}
		path := filepath.Join(t.TempDir(), "preview.css")
		if err := os.WriteFile(path, old, 0o644); err != nil {
			t.Fatal(err)
		}
		if backedUp, err := Use(path, "band"); err != nil || backedUp {
			t.Errorf("%s: Use(band) = %v, %v", f, backedUp, err)
		}
	}
	// Changed, an earlier version is the user's change, as it is.
	old, _ := earlier.ReadFile(files[0])
	changed := append(bytes.Clone(old), "h1 { color: red }\n"...)
	if got := Current(changed); !bytes.Equal(got, changed) {
		t.Error("a changed earlier version is served otherwise")
	}
	if st := stateOf(changed); !st.Modified {
		t.Errorf("a changed earlier version: %+v", st)
	}
}

func mustCSS(t *testing.T, name string) []byte {
	t.Helper()
	css, ok := CSS(name)
	if !ok {
		t.Fatalf("no style %s", name)
	}
	return css
}
