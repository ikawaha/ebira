// Package tui is a terminal UI for the ebira API. It talks to the server only
// through the generated API client, like any other UI.
package tui

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	genauth "github.com/ikawaha/ebira/gen/auth"
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
	promptEmptyTrash    // confirmation to delete everything in the trash permanently
	promptMovePrivate   // confirmation to move a note of a private notebook where AI can read it
	promptMoveNotebook  // confirmation to move a private notebook where AI can read it
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
	// auth tells how the server authenticates people, and web is the URL of
	// the web UI; passkey is set while the server requires a passkey, which
	// the TUI cannot sign in with yet.
	auth    *genauth.Client
	web     string
	passkey *passkeyGuide
	signIn  func(session string) // makes the clients send the session
	th      theme

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
	// toMove is a note in the trash being moved to a notebook chosen in the
	// notebook pane (m), and moveTo the notebook waiting for promptMovePrivate.
	toMove *gennotes.Note
	moveTo string
	// nbToMove is a notebook being moved into another notebook, chosen in the
	// notebook pane (m), or to the top ("All"); nbParent is the notebook
	// waiting for promptMoveNotebook, "" for the top.
	nbToMove *gennotes.Notebook
	nbParent string

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
	tr       *catalog // messages in the language of the UI
	zoom     bool     // the preview, and the editor opened from it, take the whole width
	live     bool
	status   string
	statusK  statusKind
	prompt   promptKind
	toDelete *gennotes.Note
	draft    *draft  // draft of promptConflict or promptRetry
	edit     *editor // inline editor, while editing
	failure  string  // reason shown with promptRetry
}

