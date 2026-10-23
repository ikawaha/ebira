package index

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Query is a search query, parsed: the terms to find, each a word or a phrase
// that may hold spaces, and the tags and the marks the notes must have, each
// mark by its ID or its name. All are combined with AND.
type Query struct {
	Terms []string
	Tags  []string
	Marks []string
}

// tagPrefix starts a tag in a query, as in tag:todo, and markPrefix a mark, as
// in mark:red.
const (
	tagPrefix  = "tag:"
	markPrefix = "mark:"
)

// ParseQuery parses q. Terms are separated by spaces. A term in double
// quotes is a phrase, which may hold spaces and matches its words in that
// order; an unclosed quote runs to the end, so that a query being typed
// still searches. tag:name, or tag:"a name" for a tag with spaces, asks for
// notes with the tag; tag: alone is dropped, as while typing a tag. mark:red,
// or mark:"a name", asks for notes with the mark, in the same way.
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
		if prefix, list := prefixAt(rs, i, &res); prefix != "" {
			var value string
			p := len([]rune(prefix))
			next := i + p
			if next < len(rs) && rs[next] == '"' {
				value, next = quoted(next)
			} else {
				for next < len(rs) && !unicode.IsSpace(rs[next]) {
					next++
				}
				value = string(rs[i+p : next])
			}
			if value = strings.TrimSpace(value); value != "" {
				*list = append(*list, value)
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

// prefixAt returns the prefix of a tag or a mark that starts at i, ignoring
// case, and the list of res its value goes to; "" for neither.
func prefixAt(rs []rune, i int, res *Query) (string, *[]string) {
	for _, x := range []struct {
		prefix string
		list   *[]string
	}{{tagPrefix, &res.Tags}, {markPrefix, &res.Marks}} {
		if p := len([]rune(x.prefix)); i+p <= len(rs) && strings.EqualFold(string(rs[i:i+p]), x.prefix) {
			return x.prefix, x.list
		}
	}
	return "", nil
}

// tagKey is the key tags are compared by in a query: NFKC and lower case, so
// that full-width and half-width forms, and upper and lower case, match.
func tagKey(tag string) string { return strings.ToLower(norm.NFKC.String(tag)) }
