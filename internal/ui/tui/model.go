// Package tui is a terminal UI for the ebira API. It talks to the server only
// through the generated API client, like any other UI.
package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

type focus int

const (
	focusList focus = iota
	focusNotebooks
	focusPreview
	focusSearch
	focusEdit
)

type statusKind int

const (
	statusInfo statusKind = iota
	statusWarn
	statusError
)

type promptKind int

const (
	promptNone promptKind = iota
	promptDelete
	promptConflict
	promptRetry         // a draft could not be saved; edit it again or discard it
	promptDiscard       // leave the inline editor with unsaved changes
	promptRename        // display name of the selected notebook
	promptAddNotebook   // display name of a new notebook
	promptAsk           // question to the assistant
	promptNotebookKind  // kind of the selected notebook
	promptUnprivate     // confirmation to show a private notebook to AI
	promptTrashNotebook // confirmation to move the selected notebook into the trash
	promptPurgeNotebook // confirmation to delete the selected notebook in the trash permanently
)

const searchDebounce = 150 * time.Millisecond

// sortDebounce delays saving the sort of a notebook, so that quick presses of
// s save only the last one: saves sent at once may reach the server out of
// order.
const sortDebounce = 400 * time.Millisecond

// Model is the Bubble Tea model of the UI.
type Model struct {
	ctx context.Context
	api *gennotes.Client
	ask *genassistant.Client
	th  theme

	width, height int

	items   []*gennotes.NoteSummary
	sel     int // index of the selected item
	top     int // index of the first visible item
	total   int
	mode    string // search mode reported by the server
	query   string // applied search query
	listSeq int    // sequence number of the latest list request
	typeSeq int    // sequence number of the latest keystroke in the search box
	pending string // note ID to select after the next list load

	notebooks []*gennotes.Notebook
	trashed   []*gennotes.TrashedNotebook // notebooks in the trash
	tags      []*gennotes.TagCount        // tags with their note counts
	sideTags  bool                        // the notebook pane lists the tags instead of the notebooks (T)
	notebook  string                      // notebook filter; empty for all notebooks except the trash
	allSort   string                      // sort of all notes, kept only while running (a notebook keeps its own)
	sortSeq   int                         // sequence number of the latest sort change
	moving    string                      // ID of the note being moved, whose update event is ours
	nbSel     int                         // cursor in the notebook pane; 0 is "All"
	promptIn  textinput.Model
	addKind   string // kind of the notebook promptAddNotebook creates
	newKind   string // kind chosen at promptNotebookKind, waiting for promptUnprivate

	note     *gennotes.Note
	noteQ    string  // query the note was loaded with, whose matches it highlights
	matches  []int   // lines of the preview with a match
	matchIdx int     // index in matches of the current match, or -1
	answer   *answer // last question to the assistant
	answerOn bool    // the preview shows answer instead of the note
	askSeq   int     // sequence number of the latest question
	rendered string  // key of the rendered preview (version, width and query)
	md       *markdown
	vp       viewport.Model
	input    textinput.Model
	focus    focus
	zoom     bool // the preview, and the editor opened from it, take the whole width
	live     bool
	status   string
	statusK  statusKind
	prompt   promptKind
	toDelete *gennotes.Note
	draft    *draft  // draft of promptConflict or promptRetry
	edit     *editor // inline editor, while editing
	failure  string  // reason shown with promptRetry
}

// New returns the UI model. Pass the same ctx to WatchEvents.
func New(ctx context.Context, api *Clients, light bool) Model {
	th := darkTheme
	glamourStyle := "dark"
	if light {
		th = lightTheme
		glamourStyle = "light"
	}
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "search notes"
	in.CharLimit = 200
	pin := textinput.New()
	pin.Prompt = ""
	pin.CharLimit = 100
	return Model{
		promptIn: pin,
		ctx:      ctx,
		api:      api.Notes,
		ask:      api.Assistant,
		th:       th,
		md:       newMarkdown(glamourStyle),
		vp:       viewport.New(),
		input:    in,
	}
}

// Messages returned by API commands.
type (
	listMsg struct {
		seq   int
		items []*gennotes.NoteSummary
		total int
		mode  string
		err   error
	}
	answerMsg struct {
		seq  int
		text string
		err  error
	}
	noteMsg struct {
		note *gennotes.Note
		q    string
		err  error
	}
	savedMsg struct {
		d    *draft
		note *gennotes.Note
		err  error
	}
	deletedMsg struct {
		id        string
		permanent bool
		err       error
	}
	restoredMsg struct {
		note *gennotes.Note
		err  error
	}
	notebooksMsg struct {
		notebooks []*gennotes.Notebook
		trashed   []*gennotes.TrashedNotebook
		tags      []*gennotes.TagCount
		err       error
	}
	// notebookOpMsg reports moving a notebook into or out of the trash, or
	// deleting it; show is the notebook filter to show afterwards.
	notebookOpMsg struct {
		status string
		show   string
		err    error
	}
	notebookSavedMsg struct {
		notebook *gennotes.Notebook
		created  bool // by createNotebook; the UI then shows it
		err      error
	}
	movedMsg    struct{ err error }
	sortTickMsg struct {
		seq      int
		notebook string
		sort     string
	}
	searchTickMsg struct{ seq int }
)

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadList(), m.loadNotebooks())
}

func (m *Model) loadNotebooks() tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		res, err := api.Notebooks(ctx)
		if err != nil {
			return notebooksMsg{err: err}
		}
		trashed, err := api.TrashedNotebooks(ctx)
		if err != nil {
			return notebooksMsg{err: err}
		}
		tags, err := api.Tags(ctx, &gennotes.TagsPayload{})
		return notebooksMsg{notebooks: res, trashed: trashed, tags: tags, err: err}
	}
}

