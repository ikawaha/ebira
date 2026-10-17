// Package importer turns Markdown files into notes: the title, tags and times
// from the front matter, the first heading or the file name, the body without
// the front matter, and the other front matter keys to keep in the note file.
// It only converts; the caller writes the note.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

// MaxSize is the largest Markdown file that can be imported, in bytes.
const MaxSize = 5 << 20

// ErrTooLarge is returned for a file larger than MaxSize.
var ErrTooLarge = fmt.Errorf("the file is larger than %d MB", MaxSize>>20)

// Input is a Markdown file to import.
type Input struct {
	Name    string    // file name, the title when there is no other
	Content string    // the file, in UTF-8
	ModTime time.Time // modification time of the file; zero when unknown
	Now     time.Time // the time of the import, for times not given otherwise
}

// Note is the note made from a Markdown file.
type Note struct {
	Title     string
	Tags      []string
	CreatedAt time.Time
	UpdatedAt time.Time
	Body      string
	// Extra holds the other front matter keys to keep in the note file, as
	// pairs of key and value nodes, in their order in the file.
	Extra []*yaml.Node
	// Hash is a hash of the content, to tell a file imported before.
	Hash string
	// Images are the relative paths of the images the body refers to, as
	// written, without repeats. URLs and absolute paths are left out.
	Images []string
	// Warnings tell what could not be converted as is.
	Warnings []string
}

// Keys of the front matter read into the note. The other keys are kept,
// except those of ebira's note files.
var (
	titleKeys   = []string{"title"}
	tagKeys     = []string{"tags"}
	createdKeys = []string{"created", "created_at", "date"}
	updatedKeys = []string{"updated", "updated_at", "modified", "lastmod"}
	// reservedKeys are the keys of ebira's note files (store.Note), which an
	// imported file must not set, such as created_by.
	reservedKeys = []string{"title", "tags", "created_at", "updated_at", "trashed_from", "order", "created_by", "body", "imported_from", "import_hash"}
)

