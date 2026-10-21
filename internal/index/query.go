package index

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Query is a search query, parsed: the terms to find, each a word or a phrase
// that may hold spaces, and the tags the notes must have. All are combined
// with AND.
type Query struct {
	Terms []string
	Tags  []string
}

// tagPrefix starts a tag in a query, as in tag:todo.
const tagPrefix = "tag:"

// ParseQuery parses q. Terms are separated by spaces. A term in double
// quotes is a phrase, which may hold spaces and matches its words in that
// order; an unclosed quote runs to the end, so that a query being typed
// still searches. tag:name, or tag:"a name" for a tag with spaces, asks for
// notes with the tag; tag: alone is dropped, as while typing a tag.
func ParseQuery(q string) Query {
	var res Query
	rs := []rune(q)
	quoted := func(i int) (string, int) { // the text after the quote at i, and the end
		j := i + 1
		for j < len(rs) && rs[j] != '"' {
			j++
		}
		s := string(rs[i+1 : min(j, len(rs))])
		return s, min(j+1, len(rs))
	}
	for i := 0; i < len(rs); {
		if unicode.IsSpace(rs[i]) {
			i++
			continue
		}
		if rs[i] == '"' {
			s, next := quoted(i)
			if s = strings.TrimSpace(s); s != "" {
				res.Terms = append(res.Terms, s)
			}
			i = next
			continue
		}
		if p := len([]rune(tagPrefix)); i+p <= len(rs) && strings.EqualFold(string(rs[i:i+p]), tagPrefix) {
			var tag string
			next := i + p
			if next < len(rs) && rs[next] == '"' {
				tag, next = quoted(next)
			} else {
				for next < len(rs) && !unicode.IsSpace(rs[next]) {
					next++
				}
				tag = string(rs[i+p : next])
			}
			if tag = strings.TrimSpace(tag); tag != "" {
				res.Tags = append(res.Tags, tag)
			}
			i = next
			continue
		}
		j := i
		for j < len(rs) && !unicode.IsSpace(rs[j]) {
			j++
		}
		res.Terms = append(res.Terms, string(rs[i:j]))
		i = j
	}
	return res
}

// tagKey is the key tags are compared by in a query: NFKC and lower case, so
// that full-width and half-width forms, and upper and lower case, match.
func tagKey(tag string) string { return strings.ToLower(norm.NFKC.String(tag)) }