// tagPrefix marks a tag in the notebook filter and the rows of the notebook
// pane: the list then has the notes with the tag in all notebooks but the
// trash. Notebook IDs cannot start with it.
const tagPrefix = "#"

func tagOf(filter string) (string, bool) { return strings.CutPrefix(filter, tagPrefix) }

// trashedPrefix marks a notebook in the trash in the notebook filter and the
// rows of the notebook pane. Its notes are not listed.
const trashedPrefix = "trash/"

func trashedID(filter string) (string, bool) { return strings.CutPrefix(filter, trashedPrefix) }

// notebookOp runs an operation on a notebook as a whole.
func (m *Model) notebookOp(status, show string, op func(ctx context.Context) error) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		return notebookOpMsg{status: status, show: show, err: op(ctx)}
	}
}

func (m *Model) loadList() tea.Cmd {
	m.listSeq++
	seq, q, ctx, api, allSort := m.listSeq, m.query, m.ctx, m.api, m.allSort
	if _, ok := trashedID(m.notebook); ok {
		// The notes of a notebook in the trash are not listed.
		return func() tea.Msg { return listMsg{seq: seq, items: []*gennotes.NoteSummary{}} }
	}
	var nb, tag *string
	if t, ok := tagOf(m.notebook); ok {
		tag = &t
	} else if m.notebook != "" {
		nb = &m.notebook
	}
	return func() tea.Msg {
		if q == "" {
			// A notebook's list comes in its own sort, which the server knows.
			var sort *string
			if nb == nil && allSort != "" {
				sort = &allSort
			}
			items, total, err := listAll(ctx, api, &gennotes.ListPayload{Notebook: nb, Tag: tag, Sort: sort})
			return listMsg{seq: seq, items: items, total: total, err: err}
		}
		res, err := api.Search(ctx, &gennotes.SearchPayload{Q: q, Notebook: nb, Tag: tag, Limit: 200})
		if err != nil {
			return listMsg{seq: seq, err: err}
		}
		return listMsg{seq: seq, items: res.Notes, total: res.Total, mode: res.Mode}
	}
}

// listPage is the number of notes read at a time.
const listPage = 500

// listAll reads every page of the list p, which holds the whole list in
// memory; its summaries are small.
func listAll(ctx context.Context, api *gennotes.Client, p *gennotes.ListPayload) ([]*gennotes.NoteSummary, int, error) {
	p.Limit = listPage
	var items []*gennotes.NoteSummary
	for {
		p.Offset = len(items)
		res, err := api.List(ctx, p)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, res.Notes...)
		if len(res.Notes) == 0 || len(items) >= res.Total {
			return items, res.Total, nil
		}
	}
}

// answer is a question to the assistant and its answer.
type answer struct {
	question string
	text     string
	err      error
	pending  bool
}

// askTimeout bounds one question; the agent may search and read several notes.
const askTimeout = 3 * time.Minute

func (m *Model) askAssistant(q string) tea.Cmd {
	m.askSeq++
	m.answer, m.answerOn = &answer{question: q, pending: true}, true
	m.renderPreview(false)
	m.setStatus(statusInfo, "asking the assistant…")
	seq, ctx, api := m.askSeq, m.ctx, m.ask
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, askTimeout)
		defer cancel()
		res, err := api.Ask(ctx, &genassistant.AskPayload{Question: q})
		if err != nil {
			return answerMsg{seq: seq, err: err}
		}
		return answerMsg{seq: seq, text: res.Answer}
	}
}

// closeAnswer shows the note again in the preview.
func (m *Model) closeAnswer() {
	if m.answerOn {
		m.answerOn = false
		m.renderPreview(false)
	}
}

// pickIndex selects item i on the user's request, which also closes the
// answer.
func (m *Model) pickIndex(i int) tea.Cmd {
	m.closeAnswer()
	return m.selectIndex(i)
}

func (m *Model) loadNote(id string) tea.Cmd {
	ctx, api, q := m.ctx, m.api, m.query
	return func() tea.Msg {
		p := &gennotes.GetPayload{ID: id}
		if q != "" {
			p.Q = &q
		}
		n, err := api.Get(ctx, p)
		return noteMsg{note: n, q: q, err: err}
	}
}

func (m *Model) save(d *draft, force bool) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		if d.id == "" {
			p := &gennotes.CreatePayload{Title: d.title, Tags: d.tags, Body: &d.body}
			if d.newID != "" {
				p.ID = &d.newID
			}
			if d.newNotebook != "" {
				p.Notebook = &d.newNotebook
			}
			n, err := api.Create(ctx, p)
			return savedMsg{d: d, note: n, err: err}
		}
		p := &gennotes.UpdatePayload{ID: d.id, Title: &d.title, Tags: d.tags, Body: &d.body}
		if p.Tags == nil {
			p.Tags = []string{}
		}
		if d.newNotebook != "" && d.newNotebook != d.notebook {
			p.Notebook = &d.newNotebook
		}
		if !force {
			p.IfMatch = &d.version
		}
		n, err := api.Update(ctx, p)
		return savedMsg{d: d, note: n, err: err}
	}
}

func (m *Model) remove(n *gennotes.Note) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		err := api.Delete(ctx, &gennotes.DeletePayload{ID: n.ID, IfMatch: &n.Version})
		return deletedMsg{id: n.ID, permanent: n.Notebook == trashNotebook, err: err}
	}
}

