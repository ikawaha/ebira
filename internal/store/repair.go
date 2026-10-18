package store

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CopyLabel is added to the title of a note made of a copy of a note.
const CopyLabel = "conflict copy"

// Repair is a change made by Repair: a copy of a note made into a note of its
// own, or removed as the same as a note.
type Repair struct {
	// Path is the file of the copy, relative to the notes directory.
	Path string
	// Note is the ID of the note made of the copy; empty when the copy was
	// removed.
	Note string
}

// Repair takes in the copies of notes left by file sync services, such as
// Dropbox, iCloud Drive, OneDrive, Google Drive and Syncthing, when a note
// was changed on two machines, so that no change is hidden:
//
//   - A file of a note with a name that is not of an ID, such as
//     "<id> (Mac's conflicted copy 2026-10-18).yaml" or "<id> 2.yaml", is
//     renamed to a new ID, with CopyLabel added to its title; when it is the
//     same as the note it copies, it is removed instead. A file that is not
//     of a note next to it, such as one put there by hand, gets a new ID
//     without the label.
//   - A note in two places, such as one moved on a machine and changed on
//     another, keeps its ID in the place with the latest updated_at; the
//     others are made into new notes the same way, or removed when the same.
//
// The new IDs and contents follow from the files alone, so that two machines
// repairing the same files make the same notes, which the sync service then
// sees as one. Files that cannot be read as notes are left as they are.
func (s *Store) Repair() ([]Repair, error) {
	places, err := s.places()
	if err != nil {
		return nil, err
	}
	var res []Repair
	for _, pl := range places {
		entries, err := os.ReadDir(filepath.Join(s.dir, filepath.FromSlash(pl)))
		if err != nil {
			return res, err
		}
		for _, e := range entries {
			name := e.Name()
			id, ok := strings.CutSuffix(name, Ext)
			if e.IsDir() || !ok || ValidID(id) || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue // a note, a notebook's file, or a file in progress
			}
			r, err := s.repairStray(pl, name)
			if err != nil {
				return res, err
			}
			if r != nil {
				res = append(res, *r)
			}
		}
	}
	files, err := s.Scan()
	if err != nil {
		return res, err
	}
	where := map[string][]string{}
	for _, f := range files {
		where[f.ID] = append(where[f.ID], placeOf(f.Notebook, f.TrashedNotebook))
	}
	ids := make([]string, 0, len(where))
	for id, pls := range where {
		if len(pls) > 1 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		rs, err := s.repairDuplicate(id, where[id])
		if err != nil {
			return res, err
		}
		res = append(res, rs...)
	}
	return res, nil
}

// repairStray takes in the file name in the place pl, which is not of an ID.
func (s *Store) repairStray(pl, name string) (*Repair, error) {
	file := filepath.Join(s.dir, filepath.FromSlash(pl), name)
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, nil // gone meanwhile
	}
	if !isNote(b) {
		return nil, nil
	}
	rel := path.Join(pl, name)
	copied := false // of a note next to it
	for _, id := range copiedIDs(strings.TrimSuffix(name, Ext)) {
		orig, err := os.ReadFile(s.path(pl, id))
		if err != nil {
			continue
		}
		if bytes.Equal(orig, b) {
			return &Repair{Path: rel}, os.Remove(file)
		}
		copied = true
	}
	id, err := s.writeCopy(pl, rel, b, copied)
	if err != nil {
		return nil, err
	}
	return &Repair{Path: rel, Note: id}, os.Remove(file)
}

// repairDuplicate keeps the note id in one of the places pls, and makes the
// others into new notes.
func (s *Store) repairDuplicate(id string, pls []string) ([]Repair, error) {
	type version struct {
		place   string
		b       []byte
		updated time.Time
	}
	vs := make([]version, 0, len(pls))
	for _, pl := range pls {
		b, err := os.ReadFile(s.path(pl, id))
		if err != nil || !isNote(b) {
			return nil, nil // in progress; next time
		}
		var n Note
		if err := yaml.Unmarshal(b, &n); err != nil {
			return nil, nil
		}
		vs = append(vs, version{pl, b, n.UpdatedAt})
	}
	// The latest first; then a place out of the trash, and the content and
	// the place, for an order every machine agrees on.
	slices.SortFunc(vs, func(a, b version) int {
		return cmp.Or(
			b.updated.Compare(a.updated),
			boolOrder(strings.HasPrefix(a.place, Trash), strings.HasPrefix(b.place, Trash)),
			bytes.Compare(a.b, b.b),
			strings.Compare(a.place, b.place),
		)
	})
	var res []Repair
	for _, v := range vs[1:] {
		rel := path.Join(v.place, id+Ext)
		r := Repair{Path: rel}
		if !bytes.Equal(v.b, vs[0].b) {
			cp, err := s.writeCopy(v.place, rel, v.b, true)
			if err != nil {
				return res, err
			}
			r.Note = cp
		}
		if err := os.Remove(s.path(v.place, id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return res, err
		}
		res = append(res, r)
	}
	return res, nil
}

// boolOrder orders false before true.
func boolOrder(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	}
	return 1
}

// writeCopy writes b, a version of a note whose file was key (a path relative
// to the notes directory), as a new note in the place pl, with CopyLabel
// added to its title if label is set, and returns its ID. The ID follows
// from key, so that two machines writing the same copy write the same file.
func (s *Store) writeCopy(pl, key string, b []byte, label bool) (string, error) {
	var n Note
	if err := yaml.Unmarshal(b, &n); err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	if label {
		n.Title = strings.TrimSpace(n.Title + " (" + CopyLabel + ")")
	}
	enc, err := encode(b, &n)
	if err != nil {
		return "", err
	}
	for i := 0; ; i++ {
		sum := sha256.Sum256([]byte(key + "\x00" + strconv.Itoa(i)))
		id := "c" + hex.EncodeToString(sum[:])[:13]
		if cur, err := os.ReadFile(s.path(pl, id)); err == nil {
			if bytes.Equal(cur, enc) {
				return id, nil // made already, such as on another machine
			}
			continue
		}
		if _, err := s.locate(id); !errors.Is(err, ErrNotFound) {
			continue
		}
		if err := writeFileAtomic(s.path(pl, id), enc); err != nil {
			return "", err
		}
		return id, nil
	}
}

// isNote reports whether b reads as a note: a mapping with a title or a
// body, without conflict markers.
func isNote(b []byte) bool {
	if hasConflictMarkers(b) {
		return false
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return false
	}
	_, title := m["title"]
	_, body := m["body"]
	return title || body
}

// copiedIDs returns the IDs a copy named name (without the extension) may be
// of: the longest leading run of the characters of IDs, such as abc of
// "abc (Mac's conflicted copy)", "abc 2" and "abc.sync-conflict-...", and
// that run without trailing separators, such as abc of "abc-Mac".
func copiedIDs(name string) []string {
	end := strings.IndexFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	})
	if end < 0 {
		end = len(name)
	}
	run := name[:end]
	var ids []string
	for run != "" {
		if ValidID(run) {
			ids = append(ids, run)
		}
		i := strings.LastIndexAny(run, "-_")
		if i < 0 {
			break
		}
		run = run[:i]
	}
	return ids
}
