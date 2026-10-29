package index

import (
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Query is a search query, parsed: the terms to find, each a word or a phrase
// that may hold spaces, and the tags and the marks the notes must have, each
// mark by its ID or its name; the same that the notes must not have; and the
// groups of which the notes must have one at least. All are combined with AND.
type Query struct {
	Terms []string
	Tags  []string
	Marks []string

	NotTerms []string
	NotTags  []string
	NotMarks []string

	Or [][]Alt
}

// Alt is one of a group joined with OR: a term, a tag or a mark, by Kind.
type Alt struct {
	Kind  string
	Value string
}

// The kinds of an Alt.
const (
	AltTerm = "term"
	AltTag  = "tag"
	AltMark = "mark"
)

// empty reports whether q asks for nothing.
func (q Query) empty() bool {
	return len(q.Terms)+len(q.Tags)+len(q.Marks)+len(q.NotTerms)+len(q.NotTags)+len(q.NotMarks)+len(q.Or) == 0
}

// found returns the terms whose matches are found: those to find and those in
// the groups.
func (q Query) found() []string {
	terms := slices.Clone(q.Terms)
	for _, g := range q.Or {
		for _, a := range g {
			if a.Kind == AltTerm {
				terms = append(terms, a.Value)
			}
		}
	}
	return terms
}

// tagPrefix starts a tag in a query, as in tag:todo, and markPrefix a mark, as
// in mark:red; orWord joins two of them, as in pizza OR pasta.
const (
	tagPrefix  = "tag:"
	markPrefix = "mark:"
	orWord     = "OR"
)

// ParseQuery parses q. Terms are separated by spaces. A term in double
// quotes is a phrase, which may hold spaces and matches its words in that
// order; an unclosed quote runs to the end, so that a query being typed
// still searches. tag:name, or tag:"a name" for a tag with spaces, asks for
// notes with the tag; tag: alone is dropped, as while typing a tag. mark:red,
// or mark:"a name", asks for notes with the mark, in the same way. A minus
// before any of them, as in -word, -"a phrase", -tag:name and -mark:red, asks
// for notes without it; a minus alone is dropped, as while typing one. OR in
// capitals between two of them, as in pizza OR pasta and tag:a OR tag:b, asks
// for notes with either, binding closer than the AND of spaces; an OR next to
// one with a minus, at an end or after another OR is dropped.
func ParseQuery(q string) Query {
	type item struct {
		Alt
		not bool
	}
	var items []item
	var or []bool // whether an OR comes before items[i]
	pendingOr := false
	add := func(kind, value string, not bool) {
		if value = strings.TrimSpace(value); value == "" {
			return
		}
		items = append(items, item{Alt{kind, value}, not})
		or = append(or, pendingOr)
		pendingOr = false
	}
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
		not := false
		if rs[i] == '-' {
			if i+1 == len(rs) || unicode.IsSpace(rs[i+1]) {
				i++
				continue
			}
			not = true
			i++
		}
		if rs[i] == '"' {
			s, next := quoted(i)
			add(AltTerm, s, not)
			i = next
			continue
		}
		if prefix, kind := prefixAt(rs, i); prefix != "" {
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
			add(kind, value, not)
			i = next
			continue
		}
		j := i
		for j < len(rs) && !unicode.IsSpace(rs[j]) {
			j++
		}
		if w := string(rs[i:j]); w == orWord && !not {
			pendingOr = len(items) > 0
		} else {
			add(AltTerm, w, not)
		}
		i = j
	}

	var res Query
	for i := 0; i < len(items); {
		it := items[i]
		j := i + 1
		for !it.not && j < len(items) && or[j] && !items[j].not {
			j++
		}
		if j-i > 1 {
			group := make([]Alt, 0, j-i)
			for _, x := range items[i:j] {
				group = append(group, x.Alt)
			}
			res.Or = append(res.Or, group)
			i = j
			continue
		}
		lists := map[string]*[]string{AltTerm: &res.Terms, AltTag: &res.Tags, AltMark: &res.Marks}
		if it.not {
			lists = map[string]*[]string{AltTerm: &res.NotTerms, AltTag: &res.NotTags, AltMark: &res.NotMarks}
		}
		*lists[it.Kind] = append(*lists[it.Kind], it.Value)
		i = j
	}
	return res
}

// prefixAt returns the prefix of a tag or a mark that starts at i, ignoring
// case, and the kind of its value; "" for neither.
func prefixAt(rs []rune, i int) (string, string) {
	for _, x := range []struct{ prefix, kind string }{{tagPrefix, AltTag}, {markPrefix, AltMark}} {
		if p := len([]rune(x.prefix)); i+p <= len(rs) && strings.EqualFold(string(rs[i:i+p]), x.prefix) {
			return x.prefix, x.kind
		}
	}
	return "", ""
}

// tagKey is the key tags are compared by in a query: NFKC and lower case, so
// that full-width and half-width forms, and upper and lower case, match.
func tagKey(tag string) string { return strings.ToLower(norm.NFKC.String(tag)) }
