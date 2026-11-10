package store

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
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

// The files of a note, images and other attachments, are kept in a directory next to the
// note file, notes/<notebook>/<id>.assets/, and move with the note. The body
// refers to them by paths relative to the note file, <id>.assets/<name>, so
// that it reads as a Markdown file put in the notebook directory. The suffix
// keeps the directory apart from notebooks in the trash, whose names follow
// the same rule as note IDs.

// AssetsSuffix is the suffix of the directory holding the files of a note.
const AssetsSuffix = ".assets"

// MaxFileSize is the largest file a note may hold.
const MaxFileSize = 50 << 20

var (
	ErrFileTooLarge = fmt.Errorf("file is larger than %d MB", MaxFileSize>>20)
	ErrFileNotFound = errors.New("file not found")
)

// imageTypes maps the content types of the images shown in the body to their
// extensions. SVG is left out on purpose: it may hold scripts.
var imageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// FileType returns the content type of a file from its first bytes, and
// whether it is an image shown in the body. Notes hold files of any type, but
// only images are served as their type, and other files as downloads, so
// that a file such as an HTML or SVG file never runs in the origin of the web
// UI.
func FileType(head []byte) (string, bool) {
	ct := http.DetectContentType(head)
	_, ok := imageTypes[ct]
	return ct, ok
}

// maxExt is the longest extension kept from the name of a file.
const maxExt = 16

// fileExt returns the extension of name made usable for a file of a note: in
// lower case, of ASCII letters and digits only, or empty if it is not.
func fileExt(name string) string {
	ext := strings.ToLower(path.Ext(path.Base(strings.ReplaceAll(name, `\`, "/"))))
	if len(ext) < 2 || len(ext) > maxExt+1 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}

// imageExt reports whether ext is the extension of the images, as AddFile
// gives it to them.
func imageExt(ext string) bool {
	for _, e := range imageTypes {
		if ext == e {
			return true
		}
	}
	return false
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
// cleaned up as CleanFileName does, with an extension as fileExt leaves it.
func validFileName(name string) bool {
	ext := fileExt(name)
	if ext != path.Ext(name) {
		return false
	}
	return CleanFileName(name, ext) == name
}

// AddFile adds a file with the content b to note id and returns its name
// (see writeFile).
func (s *Store) AddFile(id, name string, b []byte) (string, error) {
	if len(b) > MaxFileSize {
		return "", ErrFileTooLarge
	}
	place, err := s.locate(id)
	if err != nil {
		return "", err
	}
	dir := s.assetsDir(place, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// MkdirAll takes a link to a folder for the folder: files are not
	// written through one (see ErrLink).
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return "", &fs.PathError{Op: "add", Path: dir, Err: ErrLink}
	}
	return writeFile(dir, name, bytes.NewReader(b))
}

// writeFile writes a file with the content read from r, at most MaxFileSize,
// to dir and returns its name: name cleaned up (see CleanFileName) with the
// extension of its type for an image and its own extension (see fileExt) for
// another file, and a suffix such as -2 when the name is taken. Another file
// keeps no extension of an image, so that the name tells the images, which
// the UI shows as such. A file larger than MaxFileSize is not written; the
// error of reading r is returned as is.
func writeFile(dir, name string, r io.Reader) (string, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	head = head[:n]
	ext := fileExt(name)
	if ct, ok := FileType(head); ok {
		ext = imageTypes[ct]
	} else if imageExt(ext) {
		ext = ""
	}
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
		// One byte more than allowed tells a file that is too large.
		written, err := io.Copy(f, io.LimitReader(io.MultiReader(bytes.NewReader(head), r), MaxFileSize+1))
		if err == nil && written > MaxFileSize {
			err = ErrFileTooLarge
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name())
			return "", err
		}
		return name, nil
	}
}

// stagePrefix starts the names of the folders of stages in the notes
// directory: hidden from the scans, the watcher and the git sync.
const stagePrefix = ".tmp-stage-"

// staleStage is how old a stage left behind, such as by a server that
// stopped while receiving files, is when it is removed.
const staleStage = 24 * time.Hour

// Stage holds the files of a note yet to be created, such as the images of an
// import while they are received, so that they become the note's files at
// once with Adopt, or none of them with Remove.
type Stage struct {
	s   *Store
	dir string
}

// NewStage returns a new stage, a folder in the notes directory, on the same
// file system as the notes so that Adopt moves it. It removes the stages
// left behind for longer than staleStage.
func (s *Store) NewStage() (*Stage, error) {
	if entries, err := os.ReadDir(s.dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), stagePrefix) {
				continue
			}
			if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > staleStage {
				os.RemoveAll(filepath.Join(s.dir, e.Name()))
			}
		}
	}
	dir, err := os.MkdirTemp(s.dir, stagePrefix+"*")
	if err != nil {
		return nil, err
	}
	// MkdirTemp makes the folder for its owner only; it becomes the folder of
	// the note's files, which others may read as the note.
	if err := os.Chmod(dir, 0o755); err != nil {
		os.Remove(dir)
		return nil, err
	}
	return &Stage{s: s, dir: dir}, nil
}

// Add writes a file with the content read from r to the stage, as AddFile
// adds one to a note, and returns its name.
func (st *Stage) Add(name string, r io.Reader) (string, error) {
	return writeFile(st.dir, name, r)
}

// Adopt makes the files of the stage the files of note id, which has none
// yet. A stage without files is removed.
func (st *Stage) Adopt(id string) error {
	if entries, err := os.ReadDir(st.dir); err == nil && len(entries) == 0 {
		return st.Remove()
	}
	place, err := st.s.locate(id)
	if err != nil {
		return err
	}
	return os.Rename(st.dir, st.s.assetsDir(place, id))
}

// Remove removes the stage with its files, unless Adopt moved them.
func (st *Stage) Remove() error { return os.RemoveAll(st.dir) }

// File returns the path on disk of the file name of note id.
func (s *Store) File(id, name string) (string, error) {
	if !validFileName(name) {
		return "", ErrFileNotFound
	}
	place, err := s.locate(id)
	if err != nil {
		return "", err
	}
	dir := s.assetsDir(place, id)
	if !regularIn(dir, name) {
		return "", ErrFileNotFound // also a link, or one in a linked folder
	}
	return filepath.Join(dir, name), nil
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
			if fi, err := os.Lstat(s.path(place, id)); err != nil || !fi.Mode().IsRegular() {
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
// percent-encoded, and an extension as fileExt leaves it, if any. It finds
// references anywhere, such as in Markdown images and links, HTML attributes
// and code, so that a file counts as used rather than not.
var fileRef = regexp.MustCompile(`([a-z0-9][a-z0-9_-]{0,63})\.assets/((?:[\p{L}\p{N}_-]|%[0-9A-Fa-f]{2})+(?:\.[a-z0-9]{1,16})?)`)

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
