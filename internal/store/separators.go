package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SeparatorsFile is the name of the file holding the separators of a
// notebook. Like NotebookFile, it cannot be a note file, since note IDs do not
// start with "_", and it moves with the notebook.
const SeparatorsFile = "_separators" + Ext

// Separator is a bar between the notes of a notebook in the manual order, to
// arrange them. It has a heading and a date to show, but no body, and is not
// a note: it is not searched, tagged, counted or put in the trash.
type Separator struct {
	ID    string `yaml:"id"`
	Title string `yaml:"title,omitempty"`
	// Date is a date to show, as YYYY-MM-DD; empty for none.
	Date string `yaml:"date,omitempty"`
	// Color is the color of the bar, one of SeparatorColors; empty for the
	// plain one.
	Color string `yaml:"color,omitempty"`
	// Order is the key of the separator in the manual order, among the keys
	// of the notes (Note.SortKey); higher comes first.
	Order int64 `yaml:"order"`
}

// SeparatorColors are the colors a separator can have: those of the color
// marks (MarkIDs), so that the UIs draw them alike.
var SeparatorColors = []string{"red", "orange", "yellow", "green", "blue", "purple", "gray"}

// ValidSeparatorColor reports whether c is one of SeparatorColors.
func ValidSeparatorColor(c string) bool { return slices.Contains(SeparatorColors, c) }

// separatorIDRe is the pattern of separator IDs. They fit the pattern of
// note IDs, so that an API taking a note to put another after takes a
// separator as well, and start with sep- to tell them apart.
var separatorIDRe = regexp.MustCompile(`^sep-[a-z0-9_-]{1,60}$`)

// ValidSeparatorID reports whether id is a valid separator ID.
func ValidSeparatorID(id string) bool { return separatorIDRe.MatchString(id) }

// NewSeparatorID returns a random separator ID.
func NewSeparatorID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "sep-" + hex.EncodeToString(b[:])
}

// ValidDate reports whether d is a date as YYYY-MM-DD.
func ValidDate(d string) bool {
	_, err := time.Parse(time.DateOnly, d)
	return err == nil
}

// Separators reads the separators of notebook nb in the manual order, highest
// first. A missing file gives none. Entries with an invalid or repeated ID
// are left out, and an invalid date or color is dropped.
func (s *Store) Separators(nb string) ([]Separator, error) {
	if !ValidNotebook(nb) {
		return nil, ErrInvalidNotebook
	}
	p := s.separatorsPath(nb)
	b, err := readFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Separator
	if err := yaml.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	var res []Separator
	for _, sep := range list {
		if !ValidSeparatorID(sep.ID) || slices.ContainsFunc(res, func(r Separator) bool { return r.ID == sep.ID }) {
			continue
		}
		sep.Title = strings.TrimSpace(sep.Title)
		if !ValidDate(sep.Date) {
			sep.Date = ""
		}
		if !ValidSeparatorColor(sep.Color) {
			sep.Color = ""
		}
		res = append(res, sep)
	}
	SortSeparators(res)
	return res, nil
}

// SortSeparators sorts separators in the manual order: highest first, then by
// ID.
func SortSeparators(seps []Separator) {
	slices.SortStableFunc(seps, func(a, b Separator) int {
		switch {
		case a.Order > b.Order:
			return -1
		case a.Order < b.Order:
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// SaveSeparators writes the separators of notebook nb, which must exist, in
// the manual order. Comments and unknown keys of the entries already in the
// file are kept. No separators remove the file.
func (s *Store) SaveSeparators(nb string, seps []Separator) error {
	if !ValidNotebook(nb) {
		return ErrInvalidNotebook
	}
	p := s.separatorsPath(nb)
	if fi, err := os.Stat(filepath.Dir(p)); err != nil || !fi.IsDir() {
		return ErrNotFound
	}
	if len(seps) == 0 {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	old, err := readFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(old)) > 0 {
		if err := yaml.Unmarshal(old, &doc); err != nil {
			return err
		}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.SequenceNode, Tag: "!!seq"}}}
	}
	seq := doc.Content[0]
	nodes := map[string]*yaml.Node{}
	for _, n := range seq.Content {
		if n.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if id := n.Content[i+1].Value; n.Content[i].Value == "id" && nodes[id] == nil {
				nodes[id] = n // the first, as Separators reads
			}
		}
	}
	sorted := slices.Clone(seps)
	SortSeparators(sorted)
	seq.Content = seq.Content[:0]
	seq.Style = 0
	for _, sep := range sorted {
		if !ValidSeparatorID(sep.ID) {
			return fmt.Errorf("invalid separator id %q", sep.ID)
		}
		n := nodes[sep.ID]
		if n == nil {
			n = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		n.Style = 0
		set(n, "id", str(sep.ID, 0))
		if t := strings.TrimSpace(sep.Title); t != "" {
			setBefore(n, "title", str(t, 0), "order")
		} else {
			unset(n, "title")
		}
		if ValidDate(sep.Date) {
			setBefore(n, "date", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: sep.Date}, "order")
		} else {
			unset(n, "date")
		}
		if ValidSeparatorColor(sep.Color) {
			setBefore(n, "color", str(sep.Color, 0), "order")
		} else {
			unset(n, "color")
		}
		set(n, "order", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(sep.Order)})
		seq.Content = append(seq.Content, n)
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

func (s *Store) separatorsPath(nb string) string {
	return filepath.Join(s.dir, filepath.FromSlash(nb), SeparatorsFile)
}
