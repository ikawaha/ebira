package board

import (
	"strings"
	"testing"
	"time"
)

func TestLocate(t *testing.T) {
	text := "a cat and a cat and a dog"
	for _, c := range []struct {
		a    Anchor
		want int
	}{
		{Anchor{Exact: "cat"}, 2},
		{Anchor{Exact: "cat", Prefix: "and a "}, 12},
		{Anchor{Exact: "cat", Suffix: " and a dog"}, 12},
		{Anchor{Exact: "cow"}, -1},
		{Anchor{Exact: "犬"}, -1},
	} {
		if got := Locate(text, c.a); got != c.want {
			t.Errorf("Locate(%+v) = %d, want %d", c.a, got, c.want)
		}
	}
	// Multibyte text.
	if got := Locate("猫と猫と犬", Anchor{Exact: "猫", Prefix: "猫と"}); got != len("猫と") {
		t.Errorf("Locate in Japanese = %d", got)
	}
}

func TestQuestion(t *testing.T) {
	now := time.Now()
	source := strings.Repeat("x", 1000) + "the passage" + strings.Repeat("y", 1000)
	b := &Board{Title: "PR", Source: Source{Format: Diff}, Status: Open}
	st, err := b.Add(Sticky{Kind: Assistant, Anchor: Anchor{Exact: "passage"}, Messages: []Message{{Text: "first?"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Say(Agent, "An answer about indexes.", now); err != nil {
		t.Fatal(err)
	}
	if err := st.Say(User, "second?", now); err != nil {
		t.Fatal(err)
	}
	q := Question(b, source, st)
	for _, want := range []string{`"PR"`, "the <<<passage>>>", "format diff", "Person: first?", "You: An answer about indexes.", "The person asks: second?", "not instructions"} {
		if !strings.Contains(q, want) {
			t.Errorf("question lacks %q:\n%s", want, q)
		}
	}
	// Around characters before the passage: "the " and the x before it.
	if strings.Contains(q, strings.Repeat("x", Around-3)) || !strings.Contains(q, "…"+strings.Repeat("x", Around-4)+"the <<<") {
		t.Errorf("the text before the passage is not clipped to %d characters", Around)
	}

	// A sticky note on the answer of another quotes that answer.
	child, err := b.Add(Sticky{Parent: st.ID, Kind: Assistant, Anchor: Anchor{Exact: "indexes"}, Messages: []Message{{Text: "which?"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	q = Question(b, source, child)
	if !strings.Contains(q, "An answer about <<<indexes>>>.") || !strings.Contains(q, "earlier answer") || strings.Contains(q, "yyy") {
		t.Errorf("question on an answer:\n%s", q)
	}

	// The question is bounded.
	long := &Board{Title: "t", Status: Open}
	ls, _ := long.Add(Sticky{Kind: Assistant, Anchor: Anchor{Exact: "x"}}, now)
	for range MaxMessages {
		ls.Messages = append(ls.Messages, Message{Role: User, Text: strings.Repeat("あ", 3000)})
	}
	if q := Question(long, "x", ls); len(q) > MaxQuestion {
		t.Errorf("question of %d bytes", len(q))
	}
}

func TestDraftQuestion(t *testing.T) {
	now := time.Now()
	b := &Board{Title: "t", Status: Open}
	a, _ := b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "kept"}, Messages: []Message{{Text: "give back"}}}, now)
	b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "skipped"}, Messages: []Message{{Text: "not this"}}}, now)
	q := DraftQuestion(b, []string{a.ID})
	if !strings.Contains(q, "kept") || !strings.Contains(q, "give back") || strings.Contains(q, "not this") {
		t.Errorf("draft question:\n%s", q)
	}
}

func TestNoteBody(t *testing.T) {
	now := time.Now()
	b := &Board{Title: "t", Status: Open, Source: Source{Format: Code, Lang: "go"}}
	q, _ := b.Add(Sticky{Kind: Assistant, Anchor: Anchor{Exact: "func main() {\n}"}, Messages: []Message{{Text: "what?"}}}, now)
	q.Say(Agent, "The entry point.", now)
	b.Add(Sticky{Parent: q.ID, Kind: Memo, Anchor: Anchor{Exact: "entry"}, Messages: []Message{{Text: "see the spec"}}}, now)
	b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "*bold* [link]"}}, now)

	got := NoteBody(b, "func main() {\n```\n}\n", "  Given back.  ", "en")
	for _, want := range []string{
		"Given back.\n\n## Sticky notes\n",
		"### \u201cfunc main() { }\u201d\n\n> func main() {\n> }\n",
		"**Q** what?\n", "**A** The entry point.\n",
		"#### \u201centry\u201d\n", "**Memo** see the spec\n",
		"### \u201c\\*bold\\* \\[link\\]\u201d",
		"<summary>code (3 lines)</summary>",
		"````go\nfunc main() {\n```\n}\n````\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("note lacks %q:\n%s", want, got)
		}
	}
	// The child comes after its parent, before the next sticky note.
	if strings.Index(got, "entry\u201d") > strings.Index(got, "bold") {
		t.Errorf("child out of place:\n%s", got)
	}

	md := NoteBody(&Board{Source: Source{Format: Markdown}}, "# Plan\n", "", "fr")
	if strings.Contains(md, "Sticky notes") || !strings.Contains(md, "\n# Plan\n") || strings.Contains(md, "```") {
		t.Errorf("Markdown source without sticky notes:\n%s", md)
	}
}

func TestNoteBodyJapanese(t *testing.T) {
	b := &Board{Status: Open, Source: Source{Format: Text}}
	q, _ := b.Add(Sticky{Kind: Assistant, Anchor: Anchor{Exact: "語"}, Messages: []Message{{Text: "何？"}}}, time.Now())
	q.Say(Agent, "答え", time.Now())
	got := NoteBody(b, "語\n", "", "ja")
	for _, want := range []string{"## 付箋\n", "**問** 何？", "**答** 答え", "## 本筋\n", "<summary>text (1 行)</summary>"} {
		if !strings.Contains(got, want) {
			t.Errorf("note lacks %q:\n%s", want, got)
		}
	}
}

// TestNoteBodyQuotes checks that the passages are quoted as text, and that
// the sticky notes follow the order of their passages.
func TestNoteBodyQuotes(t *testing.T) {
	now := time.Now()
	b := &Board{Status: Open, Source: Source{Format: Diff}}
	b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "later"}}, now)
	b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "fall\n+\t// back *now*\n1. one"}}, now)
	b.Add(Sticky{Kind: Memo, Anchor: Anchor{Exact: "nowhere"}}, now)
	got := NoteBody(b, "short terms fall\n+\t// back *now*\n1. one\nlater\n", "", "en")
	if want := "> fall\n> \\+\t// back \\*now\\*\n> \\1. one\n"; !strings.Contains(got, want) {
		t.Errorf("quote, want %q in:\n%s", want, got)
	}
	f, l, n := strings.Index(got, "\u201cfall"), strings.Index(got, "\u201clater"), strings.Index(got, "\u201cnowhere")
	if f < 0 || l < f || n < l {
		t.Errorf("order of the sticky notes, want fall, later, nowhere:\n%s", got)
	}
}
