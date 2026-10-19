package index

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/filter"
	"github.com/ikawaha/kagome/v2/filter/ja"
	"github.com/ikawaha/kagome/v2/tokenizer"
	"github.com/kljensen/snowball/english"
	"golang.org/x/text/unicode/norm"
)

// analyzerVersion is part of every analyzer fingerprint. Bump it when the
// token output changes so that existing full-text tables are rebuilt.
const analyzerVersion = "2"

// analyzer converts text into the space-separated tokens stored in the
// full-text table. The FTS5 unicode61 tokenizer then only splits on spaces.
type analyzer struct {
	// tokenize splits normalized text into tokens.
	tokenize func(normalized string) []token
	// fingerprint identifies the token output. The full-text table is rebuilt
	// when it changes.
	fingerprint string
}

// token is a token of normalized text, with its range in rune offsets.
type token struct {
	text       string
	start, end int
}

// analyze returns the tokens of s joined by spaces.
func (a *analyzer) analyze(s string) string {
	ts := a.tokenize(normalize(s))
	words := make([]string, len(ts))
	for i, t := range ts {
		words[i] = t.text
	}
	return strings.Join(words, " ")
}

func newAnalyzer(cfg Config) (*analyzer, error) {
	switch cfg.Mode {
	case ModeBigram:
		return &analyzer{
			tokenize:    bigrams,
			fingerprint: "bigram/" + analyzerVersion,
		}, nil
	case ModeTrigram:
		return &analyzer{
			tokenize:    trigrams,
			fingerprint: "trigram/" + analyzerVersion,
		}, nil
	case ModeEn:
		return &analyzer{
			tokenize:    enTokens,
			fingerprint: "en/" + analyzerVersion + "/porter2",
		}, nil
	case ModeJa:
		return newJaAnalyzer(cfg.StopWords, cfg.StopTags)
	}
	return nil, nil
}

// normalize applies NFKC and lower-casing, so that full-width and half-width
// forms, composed and decomposed kana, and letter case all match.
func normalize(s string) string {
	return strings.ToLower(norm.NFKC.String(s))
}

// normalizeMap normalizes s as normalize does, and also returns for each rune
// of the result the range of runes of s it came from, [from[i], to[i]).
func normalizeMap(s string) (rs []rune, from, to []int) {
	var it norm.Iter
	it.InitString(norm.NFKC, s)
	src, prev, pending := 0, 0, 0 // pending: first rune of rs without a range
	for !it.Done() {
		p := it.Pos()
		seg := it.Next()
		n := utf8.RuneCountInString(s[p:it.Pos()])
		for _, r := range string(seg) {
			// strings.ToLower also lower-cases rune by rune.
			rs = append(rs, unicode.ToLower(r))
		}
		// The iterator may return part of a long expansion, such as a
		// squared era-name ligature (U+337B) to two letters, before consuming
		// its source; such output belongs to the next
		// segment that consumes input.
		if n == 0 {
			continue
		}
		for ; pending < len(rs); pending++ {
			from = append(from, src)
			to = append(to, src+n)
		}
		prev, src = src, src+n
	}
	for ; pending < len(rs); pending++ {
		from = append(from, prev)
		to = append(to, src)
	}
	return rs, from, to
}

// bigrams returns the overlapping two-character grams of s. Grams containing
// spaces, punctuation or symbols are skipped. Searching for the bigrams of a
// term as a phrase matches the term as a substring.
func bigrams(s string) []token { return ngrams(s, 2) }

// trigrams returns the overlapping three-character grams of s, as bigrams
// does. A term of fewer than three characters has none.
func trigrams(s string) []token { return ngrams(s, 3) }

// ngrams returns the overlapping grams of n characters of s, skipping grams
// containing spaces, punctuation or symbols.
func ngrams(s string, n int) []token {
	rs := []rune(s)
	var ts []token
	run := 0 // word runes ending at i
	for i, r := range rs {
		if !isWordRune(r) {
			run = 0
			continue
		}
		run++
		if run >= n {
			ts = append(ts, token{text: string(rs[i+1-n : i+1]), start: i + 1 - n, end: i + 1})
		}
	}
	return ts
}

var (
	kagomeOnce sync.Once
	kagomeTok  *tokenizer.Tokenizer
	kagomeErr  error
)

func kagome() (*tokenizer.Tokenizer, error) {
	kagomeOnce.Do(func() {
		kagomeTok, kagomeErr = tokenizer.New(ipa.Dict(), tokenizer.OmitBosEos())
	})
	return kagomeTok, kagomeErr
}

