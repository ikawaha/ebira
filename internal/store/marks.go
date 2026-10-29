package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// MarkIDs are the marks a note can have: colors, and symbols that the UIs draw
// as emoji. Their names, which say what they mean to the user, are in the
// settings file of the notes (Config.Marks), so that every machine shows the
// same.
var MarkIDs = []string{
	"red", "orange", "yellow", "green", "blue", "purple", "gray",
	"star", "important", "question", "done", "pin", "urgent", "idea",
	"wip", "hold", "dropped",
}

// MaxMarks is the number of marks a note can have. It is one for now; the
// file and the API hold a list, so that it can grow.
const MaxMarks = 1

// ValidMark reports whether id is one of MarkIDs.
func ValidMark(id string) bool { return slices.Contains(MarkIDs, id) }

// CleanMarks returns the marks of marks that are MarkIDs, without repeats, in
// their order, at most MaxMarks; unknown ones, such as from a later version,
// are left out.
func CleanMarks(marks []string) []string {
	var res []string
	for _, m := range marks {
		if ValidMark(m) && !slices.Contains(res, m) && len(res) < MaxMarks {
			res = append(res, m)
		}
	}
	return res
}

// SaveMarkNames writes the names of the marks to ConfigFile, as its marks
// key, in the order of MarkIDs; a mark without a name is left out, and no
// names remove the key. The other keys and the comments of the file are kept.
func (s *Store) SaveMarkNames(names map[string]string) error {
	p := s.ConfigPath()
	old, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var doc yaml.Node
	if len(bytes.TrimSpace(old)) > 0 {
		if err := yaml.Unmarshal(old, &doc); err != nil {
			return err
		}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	m := doc.Content[0]
	marks := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, id := range MarkIDs {
		if name := names[id]; name != "" {
			marks.Content = append(marks.Content, str(id, 0), str(name, 0))
		}
	}
	if len(marks.Content) > 0 {
		set(m, "marks", marks)
	} else {
		unset(m, "marks")
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return writeFileAtomic(p, buf.Bytes())
}
