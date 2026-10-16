package tui

import (
	"slices"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestHighlightWords(t *testing.T) {
	s := "降った\n降る 降った"
	spans := []*gennotes.TextRange{{Start: 0, End: 2}, {Start: 1, End: 5}, {Start: 4, End: 6}, {Start: 7, End: 9}, {Start: 8, End: 99}}
	got := highlightWords(s, spans)
	// "った\n降" spans lines and the duplicate "降っ" and the bad range are dropped.
	if want := []string{"降っ", "降る"}; !slices.Equal(got, want) {
		t.Errorf("highlightWords = %q, want %q", got, want)
	}
}

func TestHighlightANSI(t *testing.T) {
	const on = "<"
	tests := []struct {
		s     string
		words []string
		want  string
		lines []int
	}{
		{"plain text", []string{"text"}, "plain <text\x1b[0m", []int{0}},
		// The style in effect is restored after a match, and kept inside it.
		{"\x1b[1mbold \x1b[31mred\x1b[0m end", []string{"d r", "end"},
			"\x1b[1mbol<d \x1b[31m<r\x1b[0m\x1b[1m\x1b[31med\x1b[0m <end\x1b[0m", []int{0}},
		{"a\nb\na", []string{"a"}, "<a\x1b[0m\nb\n<a\x1b[0m", []int{0, 2}},
		// Longer words win; the shorter one is still marked elsewhere.
		{"東京都 京都", []string{"東京都", "京都"}, "<東京都\x1b[0m <京都\x1b[0m", []int{0}},
		{"no match", []string{"x"}, "no match", nil},
		{"\x1b]8;;http://x\x07link\x1b]8;;\x07", []string{"link"}, "\x1b]8;;http://x\x07<link\x1b]8;;\x07\x1b[0m", []int{0}},
	}
	for _, tt := range tests {
		got, lines := highlightANSI(tt.s, tt.words, on)
		if got != tt.want || !slices.Equal(lines, tt.lines) {
			t.Errorf("highlightANSI(%q, %q) = %q %v, want %q %v", tt.s, tt.words, got, lines, tt.want, tt.lines)
		}
	}
}
