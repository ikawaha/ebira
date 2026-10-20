package tui

import (
	"fmt"
	"image/color"
	"net/url"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// theme holds the colors of the UI.
type theme struct {
	dark                                                   bool
	accent, fg, dim, border, selBg, ok, warn, err, badgeFg color.Color
}

var (
	darkTheme = theme{
		dark:   true,
		accent: lipgloss.Color("#7aa2f7"), fg: lipgloss.Color("#c0caf5"), dim: lipgloss.Color("#737aa2"),
		border: lipgloss.Color("#3b4261"), selBg: lipgloss.Color("#283457"),
		ok: lipgloss.Color("#9ece6a"), warn: lipgloss.Color("#e0af68"), err: lipgloss.Color("#f7768e"),
		badgeFg: lipgloss.Color("#1a1b26"),
	}
	lightTheme = theme{
		accent: lipgloss.Color("#2e7de9"), fg: lipgloss.Color("#3760bf"), dim: lipgloss.Color("#848cb5"),
		border: lipgloss.Color("#a8aecb"), selBg: lipgloss.Color("#c4c8da"),
		ok: lipgloss.Color("#587539"), warn: lipgloss.Color("#8c6c3e"), err: lipgloss.Color("#f52a65"),
		badgeFg: lipgloss.Color("#e1e2e7"),
	}
)

func (t theme) style(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// markdown renders note bodies, caching a renderer per width.
type markdown struct {
	style string
	width int
	r     *glamour.TermRenderer
}

func newMarkdown(style string) *markdown { return &markdown{style: style} }

func (md *markdown) render(s string, width int) string {
	if md.r == nil || md.width != width {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(md.style), glamour.WithWordWrap(width))
		if err != nil {
			return s
		}
		md.r, md.width = r, width
	}
	out, err := md.r.Render(s)
	if err != nil {
		return s
	}
	return strings.Trim(out, "\n")
}

// localImage matches a reference to an image of a note in a body: a path
// relative to the note file, <id>.assets/<name>.
var localImage = regexp.MustCompile(`!\[[^\]]*\]\(<?([a-z0-9][a-z0-9_-]{0,63})\.assets/([^)\s>]+)>?\)`)

// showImages replaces the images of notes in a body, which a terminal cannot
// show, with their names. Glamour would print the relative path as a link.
func showImages(body string) string {
	return localImage.ReplaceAllStringFunc(body, func(ref string) string {
		name := localImage.FindStringSubmatch(ref)[2]
		if u, err := url.PathUnescape(name); err == nil {
			name = u
		}
		return "[image: " + name + "]"
	})
}

const (
	itemHeight    = 2   // lines per note in the list
	narrowWidth   = 70  // below this width only one pane is shown
	wideWidth     = 110 // from this width the notebook pane is shown
	notebookWidth = 22
)

type layout struct {
	paneTop, paneH                       int
	showNotebooks, showList, showPreview bool
	notebookW                            int
	listX, listW                         int
	previewX, previewW                   int
}

func (m *Model) computeLayout() layout {
	l := layout{paneTop: 1, paneH: max(3, m.height-2)}
	if m.width < narrowWidth || m.zoom && (m.focus == focusPreview || m.focus == focusEdit) {
		switch m.focus {
		case focusPreview, focusEdit:
			l.showPreview, l.previewW = true, m.width
		case focusNotebooks:
			l.showNotebooks, l.notebookW = true, m.width
		default:
			l.showList, l.listW = true, m.width
		}
		return l
	}
	l.showList, l.showPreview = true, true
	if m.width >= wideWidth {
		l.showNotebooks, l.notebookW, l.listX = true, notebookWidth, notebookWidth
	}
	l.listW = max(28, min(48, m.width/3))
	l.previewX = l.listX + l.listW
	l.previewW = m.width - l.previewX
	return l
}

// layout resizes the preview after a size or focus change.
func (m *Model) layout() {
	l := m.computeLayout()
	m.vp.SetWidth(max(1, l.previewW-2))
	m.vp.SetHeight(max(1, l.paneH-2))
	m.input.SetWidth(max(10, m.width/3))
	if m.edit != nil {
		m.edit.resize(max(1, l.previewW-2), max(1, l.paneH-2))
	}
	m.scrollList()
	m.renderPreview(true)
}

func (m *Model) visibleItems() int {
	return max(1, (m.computeLayout().paneH-2)/itemHeight)
}

// scrollList keeps the selected item visible.
func (m *Model) scrollList() {
	n := m.visibleItems()
	if m.sel < m.top {
		m.top = m.sel
	}
	if m.sel >= m.top+n {
		m.top = m.sel - n + 1
	}
	m.top = max(0, min(m.top, max(0, len(m.items)-n)))
}

// renderPreview renders the current note into the viewport. keepOffset keeps
// the scroll position, for re-renders of the same note.
func (m *Model) renderPreview(keepOffset bool) {
	width := m.vp.Width()
	if m.answerOn && m.answer != nil {
		m.renderAnswer(width, keepOffset)
		return
	}
	if m.note == nil {
		m.rendered, m.matches, m.matchIdx = "", nil, -1
		m.vp.SetContent(m.th.style(m.th.dim).Render("\n  " + m.tr.t("preview.none")))
		return
	}
	key := fmt.Sprintf("%s/%d/%s", m.note.Version, width, m.noteQ)
	if key == m.rendered {
		return
	}
	m.rendered = key
	var b strings.Builder
	on := m.highlightOn()
	title, _ := highlightANSI(lipgloss.NewStyle().Bold(true).Foreground(m.th.fg).Render(m.note.Title), highlightWords(m.note.Title, m.note.TitleHighlights), on)
	b.WriteString("\n  " + title + "\n")
	nb := m.notebookLabel(m.note.Notebook)
	if m.note.TrashedNotebook != nil {
		nb += " / " + strings.TrimPrefix(m.notebookName(trashedPrefix+*m.note.TrashedNotebook), "↳ ")
	}
	if m.note.TrashedFrom != nil {
		nb += " " + m.tr.t("preview.from", "notebook", m.notebookLabel(*m.note.TrashedFrom))
	}
	meta := []string{nb, formatTime(m.note.UpdatedAt), m.note.ID}
	if len(m.note.Tags) > 0 {
		meta = append([]string{tagList(m.note.Tags)}, meta...)
	}
	if createdByAI(m.note.CreatedBy) {
		meta = append(meta, aiMark+" AI")
	}
	b.WriteString("  " + m.th.style(m.th.dim).Render(strings.Join(meta, " · ")) + "\n")
	b.WriteString("  " + m.th.style(m.th.border).Render(strings.Repeat("─", max(0, width-4))) + "\n")
	header := strings.Count(b.String(), "\n")
	body, lines := highlightANSI(m.md.render(showImages(m.note.Body), max(20, width-2)), highlightWords(m.note.Body, m.note.BodyHighlights), on)
	b.WriteString(body)
	m.matches = m.matches[:0]
	for _, l := range lines {
		m.matches = append(m.matches, header+l)
	}
	offset := m.vp.YOffset()
	m.vp.SetContent(b.String())
	switch {
	case keepOffset:
		m.vp.SetYOffset(offset)
	case len(m.matches) > 0:
		// Show the first match of a new note or query.
		m.matchIdx = 0
		m.showMatch()
		return
	default:
		m.vp.GotoTop()
	}
	m.matchIdx = -1
}

// renderAnswer renders the last question and its answer into the viewport.
func (m *Model) renderAnswer(width int, keepOffset bool) {
	a := m.answer
	m.rendered, m.matches, m.matchIdx = "", nil, -1 // render the note again when the answer is closed
	// block indents and wraps text by the width of the preview.
	block := lipgloss.NewStyle().PaddingLeft(2).Width(max(10, width-2))
	var b strings.Builder
	q := lipgloss.NewStyle().Bold(true).Foreground(m.th.fg).Width(max(10, width-7)).Render(a.question)
	b.WriteString("\n" + lipgloss.JoinHorizontal(lipgloss.Top, "  "+m.th.style(m.th.accent).Bold(true).Render("Q. "), q) + "\n")
	b.WriteString("  " + m.th.style(m.th.border).Render(strings.Repeat("─", max(0, width-4))) + "\n")
	switch {
	case a.pending:
		b.WriteString("\n  " + m.th.style(m.th.dim).Render(m.tr.t("answer.thinking")))
	case a.err != nil:
		b.WriteString("\n" + block.Foreground(m.th.err).Render(errorMessage(a.err)))
	default:
		b.WriteString(m.md.render(a.text, max(20, width-2)))
	}
	offset := m.vp.YOffset()
	m.vp.SetContent(b.String())
	if keepOffset {
		m.vp.SetYOffset(offset)
	} else {
		m.vp.GotoTop()
	}
}

// showMatch scrolls the preview to the current match, unless it is visible.
func (m *Model) showMatch() {
	line := m.matches[m.matchIdx]
	if top := m.vp.YOffset(); line < top || line >= top+m.vp.Height() {
		m.vp.SetYOffset(max(0, line-2))
	}
}

// highlightOn returns the escape sequence that starts a highlighted match.
func (m Model) highlightOn() string {
	s := lipgloss.NewStyle().Foreground(m.th.badgeFg).Background(m.th.warn).Render("x")
	return s[:strings.Index(s, "x")]
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "ebira"
	return v
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	l := m.computeLayout()
	var panes []string
	if l.showNotebooks {
		panes = append(panes, m.renderNotebooks(l.notebookW, l.paneH))
	}
	if l.showList {
		panes = append(panes, m.renderList(l.listW, l.paneH))
	}
	if l.showPreview && m.edit != nil {
		title := m.tr.t("pane.newNote")
		if m.edit.id != "" {
			title = m.tr.t("pane.editing", "id", m.edit.id)
		}
		if m.edit.dirty() {
			title += "  " + m.tr.t("pane.modified")
		}
		if m.edit.outside {
			title += "  " + m.tr.t("pane.changedOutside")
		}
		panes = append(panes, m.box(title, m.edit.view(m.th, l.previewW-2), l.previewW, l.paneH, true))
	} else if l.showPreview {
		title := m.tr.t("pane.preview")
		switch {
		case m.answerOn && m.answer != nil && m.answer.pending:
			title = m.tr.t("pane.asking")
		case m.answerOn && m.answer != nil:
			title = m.tr.t("pane.answer")
		case m.note != nil:
			title = m.note.Title
		}
		lines := strings.Split(m.vp.View(), "\n")
		if pct := m.vp.ScrollPercent(); m.vp.TotalLineCount() > m.vp.Height() {
			title = fmt.Sprintf("%s  %d%%", title, int(pct*100))
		}
		panes = append(panes, m.box(title, lines, l.previewW, l.paneH, m.focus == focusPreview))
	}
	return strings.Join([]string{
		m.renderHeader(),
		lipgloss.JoinHorizontal(lipgloss.Top, panes...),
		m.renderFooter(),
	}, "\n")
}

func (m Model) renderHeader() string {
	badge := lipgloss.NewStyle().Bold(true).Foreground(m.th.badgeFg).Background(m.th.accent).Padding(0, 1).Render("ebira")
	var search string
	switch {
	case m.focus == focusSearch:
		search = m.th.style(m.th.accent).Render(" / ") + m.input.View()
	case m.query != "":
		search = m.th.style(m.th.dim).Render(" / ") + m.th.style(m.th.fg).Render(m.query) + m.th.style(m.th.dim).Render("  "+m.tr.t("header.clear"))
	default:
		search = m.th.style(m.th.dim).Render(" / " + m.tr.t("header.search"))
	}

	var right []string
	if m.query != "" {
		right = append(right, m.th.style(m.th.dim).Render(m.tr.t("header.hits", "n", m.total, "mode", m.mode)))
	} else {
		right = append(right, m.th.style(m.th.dim).Render(m.tr.t("header.notes", "n", m.total)))
	}
	if m.live {
		right = append(right, m.th.style(m.th.ok).Render(m.tr.t("header.live")))
	} else {
		right = append(right, m.th.style(m.th.err).Render(m.tr.t("header.offline")))
	}
	r := strings.Join(right, "  ") + " "
	left := badge + search
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(r)
	if gap < 1 {
		return ansi.Truncate(left, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + r
}

func (m Model) renderList(w, h int) string {
	innerW := w - 2
	var lines []string
	if len(m.items) == 0 {
		msg := m.tr.t("list.empty")
		if m.query != "" {
			msg = m.tr.t("list.noMatches")
		}
		if _, ok := trashedID(m.notebook); ok {
			msg = m.tr.t("list.trashedNotebook")
		}
		lines = append(lines, "", " "+m.th.style(m.th.dim).Render(msg))
	}
	n := (h - 2) / itemHeight
	for i := m.top; i < len(m.items) && i < m.top+n; i++ {
		it := m.items[i]
		sub := formatTime(it.UpdatedAt)
		if len(it.Tags) > 0 {
			sub = tagList(it.Tags) + " · " + sub
		}
		var subHL []*gennotes.TextRange
		if it.Snippet != nil && *it.Snippet != "" {
			sub, subHL = *it.Snippet, it.SnippetHighlights
		}
		if m.acrossNotebooks() {
			prefix := m.notebookLabel(it.Notebook) + " · "
			sub, subHL = prefix+sub, shiftRanges(subHL, prefix)
		}
		title, titleHL := " "+it.Title, shiftRanges(it.TitleHighlights, " ")
		sub, subHL = "   "+sub, shiftRanges(subHL, "   ")
		titleSt, subSt, lead := lipgloss.NewStyle().Foreground(m.th.fg), m.th.style(m.th.dim), " "
		if i == m.sel {
			lead = m.th.style(m.th.accent).Background(m.th.selBg).Render("▌")
			titleSt, subSt = titleSt.Background(m.th.selBg).Bold(true), subSt.Background(m.th.selBg)
		}
		hl := lipgloss.NewStyle().Foreground(m.th.badgeFg).Background(m.th.warn)
		// The mark of a note created by AI goes at the end of the title line,
		// apart from the title, so that a long title is cut instead.
		titleLine := padHighlight(title, titleHL, titleSt, hl.Bold(true), innerW-1)
		if createdByAI(it.CreatedBy) {
			mark := " " + aiMark + " "
			titleLine = padHighlight(title, titleHL, titleSt, hl.Bold(true), innerW-1-ansi.StringWidth(mark)) + titleSt.Render(mark)
		}
		lines = append(lines,
			lead+titleLine,
			lead+padHighlight(sub, subHL, subSt, hl, innerW-1),
		)
	}
	title := m.tr.t("pane.notes", "sort", m.tr.t("sort."+m.currentSort()))
	if m.query != "" {
		title = m.tr.t("pane.search")
	}
	if !m.computeLayout().showNotebooks {
		title += " · " + m.notebookLabel(m.notebook)
	}
	if len(m.items) > 0 {
		title = fmt.Sprintf("%s  %d/%d", title, m.sel+1, len(m.items))
	}
	return m.box(title, lines, w, h, m.focus == focusList || m.focus == focusSearch)
}

func (m Model) renderNotebooks(w, h int) string {
	innerW := w - 2
	var lines []string
	rows := m.notebookRows()
	for _, r := range rows[min(m.notebookTop(h-2), len(rows)):] {
		// The icon has a column of its own before the name, kept even without
		// an icon, so that the names line up. The arrow of a notebook in the
		// trash goes before it, one column to the right of the trash.
		icon, name := kindIcon(r.kind), m.notebookName(r.id)
		if t, ok := tagOf(r.id); ok {
			icon, name = tagPrefix, t
		}
		if r.id == trashNotebook {
			icon = trashIcon
		}
		nest := ""
		if _, ok := trashedID(r.id); ok {
			nest, name = "↳ ", strings.TrimPrefix(name, "↳ ")
		}
		label := " " + nest + pad(icon, iconWidth) + " " + name
		count := fmt.Sprintf(" %d ", r.count) // the space keeps a long name apart
		line := pad(label, innerW-1-ansi.StringWidth(count)) + count
		if r.id == m.notebook {
			mark := m.th.style(m.th.accent).Background(m.th.selBg).Render("▌")
			lines = append(lines, mark+lipgloss.NewStyle().Background(m.th.selBg).Bold(true).Foreground(m.th.fg).Render(line))
			continue
		}
		st := lipgloss.NewStyle().Foreground(m.th.fg)
		if r.id == trashNotebook {
			st = m.th.style(m.th.dim)
		}
		lines = append(lines, " "+st.Render(line))
	}
	title := m.tr.t("pane.notebooks")
	if m.sideTags {
		title = m.tr.t("pane.tags")
	}
	return m.box(title, lines, w, h, m.focus == focusNotebooks)
}

// kindIcon is the icon of a notebook kind: people in silhouette for the
// notebooks people share with AI (AI clients may write their notes), an inbox
// tray for the drop ones (AI clients may only add notes), a lock for the
// private ones (hidden from AI clients), and none for the default kind.
func kindIcon(kind string) string {
	switch kind {
	case "collab":
		return "👥"
	case "drop":
		return "📥"
	case "private":
		return "🔒"
	}
	return ""
}

// trashIcon is the icon of the trash, which has no kind: the litter in its
// bin, an emoji drawn as one by default and so two cells wide in every
// terminal, as the icons of the kinds. The wastebasket is drawn as text by
// default, and terminals disagree on its width even with the variation
// selector for the emoji presentation.
const trashIcon = "\U0001F6AE"

// iconWidth is the width of the column of the icons in the notebook column:
// the icons of the kinds are wide characters.
const iconWidth = 2

// aiMark is the mark of a note created by an AI client, shown at the end of
// its line in the list and of the preview's meta line: a four pointed star,
// the sparkles that apps put on what AI made, as a narrow character that
// keeps the columns aligned in every terminal and takes the color of the text.
const aiMark = "\u2726"

func createdByAI(v *string) bool { return v != nil && *v == "ai" }

// notebookName is the display name of a notebook filter.
func (m Model) notebookName(id string) string {
	if id == "" {
		return m.tr.t("notebook.all")
	}
	if t, ok := tagOf(id); ok {
		return "# " + t
	}
	if tid, ok := trashedID(id); ok {
		for _, nb := range m.trashed {
			if nb.ID == tid {
				return "↳ " + nb.Name
			}
		}
		return "↳ " + tid
	}
	for _, nb := range m.notebooks {
		if nb.ID == id {
			return nb.Name
		}
	}
	return id
}

// notebookLabel is the display name of a notebook filter followed by the icon
// of its kind.
func (m Model) notebookLabel(id string) string {
	if tid, ok := trashedID(id); ok {
		for _, nb := range m.trashed {
			if nb.ID == tid {
				if icon := kindIcon(nb.Kind); icon != "" {
					return m.notebookName(id) + " " + icon
				}
			}
		}
	}
	for _, nb := range m.notebooks {
		if nb.ID == id {
			if icon := kindIcon(nb.Kind); icon != "" {
				return nb.Name + " " + icon
			}
		}
	}
	return m.notebookName(id)
}

func (m Model) renderFooter() string {
	switch m.prompt {
	case promptDelete:
		t := ""
		if m.toDelete != nil {
			t = m.toDelete.Title
		}
		if m.toDelete != nil && m.toDelete.Notebook == trashNotebook {
			return m.th.style(m.th.err).Render(" " + m.tr.t("prompt.deletePermanently", "title", t))
		}
		return m.th.style(m.th.warn).Render(" " + m.tr.t("prompt.trash", "title", t))
	case promptConflict:
		return m.th.style(m.th.warn).Render(" "+m.tr.t("prompt.conflict")) +
			m.keys([][2]string{{"o", "key.overwrite"}, {"e", "key.editAgain"}, {"d", "key.discardMine"}})
	case promptRename, promptAddNotebook, promptAsk:
		label := "prompt.name"
		switch m.prompt {
		case promptAddNotebook:
			label = "prompt.newNotebook"
			if m.addKind == "collab" {
				label = "prompt.newAINotebook"
			}
		case promptAsk:
			label = "prompt.ask"
		}
		return m.th.style(m.th.accent).Render(" "+m.tr.t(label)+" ") + m.promptIn.View() + m.keys([][2]string{{"enter", "key.ok"}, {"esc", "key.cancel"}})
	case promptNotebookKind:
		name := ""
		if nb := m.selectedNotebook(); nb != nil {
			name = nb.Name
		}
		return m.th.style(m.th.accent).Render(" "+m.tr.t("prompt.kind", "name", name)) +
			m.keys([][2]string{{"d", "kind.default"}, {"c", "kind.collab"}, {"w", "kind.drop"}, {"p", "kind.private"}, {"esc", "key.cancel"}})
	case promptUnprivate:
		return m.th.style(m.th.warn).Render(" " + m.tr.t("prompt.unprivate"))
	case promptMovePrivate:
		return m.th.style(m.th.warn).Render(" " + m.tr.t("prompt.movePrivate"))
	case promptTrashNotebook:
		row := m.selectedRow()
		return m.th.style(m.th.warn).Render(" " + m.tr.t("prompt.trashNotebook", "name", m.notebookName(row.id), "n", row.count))
	case promptPurgeNotebook:
		row := m.selectedRow()
		return m.th.style(m.th.err).Render(" " + m.tr.t("prompt.purgeNotebook", "name", strings.TrimPrefix(m.notebookName(row.id), "↳ "), "n", row.count))
	case promptEmptyTrash:
		notes, notebooks := m.trashContents()
		return m.th.style(m.th.err).Render(" " + m.tr.t("prompt.emptyTrash", "n", notes, "m", notebooks))
	case promptDiscard:
		return m.th.style(m.th.warn).Render(" " + m.tr.t("prompt.discard"))
	case promptRetry:
		keys := m.keys([][2]string{{"e", "key.editAgain"}, {"d", "key.discard"}})
		reason := " " + m.tr.t("prompt.notSaved", "reason", m.failure)
		return m.th.style(m.th.warn).Render(ansi.Truncate(reason, max(10, m.width-ansi.StringWidth(keys)-1), "…")) + keys
	}
	var hints [][2]string
	switch m.focus {
	case focusSearch:
		hints = [][2]string{{"enter", "key.done"}, {"esc", "key.clear"}, {"↑↓", "key.move"}}
	case focusEdit:
		hints = [][2]string{{"ctrl+s", "key.save"}, {"esc", "key.cancel"}, {"tab/shift+tab", "key.field"}, {"ctrl+z/y", "key.undo"}}
	case focusPreview:
		hints = [][2]string{{"j/k", "key.scroll"}, {"space/b", "key.page"}, {"esc", "key.back"}, {"e", "key.edit"}, {"E", "key.editor"}}
		if m.answerOn {
			hints = [][2]string{{"j/k", "key.scroll"}, {"space/b", "key.page"}, {"esc", "key.closeAnswer"}, {"?", "key.askAgain"}}
		}
		switch {
		case m.zoom:
			hints = append(hints, [2]string{"z", "key.unzoom"})
		case m.width >= narrowWidth:
			hints = append(hints, [2]string{"z", "key.zoom"})
		}
		if len(m.matches) > 0 {
			hints = append(hints, [2]string{"ctrl+n/p", "key.match"})
		}
		hints = append(hints, [2]string{"q", "key.quit"})
	case focusNotebooks:
		hints = [][2]string{{"j/k", "key.notebook"}, {"enter", "key.notes"}, {"e", "key.rename"}, {"J/K", "key.reorder"}, {"a", "key.add"}, {"A", "key.addAI"}, {"t", "key.kind"}, {"d", "key.delete"}, {"T", "key.tags"}, {"n", "key.newNote"}, {"q", "key.quit"}}
		if m.toMove != nil {
			hints = [][2]string{{"j/k", "key.notebook"}, {"enter", "key.moveHere"}, {"esc", "key.cancel"}}
			break
		}
		if m.selectedRow().id == trashNotebook && !m.sideTags {
			hints = [][2]string{{"j/k", "key.notebook"}, {"enter", "key.notes"}, {"e", "key.rename"}, {"d", "key.emptyTrash"}, {"T", "key.tags"}, {"n", "key.newNote"}, {"q", "key.quit"}}
		}
		if _, ok := trashedID(m.selectedRow().id); ok && !m.sideTags {
			hints = [][2]string{{"j/k", "key.notebook"}, {"enter", "key.notes"}, {"u", "key.restore"}, {"d", "key.deletePermanently"}, {"T", "key.tags"}, {"q", "key.quit"}}
		}
		if m.sideTags {
			hints = [][2]string{{"j/k", "key.tag"}, {"enter", "key.notes"}, {"T", "key.notebooks"}, {"n", "key.newNote"}, {"q", "key.quit"}}
		}
	default:
		hints = [][2]string{{"j/k", "key.move"}, {"/", "key.search"}, {"?", "key.ask"}, {"enter", "key.open"}, {"e", "key.edit"}, {"n", "key.new"}, {"d", "key.delete"}, {"s", "key.sort"}}
		if m.inTrashedNotebook() {
			// The notes of a notebook in the trash are read only.
			hints = [][2]string{{"j/k", "key.move"}, {"/", "key.search"}, {"?", "key.ask"}, {"enter", "key.open"}, {"d", "key.deletePermanently"}, {"h", "key.notebooks"}}
		}
		if m.canReorder() {
			hints = append(hints, [2]string{"J/K", "key.reorder"})
		}
		if m.answerOn {
			hints = append(hints, [2]string{"esc", "key.closeAnswer"})
		}
		if len(m.matches) > 0 {
			hints = append(hints, [2]string{"ctrl+n/p", "key.match"})
		}
		if m.note != nil && m.note.Notebook == trashNotebook {
			hints = append(hints, [2]string{"u", "key.restore"}, [2]string{"m", "key.moveTo"})
		}
		if !m.computeLayout().showNotebooks {
			if m.sideTags {
				hints = append(hints, [2]string{"[ ]", "key.tag"}, [2]string{"T", "key.notebooks"})
			} else {
				hints = append(hints, [2]string{"[ ]", "key.notebook"}, [2]string{"T", "key.tags"})
			}
		}
		hints = append(hints, [2]string{"q", "key.quit"})
	}
	left := m.keys(hints)
	if m.status == "" {
		return left
	}
	c := m.th.dim
	switch m.statusK {
	case statusWarn:
		c = m.th.warn
	case statusError:
		c = m.th.err
	}
	status := m.th.style(c).Render(m.status) + " "
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(status)
	if gap < 2 {
		return ansi.Truncate(" "+status, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + status
}

func (m Model) keys(hints [][2]string) string {
	var b strings.Builder
	for _, h := range hints {
		b.WriteString(" " + lipgloss.NewStyle().Bold(true).Foreground(m.th.accent).Render(h[0]) + " " + m.th.style(m.th.dim).Render(m.tr.t(h[1])) + " ")
	}
	return b.String()
}

// box draws a rounded border with a title around lines, herdr style.
func (m Model) box(title string, lines []string, w, h int, focused bool) string {
	bc := m.th.style(m.th.border)
	tc := m.th.style(m.th.dim)
	if focused {
		bc = m.th.style(m.th.accent)
		tc = lipgloss.NewStyle().Bold(true).Foreground(m.th.accent)
	}
	innerW := max(0, w-2)
	t := " " + ansi.Truncate(title, max(0, innerW-4), "…") + " "
	rest := max(0, innerW-1-ansi.StringWidth(t))
	out := make([]string, 0, h)
	out = append(out, bc.Render("╭─")+tc.Render(t)+bc.Render(strings.Repeat("─", rest)+"╮"))
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, bc.Render("│")+pad(line, innerW)+bc.Render("│"))
	}
	out = append(out, bc.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return strings.Join(out, "\n")
}

// pad truncates or pads s to exactly w cells.
func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// padHighlight renders s in st, with the runes in spans in hl, truncated or
// padded to width w.
func padHighlight(s string, spans []*gennotes.TextRange, st, hl lipgloss.Style, w int) string {
	if len(spans) == 0 {
		return st.Render(pad(s, w))
	}
	rs := []rune(s)
	if ansi.StringWidth(s) > w {
		width := 0
		for i, r := range rs {
			width += ansi.StringWidth(string(r))
			if width > w-1 {
				rs = append(rs[:i:i], '…')
				break
			}
		}
	}
	var sb strings.Builder
	at := func(i int) bool {
		for _, sp := range spans {
			if sp.Start <= i && i < sp.End {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(rs); {
		j, in := i+1, at(i)
		for j < len(rs) && at(j) == in {
			j++
		}
		if in {
			sb.WriteString(hl.Render(string(rs[i:j])))
		} else {
			sb.WriteString(st.Render(string(rs[i:j])))
		}
		i = j
	}
	if gap := w - ansi.StringWidth(string(rs)); gap > 0 {
		sb.WriteString(st.Render(strings.Repeat(" ", gap)))
	}
	return sb.String()
}

// shiftRanges moves rs past prefix, which is put in front of the text.
func shiftRanges(rs []*gennotes.TextRange, prefix string) []*gennotes.TextRange {
	n := len([]rune(prefix))
	res := make([]*gennotes.TextRange, len(rs))
	for i, r := range rs {
		res[i] = &gennotes.TextRange{Start: r.Start + n, End: r.End + n}
	}
	return res
}

func tagList(tags []string) string {
	return "#" + strings.Join(tags, " #")
}

func formatTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}
