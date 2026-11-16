package history

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// The files of a note, images and other attachments, have no versions of
// their own in the notes directory: a file replaced by one of the same name,
// or deleted, is kept here instead, as <dir>/<note ID>/files/<rev>/<name>, so
// that it can be put back. The rev is the time it was replaced or deleted, as
// for the versions of notes.

// Limits on the files kept for a note, which may be large: at most
// MaxFileVersions, and those older than MaxAge are dropped, except the
// MinKeep newest.
const MaxFileVersions = 20

// filesDir is the directory of the files kept for a note, in its directory.
const filesDir = "files"

// fileName matches the names a note's file may have (see store.CleanFileName).
var fileName = regexp.MustCompile(`^[\p{L}\p{N}_-]+(\.[a-z0-9]{1,16})?$`)

// File is an earlier file of a note.
type File struct {
	// Rev identifies the file among those kept for the note: the time it
	// was replaced or deleted, in nanoseconds since the Unix epoch.
	Rev        string
	Name       string
	Size       int64
	ReplacedAt time.Time
}

// KeepFile keeps a copy of the file at path, named name, as an earlier file
// of note id, replaced or deleted now, and drops the files beyond the limits.
func (s *Store) KeepFile(id, name, path string) error {
	dir, err := s.noteDir(id)
	if err != nil {
		return err
	}
	if !fileName.MatchString(name) {
		return fmt.Errorf("invalid file name %q", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir = filepath.Join(dir, filesDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	now := s.now()
	var rev string
	for n := now.UnixNano(); ; n++ {
		rev = strconv.FormatInt(n, 10)
		err := os.Mkdir(filepath.Join(dir, rev), 0o700)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if err := copyFile(path, filepath.Join(dir, rev, name)); err != nil {
		os.RemoveAll(filepath.Join(dir, rev))
		return err
	}
	return pruneFiles(dir, now)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fileRevs returns the revs of the files in dir, newest first.
func fileRevs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs []string
	for _, e := range entries {
		if e.IsDir() && revPattern.MatchString(e.Name()) {
			rs = append(rs, e.Name())
		}
	}
	sortRevs(rs)
	return rs, nil
}

// pruneFiles drops the files in dir beyond the limits.
func pruneFiles(dir string, now time.Time) error {
	rs, err := fileRevs(dir)
	if err != nil {
		return err
	}
	cut := now.Add(-MaxAge).UnixNano()
	for i, rev := range rs {
		n, _ := strconv.ParseInt(rev, 10, 64)
		if i >= MaxFileVersions || i >= MinKeep && n < cut {
			if err := os.RemoveAll(filepath.Join(dir, rev)); err != nil {
				return err
			}
		}
	}
	if len(rs) == 0 {
		os.Remove(dir) // fails, and is kept, if something else is in it
	}
	return nil
}

// readFile returns the file kept as rev in dir.
func readFile(dir, rev string) (*File, string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, rev))
	if err != nil {
		return nil, "", err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !fileName.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			return nil, "", err
		}
		n, _ := strconv.ParseInt(rev, 10, 64)
		f := &File{Rev: rev, Name: e.Name(), Size: fi.Size(), ReplacedAt: time.Unix(0, n).UTC().Truncate(time.Millisecond)}
		return f, filepath.Join(dir, rev, e.Name()), nil
	}
	return nil, "", os.ErrNotExist
}

// Files returns the earlier files of note id, newest first.
func (s *Store) Files(id string) ([]*File, error) {
	dir, err := s.noteDir(id)
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, filesDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, err := fileRevs(dir)
	if err != nil {
		return nil, err
	}
	fs := []*File{}
	for _, rev := range rs {
		if f, _, err := readFile(dir, rev); err == nil {
			fs = append(fs, f)
		}
	}
	return fs, nil
}

// TakeFile returns the earlier file rev of note id and calls put with the
// path of its content, such as to put it back among the note's files; once
// put returns no error, the file is dropped from the history.
func (s *Store) TakeFile(id, rev string, put func(f *File, path string) error) error {
	dir, err := s.noteDir(id)
	if err != nil {
		return err
	}
	if !revPattern.MatchString(rev) {
		return ErrNotFound
	}
	dir = filepath.Join(dir, filesDir)
	s.mu.Lock()
	f, path, err := readFile(dir, rev)
	s.mu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// Unlocked, as put may keep another file of the note (KeepFile).
	if err := put(f, path); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(filepath.Join(dir, rev)); err != nil {
		return err
	}
	return pruneFiles(dir, s.now())
}
