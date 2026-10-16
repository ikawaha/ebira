package index

import (
	"strings"
	"testing"
)

// mark encloses the spans of s in brackets.
func mark(s string, spans []Span) string {
	rs := []rune(s)
	var sb strings.Builder
	prev := 0
	for _, sp := range spans {
		sb.WriteString(string(rs[prev:sp.Start]) + "[" + string(rs[sp.Start:sp.End]) + "]")
		prev = sp.End
	}
	sb.WriteString(string(rs[prev:]))
	return sb.String()
}

func TestNormalizeMap(t *testing.T) {
	for _, s := range []string{"", "ＡＢＣ abc", "ｶﾞｷﾞ", "が", "㍻ ① Ⅻ", "İstanbul"} {
		rs, from, to := normalizeMap(s)
		if got, want := string(rs), normalize(s); got != want {
			t.Errorf("normalizeMap(%q) = %q, want %q", s, got, want)
		}
		n := len([]rune(s))
		for i := range rs {
			if from[i] < 0 || from[i] >= to[i] || to[i] > n {
				t.Errorf("normalizeMap(%q): bad range [%d, %d) for rune %d", s, from[i], to[i], i)
			}
		}
	}
}

func TestHighlight(t *testing.T) {
	tests := []struct {
		mode Mode
		q    string
		s    string
		want string
	}{
		{ModeLike, "goroutine", "Go の GoRoutine です", "Go の [GoRoutine] です"},
		{ModeLike, "ｶﾞ", "ガイド", "[ガ]イド"},
		{ModeBigram, "京都", "東京都と京都", "東[京都]と[京都]"},
		// Full-width text matches a half-width term, and the span covers the original characters.
		{ModeBigram, "abc", "ＡＢＣとabc", "[ＡＢＣ]と[abc]"},
		// Bigrams skip symbols, as the full-text table does.
		// A character that expands under NFKC is highlighted as a whole.
		{ModeBigram, "平成", "㍻元年", "[㍻]元年"},
		{ModeBigram, "ab-cd", "ab cd abcd", "[ab cd] abcd"},
		{ModeBigram, "雨 天気", "天気は雨", "[天気]は[雨]"},
		{ModeBigram, "雨", "雨雨", "[雨雨]"},
		// A base form matches inflected forms.
		{ModeJa, "書く", "書いた手紙を書きます", "[書い]た手紙を[書き]ます"},
		{ModeJa, "京都", "東京都と京都", "東京都と[京都]"},
		// A phrase covers the stop words between its tokens.
		{ModeJa, "東京の天気", "東京の天気と大阪の天気", "[東京の天気]と大阪の天気"},
		// A term made only of stop words matches as a substring.
		{ModeJa, "の", "東京の天気", "東京[の]天気"},
		{ModeJa, "ＧＯ", "Go と go", "[Go] と [go]"},
	}
	for _, tt := range tests {
		an, err := newAnalyzer(Config{Mode: tt.mode})
		if err != nil {
			t.Fatal(err)
		}
		h := newHighlighter(an, strings.Fields(tt.q))
		if got := mark(tt.s, h.find(tt.s)); got != tt.want {
			t.Errorf("%s: find(%q) with %q = %q, want %q", tt.mode, tt.s, tt.q, got, tt.want)
		}
	}
}

func TestSnippet(t *testing.T) {
	an, err := newAnalyzer(Config{Mode: ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	h := newHighlighter(an, []string{"京都"})
	long := strings.Repeat("あ", 50)
	tests := []struct {
		body string
		want string
	}{
		{"京都\n\n  に行く", "[京都] に行く"},
		{"  先頭の空白と\t京都  ", "先頭の空白と [京都]"},
		{long + "\n京都 " + long + long, "…" + strings.Repeat("あ", 39) + " [京都] " + strings.Repeat("あ", 77) + "…"},
		{"no match", "no match"},
	}
	for _, tt := range tests {
		s, spans := snippet(tt.body, h.findInBody(tt.body, true))
		if got := mark(s, spans); got != tt.want {
			t.Errorf("snippet(%q) = %q, want %q", tt.body, got, tt.want)
		}
	}
}