// New returns the UI model, in the language lang (one of Langs; see Lang).
// Pass the same ctx to WatchEvents.
func New(ctx context.Context, api *Clients, light bool, lang string) Model {
	tr := newCatalog(lang)
	th := darkTheme
	glamourStyle := "dark"
	if light {
		th = lightTheme
		glamourStyle = "light"
	}
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = tr.t("search.placeholder")
	in.CharLimit = 200
	pin := textinput.New()
	pin.Prompt = ""
	pin.CharLimit = 100
	return Model{
		tr:       tr,
		promptIn: pin,
		ctx:      ctx,
		api:      api.Notes,
		ask:      api.Assistant,
		auth:     api.Auth,
		web:      api.Web,
		signIn:   api.SetSession,
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
	// assistantStatusMsg tells whether the assistant can answer questions.
	assistantStatusMsg struct {
		res *genassistant.StatusResult
		err error
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
	return tea.Batch(m.loadList(), m.loadNotebooks(), m.checkAuth())
}

func (m *Model) loadNotebooks() tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		res, err := api.Notebooks(ctx, &gennotes.NotebooksPayload{})
		if err != nil {
			return notebooksMsg{err: err}
		}
		trashed, err := api.TrashedNotebooks(ctx, &gennotes.TrashedNotebooksPayload{})
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

// startMove starts moving note n, which is in the trash, to a notebook: the
// notebook pane shows the notebooks to choose from.
func (m *Model) startMove(n *gennotes.Note) {
	m.toMove = n
	if m.sideTags {
		m.toggleSide()
	}
	m.focus = focusNotebooks
	m.layout()
	m.setStatus(statusInfo, "status.chooseMoveTarget", "title", n.Title)
}

// chooseMoveTarget moves the note being moved to the notebook under the
// cursor, after asking when it would show a note of a private notebook to AI.
func (m *Model) chooseMoveTarget() tea.Cmd {
	row := m.selectedRow()
	_, tag := tagOf(row.id)
	_, trashed := trashedID(row.id)
	if row.id == "" || row.id == trashNotebook || tag || trashed {
		m.setStatus(statusWarn, "status.moveTargetInvalid")
		return nil
	}
	if m.noteKind(m.toMove) == "private" && row.kind != "private" {
		m.prompt, m.moveTo = promptMovePrivate, row.id
		return nil
	}
	return m.moveNoteTo(row.id)
}

// startMoveNotebook starts moving the selected notebook into another
// notebook, or to the top: the cursor then chooses where.
func (m *Model) startMoveNotebook() {
	nb := m.selectedNotebook()
	switch {
	case nb == nil || nb.Reserved:
		m.setStatus(statusWarn, "status.notebookParentInvalid")
	case nb.Parent == nil && slices.ContainsFunc(m.notebooks, func(x *gennotes.Notebook) bool { return parentOf(x) == nb.ID }):
		m.setStatus(statusWarn, "status.notebookHasSubs")
	default:
		m.nbToMove = nb
		m.setStatus(statusInfo, "status.chooseNotebookParent", "name", nb.Name)
	}
}

// chooseNotebookParent moves the notebook being moved into the notebook under
// the cursor, or to the top on "All", after asking when it would show a
// private notebook to AI.
func (m *Model) chooseNotebookParent() tea.Cmd {
	nb, row := m.nbToMove, m.selectedRow()
	parent, kind := row.id, nb.Kind
	if parent != "" {
		p := m.notebookOf(parent)
		if p == nil || p.Reserved || p.Parent != nil || p.ID == nb.ID || p.ID == parentOf(nb) {
			m.setStatus(statusWarn, "status.notebookParentInvalid")
			return nil
		}
		kind = p.Kind
	} else if nb.Parent == nil {
		m.setStatus(statusWarn, "status.notebookParentInvalid") // at the top already
		return nil
	}
	if nb.Kind == "private" && kind != "private" {
		m.prompt, m.nbParent = promptMoveNotebook, parent
		return nil
	}
	return m.moveNotebookTo(parent)
}

// moveNotebookTo moves the notebook being moved into parent, or to the top,
// and shows it.
func (m *Model) moveNotebookTo(parent string) tea.Cmd {
	nb := m.nbToMove
	m.nbToMove = nil
	name := nb.ID
	if p := parentOf(nb); p != "" {
		name = strings.TrimPrefix(nb.ID, p+"/")
	}
	to := name
	if parent != "" {
		to = parent + "/" + name
	}
	payload := &gennotes.MoveNotebookPayload{ID: nb.ID}
	if parent != "" {
		payload.Parent = &parent
	}
	return m.notebookOp("status.notebookMoved", to, func(ctx context.Context) error {
		_, err := m.api.MoveNotebook(ctx, payload)
		return err
	})
}

// moveNoteTo moves the note being moved to notebook nb and shows nb.
func (m *Model) moveNoteTo(nb string) tea.Cmd {
	n := m.toMove
	m.toMove = nil
	m.focus = focusList
	return m.notebookOp("status.noteMoved", nb, func(ctx context.Context) error {
		_, err := m.api.Update(ctx, &gennotes.UpdatePayload{ID: n.ID, Notebook: &nb})
		return err
	})
}

// noteKind returns the kind of the notebook note n belongs to: for a note in
// the trash, the notebook it was deleted from or with.
func (m *Model) noteKind(n *gennotes.Note) string {
	if n.TrashedNotebook != nil {
		for _, nb := range m.trashed {
			if nb.ID == *n.TrashedNotebook {
				return nb.Kind
			}
		}
	}
	id := n.Notebook
	if n.TrashedFrom != nil {
		id = *n.TrashedFrom
	}
	for _, nb := range m.notebooks {
		if nb.ID == id {
			return nb.Kind
		}
	}
	return ""
}

// trashedPrefix marks a notebook in the trash in the notebook filter and the
// rows of the notebook pane. Its notes are listed to be read only: they are
// edited, deleted and restored with the notebook as a whole, as in the web UI.
const trashedPrefix = "trash/"

func trashedID(filter string) (string, bool) { return strings.CutPrefix(filter, trashedPrefix) }

// inTrash returns the ID that notebook nb has in the trash once deleted on
// its own: a sub notebook, <parent>/<sub>, becomes <parent>~<sub>, told from
// the sub notebooks deleted with their parent.
func inTrash(nb *gennotes.Notebook) string {
	if nb.Parent == nil {
		return nb.ID
	}
	return *nb.Parent + "~" + strings.TrimPrefix(nb.ID, *nb.Parent+"/")
}

// parentOf returns the parent of notebook nb, "" for one that is not a sub
// notebook.
func parentOf(nb *gennotes.Notebook) string {
	if nb.Parent == nil {
		return ""
	}
	return *nb.Parent
}

// restoredAs returns the notebook that notebook id in the trash goes back to.
func restoredAs(id string) string { return strings.Replace(id, "~", "/", 1) }

// inTrashedNotebook reports whether the list shows the notes of a notebook in
// the trash.
func (m *Model) inTrashedNotebook() bool {
	_, ok := trashedID(m.notebook)
	return ok
}

// trashContents returns the number of notes in the trash, those deleted on
// their own and those of the notebooks in the trash, and the number of the
// notebooks in the trash.
func (m *Model) trashContents() (notes, notebooks int) {
	for _, nb := range m.notebooks {
		if nb.ID == trashNotebook {
			notes += nb.Count
		}
	}
	for _, nb := range m.trashed {
		notes += nb.Count
	}
	return notes, len(m.trashed)
}

// notebookOp runs an operation on a notebook as a whole, showing the message
// status of the catalog when it is done.
func (m *Model) notebookOp(status, show string, op func(ctx context.Context) error) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		return notebookOpMsg{status: status, show: show, err: op(ctx)}
	}
}

