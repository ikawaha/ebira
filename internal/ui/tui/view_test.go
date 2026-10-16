package tui

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestPadHighlight(t *testing.T) {
	st, hl := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true)
	tests := []struct {
		s     string
		spans []*gennotes.TextRange
		w     int
		want  string
	}{
		{"東京都の天気", []*gennotes.TextRange{{Start: 1, End: 3}}, 14, "東京都の天気  "},
		{"東京都の天気", []*gennotes.TextRange{{Start: 4, End: 6}}, 8, "東京都… "},
		{"abc", nil, 5, "abc  "},
	}
	for _, tt := range tests {
		got := padHighlight(tt.s, tt.spans, st, hl, tt.w)
		if plain := ansi.Strip(got); plain != tt.want {
			t.Errorf("padHighlight(%q, %d) = %q, want %q", tt.s, tt.w, plain, tt.want)
		}
	}
}

func TestShiftRanges(t *testing.T) {
	got := shiftRanges([]*gennotes.TextRange{{Start: 0, End: 2}}, "受信 · ")
	if got[0].Start != 5 || got[0].End != 7 {
		t.Errorf("shiftRanges = %+v, want {5 7}", *got[0])
	}
}
