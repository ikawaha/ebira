package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/text/unicode/norm"
)

// Find in the note: f (ctrl+f while editing in the source mode) opens a find
// field under the panes, whose text is marked in the body of the note in the
// document pane, as the find bar of the web UI does. The search of the notes
// is not changed. The text is compared after NFKC and lower case, as in the
// web UI. enter and down move to the next match, up to the previous one, and
// esc closes the field and takes the marks away. In the source mode the
// cursor goes to the match, and stays there after esc.

// openFind opens the find field on the note of the document pane.
func (m *Model) openFind() tea.Cmd {
	if m.note == nil || m.answerOn || m.edit != nil {
		return nil
	}
	m.focus = focusPreview
	m.prompt = promptFind
	m.findIn.SetValue(m.findQ)
	m.findIn.CursorEnd()
	m.layout()
	return m.findIn.Focus()
}

// closeFind closes the find field and takes the marks away.
func (m *Model) closeFind() {
	m.prompt = promptNone
	m.findIn.Blur()
	m.findQ, m.findIdx, m.findLines, m.findPos = "", 0, nil, nil
	m.paintFind()
}

// handleFindKey handles a key of the find field.
func (m *Model) handleFindKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.closeFind()
		return nil
	case "enter", "down", "ctrl+n":
		m.stepFind(+1)
		return nil
	case "shift+enter", "up", "ctrl+p":
		m.stepFind(-1)
		return nil
	}
	var cmd tea.Cmd
	m.findIn, cmd = m.findIn.Update(msg)
	if q := m.findIn.Value(); q != m.findQ {
		m.findQ, m.findIdx = q, 0
		m.paintFind()
		m.showFind()
	}
	return cmd
}

// findCount returns the number of matches of the find field.
func (m *Model) findCount() int {
	if m.showsSource() {
		return len(m.findPos)
	}
	return len(m.findLines)
}

// stepFind moves to the next match (+1) or the previous one (-1).
func (m *Model) stepFind(d int) {
	n := m.findCount()
	if n == 0 {
		return
	}
	m.findIdx = (m.findIdx + d + n) % n
	m.paintFind()
	m.showFind()
}

// paintFind marks the matches of the find field in the document pane. The
// editor of the source mode is marked when it is drawn (see findLinesOf).
func (m *Model) paintFind() {
	m.findPos, m.findLines = nil, nil
	if m.showsSource() {
		m.findPos = findInText(m.src.body.Value(), m.findQ)
	}
	if m.note != nil && !m.answerOn {
		body, lines := findANSI(m.pvBody, m.findQ, m.findOn(false), m.findOn(true), m.findIdx)
		if !m.showsSource() {
			m.findLines = lines
		}
		offset := m.vp.YOffset()
		m.vp.SetContent(m.pvHead + body)
		m.vp.SetYOffset(offset)
	}
	if n := m.findCount(); m.findIdx >= n {
		// The note changed and has fewer matches.
		m.findIdx = 0
		if n > 0 {
			m.paintFind()
		}
	}
}

// showFind brings the current match into view: it scrolls the rendered note,
// or moves the cursor of the editor to it.
func (m *Model) showFind() {
	if m.findIdx >= m.findCount() {
		return
	}
	if m.showsSource() {
		p := m.findPos[m.findIdx]
		m.src.moveCursor(p[0], p[1])
		return
	}
	line := m.pvHeadLines + m.findLines[m.findIdx]
	if top := m.vp.YOffset(); line < top || line >= top+m.vp.Height() {
		m.vp.SetYOffset(max(0, line-2))
	}
}

// findLinesOf marks the matches of the find field in the lines of the body of
// the editor of the source mode, as drawn.
func (m *Model) findLinesOf(lines []string) []string {
	if m.prompt != promptFind || m.findQ == "" {
		return lines
	}
	s, _ := findANSI(strings.Join(lines, "\n"), m.findQ, m.findOn(false), m.findOn(false), -1)
	return strings.Split(s, "\n")
}

// findOn returns the escape sequence that starts a match of the find field,
// or the current one.
func (m Model) findOn(current bool) string {
	st := lipgloss.NewStyle().Foreground(m.th.badgeFg).Background(m.th.ok)
	if current {
		st = lipgloss.NewStyle().Bold(true).Foreground(m.th.badgeFg).Background(m.th.accent)
	}
	s := st.Render("x")
	return s[:strings.Index(s, "x")]
}

// findStatus returns the place of the current match, such as 2/5.
func (m Model) findStatus() string {
	switch n := m.findCount(); {
	case m.findQ == "":
		return ""
	case n == 0:
		return m.tr.t("find.none")
	default:
		return m.tr.t("find.count", "i", m.findIdx+1, "n", n)
	}
}

// foldRunes folds rs for finding, as NFKC and lower case, and returns the
// index in rs of each folded rune.
func foldRunes(rs []rune) ([]rune, []int) {
	var (
		folded []rune
		origin []int
	)
	for i, r := range rs {
		for _, f := range strings.ToLower(norm.NFKC.String(string(r))) {
			folded = append(folded, f)
			origin = append(origin, i)
		}
	}
	return folded, origin
}

// findRunes returns the spans in rs, as [start, end) of runes, where q
// occurs after folding, without overlaps.
func findRunes(rs []rune, q string) [][2]int {
	fq, _ := foldRunes([]rune(q))
	if len(fq) == 0 {
		return nil
	}
	fs, origin := foldRunes(rs)
	var spans [][2]int
	for i := 0; i+len(fq) <= len(fs); {
		if !slices.Equal(fs[i:i+len(fq)], fq) {
			i++
			continue
		}
		spans = append(spans, [2]int{origin[i], origin[i+len(fq)-1] + 1})
		i += len(fq)
	}
	return spans
}

// findInText returns the line and the column, in runes, of each match of q in
// s.
func findInText(s, q string) [][2]int {
	var pos [][2]int
	for i, line := range strings.Split(s, "\n") {
		for _, sp := range findRunes([]rune(line), q) {
			pos = append(pos, [2]int{i, sp[0]})
		}
	}
	return pos
}

// findANSI marks the matches of q in s, which may contain ANSI escape
// sequences, line by line: the match number cur with curOn, the others with
// on. It returns the result and the line of each match.
func findANSI(s, q, on, curOn string, cur int) (string, []int) {
	if q == "" {
		return s, nil
	}
	var (
		sb    strings.Builder
		found []int
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
		spans := findRunes(plain, q)
		if len(spans) == 0 {
			for _, it := range items {
				if it.esc != "" {
					sgr = trackSGR(sgr, it.esc)
				}
			}
			sb.WriteString(line)
			continue
		}
		// style[j] starts rune j of a match, or is "" outside of them.
		style := make([]string, len(plain))
		for _, sp := range spans {
			st := on
			if len(found) == cur {
				st = curOn
			}
			found = append(found, i)
			for k := sp[0]; k < sp[1]; k++ {
				style[k] = st
			}
		}
		restore := func() { sb.WriteString("\x1b[0m" + strings.Join(sgr, "")) }
		in, j := "", 0
		for _, it := range items {
			if it.esc != "" {
				sb.WriteString(it.esc)
				if isSGR(it.esc) {
					sgr = trackSGR(sgr, it.esc)
					if in != "" {
						sb.WriteString(in) // the sequence may have reset the mark
					}
				}
				continue
			}
			if style[j] != in {
				if in != "" {
					restore()
				}
				if style[j] != "" {
					sb.WriteString(style[j])
				}
				in = style[j]
			}
			sb.WriteRune(it.r)
			j++
		}
		if in != "" {
			restore()
		}
	}
	return sb.String(), found
}
