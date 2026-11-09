package store

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrLink is a file of the notes directory that is a symbolic link, or under
// one other than the folder of a notebook. A notebook's folder may be a link,
// as to another disk; the files in it, such as notes and settings, and the
// folders of the files of notes are not followed, so that a link put there,
// as by a sync, does not make the server read or serve a file outside the
// notes directory.
var ErrLink = errors.New("a symbolic link is not followed")

// readFile reads the file p of the notes directory, unless it is a symbolic
// link or not a regular file. The file opened is checked to be the one
// looked at, so that a link put in its place meanwhile is not followed.
func readFile(p string) ([]byte, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: p, Err: ErrLink}
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if opened, err := f.Stat(); err != nil || !os.SameFile(fi, opened) {
		return nil, &fs.PathError{Op: "read", Path: p, Err: ErrLink}
	}
	return io.ReadAll(f)
}

// notebookDir reports whether the entry e of the folder dir is the folder of
// a notebook: a folder, or a symbolic link to one, which notebooks may be.
func notebookDir(dir string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && fi.IsDir()
}

// regularIn reports whether name in the folder dir is a regular file and
// dir is no symbolic link: dir is the folder of the files of a note.
func regularIn(dir, name string) bool {
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return false
	}
	fi, err := os.Lstat(filepath.Join(dir, name))
	return err == nil && fi.Mode().IsRegular()
}