// Parse converts a Markdown file into a note.
func Parse(in Input) (*Note, error) {
	if len(in.Content) > MaxSize {
		return nil, ErrTooLarge
	}
	content := strings.TrimPrefix(in.Content, "\uFEFF")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	sum := sha256.Sum256([]byte(content))
	n := &Note{Hash: "sha256:" + hex.EncodeToString(sum[:])}

	body := content
	var fm *yaml.Node
	if text, rest, ok := splitFrontMatter(content); ok {
		var doc yaml.Node
		err := yaml.Unmarshal([]byte(text), &doc)
		switch {
		case err != nil:
			n.Warnings = append(n.Warnings, fmt.Sprintf("the front matter could not be read and was kept in the body: %v", err))
		case len(doc.Content) == 0:
			body = rest // empty front matter
		case doc.Content[0].Kind != yaml.MappingNode:
			n.Warnings = append(n.Warnings, "the front matter is not a mapping and was kept in the body")
		default:
			fm, body = doc.Content[0], rest
		}
	}

	var created, updated time.Time
	var dropped []string
	if fm != nil {
		for i := 0; i+1 < len(fm.Content); i += 2 {
			k, v := fm.Content[i], fm.Content[i+1]
			key := k.Value
			switch {
			case slices.Contains(titleKeys, key):
				if v.Kind == yaml.ScalarNode {
					n.Title = strings.TrimSpace(v.Value)
				}
			case slices.Contains(tagKeys, key):
				n.Tags = parseTags(v)
			case slices.Contains(createdKeys, key):
				if t, ok := parseTime(v); ok && created.IsZero() {
					created = t
				} else if !ok {
					n.Warnings = append(n.Warnings, fmt.Sprintf("the time %s: %q could not be read", key, v.Value))
				}
			case slices.Contains(updatedKeys, key):
				if t, ok := parseTime(v); ok && updated.IsZero() {
					updated = t
				} else if !ok {
					n.Warnings = append(n.Warnings, fmt.Sprintf("the time %s: %q could not be read", key, v.Value))
				}
			case slices.Contains(reservedKeys, key):
				dropped = append(dropped, key)
			default:
				n.Extra = append(n.Extra, k, v)
			}
		}
	}
	if len(dropped) > 0 {
		n.Warnings = append(n.Warnings, "front matter keys used by ebira were dropped: "+strings.Join(dropped, ", "))
	}

	// The first heading is the title when the front matter has none, and is
	// left out of the body when it repeats the title.
	if h, rest, ok := firstHeading(body); ok && (n.Title == "" || h == n.Title) {
		n.Title, body = h, rest
	}
	if n.Title == "" {
		base := path.Base(strings.ReplaceAll(in.Name, `\`, "/"))
		n.Title = strings.TrimSpace(norm.NFC.String(strings.TrimSuffix(base, path.Ext(base))))
	}
	if n.Title == "" || n.Title == "." || n.Title == "/" {
		n.Title = "Untitled"
	}
	n.Body = strings.TrimLeft(body, "\n")

	// Times: the front matter, else the file, else now.
	if created.IsZero() {
		created = cmpOr(in.ModTime, in.Now)
	}
	if updated.IsZero() {
		updated = cmpOr(in.ModTime, created)
	}
	if updated.Before(created) {
		updated = created
	}
	n.CreatedAt = created.UTC().Truncate(time.Second)
	n.UpdatedAt = updated.UTC().Truncate(time.Second)
	n.Images = images(n.Body)
	return n, nil
}

func cmpOr(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	return a
}

// splitFrontMatter splits a YAML front matter, between lines of "---" at the
// top and "---" or "...", from the rest of the content.
func splitFrontMatter(s string) (fm, rest string, ok bool) {
	if !strings.HasPrefix(s, "---\n") {
		return "", s, false
	}
	lines := strings.SplitAfter(s[4:], "\n")
	off := 4
	for i, l := range lines {
		if t := strings.TrimRight(l, " \t\n"); t == "---" || t == "..." {
			return s[4:off], strings.Join(lines[i+1:], ""), true
		}
		off += len(l)
	}
	return "", s, false
}

// heading matches an ATX heading of level 1, with optional closing hashes.
var heading = regexp.MustCompile(`^#[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)

// firstHeading returns the text of a level 1 heading on the first line that is
// not blank, and the body after it.
func firstHeading(body string) (string, string, bool) {
	s := strings.TrimLeft(body, "\n")
	line, rest, _ := strings.Cut(s, "\n")
	m := heading.FindStringSubmatch(line)
	if m == nil || strings.TrimSpace(m[1]) == "" {
		return "", body, false
	}
	return strings.TrimSpace(m[1]), rest, true
}

// parseTags reads tags given as a list or as a comma-separated string. A
// leading "#" is dropped, as some tools write tags so.
func parseTags(v *yaml.Node) []string {
	var raw []string
	switch v.Kind {
	case yaml.SequenceNode:
		for _, e := range v.Content {
			if e.Kind == yaml.ScalarNode {
				raw = append(raw, e.Value)
			}
		}
	case yaml.ScalarNode:
		raw = strings.Split(v.Value, ",")
	}
	var tags []string
	for _, t := range raw {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	return tags
}

// timeLayouts are the forms of times read from the front matter. Times
// without a zone are in the local time zone.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
}

func parseTime(v *yaml.Node) (time.Time, bool) {
	if v.Kind != yaml.ScalarNode {
		return time.Time{}, false
	}
	s := strings.TrimSpace(v.Value)
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Image references: Markdown images, ![alt](path "title") or ![alt](<path>),
// and HTML img elements.
var (
	mdImage   = regexp.MustCompile(`(!\[[^\]]*\]\(\s*)(<[^>\n]*>|[^)\s]+)`)
	htmlImage = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc\s*=\s*["'])([^"']+)`)
	scheme    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

// target returns the path of an image as written, without angle brackets.
func target(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">")
}

// relative reports whether p is a relative path, not a URL, an absolute path
// or a fragment.
func relative(p string) bool {
	return p != "" && !scheme.MatchString(p) && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "#") && !strings.HasPrefix(p, `\`)
}

// images returns the relative paths of the images body refers to.
func images(body string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{mdImage, htmlImage} {
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if p := target(m[2]); relative(p) && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// RewriteImages replaces the paths of images in body, as Images gives them,
// with the paths in paths.
func RewriteImages(body string, paths map[string]string) string {
	for _, re := range []*regexp.Regexp{mdImage, htmlImage} {
		body = re.ReplaceAllStringFunc(body, func(s string) string {
			m := re.FindStringSubmatch(s)
			p, ok := paths[target(m[2])]
			if !ok {
				return s
			}
			return m[1] + p
		})
	}
	return body
}

// ErrNotUTF8 is for callers that read files: a file to import must be UTF-8.
var ErrNotUTF8 = errors.New("the file is not UTF-8")
