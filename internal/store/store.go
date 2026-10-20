// Package store reads and writes the YAML files in the notes directory.
// Each note is a single file, notes/<notebook>/<id>.yaml, meant to be synced
// with git. Note IDs are unique across notebooks. The inbox and trash
// notebooks always exist.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	ErrNotFound        = errors.New("note not found")
	ErrExists          = errors.New("note already exists")
	ErrInvalidID       = errors.New("invalid note id")
	ErrInvalidNotebook = errors.New("invalid notebook name")
	ErrDuplicate       = errors.New("note id is used in more than one notebook")
	ErrNotebookExists  = errors.New("notebook already exists")
	ErrReserved        = errors.New("inbox and trash cannot be deleted")
	// ErrConflict is returned for a note file with the conflict markers of
	// git, left by a merge whose conflict is not resolved yet.
	ErrConflict = errors.New("the note file has conflict markers of git; resolve the conflict")
)

// CreatedByAI is Note.CreatedBy for a note created by an AI client.
const CreatedByAI = "ai"

// Ext is the extension of note files.
const Ext = ".yaml"

// Reserved notebooks.
const (
	Inbox = "inbox" // default notebook for new notes
	Trash = "trash" // deleted notes, until deleted permanently
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Note is a single note.
type Note struct {
	ID        string    `yaml:"-"`
	Notebook  string    `yaml:"-"`
	Title     string    `yaml:"title"`
	Tags      []string  `yaml:"tags"`
	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`
	// TrashedFrom is the notebook a note deleted on its own came from.
	TrashedFrom string `yaml:"trashed_from,omitempty"`
	// Order is the note's place in the manual order of its notebook, set when
	// people move the note; higher comes first. Without it, the note sorts by
	// its creation time (see SortKey), so that only moved notes are written.
	Order *int64 `yaml:"order,omitempty"`
	// CreatedBy is CreatedByAI for a note created by an AI client, so that
	// people can tell such notes apart, such as in an inbox of the drop kind;
	// empty for people.
	CreatedBy string `yaml:"created_by,omitempty"`
	// ImportedFrom is the name of the file a note was imported from, and
	// ImportHash a hash of its content, to tell a file imported before.
	ImportedFrom string `yaml:"imported_from,omitempty"`
	ImportHash   string `yaml:"import_hash,omitempty"`
	Body         string `yaml:"body"`
	// Extra holds more keys for a new note file, such as the front matter of
	// an imported file: pairs of key and value nodes. Keys of the note itself
	// are ignored, and so is Extra when the file exists, whose other keys are
	// kept as they are.
	Extra []*yaml.Node `yaml:"-"`
	// TrashedNotebook is set for a note in a notebook deleted as a whole: the
	// ID of that notebook, which is kept as trash/<id>. Notebook is then Trash.
	TrashedNotebook string `yaml:"-"`
	// Version is a strong entity tag computed from the file content.
	Version string `yaml:"-"`
}

// SortKey returns the key of the note in the manual order: Order if set, else
// the creation time in milliseconds. Higher comes first.
func (n *Note) SortKey() int64 {
	if n.Order != nil {
		return *n.Order
	}
	return n.CreatedAt.UnixMilli()
}

// File describes a note file in the notes directory.
type File struct {
	ID              string
	Notebook        string
	TrashedNotebook string // see Note.TrashedNotebook
	ModTime         time.Time
	Size            int64
}

// A place is the directory of a note relative to the notes directory: a
// notebook, or Trash/<id> for a notebook in the trash.

func placeOf(notebook, trashedNotebook string) string {
	if trashedNotebook != "" {
		return Trash + "/" + trashedNotebook
	}
	return notebook
}

func splitPlace(place string) (notebook, trashedNotebook string) {
	if nb, ok := strings.CutPrefix(place, Trash+"/"); ok {
		return Trash, nb
	}
	return place, ""
}

// places returns the places that may hold notes: the notebooks and the
// notebooks in the trash.
func (s *Store) places() ([]string, error) {
	nbs, err := s.Notebooks()
	if err != nil {
		return nil, err
	}
	trashed, err := s.TrashedNotebooks()
	if err != nil {
		return nil, err
	}
	for _, nb := range trashed {
		nbs = append(nbs, placeOf(Trash, nb))
	}
	return nbs, nil
}

// Store manages the notes directory.
type Store struct {
	dir string
}

// New returns a Store for the notes directory dir, creating it and the
// reserved notebooks if needed.
func New(dir string) (*Store, error) {
	for _, nb := range []string{Inbox, Trash} {
		if err := os.MkdirAll(filepath.Join(dir, nb), 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir}, nil
}

// ValidID reports whether id can be used as a note ID.
func ValidID(id string) bool { return nameRe.MatchString(id) }

// ValidNotebook reports whether name can be used as a notebook name.
func ValidNotebook(name string) bool { return nameRe.MatchString(name) }

// NewID returns a random note ID.
func NewID() string {
	var b [7]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// Dir returns the notes directory.
func (s *Store) Dir() string { return s.dir }

// path returns the path of note id in place (a notebook or Trash/<id>).
func (s *Store) path(place, id string) string {
	return filepath.Join(s.dir, filepath.FromSlash(place), id+Ext)
}

// locate returns the place (see placeOf) that holds the note.
func (s *Store) locate(id string) (string, error) {
	if !ValidID(id) {
		return "", ErrInvalidID
	}
	// Stat the file in each place. filepath.Glob would read every name in
	// every notebook, which costs O(number of notes) per lookup.
	all, err := s.places()
	if err != nil {
		return "", err
	}
	var nbs []string
	for _, nb := range all {
		if fi, err := os.Stat(s.path(nb, id)); err == nil && fi.Mode().IsRegular() {
			nbs = append(nbs, nb)
		}
	}
	switch len(nbs) {
	case 0:
		return "", ErrNotFound
	case 1:
		return nbs[0], nil
	}
	return "", fmt.Errorf("%w: %s in %s", ErrDuplicate, id, strings.Join(nbs, ", "))
}

// Stat returns the file information of the note.
func (s *Store) Stat(id string) (File, error) {
	nb, err := s.locate(id)
	if err != nil {
		return File{}, err
	}
	fi, err := os.Stat(s.path(nb, id))
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	notebook, trashed := splitPlace(nb)
	return File{ID: id, Notebook: notebook, TrashedNotebook: trashed, ModTime: fi.ModTime(), Size: fi.Size()}, nil
}

// Load reads a note.
func (s *Store) Load(id string) (*Note, error) {
	nb, err := s.locate(id)
	if err != nil {
		return nil, err
	}
	return s.load(nb, id)
}

// LoadFile reads the note of f, for callers that know where it is, such as
// Sync with the files from Scan. It does not check other places for a
// duplicate.
func (s *Store) LoadFile(f File) (*Note, error) {
	if !ValidNotebook(f.Notebook) || (f.TrashedNotebook != "" && (f.Notebook != Trash || !ValidNotebook(f.TrashedNotebook))) {
		return nil, ErrInvalidNotebook
	}
	if !ValidID(f.ID) {
		return nil, ErrInvalidID
	}
	return s.load(placeOf(f.Notebook, f.TrashedNotebook), f.ID)
}

func (s *Store) load(nb, id string) (*Note, error) {
	b, err := os.ReadFile(s.path(nb, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if hasConflictMarkers(b) {
		return nil, fmt.Errorf("%s: %w", s.path(nb, id), ErrConflict)
	}
	var n Note
	if err := yaml.Unmarshal(b, &n); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path(nb, id), err)
	}
	n.ID = id
	n.Notebook, n.TrashedNotebook = splitPlace(nb)
	n.Version = version(b)
	if n.Tags == nil {
		n.Tags = []string{}
	}
	return &n, nil
}

// hasConflictMarkers reports whether b has the markers git writes around a
// conflict. They start a line, which the lines of a body never do, as the
// body is indented.
func hasConflictMarkers(b []byte) bool {
	return conflictStart.Match(b) && conflictEnd.Match(b)
}

var (
	conflictStart = regexp.MustCompile(`(?m)^<<<<<<< `)
	conflictEnd   = regexp.MustCompile(`(?m)^>>>>>>> `)
)

// IsNotePath reports whether p, a path relative to the notes directory with
// slashes, is the place of a note file: <notebook>/<id>.yaml, or
// trash/<notebook>/<id>.yaml for a notebook in the trash.
func IsNotePath(p string) bool {
	_, _, ok := splitNotePath(p)
	return ok
}

func splitNotePath(p string) (place, id string, ok bool) {
	dir, file := path.Split(p)
	id, ok = strings.CutSuffix(file, Ext)
	place = strings.TrimSuffix(dir, "/")
	if !ok || !ValidID(id) {
		return "", "", false
	}
	nb, trashed := splitPlace(place)
	if !ValidNotebook(nb) || (trashed != "" && !ValidNotebook(trashed)) {
		return "", "", false
	}
	return place, id, true
}

// ConflictCopy writes b, a version of the note file at p (a path relative to
// the notes directory, with slashes), as a new note next to it, with a new
// ID and label added to its title, keeping its other keys and comments. It is
// for the other version of a note changed on two machines. It returns the
// path of the new note's file.
func (s *Store) ConflictCopy(p string, b []byte, label string) (string, error) {
	place, _, ok := splitNotePath(p)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrInvalidID, p)
	}
	var n Note
	if err := yaml.Unmarshal(b, &n); err != nil {
		return "", fmt.Errorf("%s: %w", p, err)
	}
	n.Title = strings.TrimSpace(n.Title + " (" + label + ")")
	enc, err := encode(b, &n)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(s.dir, filepath.FromSlash(place)), 0o755); err != nil {
		return "", err
	}
	for {
		id := NewID()
		if _, err := s.locate(id); !errors.Is(err, ErrNotFound) {
			continue // taken, or a duplicate
		}
		f, err := os.OpenFile(s.path(place, id), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(enc); err != nil {
			f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return path.Join(place, id+Ext), nil
	}
}

// Version returns the current version of the note file without parsing it.
func (s *Store) Version(id string) (string, error) {
	nb, err := s.locate(id)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(s.path(nb, id))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return version(b), nil
}

// Create writes a new note file into n.Notebook, creating the notebook if
// needed, and sets n.Version. It returns ErrExists if a note with the same ID
// exists in any notebook.
func (s *Store) Create(n *Note) error {
	if !ValidID(n.ID) {
		return ErrInvalidID
	}
	if !ValidNotebook(n.Notebook) {
		return ErrInvalidNotebook
	}
	switch _, err := s.locate(n.ID); {
	case err == nil, errors.Is(err, ErrDuplicate):
		return ErrExists
	case !errors.Is(err, ErrNotFound):
		return err
	}
	b, err := encode(nil, n)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.dir, n.Notebook), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(n.Notebook, n.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return ErrExists
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	n.Version = version(b)
	return nil
}

// Save overwrites an existing note in place, keeping comments and unknown keys
// in the file. It updates n.Version and n.Notebook. Use Move to change the
// notebook.
func (s *Store) Save(n *Note) error {
	nb, err := s.locate(n.ID)
	if err != nil {
		return err
	}
	p := s.path(nb, n.ID)
	old, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	b, err := encode(old, n)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(p, b); err != nil {
		return err
	}
	n.Notebook, n.TrashedNotebook = splitPlace(nb)
	n.Version = version(b)
	return nil
}

// Move moves the note to the notebook to, creating it if needed.
func (s *Store) Move(id, to string) error {
	if !ValidNotebook(to) {
		return ErrInvalidNotebook
	}
	nb, err := s.locate(id)
	if err != nil {
		return err
	}
	if nb == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(s.dir, to), 0o755); err != nil {
		return err
	}
	// The files of the note move with it.
	from, dest := s.assetsDir(nb, id), s.assetsDir(to, id)
	_, err = os.Stat(from)
	hasFiles := err == nil
	if hasFiles {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("cannot move the files of note %s: %s exists", id, dest)
		}
	}
	if err := os.Rename(s.path(nb, id), s.path(to, id)); err != nil {
		return err
	}
	if hasFiles {
		if err := os.Rename(from, dest); err != nil {
			// Put the note back, so that it stays with its files.
			if err2 := os.Rename(s.path(to, id), s.path(nb, id)); err2 != nil {
				return errors.Join(err, err2)
			}
			return err
		}
	}
	return nil
}

// Delete removes the note file and its files.
func (s *Store) Delete(id string) error {
	nb, err := s.locate(id)
	if err != nil {
		return err
	}
	err = os.Remove(s.path(nb, id))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(s.assetsDir(nb, id))
}

// MatchVersion reports whether the If-Match header value ifMatch matches the
// entity tag v. It accepts "*", a comma-separated list of tags, and tags
// without quotes. Weak tags never match, since If-Match uses the strong
// comparison (RFC 9110, Section 13.1.1).
func MatchVersion(ifMatch, v string) bool {
	for _, tag := range strings.Split(ifMatch, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		if strings.HasPrefix(tag, "W/") {
			continue
		}
		if strings.Trim(tag, `"`) == strings.Trim(v, `"`) {
			return true
		}
	}
	return false
}

// version returns a strong entity tag for the file content b.
func version(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// NotebookFile is the name of the file holding a notebook's settings. It
// cannot be a note file, since note IDs do not start with "_". It also keeps
// an empty notebook in git, which does not record empty directories.
const NotebookFile = "_notebook" + Ext

// NotebookMeta holds the settings of a notebook.
type NotebookMeta struct {
	Name  string `yaml:"name,omitempty"`  // display name; empty for the default
	Order int    `yaml:"order,omitempty"` // sort key; lower comes first
	// Kind is KindCollab for a notebook people share with AI, whose notes AI
	// clients may write, KindDrop for one they may only add notes to, or
	// KindPrivate for one hidden from them. Empty for the default kind,
	// read-only for them. A notebook without a kind in its file has
	// DefaultKind.
	Kind string `yaml:"kind,omitempty"`
	// Sort is the default order of the note list (one of Sorts); empty for
	// SortOrder.
	Sort string `yaml:"sort,omitempty"`
}

// Orders of a note list (NotebookMeta.Sort).
const (
	SortOrder   = "order"   // manual order (Note.SortKey), highest first
	SortCreated = "created" // newest first
	SortUpdated = "updated" // newest first
	SortTitle   = "title"   // by code point, ignoring case and width
)

// Sorts lists the orders of a note list.
var Sorts = []string{SortOrder, SortCreated, SortUpdated, SortTitle}

// Kinds of notebooks other than the default.
const (
	KindCollab  = "collab"  // AI clients may write its notes
	KindDrop    = "drop"    // AI clients may add notes but cannot see them
	KindPrivate = "private" // AI clients cannot see it

	// kindDefault is the default kind in a settings file, written only when
	// it differs from DefaultKind, as for an inbox made default.
	kindDefault = "default"
)

// DefaultKind returns the kind of notebook nb when its settings do not set
// one: KindDrop for the inbox, where AI clients may add notes without seeing
// those of people, and the default kind ("") for the others.
func DefaultKind(nb string) string {
	if nb == Inbox {
		return KindDrop
	}
	return ""
}

// NotebookMeta reads the settings of the notebook. A missing file gives the
// defaults.
func (s *Store) NotebookMeta(nb string) (NotebookMeta, error) {
	if !ValidNotebook(nb) {
		return NotebookMeta{}, ErrInvalidNotebook
	}
	return s.readNotebookMeta(filepath.Join(s.dir, nb, NotebookFile), nb)
}

// readNotebookMeta reads the settings file p of notebook nb.
func (s *Store) readNotebookMeta(p, nb string) (NotebookMeta, error) {
	m := NotebookMeta{Kind: DefaultKind(nb)}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("%s: %w", p, err)
	}
	m.Name = strings.TrimSpace(m.Name)
	switch {
	case m.Kind == kindDefault:
		m.Kind = ""
	case m.Kind == KindCollab && nb != Inbox && nb != Trash:
	case (m.Kind == KindPrivate || m.Kind == KindDrop) && nb != Trash:
	default:
		m.Kind = DefaultKind(nb) // an unknown kind, or one a reserved notebook cannot have
	}
	if !slices.Contains(Sorts, m.Sort) || m.Sort == SortOrder {
		m.Sort = "" // unknown sorts fall back to the default
	}
	return m, nil
}

// SaveNotebookMeta writes the settings of the notebook, creating the notebook
// if needed. Comments and unknown keys in an existing file are kept.
func (s *Store) SaveNotebookMeta(nb string, m NotebookMeta) error {
	if !ValidNotebook(nb) {
		return ErrInvalidNotebook
	}
	dir := filepath.Join(s.dir, nb)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, NotebookFile)
	var doc yaml.Node
	old, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(old) > 0 {
		if err := yaml.Unmarshal(old, &doc); err != nil {
			return err
		}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	root := doc.Content[0]
	if m.Name != "" {
		set(root, "name", str(m.Name, 0))
	} else {
		unset(root, "name")
	}
	if m.Order != 0 {
		set(root, "order", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(m.Order)})
	} else {
		unset(root, "order")
	}
	switch {
	case m.Kind == DefaultKind(nb):
		unset(root, "kind")
	case m.Kind == "":
		set(root, "kind", str(kindDefault, 0))
	default:
		set(root, "kind", str(m.Kind, 0))
	}
	if m.Sort != "" && m.Sort != SortOrder {
		set(root, "sort", str(m.Sort, 0))
	} else {
		unset(root, "sort")
	}
	root.Style = 0
	if len(root.Content) == 0 {
		root.Style = yaml.FlowStyle // "{}"
	}
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return writeFileAtomic(p, []byte(sb.String()))
}

// Notebooks returns the names of the notebooks: the directories under the
// notes directory with valid names, sorted, including inbox and trash.
func (s *Store) Notebooks() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var nbs []string
	for _, e := range entries {
		if e.IsDir() && ValidNotebook(e.Name()) {
			nbs = append(nbs, e.Name())
		}
	}
	return nbs, nil
}

// Scan lists the note files in all notebooks, including the notebooks in the
// trash, sorted by place and ID.
func (s *Store) Scan() ([]File, error) {
	nbs, err := s.places()
	if err != nil {
		return nil, err
	}
	var files []File
	for _, nb := range nbs {
		entries, err := os.ReadDir(filepath.Join(s.dir, filepath.FromSlash(nb)))
		if err != nil {
			return nil, err
		}
		notebook, trashed := splitPlace(nb)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, Ext) {
				continue
			}
			id := strings.TrimSuffix(name, Ext)
			if !ValidID(id) {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				return nil, err
			}
			files = append(files, File{ID: id, Notebook: notebook, TrashedNotebook: trashed, ModTime: fi.ModTime(), Size: fi.Size()})
		}
	}
	return files, nil
}

// TrashedNotebooks returns the IDs of the notebooks in the trash: the
// directories under the trash with valid names, sorted.
func (s *Store) TrashedNotebooks() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, Trash))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var nbs []string
	for _, e := range entries {
		if e.IsDir() && ValidNotebook(e.Name()) {
			nbs = append(nbs, e.Name())
		}
	}
	return nbs, nil
}

// TrashNotebook moves notebook nb with its notes and settings into the trash,
// as Trash/<nb>. It fails with ErrNotebookExists when the trash already holds
// a notebook with the same ID.
func (s *Store) TrashNotebook(nb string) error {
	if !ValidNotebook(nb) {
		return ErrInvalidNotebook
	}
	if nb == Inbox || nb == Trash {
		return ErrReserved
	}
	from, to := filepath.Join(s.dir, nb), filepath.Join(s.dir, Trash, nb)
	if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("%w in the trash: %s", ErrNotebookExists, nb)
	}
	return os.Rename(from, to)
}

// RestoreNotebook moves notebook nb out of the trash. It fails with
// ErrNotebookExists when a notebook with the same ID exists.
func (s *Store) RestoreNotebook(nb string) error {
	if !ValidNotebook(nb) {
		return ErrInvalidNotebook
	}
	from, to := filepath.Join(s.dir, Trash, nb), filepath.Join(s.dir, nb)
	if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("%w: %s", ErrNotebookExists, nb)
	}
	return os.Rename(from, to)
}

// DeleteTrashedNotebook permanently deletes notebook nb in the trash with its
// notes.
func (s *Store) DeleteTrashedNotebook(nb string) error {
	if !ValidNotebook(nb) {
		return ErrInvalidNotebook
	}
	dir := filepath.Join(s.dir, Trash, nb)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return os.RemoveAll(dir)
}

// EmptyTrash permanently deletes the notes in the trash with their files, and
// the notebooks in the trash with their notes. The settings of the trash
// itself and files ebira does not know are left alone.
func (s *Store) EmptyTrash() error {
	dir := filepath.Join(s.dir, Trash)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		id, note := strings.CutSuffix(name, Ext)
		assets, files := strings.CutSuffix(name, AssetsSuffix)
		switch {
		case e.IsDir() && ValidNotebook(name), e.IsDir() && files && ValidID(assets):
			err = os.RemoveAll(filepath.Join(dir, name))
		case !e.IsDir() && note && ValidID(id):
			err = os.Remove(filepath.Join(dir, name))
		default:
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// TrashedNotebookMeta reads the settings of notebook nb in the trash.
func (s *Store) TrashedNotebookMeta(nb string) (NotebookMeta, error) {
	if !ValidNotebook(nb) {
		return NotebookMeta{}, ErrInvalidNotebook
	}
	return s.readNotebookMeta(filepath.Join(s.dir, Trash, nb, NotebookFile), nb)
}

// MigrateFlat moves note files directly under the notes directory, the layout
// before notebooks, into the inbox. It returns the moved IDs.
func (s *Store) MigrateFlat() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var moved []string
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), Ext)
		if e.IsDir() || !ok || !ValidID(id) {
			continue
		}
		if _, err := s.locate(id); !errors.Is(err, ErrNotFound) {
			return moved, fmt.Errorf("cannot move %s into %s: a note with the same id exists", e.Name(), Inbox)
		}
		if err := os.Rename(filepath.Join(s.dir, e.Name()), s.path(Inbox, id)); err != nil {
			return moved, err
		}
		moved = append(moved, id)
	}
	slices.Sort(moved)
	return moved, nil
}

// encode marshals n into YAML. When old is given, it edits the nodes of that
// document so that comments and key order are preserved.
func encode(old []byte, n *Note) ([]byte, error) {
	var doc yaml.Node
	if len(old) > 0 {
		if err := yaml.Unmarshal(old, &doc); err != nil {
			return nil, err
		}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	m := doc.Content[0]

	tags := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, t := range n.Tags {
		tags.Content = append(tags.Content, str(t, 0))
	}
	bodyStyle := yaml.LiteralStyle
	if n.Body == "" {
		bodyStyle = yaml.DoubleQuotedStyle
	}
	set(m, "title", str(n.Title, 0))
	set(m, "tags", tags)
	set(m, "created_at", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: n.CreatedAt.Format(time.RFC3339)})
	set(m, "updated_at", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: n.UpdatedAt.Format(time.RFC3339)})
	if n.TrashedFrom != "" {
		set(m, "trashed_from", str(n.TrashedFrom, 0))
	} else {
		unset(m, "trashed_from")
	}
	if n.Order != nil {
		setBefore(m, "order", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(*n.Order)}, "body")
	} else {
		unset(m, "order")
	}
	if len(old) == 0 {
		for i := 0; i+1 < len(n.Extra); i += 2 {
			if k := n.Extra[i].Value; !slices.Contains(noteKeys, k) && !has(m, k) {
				setBefore(m, k, n.Extra[i+1], "body")
			}
		}
	}
	for _, kv := range []struct{ key, value string }{{"imported_from", n.ImportedFrom}, {"import_hash", n.ImportHash}} {
		if kv.value != "" {
			setBefore(m, kv.key, str(kv.value, 0), "body")
		} else {
			unset(m, kv.key)
		}
	}
	if n.CreatedBy != "" {
		setBefore(m, "created_by", str(n.CreatedBy, 0), "body")
	} else {
		unset(m, "created_by")
	}
	set(m, "body", str(n.Body, bodyStyle))

	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func str(v string, style yaml.Style) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: style}
}

// set sets the value of key in the mapping node m to v, carrying over comments of the old value.
func set(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			v.HeadComment, v.LineComment, v.FootComment = old.HeadComment, old.LineComment, old.FootComment
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

// setBefore is set, but a new key goes before the key next instead of last,
// so that a short key does not end up after a long body.
func setBefore(m *yaml.Node, key string, v *yaml.Node, next string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			set(m, key, v)
			return
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == next {
			k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			m.Content = append(m.Content[:i], append([]*yaml.Node{k, v}, m.Content[i:]...)...)
			return
		}
	}
	set(m, key, v)
}

// noteKeys are the keys of note files that Note sets.
var noteKeys = []string{"title", "tags", "created_at", "updated_at", "trashed_from", "order", "created_by", "imported_from", "import_hash", "body"}

// has reports whether the mapping node m has key.
func has(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return true
		}
	}
	return false
}

// unset removes key from the mapping node m.
func unset(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func writeFileAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
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
	return os.Rename(tmp, path)
}
