// Package watch notifies changes of files in a directory and its immediate
// subdirectories, and in the subdirectories of nested directories that hold
// directories too (such as the trash, which holds deleted notebooks).
package watch

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Run watches dir and its immediate subdirectories, including ones created
// later, and calls fn once changes have settled for the debounce duration.
// The subdirectories named in nested (such as "trash") hold directories too,
// whose immediate subdirectories are watched as well.
//
// Changes are files with the extension ext in a subdirectory, and
// subdirectories being created, removed or renamed. Directories with an
// extension, such as those holding the files of a note, are not watched. Bursts of events, such as
// those from a git pull, result in a single call. Run blocks until ctx is done.
// Watcher errors are passed to onErr.
func Run(ctx context.Context, dir, ext string, debounce time.Duration, fn func(), onErr func(error), nested ...string) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	dir = filepath.Clean(dir)
	// parents are the directories whose subdirectories are watched.
	parents := []string{dir}
	for _, n := range nested {
		parents = append(parents, filepath.Join(dir, n))
	}
	for _, p := range parents {
		if err := watchTree(w, p); err != nil && (p == dir || !os.IsNotExist(err)) {
			return err
		}
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
			if ev.Op == fsnotify.Chmod || hidden(filepath.Base(ev.Name)) {
				continue
			}
			parent := filepath.Dir(ev.Name)
			if slices.Contains(parents, parent) {
				// A subdirectory was created, removed or renamed, or a file
				// directly in a parent changed. Those directly under dir are
				// not notes; those in a nested parent (the trash) are.
				if ev.Op.Has(fsnotify.Create) && !skipped(filepath.Base(ev.Name)) {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						if err := watchTree(w, ev.Name); err != nil {
							onErr(err)
						}
						timer.Reset(debounce)
						continue
					}
				}
				if filepath.Ext(ev.Name) == ext && parent == dir {
					continue
				}
				if filepath.Ext(ev.Name) == ext || filepath.Ext(ev.Name) == "" {
					timer.Reset(debounce)
				}
				continue
			}
			if filepath.Ext(ev.Name) == ext {
				timer.Reset(debounce)
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

// watchTree watches dir and its immediate subdirectories.
func watchTree(w *fsnotify.Watcher, dir string) error {
	if err := w.Add(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && !hidden(e.Name()) && !skipped(e.Name()) {
			if err := w.Add(filepath.Join(dir, e.Name())); err != nil {
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