func (m *Model) restore(n *gennotes.Note) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		res, err := api.Restore(ctx, &gennotes.RestorePayload{ID: n.ID, IfMatch: &n.Version})
		return restoredMsg{note: res, err: err}
	}
}

func (m *Model) saveNotebook(p *gennotes.SaveNotebookPayload) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		nb, err := api.SaveNotebook(ctx, p)
		return notebookSavedMsg{notebook: nb, err: err}
	}
}

// createNotebook creates a notebook named name of kind ("default" or "collab");
// the server derives its ID.
func (m *Model) createNotebook(name, kind string) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		nb, err := api.CreateNotebook(ctx, &gennotes.CreateNotebookPayload{Name: name, Kind: kind})
		return notebookSavedMsg{notebook: nb, created: true, err: err}
	}
}

// selectedNotebook returns the notebook under the cursor of the notebook
// pane, or nil for "All".
func (m *Model) selectedNotebook() *gennotes.Notebook {
	rows := m.notebookRows()
	if m.nbSel <= 0 || m.nbSel >= len(rows) {
		return nil
	}
	for _, nb := range m.notebooks {
		if nb.ID == rows[m.nbSel].id {
			return nb
		}
	}
	return nil
}

// reorderNotebook moves the selected notebook up (-1) or down (+1) among the
// notebooks other than inbox and trash, renumbering their orders.
func (m *Model) reorderNotebook(delta int) tea.Cmd {
	cur := m.selectedNotebook()
	if cur == nil || cur.Reserved {
		return nil
	}
	var others []*gennotes.Notebook
	for _, nb := range m.notebooks {
		if !nb.Reserved {
			others = append(others, nb)
		}
	}
	i := slices.Index(others, cur)
	j := i + delta
	if i < 0 || j < 0 || j >= len(others) {
		return nil
	}
	others[i], others[j] = others[j], others[i]
	var cmds []tea.Cmd
	for k, nb := range others {
		order := (k + 1) * 10
		if nb.Order != order {
			cmds = append(cmds, m.saveNotebook(&gennotes.SaveNotebookPayload{ID: nb.ID, Order: &order}))
		}
	}
	return tea.Batch(cmds...)
}

// sorts are the orders of the note list, in the order the s key cycles them.
var sorts = []string{"order", "created", "updated", "title"}

// currentSort returns the order of the note list shown.
func (m *Model) currentSort() string {
	if m.acrossNotebooks() {
		return cmp.Or(m.allSort, "order")
	}
	for _, nb := range m.notebooks {
		if nb.ID == m.notebook {
			return nb.Sort
		}
	}
	return "order"
}

// canReorder reports whether the notes shown can be moved by hand: those of a
// notebook in the manual order, without a search.
func (m *Model) canReorder() bool {
	if _, ok := trashedID(m.notebook); ok || m.query != "" || m.acrossNotebooks() || m.notebook == trashNotebook {
		return false
	}
	return m.currentSort() == "order"
}

// cycleSort switches the note list to the next order. A notebook keeps it in
// its settings, shared through git; all notes keep it while the TUI runs.
func (m *Model) cycleSort() tea.Cmd {
	if _, ok := trashedID(m.notebook); ok {
		return nil
	}
	next := sorts[(slices.Index(sorts, m.currentSort())+1)%len(sorts)]
	m.setStatus(statusInfo, "sort: %s", next)
	if m.acrossNotebooks() {
		m.allSort = next
		return m.loadList()
	}
	// Take the new sort at once, so that the next press cycles from it, and
	// save it after a pause.
	for _, nb := range m.notebooks {
		if nb.ID == m.notebook {
			nb.Sort = next
		}
	}
	m.sortSeq++
	msg := sortTickMsg{seq: m.sortSeq, notebook: m.notebook, sort: next}
	return tea.Tick(sortDebounce, func(time.Time) tea.Msg { return msg })
}

// moveNote moves the selected note up (-1) or down (+1) in the manual order.
func (m *Model) moveNote(delta int) tea.Cmd {
	if !m.canReorder() {
		m.setStatus(statusWarn, "J/K moves notes of a notebook sorted by order, without a search (s changes the sort)")
		return nil
	}
	j := m.sel + delta
	if m.sel < 0 || m.sel >= len(m.items) || j < 0 || j >= len(m.items) {
		return nil
	}
	id := m.items[m.sel].ID
	// Down: after the next note. Up: after the note above the previous one,
	// or first.
	var after *string
	if delta > 0 {
		after = &m.items[j].ID
	} else if j > 0 {
		after = &m.items[j-1].ID
	}
	m.pending = id // keep the cursor on the moved note
	m.moving = id
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		_, err := api.Move(ctx, &gennotes.MovePayload{ID: id, After: after})
		return movedMsg{err: err}
	}
}

// defaultNotebook is the notebook for a new note: the selected one, or the
// inbox when all notes or the trash are shown.
func (m *Model) defaultNotebook() string {
	if m.acrossNotebooks() || m.notebook == trashNotebook {
		return inboxNotebook
	}
	return m.notebook
}

// defaultTags are the tags of a new note: the selected tag, if any.
func (m *Model) defaultTags() []string {
	if t, ok := tagOf(m.notebook); ok {
		return []string{t}
	}
	return nil
}

// acrossNotebooks reports whether the list has notes of all notebooks: all
// notes, or the notes with a tag.
func (m *Model) acrossNotebooks() bool {
	_, tag := tagOf(m.notebook)
	return m.notebook == "" || tag
}

// notebookRow is a row of the notebook pane.
type notebookRow struct {
	id    string // empty for all notes
	count int
	kind  string // "default", "collab" (AI clients may write), "drop" (they may only add notes) or "private" (hidden from them)
}