func (m *Model) loadList() tea.Cmd {
	m.listSeq++
	seq, q, ctx, api, allSort := m.listSeq, m.query, m.ctx, m.api, m.allSort
	if tid, ok := trashedID(m.notebook); ok {
		return func() tea.Msg { return loadTrashedNotebook(ctx, api, tid, q, seq) }
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

// loadTrashedNotebook lists the notes of notebook tid in the trash, or those
// matching q. The search takes no notebook in the trash, so it searches the
// trash and keeps the notes of tid.
func loadTrashedNotebook(ctx context.Context, api *gennotes.Client, tid, q string, seq int) tea.Msg {
	trash := trashNotebook
	if q == "" {
		items, total, err := listAll(ctx, api, &gennotes.ListPayload{Notebook: &trash, TrashedNotebook: &tid})
		return listMsg{seq: seq, items: items, total: total, err: err}
	}
	res, err := api.Search(ctx, &gennotes.SearchPayload{Q: q, Notebook: &trash, Limit: 200})
	if err != nil {
		return listMsg{seq: seq, err: err}
	}
	items := []*gennotes.NoteSummary{}
	for _, n := range res.Notes {
		if n.TrashedNotebook != nil && *n.TrashedNotebook == tid {
			items = append(items, n)
		}
	}
	return listMsg{seq: seq, items: items, total: len(items), mode: res.Mode}
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

// checkAssistant asks the server whether the assistant can answer questions,
// so that the question prompt is closed at once when it cannot.
func (m *Model) checkAssistant() tea.Cmd {
	if m.ask == nil {
		return nil
	}
	ctx, api := m.ctx, m.ask
	return func() tea.Msg {
		res, err := api.Status(ctx)
		return assistantStatusMsg{res: res, err: err}
	}
}

// askTimeout bounds one question; the agent may search and read several notes.
const askTimeout = 3 * time.Minute

func (m *Model) askAssistant(q string) tea.Cmd {
	m.askSeq++
	m.answer, m.answerOn = &answer{question: q, pending: true}, true
	m.renderPreview(false)
	m.setStatus(statusInfo, "status.asking")
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
// notebooks other than inbox and trash, or among the sub notebooks of its
// parent, renumbering their orders.
func (m *Model) reorderNotebook(delta int) tea.Cmd {
	cur := m.selectedNotebook()
	if cur == nil || cur.Reserved {
		return nil
	}
	var others []*gennotes.Notebook
	for _, nb := range m.notebooks {
		if !nb.Reserved && parentOf(nb) == parentOf(cur) {
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
	m.setStatus(statusInfo, "status.sort", "sort", m.tr.t("sort."+next))
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
		m.setStatus(statusWarn, "status.reorderHint")
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
	if m.acrossNotebooks() || m.notebook == trashNotebook || m.inTrashedNotebook() {
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

// setStatus shows the message key of the catalog, with args as pairs of a
// name and a value (see catalog.t).
func (m *Model) setStatus(k statusKind, key string, args ...any) {
	m.setStatusText(k, m.tr.t(key, args...))
}

// setStatusText shows s, a message already in the language of the UI or
// from the server.
func (m *Model) setStatusText(k statusKind, s string) {
	m.statusK, m.status = k, s
}

// errText is the message of err, in the language of the UI when it is one
// of the TUI's own (see errMsg).
func (m *Model) errText(err error) string {
	if me, ok := errors.AsType[*msgError](err); ok {
		return m.tr.t(me.key, me.args...)
	}
	// An important operation asks for a fresh sign-in with the passkey,
	// which the TUI, signed in through the web UI, cannot do.
	if errorName(err) == "verification_required" {
		return m.tr.t("error.verifyInWeb", "url", m.web)
	}
	return errorMessage(err)
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
		if errorName(msg.err) == "unauthorized" {
			return m, m.checkAuth()
		}
		if msg.err != nil {
			m.setStatus(statusError, "status.loadFailed", "err", msg.err)
			return m, nil
		}
		return m, m.applyList(msg)

	case assistantStatusMsg:
		// When the status is unknown, such as with an older server, the
		// question itself tells.
		if msg.err != nil || msg.res.Available || m.prompt != promptAsk {
			return m, nil
		}
		m.prompt = promptNone
		m.promptIn.Blur()
		var reason string
		if msg.res.Reason != nil {
			reason = *msg.res.Reason
		}
		m.setStatus(statusError, "status.assistantUnavailable", "err", reason)
		return m, nil

	case answerMsg:
		if msg.seq != m.askSeq || m.answer == nil {
			return m, nil // a newer question was asked
		}
		m.answer.pending, m.answer.text, m.answer.err = false, msg.text, msg.err
		m.answerOn = true // show it even if the user moved on meanwhile
		m.renderPreview(false)
		if msg.err != nil {
			m.setStatus(statusError, "status.assistantFailed", "err", errorMessage(msg.err))
		} else {
			m.setStatus(statusInfo, "status.answered")
		}
		return m, nil

	case noteMsg:
		if msg.err != nil {
			if errorName(msg.err) == "not_found" {
				return m, nil // deleted meanwhile; the list reload follows
			}
			m.setStatus(statusError, "status.loadFailed", "err", msg.err)
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
				m.setStatus(statusWarn, "status.notDeletedChanged")
				return m, m.loadNote(msg.id)
			}
			m.setStatus(statusError, "status.deleteFailed", "err", msg.err)
			return m, nil
		}
		if msg.permanent {
			m.setStatus(statusInfo, "status.deletedPermanently")
		} else {
			m.setStatus(statusInfo, "status.movedToTrash")
		}
		return m, tea.Batch(m.loadList(), m.loadNotebooks())

	case restoredMsg:
		if msg.err != nil {
			m.setStatus(statusError, "status.restoreFailed", "err", msg.err)
			return m, nil
		}
		m.setStatus(statusInfo, "status.restored", "notebook", m.notebookName(msg.note.Notebook))
		return m, tea.Batch(m.loadList(), m.loadNotebooks())

	case notebookOpMsg:
		if msg.err != nil {
			m.setStatusText(statusError, m.errText(msg.err))
			return m, nil
		}
		m.setStatus(statusInfo, msg.status)
		m.notebook, m.sel, m.top = msg.show, 0, 0
		return m, tea.Batch(m.loadNotebooks(), m.loadList())

	case notebookSavedMsg:
		if msg.err != nil {
			m.setStatus(statusError, "status.notebookNotSaved", "err", m.errText(msg.err))
			return m, nil
		}
		if msg.created {
			// Show the new notebook; the cursor follows it once the list is loaded.
			m.notebook, m.sel, m.top = msg.notebook.ID, 0, 0
			m.setStatus(statusInfo, "status.notebookCreated", "name", msg.notebook.Name, "id", msg.notebook.ID)
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
			m.setStatus(statusError, "status.notMoved", "err", m.errText(msg.err))
			return m, nil
		}
		return m, m.loadList()

	case notebooksMsg:
		if errorName(msg.err) == "unauthorized" {
			return m, m.checkAuth()
		}
		if msg.err != nil {
			m.setStatus(statusError, "status.loadFailed", "err", msg.err)
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
		switch {
		case msg.live && m.passkey != nil:
			// The server no longer requires a passkey, such as after a
			// restart with auth: none.
			m.passkey = nil
			m.setStatusText(statusInfo, "")
			return m, tea.Batch(m.loadList(), m.loadNotebooks())
		case errorName(msg.err) == "unauthorized":
			// Ask again while no passkey is registered, to begin the sign-in
			// once one is.
			if m.passkey == nil || !m.passkey.registered {
				return m, m.checkAuth()
			}
		case !msg.live && msg.err != nil:
			m.setStatus(statusWarn, "status.disconnected")
		}
		return m, nil

	case authMsg, deviceStartMsg, deviceTickMsg, deviceTokenMsg, browserOpenedMsg:
		return m, m.handleAuth(msg)

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
		if ev.Type == "deleted" {
			m.setStatus(statusWarn, "status.editingDeleted")
		} else {
			m.setStatus(statusWarn, "status.editingUpdated")
		}
	}
	if ev.ID != nil && m.note != nil && m.note.ID == *ev.ID && ev.Type == "updated" {
		if m.moving == *ev.ID {
			m.moving = "" // the move by J/K, not an edit outside
		} else {
			m.setStatus(statusInfo, "status.updatedOutside", "title", m.note.Title)
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
		d, err = newDraft("", m.defaultNotebook(), "", "", m.defaultTags(), "", m.tr.t("draft.idHint"))
	} else {
		d, err = newDraft(n.ID, n.Notebook, n.Version, n.Title, n.Tags, n.Body, m.tr.t("draft.idHint"))
	}
	if err != nil {
		m.setStatus(statusError, "status.editFailed", "err", err)
		return nil
	}
	return openEditor(d)
}

func (m *Model) finishEdit(msg editorDoneMsg) tea.Cmd {
	d := msg.d
	if msg.err != nil {
		m.setStatus(statusError, "status.editorFailed", "err", msg.err, "path", d.path)
		return nil
	}
	changed, err := d.load()
	if !changed && err == nil {
		d.remove()
		m.setStatus(statusInfo, "status.noChanges")
		return nil
	}
	if err != nil {
		m.retry(d, m.errText(err))
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
			m.retry(msg.d, m.tr.t("status.exists", "id", msg.d.newID))
		default:
			m.retry(msg.d, msg.err.Error())
		}
		return nil
	}
	msg.d.remove()
	if msg.d.path == "" {
		m.closeEditor()
	}
	m.setStatus(statusInfo, "status.saved", "title", msg.note.Title)
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
		m.setStatus(statusInfo, "status.cancelled")
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
				return m, m.notebookOp("status.notebookTrashed", trashedPrefix+inTrash(nb), func(ctx context.Context) error {
					return m.api.DeleteNotebook(ctx, &gennotes.DeleteNotebookPayload{ID: id})
				})
			}
			return m, nil
		}
		if id, ok := trashedID(m.selectedRow().id); ok {
			return m, m.notebookOp("status.notebookDeleted", trashNotebook, func(ctx context.Context) error {
				return m.api.DeleteTrashedNotebook(ctx, &gennotes.DeleteTrashedNotebookPayload{ID: id})
			})
		}
		return m, nil
	case promptEmptyTrash:
		m.prompt = promptNone
		if key == "y" {
			return m, m.notebookOp("status.trashEmptied", trashNotebook, func(ctx context.Context) error {
				return m.api.EmptyTrash(ctx, &gennotes.EmptyTrashPayload{})
			})
		}
		return m, nil
	case promptMovePrivate:
		m.prompt = promptNone
		if key == "y" {
			return m, m.moveNoteTo(m.moveTo)
		}
		return m, nil // still choosing where to move the note
	case promptMoveNotebook:
		m.prompt = promptNone
		if key == "y" {
			return m, m.moveNotebookTo(m.nbParent)
		}
		return m, nil // still choosing where to move the notebook
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
			m.setStatus(statusInfo, "status.discarded")
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
	if m.focus == focusNotebooks && m.toMove != nil {
		// Choosing the notebook to move a note to: the cursor moves as usual.
		switch key {
		case "esc":
			m.toMove = nil
			m.focus = focusList
			m.setStatus(statusInfo, "status.moveCancelled")
			return m, nil
		case "enter":
			return m, m.chooseMoveTarget()
		case "j", "k", "down", "up", "g", "G", "home", "end", "pgup", "pgdown":
		default:
			return m, nil
		}
	}
	if m.focus == focusNotebooks && m.nbToMove != nil {
		// Choosing the notebook to move a notebook into.
		switch key {
		case "esc":
			m.nbToMove = nil
			m.setStatus(statusInfo, "status.moveCancelled")
			return m, nil
		case "enter":
			return m, m.chooseNotebookParent()
		case "j", "k", "down", "up", "g", "G", "home", "end", "pgup", "pgdown":
		default:
			return m, nil
		}
	}
	if m.focus == focusNotebooks {
		switch key {
		case "m":
			if !m.sideTags {
				m.startMoveNotebook()
			}
			return m, nil
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
			if row.id == trashNotebook {
				if notes, notebooks := m.trashContents(); notes+notebooks == 0 {
					m.setStatus(statusInfo, "status.trashAlreadyEmpty")
				} else {
					m.prompt = promptEmptyTrash
				}
			} else if _, ok := trashedID(row.id); ok {
				m.prompt = promptPurgeNotebook
			} else if nb := m.selectedNotebook(); nb != nil && !nb.Reserved {
				m.prompt = promptTrashNotebook
			} else if _, ok := tagOf(row.id); !ok {
				m.setStatus(statusWarn, "status.reservedNotebook")
			}
			return m, nil
		case "u":
			if id, ok := trashedID(m.selectedRow().id); ok {
				return m, m.notebookOp("status.notebookRestored", restoredAs(id), func(ctx context.Context) error {
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
	case "o":
		// While waiting for the approval of the sign-in, open its page.
		if m.passkey != nil && m.passkey.device != nil {
			return m, openURL(m.passkey.device.VerificationURI)
		}
	case "?":
		m.prompt = promptAsk
		m.promptIn.CharLimit = 2000 // as the API allows
		m.promptIn.SetValue("")
		return m, tea.Batch(m.promptIn.Focus(), m.checkAssistant())
	case "/":
		m.focus = focusSearch
		return m, m.input.Focus()
	case "e", "E":
		if m.note != nil && m.inTrashedNotebook() {
			m.setStatus(statusWarn, "status.trashedNotebookReadOnly")
			return m, nil
		}
	case "m":
		if m.note != nil && m.note.Notebook == trashNotebook {
			m.startMove(m.note)
		}
		return m, nil
	}
	switch key {
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
		m.setStatus(statusInfo, "status.match", "i", m.matchIdx+1, "n", n)
		return m, nil
	case "T":
		m.toggleSide()
		return m, nil
	case "[":
		return m, m.selectNotebook(m.nbSel - 1)
	case "]":
		return m, m.selectNotebook(m.nbSel + 1)
	case "r":
		m.setStatus(statusInfo, "status.reloaded")
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
	m.edit = newEditor(m.th, m.tr, n, m.defaultNotebook())
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
		m.retry(d, m.errText(err))
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
		m.setStatus(statusInfo, "status.discarded")
		return
	}
	m.setStatus(statusInfo, "status.draftDiscarded", "path", d.path)
}
