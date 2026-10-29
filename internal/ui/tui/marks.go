package tui

import (
	"context"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// Marks of notes: a note has one at most for now, a color drawn as a colored
// dot or a symbol drawn as an emoji, which the list shows before the title.
// M chooses the mark of the note in the preview, as the web UI does; the
// names of the marks come from the server (GET /marks).

// markEmoji are the emoji of the marks that are symbols. They have the
// emoji presentation, so that terminals draw them two columns wide.
var markEmoji = map[string]string{"star": "⭐", "important": "❗", "question": "❓", "done": "✅", "pin": "📌", "urgent": "🔥", "idea": "💡", "wip": "🚧", "hold": "⏳", "dropped": "❌"}

// markColors are the colors of the marks that are colors, which read on dark
// and light backgrounds alike.
var markColors = map[string]color.Color{
	"red": lipgloss.Color("#e5484d"), "orange": lipgloss.Color("#f76b15"), "yellow": lipgloss.Color("#d6a21a"),
	"green": lipgloss.Color("#30a46c"), "blue": lipgloss.Color("#3e63dd"), "purple": lipgloss.Color("#8e4ec6"),
	"gray": lipgloss.Color("#8b8d98"),
}

// markBadge returns mark id as drawn, and its width; "" for an unknown mark.
func markBadge(id string) (string, int) {
	if e, ok := markEmoji[id]; ok {
		return e, 2
	}
	if c, ok := markColors[id]; ok {
		return lipgloss.NewStyle().Foreground(c).Render("●"), 1
	}
	return "", 0
}

// noteMarkBadge returns the badge of the first known mark of marks after a
// space, and its width; "" for none.
func noteMarkBadge(marks []string) (string, int) {
	return styledMarkBadge(marks, lipgloss.NewStyle())
}

// styledMarkBadge is noteMarkBadge drawn in base, such as on the background
// of a selected row, ahead of the space before the title.
func styledMarkBadge(marks []string, base lipgloss.Style) (string, int) {
	for _, id := range marks {
		if e, ok := markEmoji[id]; ok {
			return base.Render(" " + e), 3
		}
		if c, ok := markColors[id]; ok {
			return base.Render(" ") + base.Foreground(c).Render("●"), 2
		}
	}
	return "", 0
}

// markName returns what mark id is called: the name the user gave it, else
// what it looks like.
func (m Model) markName(id string) string {
	for _, mk := range m.marks {
		if mk.ID == id && mk.Name != nil && *mk.Name != "" {
			return *mk.Name
		}
	}
	return m.tr.t("mark." + id)
}

// markChoices are the choices of promptMark: the marks, then none ("").
func (m Model) markChoices() []string {
	var ids []string
	for _, mk := range m.marks {
		if _, w := markBadge(mk.ID); w > 0 {
			ids = append(ids, mk.ID)
		}
	}
	return append(ids, "")
}

// startMark opens promptMark for the note in the preview, at its mark.
func (m *Model) startMark() {
	if m.note == nil || m.note.Notebook == trashNotebook || len(m.marks) == 0 {
		return
	}
	m.markSel = 0
	if len(m.note.Marks) > 0 {
		m.markSel = max(0, slices.Index(m.markChoices(), m.note.Marks[0]))
	}
	m.prompt = promptMark
}

// handleMarkKey handles a key at promptMark: ← and → choose, enter sets it.
func (m *Model) handleMarkKey(key string) tea.Cmd {
	choices := m.markChoices()
	switch key {
	case "left", "h", "shift+tab":
		m.markSel = (m.markSel + len(choices) - 1) % len(choices)
	case "right", "l", "tab":
		m.markSel = (m.markSel + 1) % len(choices)
	case "enter":
		m.prompt = promptNone
		if m.note == nil {
			return nil
		}
		marks := []string{}
		if id := choices[m.markSel]; id != "" {
			marks = []string{id}
		}
		return m.setMarks(m.note.ID, marks)
	case "esc":
		m.prompt = promptNone
	}
	return nil
}

// markedMsg reports setting the marks of note id.
type markedMsg struct {
	id  string
	err error
}

func (m *Model) setMarks(id string, marks []string) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		_, err := api.SetMarks(ctx, &gennotes.SetMarksPayload{ID: id, Marks: marks})
		return markedMsg{id: id, err: err}
	}
}

// renderMarkPrompt renders promptMark: the marks in a row, the chosen one
// framed, and its name.
func (m Model) renderMarkPrompt() string {
	choices := m.markChoices()
	var b strings.Builder
	b.WriteString(m.th.style(m.th.accent).Render(" " + m.tr.t("prompt.mark") + " "))
	for i, id := range choices {
		badge := "✕"
		if id != "" {
			badge, _ = markBadge(id)
		}
		if i == m.markSel {
			b.WriteString(m.th.style(m.th.accent).Render("[") + badge + m.th.style(m.th.accent).Render("]"))
		} else {
			b.WriteString(" " + badge + " ")
		}
	}
	name := m.tr.t("mark.none")
	if id := choices[min(m.markSel, len(choices)-1)]; id != "" {
		name = m.markName(id)
	}
	b.WriteString(" " + m.th.style(m.th.fg).Render(name))
	return b.String() + m.keys([][2]string{{"←→", "key.choose"}, {"enter", "key.ok"}, {"esc", "key.cancel"}})
}

// loadMarks reads the marks with their names; none from a server without
// them.
func loadMarks(ctx context.Context, api *gennotes.Client) []*gennotes.MarkInfo {
	marks, err := api.Marks(ctx, &gennotes.MarksPayload{})
	if err != nil {
		return nil
	}
	return marks
}