// notebookRows returns the rows of the notebook pane: all notes, then the
// notebooks or, with sideTags, the tags.
func (m *Model) notebookRows() []notebookRow {
	if m.sideTags {
		return m.tagRows()
	}
	return m.notebookListRows()
}

// allRow is the row of all notes, at the top of the notebook pane.
func (m *Model) allRow() notebookRow {
	all := 0
	for _, nb := range m.notebooks {
		if nb.ID != trashNotebook {
			all += nb.Count
		}
	}
	return notebookRow{id: "", count: all}
}

// notebookListRows returns the rows of the notebooks.
func (m *Model) notebookListRows() []notebookRow {
	rows := []notebookRow{m.allRow()}
	for _, nb := range m.notebooks {
		rows = append(rows, notebookRow{id: nb.ID, count: nb.Count, kind: nb.Kind})
	}
	// The notebooks in the trash follow the trash.
	for _, nb := range m.trashed {
		rows = append(rows, notebookRow{id: trashedPrefix + nb.ID, count: nb.Count, kind: nb.Kind})
	}
	return rows
}

// tagRows returns the rows of the tags. The selected tag stays even when no
// note has it any more, so that the rows do not change under the user.
func (m *Model) tagRows() []notebookRow {
	rows := []notebookRow{m.allRow()}
	selected, isTag := tagOf(m.notebook)
	for _, t := range m.tags {
		rows = append(rows, notebookRow{id: tagPrefix + t.Name, count: t.Count})
		if t.Name == selected {
			isTag = false
		}
	}
	if isTag {
		rows = append(rows, notebookRow{id: m.notebook})
	}
	return rows
}

// toggleSide switches the notebook pane between the notebooks and the tags,
// as Quiver does. The list stays; the cursor goes to its row, if any.
func (m *Model) toggleSide() {
	m.sideTags = !m.sideTags
	m.nbSel = max(0, slices.IndexFunc(m.notebookRows(), func(r notebookRow) bool { return r.id == m.notebook }))
}

// notebookTop returns the first row of the notebook pane shown in h rows, so
// that the row under the cursor is in view.
func (m *Model) notebookTop(h int) int {
	return max(0, m.nbSel-max(1, h)+1)
}

// selectedRow returns the row under the cursor of the notebook pane.
func (m *Model) selectedRow() notebookRow {
	rows := m.notebookRows()
	if m.nbSel < 0 || m.nbSel >= len(rows) {
		return notebookRow{}
	}
	return rows[m.nbSel]
}

// selectNotebook shows the notes of the notebook at row i of the notebook pane.
func (m *Model) selectNotebook(i int) tea.Cmd {
	rows := m.notebookRows()
	i = max(0, min(i, len(rows)-1))
	m.nbSel = i
	if rows[i].id == m.notebook {
		return nil
	}
	m.notebook = rows[i].id
	m.sel, m.top = 0, 0
	return m.loadList()
}

func (m *Model) setStatus(k statusKind, format string, args ...any) {
	m.statusK = k
	m.status = fmt.Sprintf(format, args...)
}

func (m *Model) selected() *gennotes.NoteSummary {
	if m.sel < 0 || m.sel >= len(m.items) {
		return nil
	}
	return m.items[m.sel]
}

