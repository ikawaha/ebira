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

// NoteBody returns the body of the note board b is saved as, with source as
// its source and result as what is given back, in the words of lang (en or
// ja): what is given back, the sticky notes, each under a heading with the
// sticky notes on its answer one level below, and the source, folded as it
// may be long.
func NoteBody(b *Board, source, result, lang string) string {
	var sb strings.Builder
	if r := strings.TrimSpace(result); r != "" {
		sb.WriteString(r)
		sb.WriteString("\n\n")
	}
	children := map[string][]*Sticky{}
	var roots []*Sticky
	for i := range b.Stickies {
		s := &b.Stickies[i]
		if s.Parent == "" || b.Find(s.Parent) == nil {
			roots = append(roots, s)
		} else {
			children[s.Parent] = append(children[s.Parent], s)
		}
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
	if len(roots) > 0 {
		fmt.Fprintf(&sb, "## %s\n", label(lang, "stickies"))
		var write func(s *Sticky, level int)
		write = func(s *Sticky, level int) {
			fmt.Fprintf(&sb, "\n%s %s\n\n", strings.Repeat("#", min(level, 6)), heading(s.Anchor.Exact))
			sb.WriteString(quote(s.Anchor.Exact))
			for _, m := range s.Messages {
				who := "question"
				if m.Role == Agent {
					who = "answer"
				} else if s.Kind == Memo {
					who = "memo"
				}
				who = "**" + label(lang, who) + "**"
				fmt.Fprintf(&sb, "\n%s %s\n", who, strings.TrimSpace(m.Text))
			}
			for _, c := range children[s.ID] {
				write(c, level+1)
			}
		}
		for _, s := range roots {
			write(s, 3)
		}
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
