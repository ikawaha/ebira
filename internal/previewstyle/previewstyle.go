// Package previewstyle holds the built-in styles of the preview of the web UI,
// and writes the one chosen to the CSS file for the preview, preview.css next
// to the settings file, where the user may change it.
//
// The file is the only record: which style is used is told from its content.
// No file is the default style, band; a file equal to a built-in style is that
// style; one that starts with the header of a built-in style but differs from
// it is that style changed; any other is the user's own CSS. A file equal to
// an earlier version of a built-in style is that style too, served as it is
// now. A file that is not a built-in style as it is is copied to
// preview.css.bak before it is replaced or removed, so that changes are never
// lost.
package previewstyle

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

//go:embed styles/*.css
var styles embed.FS

// earlier holds the earlier versions of the built-in styles, as
// <name>-<n>.css, which preview.css may still be as written when chosen.
//
//go:embed earlier/*.css
var earlier embed.FS

// Names are the built-in styles, in the order offered. plain adds nothing
// to the preview as the web UI draws it.
var Names = []string{"band", "document", "compact", "lined", "plain"}

// Default is the style drawn without the file, which choosing it removes.
const Default = "band"

// NoFile is the name an earlier web UI gave to no file, then the preview as
// the web UI draws it, and now Default.
const NoFile = "default"

// BackupSuffix is added to the name of the file to keep a copy of it.
const BackupSuffix = ".bak"

// CSS returns the built-in style name, or false if there is none.
func CSS(name string) ([]byte, bool) {
	if !slices.Contains(Names, name) {
		return nil, false
	}
	b, err := styles.ReadFile("styles/" + name + ".css")
	if err != nil {
		panic(err) // every name has its file
	}
	return b, true
}

// header matches the first line of a built-in style, which names it.
var header = regexp.MustCompile(`\A\s*/\*\s*ebira preview style:\s*([a-z0-9-]+)`)

// State is the state of the CSS file for the preview.
type State struct {
	// Style is the built-in style the file is, or was made from; Default
	// when there is no file, and empty for the user's own CSS.
	Style string
	// Modified tells the file differs from the built-in style it was made
	// from.
	Modified bool
}

// Read tells the state of the CSS file at path.
func Read(path string) (State, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{Style: Default}, nil
	}
	if err != nil {
		return State{}, err
	}
	return stateOf(b), nil
}

func stateOf(b []byte) State {
	for _, name := range Names {
		if css, _ := CSS(name); bytes.Equal(b, css) {
			return State{Style: name}
		}
	}
	if name := earlierOf(b); name != "" {
		return State{Style: name}
	}
	if m := header.FindSubmatch(b); m != nil && slices.Contains(Names, string(m[1])) {
		return State{Style: string(m[1]), Modified: true}
	}
	return State{}
}

// earlierOf returns the name of the built-in style b is an earlier version
// of, or "".
func earlierOf(b []byte) string {
	files, _ := fs.Glob(earlier, "earlier/*.css")
	for _, f := range files {
		if old, _ := earlier.ReadFile(f); bytes.Equal(b, old) {
			name := strings.TrimSuffix(path.Base(f), ".css")
			return name[:strings.LastIndexByte(name, '-')]
		}
	}
	return ""
}

// Current returns css as served: the built-in style as it is now when css is
// an earlier version of it, which the user did not change, and css itself
// otherwise.
func Current(css []byte) []byte {
	if name := earlierOf(css); name != "" {
		now, _ := CSS(name)
		return now
	}
	return css
}

// Use makes the CSS file at path the built-in style name, or removes it for
// Default, which NoFile names too. A file that is not a built-in style as it is is copied to the
// backup first, replacing an older one; backedUp tells it was.
func Use(path, name string) (backedUp bool, err error) {
	if name == NoFile {
		name = Default
	}
	css, ok := CSS(name)
	if !ok {
		return false, fmt.Errorf("no preview style %q", name)
	}
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		old = nil
	case err != nil:
		return false, err
	}
	if old != nil {
		if st := stateOf(old); st.Style == "" || st.Modified {
			if err := writeFile(path+BackupSuffix, old); err != nil {
				return false, err
			}
			backedUp = true
		}
	}
	if name == Default {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return backedUp, err
		}
		return backedUp, nil
	}
	return backedUp, writeFile(path, css)
}

// writeFile writes b to path through a temporary file, so that the server
// reading it never sees half of it.
func writeFile(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-preview-*.css")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