// selectIndex moves the selection and loads the note if it changed.
func (m *Model) selectIndex(i int) tea.Cmd {
	if len(m.items) == 0 {
		return nil
	}
	i = max(0, min(i, len(m.items)-1))
	changed := i != m.sel || m.note == nil || m.note.ID != m.items[i].ID
	m.sel = i
	m.scrollList()
	if !changed {
		return nil
	}
	return m.loadNote(m.items[i].ID)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	tm, cmd := m.update(msg)
	if m, ok := tm.(Model); ok && m.zoom && m.focus != focusPreview && m.focus != focusEdit {
		// The zoom ends once the focus leaves the preview and the editor,
		// so that it does not come back by surprise.
		m.zoom = false
		m.layout()
		return m, cmd
	}
	return tm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case listMsg:
		if msg.seq != m.listSeq {
			return m, nil // stale response
		}
		if msg.err != nil {
			m.setStatus(statusError, "load failed: %v", msg.err)
			return m, nil
		}
		return m, m.applyList(msg)

	case answerMsg:
		if msg.seq != m.askSeq || m.answer == nil {
			return m, nil // a newer question was asked
		}
		m.answer.pending, m.answer.text, m.answer.err = false, msg.text, msg.err
		m.answerOn = true // show it even if the user moved on meanwhile
		m.renderPreview(false)
		if msg.err != nil {
			m.setStatus(statusError, "the assistant failed: %s", errorMessage(msg.err))
		} else {
			m.setStatus(statusInfo, "answered")
		}
		return m, nil

	case noteMsg:
		if msg.err != nil {
			if errorName(msg.err) == "not_found" {
				return m, nil // deleted meanwhile; the list reload follows
			}
			m.setStatus(statusError, "load failed: %v", msg.err)
			return m, nil
		}
		if cur := m.selected(); cur == nil || cur.ID != msg.note.ID {
			return m, nil // the selection moved on
		}
		keep := m.note != nil && m.note.ID == msg.note.ID && m.noteQ == msg.q
		m.note, m.noteQ = msg.note, msg.q
		m.renderPreview(keep)
		return m, nil

	case sortTickMsg:
		if msg.seq != m.sortSeq {
			return m, nil // a later press follows
		}
		return m, m.saveNotebook(&gennotes.SaveNotebookPayload{ID: msg.notebook, Sort: &msg.sort})

	case searchTickMsg:
		if msg.seq != m.typeSeq {
			return m, nil
		}
		q := strings.TrimSpace(m.input.Value())
		if q == m.query {
			return m, nil
		}
		m.query = q
		return m, m.loadList()

	case editorDoneMsg:
		return m, m.finishEdit(msg)

	case savedMsg:
		return m, m.finishSave(msg)

	case deletedMsg:
		if msg.err != nil {
			if errorName(msg.err) == "precondition_failed" {
				m.setStatus(statusWarn, "not deleted: the note was changed outside; review it and try again")
				return m, m.loadNote(msg.id)
			}
			m.setStatus(statusError, "delete failed: %v", msg.err)
			return m, nil
		}
		if msg.permanent {
			m.setStatus(statusInfo, "deleted permanently")
		} else {
			m.setStatus(statusInfo, "moved to the trash (open the trash and press u to restore)")
		}
		return m, tea.Batch(m.loadList(), m.loadNotebooks())

	case restoredMsg:
		if msg.err != nil {
			m.setStatus(statusError, "restore failed: %v", msg.err)
			return m, nil
		}
		m.setStatus(statusInfo, "restored to %s", msg.note.Notebook)
		return m, tea.Batch(m.loadList(), m.loadNotebooks())

	case notebookOpMsg:
		if msg.err != nil {
			m.setStatus(statusError, "%s", errorMessage(msg.err))
			return m, nil
		}
		m.setStatus(statusInfo, "%s", msg.status)
		m.notebook, m.sel, m.top = msg.show, 0, 0
		return m, tea.Batch(m.loadNotebooks(), m.loadList())

	case notebookSavedMsg:
		if msg.err != nil {
			m.setStatus(statusError, "notebook not saved: %s", errorMessage(msg.err))
			return m, nil
		}
		if msg.created {
			// Show the new notebook; the cursor follows it once the list is loaded.
			m.notebook, m.sel, m.top = msg.notebook.ID, 0, 0
			m.setStatus(statusInfo, "created %q (folder %s)", msg.notebook.Name, msg.notebook.ID)
			return m, tea.Batch(m.loadNotebooks(), m.loadList())
		}
		if msg.notebook.ID == m.notebook {
			// The sort of the list may have changed.
			return m, tea.Batch(m.loadNotebooks(), m.loadList())
		}
		return m, m.loadNotebooks()

	case movedMsg:
		if msg.err != nil {
			m.pending, m.moving = "", ""
			m.setStatus(statusError, "not moved: %s", errorMessage(msg.err))
			return m, nil
		}
		return m, m.loadList()

	case notebooksMsg:
		if msg.err != nil {
			m.setStatus(statusError, "load failed: %v", msg.err)
			return m, nil
		}
		m.notebooks, m.trashed, m.tags = msg.notebooks, msg.trashed, msg.tags
		if _, ok := trashedID(m.notebook); ok && !slices.ContainsFunc(m.notebookListRows(), func(r notebookRow) bool { return r.id == m.notebook }) {
			m.notebook = trashNotebook // the notebook left the trash
		}
		m.nbSel = 0
		for i, r := range m.notebookRows() {
			if r.id == m.notebook {
				m.nbSel = i
			}
		}
		// The preview shows the notebook's name, which may have changed or
		// arrived after the note.
		m.rendered = ""
		m.renderPreview(true)
		return m, nil

	case eventMsg:
		return m, m.handleEvent(msg.ev)

	case connMsg:
		m.live = msg.live
		if !msg.live && msg.err != nil {
			m.setStatus(statusWarn, "event stream disconnected; reconnecting…")
		}
		return m, nil

	case tea.MouseClickMsg:
		return m, m.handleClick(msg.Mouse())

	case tea.MouseWheelMsg:
		return m, m.handleWheel(msg.Mouse())

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		if m.focus == focusEdit && m.prompt == promptNone {
			return m, m.edit.updateOther(msg)
		}
	}

	// Other messages, such as cursor blinks and pastes, go to the focused input.
	if m.prompt == promptRename || m.prompt == promptAddNotebook || m.prompt == promptAsk {
		var cmd tea.Cmd
		m.promptIn, cmd = m.promptIn.Update(msg)
		return m, cmd
	}
	switch m.focus {
	case focusSearch:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case focusEdit:
		return m, m.edit.updateOther(msg)
	}
	return m, nil
}

func (m *Model) applyList(msg listMsg) tea.Cmd {
	prev := ""
	if s := m.selected(); s != nil {
		prev = s.ID
	}
	if m.pending != "" {
		prev, m.pending = m.pending, ""
	}
	m.items, m.total, m.mode = msg.items, msg.total, msg.mode
	if len(m.items) == 0 {
		m.sel, m.top, m.note = 0, 0, nil
		m.renderPreview(false)
		return nil
	}
	idx := min(m.sel, len(m.items)-1)
	for i, it := range m.items {
		if it.ID == prev {
			idx = i
			break
		}
	}
	m.sel = -1 // force a reload of the selected note
	return m.selectIndex(idx)
}

func (m *Model) handleEvent(ev *gennotes.NoteEvent) tea.Cmd {
	cmds := []tea.Cmd{m.loadList(), m.loadNotebooks()}
	if e := m.edit; e != nil && ev.ID != nil && e.id == *ev.ID && (ev.Type == "updated" || ev.Type == "deleted") {
		e.outside = true
		m.setStatus(statusWarn, "the note you are editing was %s outside", ev.Type)
	}
	if ev.ID != nil && m.note != nil && m.note.ID == *ev.ID && ev.Type == "updated" {
		if m.moving == *ev.ID {
			m.moving = "" // the move by J/K, not an edit outside
		} else {
			m.setStatus(statusInfo, "%q was updated outside", m.note.Title)
		}
		cmds = append(cmds, m.loadNote(*ev.ID))
	}
	return tea.Batch(cmds...)
}

