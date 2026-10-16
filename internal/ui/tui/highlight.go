package tui

import (
	"slices"
	"strings"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// highlightWords returns the distinct texts of s in spans, longest first.
// Texts that span lines are dropped, since rendered text is matched line by
// line.
func highlightWords(s string, spans []*gennotes.TextRange) []string {
	rs := []rune(s)
	var words []string
	for _, sp := range spans {
		if sp.Start < 0 || sp.End > len(rs) || sp.Start >= sp.End {
			continue
		}
		w := string(rs[sp.Start:sp.End])
		if !strings.Contains(w, "\n") && !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	slices.SortStableFunc(words, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })
	return words
}

// highlightANSI highlights every occurrence of words in s, which may contain
// ANSI escape sequences, by starting each match with the escape sequence on.
// After a match the style in effect is restored. It returns the result and
// the indexes of the lines that have a match.
func highlightANSI(s string, words []string, on string) (string, []int) {
	if len(words) == 0 {
		return s, nil
	}
	var (
		sb    strings.Builder
		lines []int
		sgr   []string // SGR sequences since the last reset
	)
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			sb.WriteByte('\n')
		}
		items := splitANSI(line)
		var plain []rune
		for _, it := range items {
			if it.esc == "" {
				plain = append(plain, it.r)
			}
		}
		mark := markWords(plain, words)
		if !slices.Contains(mark, true) {
			for _, it := range items {
				if it.esc != "" {
					sgr = trackSGR(sgr, it.esc)
				}
			}
			sb.WriteString(line)
			continue
		}
		lines = append(lines, i)
		restore := func() { sb.WriteString("\x1b[0m" + strings.Join(sgr, "")) }
		in, j := false, 0
		for _, it := range items {
			if it.esc != "" {
				sb.WriteString(it.esc)
				if isSGR(it.esc) {
					sgr = trackSGR(sgr, it.esc)
					if in {
						sb.WriteString(on) // the sequence may have reset the highlight
					}
				}
				continue
			}
			if mark[j] && !in {
				sb.WriteString(on)
				in = true
			} else if !mark[j] && in {
				restore()
				in = false
			}
			sb.WriteRune(it.r)
			j++
		}
		if in {
			restore()
		}
	}
	return sb.String(), lines
}

// ansiItem is an escape sequence or a printed rune.
type ansiItem struct {
	esc string
	r   rune
}

// splitANSI splits s into escape sequences (CSI, OSC and two-byte ones) and
// runes.
func splitANSI(s string) []ansiItem {
	var items []ansiItem
	for i := 0; i < len(s); {
		if s[i] != 0x1b || i+1 >= len(s) {
			r := []rune(s[i:])[0]
			items = append(items, ansiItem{r: r})
			i += len(string(r))
			continue
		}
		j := i + 2
		switch s[i+1] {
		case '[': // CSI: parameters up to a final byte in 0x40-0x7e
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			j = min(j+1, len(s))
		case ']': // OSC: up to BEL or ST (ESC \)
			for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
				j++
			}
			if j < len(s) && s[j] == 0x1b {
				j++
			}
			j = min(j+1, len(s))
		}
		items = append(items, ansiItem{esc: s[i:j]})
		i = j
	}
	return items
}

func isSGR(esc string) bool { return strings.HasPrefix(esc, "\x1b[") && strings.HasSuffix(esc, "m") }

func trackSGR(sgr []string, esc string) []string {
	if !isSGR(esc) {
		return sgr
	}
	if esc == "\x1b[0m" || esc == "\x1b[m" {
		return sgr[:0]
	}
	return append(sgr, esc)
}

// markWords reports for each rune of plain whether it is in an occurrence of
// any word.
func markWords(plain []rune, words []string) []bool {
	mark := make([]bool, len(plain))
	for _, w := range words {
		wr := []rune(w)
		for i := 0; i+len(wr) <= len(plain); i++ {
			if slices.Equal(plain[i:i+len(wr)], wr) {
				for k := i; k < i+len(wr); k++ {
					mark[k] = true
				}
			}
		}
	}
	return mark
}