// newJaAnalyzer returns an analyzer that splits Japanese text into morphemes
// with kagome, drops stop words and morphemes with stop tags, and reduces
// verbs and adjectives to their base forms. Nil stopWords or stopTags use kagome's
// defaults, which come from Lucene's Japanese analyzer.
func newJaAnalyzer(stopWords, stopTags []string) (*analyzer, error) {
	t, err := kagome()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	var opts []ja.FilterOption
	if stopWords != nil {
		words := make([]string, len(stopWords))
		for i, w := range stopWords {
			words[i] = normalize(w)
		}
		opts = append(opts, ja.StopWordsFilterOption(words))
		h.Write([]byte("words\x00" + strings.Join(words, "\x00") + "\x00"))
	}
	if stopTags != nil {
		opts = append(opts, ja.StopTagsFilterOption(parsePOS(stopTags)))
		h.Write([]byte("tags\x00" + strings.Join(stopTags, "\x00") + "\x00"))
	}
	f, err := ja.NewFilter(opts...)
	if err != nil {
		return nil, err
	}
	return &analyzer{
		tokenize: func(s string) []token {
			var ts []token
			for _, tok := range t.Analyze(s, tokenizer.Search) {
				// Yield drops a stop word or a stop tag and returns the base
				// form of a verb or an adjective.
				for _, w := range f.Yield([]tokenizer.Token{tok}) {
					if strings.IndexFunc(w, isWordRune) >= 0 {
						ts = append(ts, token{text: w, start: tok.Start, end: tok.End})
					}
				}
			}
			return ts
		},
		fingerprint: "ja/" + analyzerVersion + "/" + hex.EncodeToString(h.Sum(nil))[:16],
	}, nil
}

// parsePOS parses parts of speech written as the IPA dictionary names of
// their levels joined by "-" (up to four levels), padding the
// missing levels with "*". As in Lucene, a stop tag matches a token only when
// all four levels are equal; "*" is the dictionary's value for an unspecified
// level, not a wildcard.
func parsePOS(tags []string) []filter.POS {
	const levels = 4
	ps := make([]filter.POS, 0, len(tags))
	for _, tag := range tags {
		p := strings.Split(tag, "-")
		for len(p) < levels {
			p = append(p, "*")
		}
		ps = append(ps, p)
	}
	return ps
}

// LoadWordList reads a list with one entry per line, in the format of Lucene's
// stopwords.txt and stoptags.txt: lines starting with "#" and empty lines are
// ignored.
func LoadWordList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	list := []string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		list = append(list, line)
	}
	return list, s.Err()
}

// englishStopWords are the stop words of the en mode: those of Lucene's
// English analyzer. The longer list of Snowball drops words that matter in
// a search, such as not and only.
var englishStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true,
	"but": true, "by": true, "for": true, "if": true, "in": true, "into": true, "is": true,
	"it": true, "no": true, "not": true, "of": true, "on": true, "or": true, "such": true,
	"that": true, "the": true, "their": true, "then": true, "there": true, "these": true,
	"they": true, "this": true, "to": true, "was": true, "will": true, "with": true,
}

// enTokens returns the tokens of the en mode, as Lucene's CJK analyzer does
// with the stemming of its English analyzer: a run of letters and digits
// other than Chinese, Japanese and Korean is a word, reduced to its stem by
// the English stemmer of Snowball (Porter2), so that plans and planning both
// match plan; English stop words are dropped. A run of Chinese, Japanese or
// Korean characters is split into bigrams as in the bigram mode. An
// apostrophe between letters stays in the word, so that the stemmer drops a
// possessive 's.
func enTokens(s string) []token {
	rs := []rune(s)
	var ts []token
	for i := 0; i < len(rs); {
		switch {
		case isCJK(rs[i]):
			j := i
			for j < len(rs) && isCJK(rs[j]) {
				j++
			}
			for k := i; k+1 < j; k++ {
				ts = append(ts, token{text: string(rs[k : k+2]), start: k, end: k + 2})
			}
			i = j
		case isWordRune(rs[i]):
			j := i
			for j < len(rs) && (isWordRune(rs[j]) && !isCJK(rs[j]) || isApostrophe(rs[j]) && j > i && j+1 < len(rs) && unicode.IsLetter(rs[j+1]) && !isCJK(rs[j+1])) {
				j++
			}
			w := strings.ReplaceAll(string(rs[i:j]), "\u2019", "'")
			if !englishStopWords[w] {
				ts = append(ts, token{text: english.Stem(w, true), start: i, end: j})
			}
			i = j
		default:
			i++
		}
	}
	return ts
}

// isCJK reports whether r is a Chinese, Japanese or Korean character, which
// the en mode splits into bigrams instead of words.
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		r == '\u30fc' || r == '\u3005' // the prolonged sound mark and the iteration mark
}

func isApostrophe(r rune) bool { return r == '\'' || r == '\u2019' }

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Mn, r)
}
