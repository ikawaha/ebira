package index

import (
	"slices"
	"strings"
	"unicode"
)

// Span is a range of text in rune (Unicode code point) offsets; End is
// exclusive.
type Span struct {
	Start, End int
}

// highlighter finds where the terms of a query occur in a text, matching them
// as the search does: a term with tokens matches the same token sequence as a
// phrase, and the other terms match as substrings of the normalized text.
type highlighter struct {
	an    *analyzer
	terms []hlTerm
}

type hlTerm struct {
	tokens []string // matched as a phrase when not empty
	like   []rune   // normalized term, matched as a substring otherwise
}

func newHighlighter(an *analyzer, terms []string) *highlighter {
	h := &highlighter{an: an}
	for _, t := range terms {
		var ht hlTerm
		if an != nil {
			for _, tok := range an.terms(t) {
				ht.tokens = append(ht.tokens, tok.text)
			}
		}
		if len(ht.tokens) == 0 {
			ht.like = []rune(normalize(t))
			if len(ht.like) == 0 {
				continue
			}
		}
		h.terms = append(h.terms, ht)
	}
	return h
}

// find returns the sorted, non-overlapping spans of s matched by any term.
func (h *highlighter) find(s string) []Span {
	if len(h.terms) == 0 || s == "" {
		return nil
	}
	rs, from, to := normalizeMap(s)
	var toks []token
	if h.an != nil {
		toks = h.an.tokenize(string(rs))
	}
	var spans []Span
	add := func(start, end int) { // normalized offsets to offsets of s
		spans = append(spans, Span{from[start], to[end-1]})
	}
	for _, t := range h.terms {
		if len(t.tokens) > 0 {
			for i := 0; i+len(t.tokens) <= len(toks); i++ {
				if phraseAt(toks[i:], t.tokens) {
					add(toks[i].start, toks[i+len(t.tokens)-1].end)
				}
			}
			continue
		}
		for i := 0; i+len(t.like) <= len(rs); i++ {
			if slices.Equal(rs[i:i+len(t.like)], t.like) {
				add(i, i+len(t.like))
			}
		}
	}
	return mergeSpans(spans)
}

func phraseAt(tokens []token, words []string) bool {
	for i, w := range words {
		if tokens[i].text != w {
			return false
		}
	}
	return true
}

// mergeSpans sorts spans and joins the overlapping or adjacent ones.
func mergeSpans(spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}
	slices.SortFunc(spans, func(a, b Span) int { return a.Start - b.Start })
	res := spans[:1]
	for _, sp := range spans[1:] {
		last := &res[len(res)-1]
		if sp.Start <= last.End {
			last.End = max(last.End, sp.End)
			continue
		}
		res = append(res, sp)
	}
	return res
}

// highlighters merges the matches of several highlighters, one for each
// full-text table that Search merges.
type highlighters []*highlighter

func (hs highlighters) find(s string) []Span {
	var spans []Span
	for _, h := range hs {
		spans = append(spans, h.find(s)...)
	}
	return mergeSpans(spans)
}

// findInBody is like highlighter.findInBody. With forSnippet, each highlighter
// covers the snippet around its own first match, which is not before the first
// match of all, so the merged spans cover the snippet around that one.
func (hs highlighters) findInBody(body string, forSnippet bool) []Span {
	var spans []Span
	for _, h := range hs {
		spans = append(spans, h.findInBody(body, forSnippet)...)
	}
	return mergeSpans(spans)
}

// snippetWidth is the number of runes of a snippet before its first match;
// twice as many follow.
const snippetWidth = 40

// findInBody returns the spans of body matched by any term, or with forSnippet
// only those up to the end of the snippet around the first match. It works
// line by line so that a long note is not analyzed beyond what the snippet
// needs; no term spans lines, since terms contain no spaces and tokens never
// include a newline.
func (h *highlighter) findInBody(body string, forSnippet bool) []Span {
	var spans []Span
	off := 0
	for line := range strings.SplitSeq(body, "\n") {
		if forSnippet && len(spans) > 0 && off >= spans[0].Start+snippetWidth*2 {
			break
		}
		for _, sp := range h.find(line) {
			spans = append(spans, Span{sp.Start + off, sp.End + off})
		}
		off += len([]rune(line)) + 1
	}
	return spans
}

// snippet extracts the text of body around the first span, with runs of
// white space collapsed into one space, and returns it with the spans moved
// into it.
func snippet(body string, spans []Span) (string, []Span) {
	rs := []rune(body)
	pos := 0
	if len(spans) > 0 {
		pos = spans[0].Start
	}
	start, end := max(0, pos-snippetWidth), min(len(rs), pos+snippetWidth*2)
	var out []rune
	if start > 0 {
		out = append(out, '…')
	}
	// at[i] is the offset in out of rs[start+i].
	at := make([]int, end-start+1)
	space := false
	for i := start; i < end; i++ {
		if unicode.IsSpace(rs[i]) {
			space = len(out) > 0 && (start == 0 || len(out) > 1)
			at[i-start] = len(out)
			continue
		}
		if space {
			out = append(out, ' ')
			space = false
		}
		at[i-start] = len(out)
		out = append(out, rs[i])
	}
	at[end-start] = len(out)
	if end < len(rs) {
		out = append(out, '…')
	}
	var moved []Span
	for _, sp := range spans {
		s, e := max(sp.Start, start), min(sp.End, end)
		if s >= e {
			continue
		}
		if sp := (Span{at[s-start], at[e-start]}); sp.Start < sp.End {
			moved = append(moved, sp)
		}
	}
	return string(out), moved
}
