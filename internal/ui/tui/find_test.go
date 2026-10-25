package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestFindRunes(t *testing.T) {
	tests := []struct {
		s, q string
		want [][2]int
	}{
		{"Go and GO and go", "go", [][2]int{{0, 2}, {7, 9}, {14, 16}}},
		{"ＡＢＣとabc", "abc", [][2]int{{0, 3}, {4, 7}}}, // full width folds to half width
		{"検索と検索", "検索", [][2]int{{0, 2}, {3, 5}}},
		{"aaaa", "aa", [][2]int{{0, 2}, {2, 4}}}, // no overlaps
		{"㍿の話", "株式会社", [][2]int{{0, 1}}},        // one rune folding into several
		{"text", "", nil},
		{"text", "none", nil},
	}
	for _, tt := range tests {
		if got := findRunes([]rune(tt.s), tt.q); !slices.Equal(got, tt.want) {
			t.Errorf("findRunes(%q, %q) = %v, want %v", tt.s, tt.q, got, tt.want)
		}
	}
	if got := findInText("one\nTwo two\nthree", "two"); !slices.Equal(got, [][2]int{{1, 0}, {1, 4}}) {
		t.Errorf("findInText = %v", got)
	}
}

func TestFindANSI(t *testing.T) {
	s := "\x1b[1mtitle\x1b[0m\nfind me, Find me\nnothing"
	got, lines := findANSI(s, "find", "\x1b[7m", "\x1b[4m", 1)
	if !slices.Equal(lines, []int{1, 1}) {
		t.Errorf("lines: %v", lines)
	}
	if !strings.Contains(got, "\x1b[7mfind\x1b[0m me, \x1b[4mFind\x1b[0m me") {
		t.Errorf("marks: %q", got)
	}
	if ansi.Strip(got) != ansi.Strip(s) {
		t.Errorf("text changed: %q", ansi.Strip(got))
	}
}

func TestFindRendered(t *testing.T) {
	m := New(context.Background(), &Clients{}, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	m.items = []*gennotes.NoteSummary{{ID: "plan", Title: "Plan", Notebook: "inbox"}}
	m.note = &gennotes.Note{ID: "plan", Version: "v1", Title: "Plan", Body: "alpha beta\n\nAlpha gamma", Notebook: "inbox", UpdatedAt: "2026-10-25T10:00:00Z"}
	m.renderPreview(false)

	// f in the list opens the field on the document pane.
	m, _ = press(t, m, text("f"))
	if m.prompt != promptFind || m.focus != focusPreview {
		t.Fatalf("f: prompt %v, focus %v", m.prompt, m.focus)
	}
	m, _ = press(t, m, text("a"), text("l"), text("p"))
	if m.findQ != "alp" || m.findCount() != 2 || m.findIdx != 0 || !strings.Contains(m.renderFooter(), "1/2") {
		t.Fatalf("typed: %q, %d matches, footer %q", m.findQ, m.findCount(), ansi.Strip(m.renderFooter()))
	}
	if !strings.Contains(m.vp.View(), m.findOn(true)+"alp") {
		t.Error("the current match is not marked")
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.findIdx != 1 || !strings.Contains(m.vp.View(), m.findOn(true)+"Alp") {
		t.Errorf("enter: current %d", m.findIdx)
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.findIdx != 0 {
		t.Errorf("enter at the last: current %d", m.findIdx)
	}

	// esc closes it and takes the marks away; the search is not changed.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.prompt != promptNone || m.findQ != "" || strings.Contains(m.vp.View(), m.findOn(false)) || m.query != "" || m.focus != focusPreview {
		t.Errorf("esc: prompt %v, find %q, query %q, focus %v", m.prompt, m.findQ, m.query, m.focus)
	}
}

func TestFindSource(t *testing.T) {
	var kept []bool
	n := &gennotes.Note{ID: "plan", Version: "v1", Title: "Plan", Body: "one\ntwo words\nmore words", Notebook: "inbox", UpdatedAt: "2026-10-25T10:00:00Z"}
	m := sourceModel(t, n, &kept)
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	// f is a letter there, and ctrl+f finds.
	m, _ = press(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.prompt != promptFind || m.src.body.Value() != n.Body {
		t.Fatalf("ctrl+f: prompt %v, body %q", m.prompt, m.src.body.Value())
	}
	for _, r := range "WORDS" {
		m, _ = press(t, m, text(string(r)))
	}
	if m.findCount() != 2 || m.src.body.Line() != 1 || m.src.body.Column() != 4 {
		t.Fatalf("typed: %d matches, cursor %d:%d", m.findCount(), m.src.body.Line(), m.src.body.Column())
	}
	if v := m.render(); !strings.Contains(v, m.findOn(false)+"words") {
		t.Error("the matches are not marked in the editor")
	}
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.src.body.Line() != 2 || m.src.body.Column() != 5 {
		t.Errorf("down: cursor %d:%d", m.src.body.Line(), m.src.body.Column())
	}

	// esc closes it; the cursor stays at the match, ready to edit.
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape}, text("!"))
	if m.prompt != promptNone || m.src.body.Value() != "one\ntwo words\nmore !words" || m.src.dirty() != true {
		t.Errorf("esc: prompt %v, body %q", m.prompt, m.src.body.Value())
	}
}
