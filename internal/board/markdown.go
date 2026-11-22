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
		sb.WriteString(r)
		sb.WriteString("\n\n")
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
		fmt.Fprintf(sb, "\n%s %s\n\n", strings.Repeat("#", min(level, 6)), heading(s.Anchor.Exact))
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

// Appendix returns what closing board b adds at the end of the note it was
// opened from, in the words of lang: under a heading of the day, the next
// points (result), the sticky notes chosen to give back (stickies), and a
// link to the minutes, if they were kept.
func Appendix(b *Board, source, result string, stickies []string, lang, day string, minutes *NoteRef) string {
	chosen := make(map[string]bool, len(stickies))
	for _, id := range stickies {
		chosen[id] = true
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n", strings.ReplaceAll(label(lang, "discussed"), "{date}", day))
	if r := strings.TrimSpace(result); r != "" {
		fmt.Fprintf(&sb, "\n### %s\n\n%s\n", label(lang, "next_points"), r)
	}
	var st strings.Builder
	if writeStickies(&st, b, source, lang, 3, func(s *Sticky) bool { return chosen[s.ID] }) {
		sb.WriteString("\n")
		sb.WriteString(st.String())
	}
	if minutes != nil {
		fmt.Fprintf(&sb, "\n%s: %s\n", label(lang, "minutes"), minutes.link())
	}
	return sb.String()
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
