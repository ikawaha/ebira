// Package watch notifies changes of files in a directory and in the
// directories under it, down to a given depth (such as the notebooks, their
// sub notebooks, and the notebooks in the trash with theirs).
package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Run watches dir and the directories under it down to depth levels (1 for
// its immediate subdirectories), including ones created later, and calls fn
// once changes have settled for the debounce duration.
//
// Changes are files with the extension ext under dir, but not directly in
// it, and directories being created, removed or renamed. Directories with an
// extension, such as those holding the files of a note, are not watched.
// Bursts of events, such as those from a git pull, result in a single call.
// Run blocks until ctx is done. Watcher errors are passed to onErr.
func Run(ctx context.Context, dir, ext string, debounce time.Duration, fn func(), onErr func(error), depth int) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	dir = filepath.Clean(dir)
	// level returns the depth of path p under dir, 0 for dir itself.
	level := func(p string) int {
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return 0
		}
		return strings.Count(rel, string(filepath.Separator)) + 1
	}
	if err := watchTree(w, dir, depth); err != nil {
		return err
	}

	timer := time.NewTimer(debounce)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			name := filepath.Base(ev.Name)
			if ev.Op == fsnotify.Chmod || hidden(name) {
				continue
			}
			parent := filepath.Dir(ev.Name)
			if ev.Op.Has(fsnotify.Create) && !skipped(name) {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					// A directory was created or moved in: watch it, and
					// what is under it, as deep as allowed.
					if l := level(ev.Name); l <= depth {
						if err := watchTree(w, ev.Name, depth-l); err != nil {
							onErr(err)
						}
					}
					timer.Reset(debounce)
					continue
				}
			}
			switch filepath.Ext(ev.Name) {
			case ext:
				if parent != dir { // files directly under dir are not notes
					timer.Reset(debounce)
				}
			case "":
				timer.Reset(debounce) // a directory removed or renamed
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			onErr(err)
		case <-timer.C:
			fn()
		}
	}
}

// watchTree watches dir and the directories under it, down to depth levels.
func watchTree(w *fsnotify.Watcher, dir string, depth int) error {
	if err := w.Add(dir); err != nil {
		return err
	}
	if depth <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && !hidden(e.Name()) && !skipped(e.Name()) {
			if err := watchTree(w, filepath.Join(dir, e.Name()), depth-1); err != nil {
				return err
			}
		}
	}
	return nil
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

// skipped reports whether a directory is not watched: one with an extension,
// such as the files of a note (<id>.assets), which holds no notes. Notebook
// names have no dots.
func skipped(name string) bool { return filepath.Ext(name) != "" }