func (m *Model) startEdit(n *gennotes.Note) tea.Cmd {
	var (
		d   *draft
		err error
	)
	if n == nil {
		d, err = newDraft("", m.defaultNotebook(), "", "", m.defaultTags(), "")
	} else {
		d, err = newDraft(n.ID, n.Notebook, n.Version, n.Title, n.Tags, n.Body)
	}
	if err != nil {
		m.setStatus(statusError, "edit failed: %v", err)
		return nil
	}
	return openEditor(d)
}

func (m *Model) finishEdit(msg editorDoneMsg) tea.Cmd {
	d := msg.d
	if msg.err != nil {
		m.setStatus(statusError, "editor failed: %v (draft kept at %s)", msg.err, d.path)
		return nil
	}
	changed, err := d.load()
	if !changed && err == nil {
		d.remove()
		m.setStatus(statusInfo, "no changes")
		return nil
	}
	if err != nil {
		m.retry(d, err.Error())
		return nil
	}
	return m.save(d, false)
}

// retry asks whether to edit the draft again or discard it.
func (m *Model) retry(d *draft, reason string) {
	m.prompt, m.draft, m.failure = promptRetry, d, reason
}

func (m *Model) finishSave(msg savedMsg) tea.Cmd {
	if msg.err != nil {
		switch errorName(msg.err) {
		case "precondition_failed":
			m.prompt, m.draft = promptConflict, msg.d
		case "already_exists":
			m.retry(msg.d, fmt.Sprintf("a note with id %q already exists", msg.d.newID))
		default:
			m.retry(msg.d, msg.err.Error())
		}
		return nil
	}
	msg.d.remove()
	if msg.d.path == "" {
		m.closeEditor()
	}
	m.setStatus(statusInfo, "saved %q", msg.note.Title)
	m.pending = msg.note.ID
	return m.loadList()
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.prompt {
	case promptDelete:
		m.prompt = promptNone
		if key == "y" && m.toDelete != nil {
			n := m.toDelete
			m.toDelete = nil
			return m, m.remove(n)
		}
		m.toDelete = nil
		m.setStatus(statusInfo, "cancelled")
		return m, nil
	case promptConflict:
		d := m.draft
		switch key {
		case "o":
			m.prompt, m.draft = promptNone, nil
			return m, m.save(d, true)
		case "e":
			m.prompt, m.draft = promptNone, nil
			return m, m.editAgain(d)
		case "d", "esc":
			m.prompt, m.draft = promptNone, nil
			m.discard(d)
			return m, m.loadNote(d.id)
		}
		return m, nil
	case promptRetry:
		d := m.draft
		switch key {
		case "e", "enter":
			m.prompt, m.draft = promptNone, nil
			return m, m.editAgain(d)
		case "d", "esc":
			m.prompt, m.draft = promptNone, nil
			m.discard(d)
		}
		return m, nil
	case promptRename, promptAddNotebook, promptAsk:
		switch key {
		case "esc":
			m.prompt = promptNone
			m.promptIn.Blur()
			return m, nil
		case "enter":
			kind, v := m.prompt, strings.TrimSpace(m.promptIn.Value())
			m.prompt = promptNone
			m.promptIn.Blur()
			if kind == promptAsk {
				if v == "" {
					return m, nil
				}
				return m, m.askAssistant(v)
			}
			if kind == promptRename {
				if nb := m.selectedNotebook(); nb != nil {
					return m, m.saveNotebook(&gennotes.SaveNotebookPayload{ID: nb.ID, Name: &v})
				}
				return m, nil
			}
			if v == "" {
				return m, nil
			}
			return m, m.createNotebook(v, m.addKind)
		}
		var cmd tea.Cmd
		m.promptIn, cmd = m.promptIn.Update(msg)
		return m, cmd
	case promptNotebookKind:
		nb := m.selectedNotebook()
		kind := map[string]string{"d": "default", "c": "collab", "w": "drop", "p": "private"}[key]
		switch {
		case key == "esc" || nb == nil:
			m.prompt = promptNone
		case kind == "":
		case nb.Kind == "private" && (kind == "default" || kind == "collab"):
			m.prompt, m.newKind = promptUnprivate, kind // AI will see it
		default:
			m.prompt = promptNone
			return m, m.saveNotebook(&gennotes.SaveNotebookPayload{ID: nb.ID, Kind: &kind})
		}
		return m, nil
	case promptTrashNotebook, promptPurgeNotebook:
		kind := m.prompt
		m.prompt = promptNone
		if key != "y" {
			return m, nil
		}
		if kind == promptTrashNotebook {
			if nb := m.selectedNotebook(); nb != nil {
				id := nb.ID
				return m, m.notebookOp("moved the notebook to the trash", trashedPrefix+id, func(ctx context.Context) error {
					return m.api.DeleteNotebook(ctx, &gennotes.DeleteNotebookPayload{ID: id})
				})
			}
			return m, nil
		}
		if id, ok := trashedID(m.selectedRow().id); ok {
			return m, m.notebookOp("deleted the notebook permanently", trashNotebook, func(ctx context.Context) error {
				return m.api.DeleteTrashedNotebook(ctx, &gennotes.DeleteTrashedNotebookPayload{ID: id})
			})
		}
		return m, nil
	case promptUnprivate:
		m.prompt = promptNone
		if nb := m.selectedNotebook(); key == "y" && nb != nil {
			kind := m.newKind
			return m, m.saveNotebook(&gennotes.SaveNotebookPayload{ID: nb.ID, Kind: &kind})
		}
		return m, nil
	case promptDiscard:
		m.prompt = promptNone
		if key == "y" {
			m.closeEditor()
			m.setStatus(statusInfo, "discarded your changes")
		}
		return m, nil
	}

	if m.focus == focusEdit {
		switch key {
		case "ctrl+s":
			return m, m.saveEditor()
		case "esc":
			if m.edit.dirty() {
				m.prompt = promptDiscard
				return m, nil
			}
			m.closeEditor()
			return m, nil
		}
		return m, m.edit.update(msg)
	}

	if m.focus == focusSearch {
		switch key {
		case "esc":
			m.input.SetValue("")
			m.input.Blur()
			m.focus = focusList
			if m.query != "" {
				m.query = ""
				return m, m.loadList()
			}
			return m, nil
		case "enter":
			m.input.Blur()
			m.focus = focusList
			m.typeSeq++
			q := strings.TrimSpace(m.input.Value())
			if q != m.query {
				m.query = q
				return m, m.loadList()
			}
			return m, nil
		case "up", "ctrl+p":
			return m, m.pickIndex(m.sel - 1)
		case "down", "ctrl+n":
			return m, m.pickIndex(m.sel + 1)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.typeSeq++
		seq := m.typeSeq
		tick := tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchTickMsg{seq: seq} })
		return m, tea.Batch(cmd, tick)
	}

	// Keys of the notebook pane that mean something else elsewhere.
	if m.focus == focusNotebooks {
		switch key {
		case "e":
			if nb := m.selectedNotebook(); nb != nil {
				m.prompt = promptRename
				m.promptIn.CharLimit = 100
				m.promptIn.SetValue(nb.Name)
				m.promptIn.CursorEnd()
				return m, m.promptIn.Focus()
			}
			return m, nil
		case "a", "A":
			m.prompt, m.addKind = promptAddNotebook, "default"
			if key == "A" {
				m.addKind = "collab"
			}
			m.promptIn.CharLimit = 100 // as the API allows
			m.promptIn.SetValue("")
			return m, m.promptIn.Focus()
		case "t":
			if nb := m.selectedNotebook(); nb != nil && nb.ID != trashNotebook {
				m.prompt = promptNotebookKind
			}
			return m, nil
		case "J":
			return m, m.reorderNotebook(+1)
		case "K":
			return m, m.reorderNotebook(-1)
		case "d":
			row := m.selectedRow()
			if _, ok := trashedID(row.id); ok {
				m.prompt = promptPurgeNotebook
			} else if nb := m.selectedNotebook(); nb != nil && !nb.Reserved {
				m.prompt = promptTrashNotebook
			} else if _, ok := tagOf(row.id); !ok {
				m.setStatus(statusWarn, "inbox and trash cannot be deleted")
			}
			return m, nil
		case "u":
			if id, ok := trashedID(m.selectedRow().id); ok {
				return m, m.notebookOp("restored the notebook", id, func(ctx context.Context) error {
					_, err := m.api.RestoreNotebook(ctx, &gennotes.RestoreNotebookPayload{ID: id})
					return err
				})
			}
			return m, nil
		}
	}

	// Keys shared by the panes.
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.prompt = promptAsk
		m.promptIn.CharLimit = 2000 // as the API allows
		m.promptIn.SetValue("")
		return m, m.promptIn.Focus()
	case "/":
		m.focus = focusSearch
		return m, m.input.Focus()
	case "n":
		return m, m.openEditor(nil)
	case "e":
		if m.note != nil {
			return m, m.openEditor(m.note)
		}
		return m, nil
	case "N":
		return m, m.startEdit(nil)
	case "E":
		if m.note != nil {
			return m, m.startEdit(m.note)
		}
		return m, nil
	case "d":
		if m.note != nil {
			m.toDelete = m.note
			m.prompt = promptDelete
		}
		return m, nil
	case "u":
		if m.note != nil && m.note.Notebook == trashNotebook {
			return m, m.restore(m.note)
		}
		return m, nil
	case "ctrl+n", "ctrl+p":
		if len(m.matches) == 0 {
			return m, nil
		}
		n := len(m.matches)
		if key == "ctrl+n" {
			m.matchIdx = (m.matchIdx + 1) % n
		} else {
			m.matchIdx = (max(m.matchIdx, 0) + n - 1) % n
		}
		m.showMatch()
		m.setStatus(statusInfo, "match %d/%d", m.matchIdx+1, n)
		return m, nil
	case "T":
		m.toggleSide()
		return m, nil
	case "[":
		return m, m.selectNotebook(m.nbSel - 1)
	case "]":
		return m, m.selectNotebook(m.nbSel + 1)
	case "r":
		m.setStatus(statusInfo, "reloaded")
		return m, m.loadList()
	case "tab", "shift+tab":
		order := []focus{focusList, focusPreview}
		if m.computeLayout().showNotebooks {
			order = []focus{focusNotebooks, focusList, focusPreview}
		}
		i := 0
		for j, f := range order {
			if f == m.focus {
				i = j
			}
		}
		if key == "tab" {
			i = (i + 1) % len(order)
		} else {
			i = (i + len(order) - 1) % len(order)
		}
		m.focus = order[i]
		m.layout()
		return m, nil
	}

	if m.focus == focusNotebooks {
		switch key {
		case "j", "down":
			return m, m.selectNotebook(m.nbSel + 1)
		case "k", "up":
			return m, m.selectNotebook(m.nbSel - 1)
		case "g", "home":
			return m, m.selectNotebook(0)
		case "G", "end":
			return m, m.selectNotebook(len(m.notebookRows()) - 1)
		case "enter", "l", "right":
			m.focus = focusList
			m.layout()
		}
		return m, nil
	}

	if m.focus == focusPreview {
		switch key {
		case "z":
			if m.width >= narrowWidth {
				m.zoom = !m.zoom
				m.layout()
			}
		case "esc", "h", "left":
			if key == "esc" && m.answerOn {
				m.closeAnswer()
				return m, nil
			}
			if key == "esc" && m.zoom {
				m.zoom = false
				m.layout()
				return m, nil
			}
			m.focus = focusList
			m.layout()
		case "j", "down":
			m.vp.ScrollDown(1)
		case "k", "up":
			m.vp.ScrollUp(1)
		case "space", "pgdown", "f":
			m.vp.PageDown()
		case "b", "pgup":
			m.vp.PageUp()
		case "g", "home":
			m.vp.GotoTop()
		case "G", "end":
			m.vp.GotoBottom()
		}
		return m, nil
	}

	switch key {
	case "j", "down":
		return m, m.pickIndex(m.sel + 1)
	case "k", "up":
		return m, m.pickIndex(m.sel - 1)
	case "J":
		return m, m.moveNote(+1)
	case "K":
		return m, m.moveNote(-1)
	case "s":
		return m, m.cycleSort()
	case "g", "home":
		return m, m.pickIndex(0)
	case "G", "end":
		return m, m.pickIndex(len(m.items) - 1)
	case "pgdown":
		return m, m.pickIndex(m.sel + m.visibleItems())
	case "pgup":
		return m, m.pickIndex(m.sel - m.visibleItems())
	case "enter", "l", "right":
		if m.note != nil {
			m.focus = focusPreview
			m.layout()
		}
	case "h", "left":
		if m.computeLayout().showNotebooks || m.width < narrowWidth {
			m.focus = focusNotebooks
			m.layout()
		}
	case "esc":
		if m.answerOn {
			m.closeAnswer()
			return m, nil
		}
		if m.query != "" {
			m.query = ""
			m.input.SetValue("")
			return m, m.loadList()
		}
	}
	return m, nil
}

