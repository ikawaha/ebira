package board

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// labelsJSON holds the words of the notes boards are saved as, by language:
// en, the base, and ja.
//
//go:embed labels.json
var labelsJSON []byte

var labels = func() map[string]map[string]string {
	var m map[string]map[string]string
	if err := json.Unmarshal(labelsJSON, &m); err != nil {
		panic(err)
	}
	return m
}()

// label returns the word key in lang, or in English.
func label(lang, key string) string {
	if s, ok := labels[lang][key]; ok {
		return s
	}
	return labels["en"][key]
}

// Tag is the tag of the notes boards are saved as.
const Tag = "whiteboard"

// NoteRef names a note, for a link to it.
type NoteRef struct {
	ID, Title string
}

// link returns a Markdown link to n, which the web UI opens as a note.
func (n NoteRef) link() string {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = n.ID
	}
	return "[" + escapeInline(title) + "](/#note/" + n.ID + ")"
}

// NoteBody returns the body of the note the minutes of board b are kept in,
// with source as its source and result as the next points, in the words of
// lang (en or ja): a link to the note b was opened from, if from is given,
// the next points, the sticky notes, each under a heading with the sticky
// notes on its answer one level below, and the source, folded as it may be
// long.
func NoteBody(b *Board, source, result, lang string, from *NoteRef) string {
	var sb strings.Builder
	if from != nil {
		fmt.Fprintf(&sb, "%s: %s\n\n", label(lang, "from_note"), from.link())
	}
	if r := strings.TrimSpace(result); r != "" {
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", label(lang, "points"), r)
	}
	if writeStickies(&sb, b, source, lang, 2, nil) {
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "## %s\n\n", label(lang, "source"))
	lines := strings.Count(source, "\n")
	if !strings.HasSuffix(source, "\n") {
		lines++
	}
	n := strings.ReplaceAll(label(lang, "lines"), "{n}", strconv.Itoa(lines))
	fmt.Fprintf(&sb, "<details>\n<summary>%s (%s)</summary>\n\n", b.Source.Format, n)
	sb.WriteString(fence(source, b.Source))
	sb.WriteString("\n</details>\n")
	return sb.String()
}

// writeStickies writes the sticky notes of b under a heading of level, in
// the order of their passages in source, each under a heading one level
// below with the sticky notes on its answer below it. With keep, only the
// sticky notes it keeps are written: one whose parent is not kept goes with
// the others. It reports whether it wrote any.
func writeStickies(sb *strings.Builder, b *Board, source, lang string, level int, keep func(*Sticky) bool) bool {
	kept := func(s *Sticky) bool { return keep == nil || keep(s) }
	children := map[string][]*Sticky{}
	var roots []*Sticky
	for i := range b.Stickies {
		s := &b.Stickies[i]
		if !kept(s) {
			continue
		}
		if p := b.Find(s.Parent); s.Parent == "" || p == nil || !kept(p) {
			roots = append(roots, s)
		} else {
			children[s.Parent] = append(children[s.Parent], s)
		}
	}
	if len(roots) == 0 {
		return false
	}
	// In the order of the passages in the source, as on the board; those
	// whose passage is not found go last.
	at := map[string]int{}
	for _, s := range roots {
		at[s.ID] = Locate(source, s.Anchor)
		if at[s.ID] < 0 {
			at[s.ID] = len(source)
		}
	}
	slices.SortStableFunc(roots, func(x, y *Sticky) int { return at[x.ID] - at[y.ID] })
	fmt.Fprintf(sb, "%s %s\n", strings.Repeat("#", level), label(lang, "stickies"))
	var write func(s *Sticky, level int)
	write = func(s *Sticky, level int) {
		h := heading(s.Anchor.Exact)
		if s.Resolved {
			h += " " + label(lang, "resolved")
		}
		fmt.Fprintf(sb, "\n%s %s\n\n", strings.Repeat("#", min(level, 6)), h)
		sb.WriteString(quote(s.Anchor.Exact))
		for _, m := range s.Messages {
			who := "question"
			if m.Role == Agent {
				who = "answer"
			} else if s.Kind == Memo {
				who = "memo"
			}
			who = "**" + label(lang, who) + "**"
			fmt.Fprintf(sb, "\n%s %s\n", who, strings.TrimSpace(m.Text))
		}
		for _, c := range children[s.ID] {
			write(c, level+1)
		}
	}
	for _, s := range roots {
		write(s, level+1)
	}
	return true
}

