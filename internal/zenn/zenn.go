// Package zenn writes notes as articles of Zenn (zenn.dev) to the GitHub
// repository that Zenn deploys from: articles/<slug>.md and the images of the
// article in images/<slug>/. Zenn's public API makes no articles, and its
// internal one would need the session of the user's account, so the articles
// go through the repository, the way Zenn takes them.
//
// The repository is cloned for ebira alone, in the state directory of the
// notes: ebira resets the clone to the remote branch, writes the article,
// commits and pushes, so nothing of the user's own work trees is touched.
package zenn

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Settings tell where the articles go.
type Settings struct {
	// Repo is the URL of the GitHub repository Zenn deploys from.
	Repo string
	// Branch is the branch Zenn deploys from; empty for the default branch
	// of the repository.
	Branch string
	// User is the Zenn user name, to make the URLs of the articles.
	User string
}

// Configured reports whether articles can be written.
func (s Settings) Configured() bool { return s.Repo != "" }

// ArticleURL returns the URL of the article slug on Zenn, or "" without the
// repository or the user name: the articles are linked to only while notes
// go to Zenn.
func (s Settings) ArticleURL(slug string) string {
	if s.Repo == "" || s.User == "" {
		return ""
	}
	return "https://zenn.dev/" + s.User + "/articles/" + slug
}

// The rules of Zenn for articles, as its editor checks them.
const (
	MaxTitle      = 70 // in UTF-16 units, as JavaScript counts
	MaxTopics     = 5
	MaxTopic      = 18 // in UTF-16 units
	DefaultType   = "tech"
	maxEmojiBytes = 32
)

// Emoji are the emoji an article gets when none is chosen, one at random,
// as the articles on Zenn differ by them at a glance.
var Emoji = []string{"📝", "📘", "📗", "📙", "🔖", "💡", "🔍", "🛠", "🧪", "🧩", "🚀", "🌱", "🐦", "🦔", "🐙", "🍵", "🎯", "🧭", "⚙️", "✨"}

// RandomEmoji returns one of Emoji at random.
func RandomEmoji() string { return Emoji[rand.IntN(len(Emoji))] }

// Types are the types of articles: technical articles and ideas.
var Types = []string{"tech", "idea"}

var slugRe = regexp.MustCompile(`^[0-9a-z_-]{12,50}$`)

// ValidSlug reports whether s may be the slug of an article: 12 to 50 of
// lower case letters, digits, hyphens and underscores.
func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// jsLen returns the length of s as JavaScript counts it, in UTF-16 units.
func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// topicSymbol reports whether r is a character Zenn refuses in topics: the
// ASCII spaces, punctuation and symbols.
func topicSymbol(r rune) bool {
	return r < utf8.RuneSelf && (r == ' ' || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsControl(r))
}

// CheckTopic tells why t cannot be a topic, or nil when it can.
func CheckTopic(t string) error {
	switch {
	case t == "":
		return errors.New("a topic is empty")
	case jsLen(t) > MaxTopic:
		return fmt.Errorf("topic %q is longer than %d characters", t, MaxTopic)
	case strings.ContainsFunc(t, topicSymbol):
		return fmt.Errorf("topic %q has a space or a symbol: write C++ as cpp, C# as csharp", t)
	}
	return nil
}

// Topics makes the topics of an article from the tags of a note: in NFKC and
// lower case, without repeats, up to MaxTopics. It also returns the tags left
// out, which cannot be topics or come after the last one.
func Topics(tags []string) (topics, dropped []string) {
	for _, tag := range tags {
		t := strings.ToLower(norm.NFKC.String(strings.TrimSpace(tag)))
		switch {
		case CheckTopic(t) != nil, len(topics) == MaxTopics:
			dropped = append(dropped, tag)
		case !slices.Contains(topics, t):
			topics = append(topics, t)
		}
	}
	return topics, dropped
}

// CheckEmoji tells why e cannot be the emoji of an article. Zenn takes one
// emoji; this only refuses what is plainly not one, and Zenn checks the rest
// when it deploys.
func CheckEmoji(e string) error {
	if e == "" || len(e) > maxEmojiBytes || strings.ContainsFunc(e, func(r rune) bool {
		return r < utf8.RuneSelf || unicode.IsSpace(r) || unicode.IsLetter(r) || unicode.IsNumber(r)
	}) {
		return fmt.Errorf("emoji %q is not one emoji", e)
	}
	return nil
}

// Article is an article to write.
type Article struct {
	Slug      string
	Title     string
	Emoji     string
	Type      string
	Topics    []string
	Published bool
	// Body is the Markdown of the article, with the paths of its images in
	// the repository (see Convert).
	Body string
	// Images are the images of the article: their names in images/<slug>/,
	// and the paths of the files to copy there.
	Images []Image
	// Source is the hash of the note the article was made from, kept with
	// the note rather than written (see store.ZennSource).
	Source string
}

// Image is an image of an article.
type Image struct {
	Name string // the name in images/<slug>/
	Path string // the file to copy
}

// Check tells why a cannot be written, or nil when it can.
func (a *Article) Check() error {
	if !ValidSlug(a.Slug) {
		return fmt.Errorf("slug %q is not 12 to 50 of lower case letters, digits, hyphens and underscores", a.Slug)
	}
	if strings.TrimSpace(a.Title) == "" {
		return errors.New("the article has no title")
	}
	if jsLen(a.Title) > MaxTitle {
		return fmt.Errorf("the title is longer than %d characters", MaxTitle)
	}
	if err := CheckEmoji(a.Emoji); err != nil {
		return err
	}
	if !slices.Contains(Types, a.Type) {
		return fmt.Errorf("unknown type %q (%s)", a.Type, strings.Join(Types, ", "))
	}
	if len(a.Topics) > MaxTopics {
		return fmt.Errorf("more than %d topics", MaxTopics)
	}
	for _, t := range a.Topics {
		if err := CheckTopic(t); err != nil {
			return err
		}
	}
	return nil
}

// File returns the content of the article file: the front matter, in Zenn's
// order, and the body.
func (a *Article) File() ([]byte, error) {
	topics := make([]string, len(a.Topics))
	for i, t := range a.Topics {
		topics[i] = quote(t)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "---\ntitle: %s\nemoji: %s\ntype: %s\ntopics: [%s]\npublished: %t\n---\n\n",
		quote(a.Title), quote(a.Emoji), quote(a.Type), strings.Join(topics, ", "), a.Published)
	b.WriteString(strings.TrimRight(a.Body, "\n"))
	b.WriteString("\n")
	return b.Bytes(), nil
}

// quote writes s as a double-quoted YAML scalar, escaping only what must be,
// so that emoji and other text stay readable (a YAML encoder escapes the
// characters outside the basic plane).
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || r == '\ufeff':
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Hash returns the hash of the content of an article file, to tell later
// whether the file was changed since ebira wrote it.
func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