func (m *Model) handleClick(ms tea.Mouse) tea.Cmd {
	if ms.Button != tea.MouseLeft || m.prompt != promptNone || m.focus == focusEdit {
		return nil
	}
	l := m.computeLayout()
	switch {
	case ms.Y == 0:
		m.focus = focusSearch
		return m.input.Focus()
	case l.showNotebooks && ms.X < l.listX && ms.Y > l.paneTop && ms.Y < l.paneTop+l.paneH-1:
		if m.focus == focusSearch {
			m.input.Blur()
		}
		m.focus = focusNotebooks
		return m.selectNotebook(m.notebookTop(l.paneH-2) + ms.Y - l.paneTop - 1)
	case l.showList && ms.X >= l.listX && ms.X < l.listX+l.listW && ms.Y > l.paneTop && ms.Y < l.paneTop+l.paneH-1:
		if m.focus == focusSearch {
			m.input.Blur()
		}
		m.focus = focusList
		row := ms.Y - l.paneTop - 1
		return m.pickIndex(m.top + row/itemHeight)
	case l.showPreview && ms.X >= l.previewX:
		if m.focus == focusSearch {
			m.input.Blur()
		}
		m.focus = focusPreview
	}
	return nil
}

func (m *Model) handleWheel(ms tea.Mouse) tea.Cmd {
	if m.focus == focusEdit {
		return nil
	}
	l := m.computeLayout()
	step := 0
	switch ms.Button {
	case tea.MouseWheelDown:
		step = 1
	case tea.MouseWheelUp:
		step = -1
	default:
		return nil
	}
	if l.showNotebooks && ms.X < l.listX {
		return m.selectNotebook(m.nbSel + step)
	}
	if l.showList && ms.X < l.listX+l.listW {
		return m.pickIndex(m.sel + step)
	}
	if step > 0 {
		m.vp.ScrollDown(3)
	} else {
		m.vp.ScrollUp(3)
	}
	return nil
}