// AddDiscussion returns body, the body of the note a board was opened from,
// with a line linking to the minutes of a discussion on it, of the day, in
// its section of discussions at the end (Appendix: discussions), made if it
// has none. A section in another language, or not at the end any more, takes
// the line too, after its last line.
func AddDiscussion(body, lang, day string, minutes NoteRef) string {
	line := "- " + day + " " + minutes.link()
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	code := inCode(lines)
	at := -1
	for i, l := range lines {
		if !code[i] && isDiscussions(l) {
			at = i
		}
	}
	if at < 0 {
		head := strings.TrimRight(body, "\n")
		if head != "" {
			head += "\n\n"
		}
		return head + "## " + label(lang, "discussions") + "\n\n" + line + "\n"
	}
	// The section runs to the next heading of its level or above.
	end := len(lines)
	for i := at + 1; i < len(lines); i++ {
		if m := headingLevel.FindString(lines[i]); m != "" && !code[i] && len(strings.TrimSpace(m)) <= 2 {
			end = i
			break
		}
	}
	last := end
	for last > at+1 && strings.TrimSpace(lines[last-1]) == "" {
		last--
	}
	add := []string{line}
	if last == at+1 {
		add = []string{"", line} // a blank line below the heading
	}
	if last < len(lines) && strings.TrimSpace(lines[last]) != "" {
		add = append(add, "") // and above the next heading
	}
	out := append(append(append([]string{}, lines[:last]...), add...), lines[last:]...)
	return strings.Join(out, "\n") + "\n"
}

// inCode tells which of lines are in a fenced code block, its fences
// included: a heading there is text, not a heading.
func inCode(lines []string) []bool {
	code := make([]bool, len(lines))
	open := ""
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if len(l)-len(t) > 3 {
			code[i] = open != ""
			continue
		}
		m := codeFence.FindString(t)
		switch {
		case open == "" && m != "":
			open, code[i] = m, true
		case open != "":
			code[i] = true
			if m != "" && m[0] == open[0] && len(m) >= len(open) && strings.TrimSpace(t[len(m):]) == "" {
				open = ""
			}
		}
	}
	return code
}

// codeFence matches the fence of a fenced code block.
var codeFence = regexp.MustCompile("^(```+|~~~+)")

// headingLevel matches the marks of an ATX heading.
var headingLevel = regexp.MustCompile(`^#{1,6}\s`)

// isDiscussions reports whether line is the heading of the section of
// discussions, in any language.
func isDiscussions(line string) bool {
	for _, words := range labels {
		if h, ok := words["discussions"]; ok && strings.TrimSpace(line) == "## "+h {
			return true
		}
	}
	return false
}

// AddBeforeDiscussions returns body with text added at the end of what the
// person wrote: before its section of discussions (Appendix: discussions)
// when that section is the last one, else at the end.
func AddBeforeDiscussions(body, text string) string {
	text = strings.Trim(text, "\n")
	if text == "" {
		return body
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	code := inCode(lines)
	at := -1
	for i, l := range lines {
		if !code[i] && isDiscussions(l) {
			at = i
		}
	}
	// Only a section that runs to the end: one followed by another of its
	// level, or above, is the person's own.
	if at >= 0 {
		for i := at + 1; i < len(lines); i++ {
			if m := headingLevel.FindString(lines[i]); m != "" && !code[i] && len(strings.TrimSpace(m)) <= 2 {
				at = -1
				break
			}
		}
	}
	if at < 0 {
		head := strings.TrimRight(body, "\n")
		if head != "" {
			head += "\n\n"
		}
		return head + text + "\n"
	}
	head := strings.TrimRight(strings.Join(lines[:at], "\n"), "\n")
	if head != "" {
		head += "\n\n"
	}
	return head + text + "\n\n" + strings.Join(lines[at:], "\n") + "\n"
}

// heading returns the heading of a sticky note on passage: its start, quoted.
func heading(passage string) string {
	return "“" + escapeInline(oneLine(passage, 40)) + "”"
}

// escapeInline keeps the characters of s that Markdown would read as
// markup in a heading from being read so.
func escapeInline(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", `\<`, "#", `\#`, "$", `\$`, "|", `\|`)
	return r.Replace(s)
}

// blockStart matches the start of a line that Markdown reads as a block,
// such as an item of a list or a quote, after its markup is escaped.
var blockStart = regexp.MustCompile(`^([-+=>~]|\d+[.)])`)

// quote returns passage as a block quote of its text: the passage is text,
// such as code or a diff, rather than Markdown, so its markup is escaped and
// its indents dropped.
func quote(passage string) string {
	var sb strings.Builder
	for _, l := range strings.Split(strings.TrimRight(passage, "\n"), "\n") {
		l = escapeInline(strings.TrimSpace(l))
		if blockStart.MatchString(l) {
			l = `\` + l
		}
		sb.WriteString("> ")
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return sb.String()
}

// fence returns source as it is shown in the note: Markdown as is, the
// others in a code block fenced longer than any fence inside.
func fence(source string, src Source) string {
	if src.Format == Markdown {
		return strings.TrimRight(source, "\n") + "\n"
	}
	lang := ""
	switch src.Format {
	case Diff:
		lang = "diff"
	case Code:
		lang = src.Lang
	}
	f := "```"
	for strings.Contains(source, f) {
		f += "`"
	}
	return f + lang + "\n" + strings.TrimRight(source, "\n") + "\n" + f + "\n"
}
