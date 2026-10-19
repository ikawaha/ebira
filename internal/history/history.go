// Package history keeps the earlier versions of notes on this device, so that
// a note can be put back as it was before it was saved or changed outside.
//
// A version is written when a note's title, tags or body is replaced: by a
// save through the API, or by a change of the file found by a sync (an
// external editor, git pull). The versions live in the state directory of the
// notes directory, as <dir>/<note ID>/<rev>.yaml, and are not synced with the
// notes: the notes repository and file sync services stay as small as they
// are, and git keeps its own history.
package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Limits on the versions kept for a note: at most MaxVersions, and those
// replaced more than MaxAge ago are dropped, except the MinKeep newest, so
// that a note edited long ago can still go back one step.
const (
	MaxVersions = 50
	MaxAge      = 30 * 24 * time.Hour
	MinKeep     = 5
)

// ErrNotFound is returned for a version that does not exist.
var ErrNotFound = errors.New("version not found")

// Version is an earlier version of a note.
type Version struct {
	// Rev identifies the version among those of the note: the time it was
	// replaced, in nanoseconds since the Unix epoch.
	Rev        string    `yaml:"-"`
	Title      string    `yaml:"title"`
	Tags       []string  `yaml:"tags"`
	UpdatedAt  time.Time `yaml:"updated_at"`
	ReplacedAt time.Time `yaml:"replaced_at"`
	Body       string    `yaml:"body"`
}

// Store keeps the versions of notes under a directory.
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

// New returns a store of versions under dir, which is created if needed.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, now: time.Now}, nil
}

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	revPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
)

func (s *Store) noteDir(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("invalid note ID %q", id)
	}
	return filepath.Join(s.dir, id), nil
}

// Add keeps v as a version of note id, replaced now, and drops the versions
// beyond the limits.
func (s *Store) Add(id string, v Version) error {
	dir, err := s.noteDir(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	now := s.now()
	v.ReplacedAt = now.UTC().Truncate(time.Second)
	b, err := yaml.Marshal(&v)
	if err != nil {
		return err
	}
	// The rev is the time in nanoseconds, made unique if two versions are
	// replaced within the same one.
	for n := now.UnixNano(); ; n++ {
		path := filepath.Join(dir, strconv.FormatInt(n, 10)+".yaml")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(path)
			return err
		}
		break
	}
	return s.prune(dir, now)
}

// revs returns the revs of the versions in dir, newest first.
func revs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs []string
	for _, e := range entries {
		rev, ok := strings.CutSuffix(e.Name(), ".yaml")
		if ok && !e.IsDir() && revPattern.MatchString(rev) {
			rs = append(rs, rev)
		}
	}
	// Revs of the same length compare as numbers; all are 19 digits until 2286.
	slices.SortFunc(rs, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(b, a)
	})
	return rs, nil
}

// prune drops the versions in dir beyond the limits.
func (s *Store) prune(dir string, now time.Time) error {
	rs, err := revs(dir)
	if err != nil {
		return err
	}
	cut := now.Add(-MaxAge).UnixNano()
	for i, rev := range rs {
		n, _ := strconv.ParseInt(rev, 10, 64)
		if i >= MaxVersions || i >= MinKeep && n < cut {
			if err := os.Remove(filepath.Join(dir, rev+".yaml")); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

// List returns the versions of note id, newest first.
func (s *Store) List(id string) ([]*Version, error) {
	dir, err := s.noteDir(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, err := revs(dir)
	if err != nil {
		return nil, err
	}
	vs := []*Version{}
	for _, rev := range rs {
		v, err := read(dir, rev)
		if err != nil {
			continue // skip a broken file rather than hide the others
		}
		vs = append(vs, v)
	}
	return vs, nil
}

// Get returns the version rev of note id.
func (s *Store) Get(id, rev string) (*Version, error) {
	dir, err := s.noteDir(id)
	if err != nil {
		return nil, err
	}
	if !revPattern.MatchString(rev) {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := read(dir, rev)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return v, err
}

func read(dir, rev string) (*Version, error) {
	b, err := os.ReadFile(filepath.Join(dir, rev+".yaml"))
	if err != nil {
		return nil, err
	}
	var v Version
	if err := yaml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", filepath.Base(dir), rev, err)
	}
	v.Rev = rev
	if v.Tags == nil {
		v.Tags = []string{}
	}
	return &v, nil
}

// Remove drops the versions of note id, such as when the note is deleted for
// good.
func (s *Store) Remove(id string) error {
	dir, err := s.noteDir(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(dir)
}

// Sweep drops the versions of the notes for which keep returns false, such as
// notes that no longer exist, and applies the limits to the others: Add
// applies them only to the note it adds to, so the versions of a note not
// edited since would otherwise stay past MaxAge.
func (s *Store) Sweep(keep func(id string) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	now := s.now()
	for _, e := range entries {
		if !e.IsDir() || !idPattern.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(s.dir, e.Name())
		if !keep(e.Name()) {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
			continue
		}
		if err := s.prune(dir, now); err != nil {
			return err
		}
		if rs, err := revs(dir); err == nil && len(rs) == 0 {
			os.Remove(dir) // fails, and is kept, if something else is in it
		}
	}
	return nil
}
