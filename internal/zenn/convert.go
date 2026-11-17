package zenn

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Warning tells what Convert or Topics had to leave out of an article, for
// the UI to show in its language.
type Warning struct {
	// Kind is file (a file of a note that is no image of this note, whose
	// link became its text), note (a link to a note that is no article,
	// which became its text) or tag (a tag that is no topic).
	Kind string
	// Name is the name of the file, the ID of the note or the tag.
	Name string
}

// The kinds of warnings.
const (
	WarnFile = "file"
	WarnNote = "note"
	WarnTag  = "tag"
)

// Converted is the body of a note made the body of an article.
type Converted struct {
	Body string
	// Images are the names of the files of the note shown as images, in
	// order of appearance.
	Images   []string
	Warnings []Warning
}

var (
	fenceRe = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	// linkRe matches a Markdown link or image with an inline destination.
	linkRe = regexp.MustCompile(`(!?)\[([^\[\]]*)\]\(\s*<?([^()\s<>]+)>?(\s+"[^"]*")?\s*\)`)
	// noteLinkRe matches a link to a note of the web UI.
	noteLinkRe = regexp.MustCompile(`^(?:https?://[^/\s]+)?/(?:ui/)?#note/([a-z0-9][a-z0-9_-]{0,63})$`)
	// fileRe matches a reference to a file of a note, as store.FileRefs.
	fileRe = regexp.MustCompile(`(?:\./)?([a-z0-9][a-z0-9_-]{0,63})\.assets/((?:[\p{L}\p{N}_-]|%[0-9A-Fa-f]{2})+(?:\.[a-z0-9]{1,16})?)`)
)

// Convert makes the body of note id the body of the article slug: the images
// of the note go to /images/<slug>/, as Zenn takes them; a link to another
// note goes to its article, by articleURL, or becomes its text when it has
// none; a link to another file of a note, which Zenn cannot hold, becomes its
// text. isImage tells whether a file of the note is an image. Code is left as
// it is.
func Convert(body, id, slug string, isImage func(name string) bool, articleURL func(id string) string) Converted {
	c := &converter{id: id, slug: slug, isImage: isImage, articleURL: articleURL}
	var b strings.Builder
	var fence string // the fence of the code block the line is in
	lines := strings.SplitAfter(body, "\n")
	for _, line := range lines {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			switch {
			case fence == "":
				fence = m[1]
				b.WriteString(line)
				continue
			case m[1][0] == fence[0] && len(m[1]) >= len(fence) && strings.TrimSpace(line[len(m[0]):]) == "":
				fence = ""
				b.WriteString(line)
				continue
			}
		}
		if fence != "" {
			b.WriteString(line)
			continue
		}
		b.WriteString(c.line(line))
	}
	return Converted{Body: b.String(), Images: c.images, Warnings: c.warnings}
}

type converter struct {
	id, slug   string
	isImage    func(name string) bool
	articleURL func(id string) string
	images     []string
	warnings   []Warning
}

// line converts a line outside code blocks, leaving its code spans.
func (c *converter) line(s string) string {
	var b strings.Builder
	for s != "" {
		i := strings.IndexByte(s, '`')
		if i < 0 {
			b.WriteString(c.text(s))
			break
		}
		b.WriteString(c.text(s[:i]))
		n := len(s[i:]) - len(strings.TrimLeft(s[i:], "`"))
		tick := s[i : i+n]
		end := closingTicks(s[i+n:], tick)
		if end < 0 {
			b.WriteString(tick)
			s = s[i+n:]
			continue
		}
		b.WriteString(s[i : i+n+end+n])
		s = s[i+n+end+n:]
	}
	return b.String()
}

// closingTicks returns where the run of backticks tick closes a code span in
// s, or -1.
func closingTicks(s, tick string) int {
	for off := 0; off < len(s); {
		j := strings.Index(s[off:], tick)
		if j < 0 {
			return -1
		}
		j += off
		k := j + len(tick)
		if (j == 0 || s[j-1] != '`') && (k == len(s) || s[k] != '`') {
			return j
		}
		off = k
		for off < len(s) && s[off] == '`' {
			off++
		}
	}
	return -1
}

// text converts text outside code.
func (c *converter) text(s string) string {
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := linkRe.FindStringSubmatch(m)
		bang, label, dest, title := sm[1], sm[2], sm[3], sm[4]
		link := func(to string) string { return bang + "[" + label + "](" + to + title + ")" }
		if nm := noteLinkRe.FindStringSubmatch(dest); nm != nil {
			if u := c.articleURL(nm[1]); u != "" {
				return link(u)
			}
			c.warn(WarnNote, nm[1])
			return label
		}
		if fm := fileRe.FindStringSubmatch(dest); fm != nil && fm[0] == dest {
			if p, ok := c.image(fm[1], fm[2]); ok {
				return link(p)
			}
			c.warn(WarnFile, fileName(fm[2]))
			if bang != "" && label == "" {
				return ""
			}
			return label
		}
		return m
	})
	// References elsewhere, such as in the src of an HTML image.
	return fileRe.ReplaceAllStringFunc(s, func(m string) string {
		fm := fileRe.FindStringSubmatch(m)
		if p, ok := c.image(fm[1], fm[2]); ok {
			return p
		}
		c.warn(WarnFile, fileName(fm[2]))
		return m
	})
}

// image returns the path in the repository of the file name of note id, as
// referred to, when it is an image of this note.
func (c *converter) image(id, ref string) (string, bool) {
	name := fileName(ref)
	if id != c.id || !c.isImage(name) {
		return "", false
	}
	if !slices.Contains(c.images, name) {
		c.images = append(c.images, name)
	}
	return "/images/" + c.slug + "/" + url.PathEscape(name), true
}

func (c *converter) warn(kind, name string) {
	w := Warning{Kind: kind, Name: name}
	for _, x := range c.warnings {
		if x == w {
			return
		}
	}
	c.warnings = append(c.warnings, w)
}

// fileName returns the name of a file as referred to, maybe percent-encoded.
func fileName(ref string) string {
	if u, err := url.PathUnescape(ref); err == nil {
		return norm.NFC.String(u)
	}
	return ref
}