// openEditor starts editing n, or a new note when n is nil, in the UI.
func (m *Model) openEditor(n *gennotes.Note) tea.Cmd {
	m.edit = newEditor(m.th, n, m.defaultNotebook())
	if tags := m.defaultTags(); n == nil && tags != nil {
		m.edit.inputs[fieldTags].SetValue(strings.Join(tags, ", "))
		m.edit.orig = m.edit.snapshot()
	}
	m.focus = focusEdit
	m.layout()
	return m.edit.focus(m.edit.field)
}

func (m *Model) closeEditor() {
	m.edit = nil
	if m.focus == focusEdit {
		m.focus = focusPreview
		if m.note == nil {
			m.focus = focusList
		}
	}
	m.layout()
}

func (m *Model) saveEditor() tea.Cmd {
	d, err := m.edit.draft()
	if err != nil {
		m.retry(d, err.Error())
		return nil
	}
	return m.save(d, false)
}

// editAgain resumes editing a draft that could not be saved: the inline
// editor still holds it, while an external draft is reopened in the editor.
func (m *Model) editAgain(d *draft) tea.Cmd {
	if d.path == "" && m.edit != nil {
		m.focus = focusEdit
		return m.edit.focus(m.edit.field)
	}
	return openEditor(d)
}

// discard gives up a draft that could not be saved.
func (m *Model) discard(d *draft) {
	if d.path == "" {
		m.closeEditor()
		m.setStatus(statusInfo, "discarded your changes")
		return
	}
	m.setStatus(statusInfo, "discarded the draft (copy at %s)", d.path)
}
