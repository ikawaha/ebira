package store

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// The files of a note, such as images, are kept in a directory next to the
// note file, notes/<notebook>/<id>.assets/, and move with the note. The body
// refers to them by paths relative to the note file, <id>.assets/<name>, so
// that it reads as a Markdown file put in the notebook directory. The suffix
// keeps the directory apart from notebooks in the trash, whose names follow
// the same rule as note IDs.

// AssetsSuffix is the suffix of the directory holding the files of a note.
const AssetsSuffix = ".assets"

// MaxFileSize is the largest file a note may hold.
const MaxFileSize = 10 << 20

var (
	ErrUnsupportedFile = errors.New("unsupported file: only PNG, JPEG, GIF and WebP images are accepted")
	ErrFileTooLarge    = fmt.Errorf("file is larger than %d MB", MaxFileSize>>20)
	ErrFileNotFound    = errors.New("file not found")
)

// fileTypes maps the content types of the files a note may hold to their
// extensions. SVG is left out on purpose: it may hold scripts.
var fileTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// FileType returns the content type of a file from its first bytes, and
// whether a note may hold such a file.
func FileType(head []byte) (string, bool) {
	ct := http.DetectContentType(head)
	_, ok := fileTypes[ct]
	return ct, ok
}

// FilePath returns the path of a file of note id, relative to the note file,
// as the body refers to it.
func FilePath(id, name string) string { return id + AssetsSuffix + "/" + name }

// assetsDir returns the directory of the files of note id in place.
func (s *Store) assetsDir(place, id string) string {
	return filepath.Join(s.dir, filepath.FromSlash(place), id+AssetsSuffix)
}

// CleanFileName returns name made usable as the name of a file of a note,
// with the extension ext: the base name without its extension, in NFC, with
// runs of characters other than letters, digits, "-" and "_" replaced by
// "-", and at most 80 characters long.
func CleanFileName(name, ext string) string {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	stem := norm.NFC.String(strings.TrimSuffix(base, path.Ext(base)))
	var b strings.Builder
	dash := false
	for _, r := range stem {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteRune('-')
			dash = true
		}
	}
	stem = strings.Trim(b.String(), "-_")
	if r := []rune(stem); len(r) > 80 {
		stem = strings.TrimRight(string(r[:80]), "-_")
	}
	if stem == "" {
		stem = "file"
	}
	return stem + ext
}

// validFileName reports whether name can be the name of a file of a note:
// cleaned up as CleanFileName does, with an extension of an accepted type.
func validFileName(name string) bool {
	ext := path.Ext(name)
	for _, e := range fileTypes {
		if e == ext {
			return CleanFileName(name, ext) == name
		}
	}
	return false
}

// AddFile adds a file with the content b to note id and returns its name:
// name cleaned up (see CleanFileName) with the extension of its type, and a
// suffix such as -2 when the name is taken.
func (s *Store) AddFile(id, name string, b []byte) (string, error) {
	if len(b) > MaxFileSize {
		return "", ErrFileTooLarge
	}
	ct, ok := FileType(b)
	if !ok {
		return "", ErrUnsupportedFile
	}
	place, err := s.locate(id)
	if err != nil {
		return "", err
	}
	dir := s.assetsDir(place, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := fileTypes[ct]
	clean := CleanFileName(name, ext)
	stem := strings.TrimSuffix(clean, ext)
	for i := 1; ; i++ {
		name := clean
		if i > 1 {
			name = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(b); err != nil {
			f.Close()
			os.Remove(f.Name())
			return "", err
		}
		if err := f.Close(); err != nil {
			os.Remove(f.Name())
			return "", err
		}
		return name, nil
	}
}

// File returns the path on disk of the file name of note id.
func (s *Store) File(id, name string) (string, error) {
	if !validFileName(name) {
		return "", ErrFileNotFound
	}
	place, err := s.locate(id)
	if err != nil {
		return "", err
	}
	p := filepath.Join(s.assetsDir(place, id), name)
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return "", ErrFileNotFound
	}
	return p, nil
}

// DeleteFile removes the file name of note id, and the directory of the
// note's files once it is empty.
func (s *Store) DeleteFile(id, name string) error {
	p, err := s.File(id, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(p)) // fails while other files are left
	return nil
}

// NoteFile describes a file of a note on disk.
type NoteFile struct {
	NoteID  string
	Name    string
	Size    int64
	ModTime time.Time
}

// AllFiles lists the files of all notes, including those in the trash,
// sorted by note ID and name. Only files of notes that exist, with names that
// AddFile could have given, are listed; others cannot be served or deleted
// through the API either.
func (s *Store) AllFiles() ([]NoteFile, error) {
	places, err := s.places()
	if err != nil {
		return nil, err
	}
	var files []NoteFile
	for _, place := range places {
		dir := filepath.Join(s.dir, filepath.FromSlash(place))
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			id, ok := strings.CutSuffix(e.Name(), AssetsSuffix)
			if !e.IsDir() || !ok || !ValidID(id) {
				continue
			}
			if fi, err := os.Stat(s.path(place, id)); err != nil || !fi.Mode().IsRegular() {
				continue // the files of a note that is gone
			}
			names, err := os.ReadDir(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			for _, f := range names {
				if !f.Type().IsRegular() || !validFileName(f.Name()) {
					continue
				}
				fi, err := f.Info()
				if err != nil {
					return nil, err
				}
				files = append(files, NoteFile{NoteID: id, Name: f.Name(), Size: fi.Size(), ModTime: fi.ModTime()})
			}
		}
	}
	slices.SortFunc(files, func(a, b NoteFile) int {
		return cmp.Or(cmp.Compare(a.NoteID, b.NoteID), cmp.Compare(a.Name, b.Name))
	})
	return files, nil
}

// fileRef matches a reference to a file of a note in a body, as a path
// relative to the note file: <id>.assets/<name>, with the name as is or
// percent-encoded. It finds references anywhere, such as in Markdown images
// and links, HTML attributes and code, so that a file counts as used rather
// than not.
var fileRef = regexp.MustCompile(`([a-z0-9][a-z0-9_-]{0,63})\.assets/((?:[\p{L}\p{N}_-]|%[0-9A-Fa-f]{2})+\.(?:png|jpg|gif|webp))`)

// FileRef is a reference to a file of a note found in a body.
type FileRef struct {
	NoteID string
	Name   string
}

// FileRefs returns the references to files of notes in body, in order of
// appearance and without repeats.
func FileRefs(body string) []FileRef {
	var refs []FileRef
	for _, m := range fileRef.FindAllStringSubmatch(body, -1) {
		name := m[2]
		if u, err := url.PathUnescape(name); err == nil {
			name = norm.NFC.String(u)
		}
		r := FileRef{NoteID: m[1], Name: name}
		if !slices.Contains(refs, r) {
			refs = append(refs, r)
		}
	}
	return refs
}
