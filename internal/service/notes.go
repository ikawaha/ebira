// Package service implements the notes service (gen/notes.Service).
// The YAML files are the source of truth; the SQLite index follows them.
package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/history"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/store"
)

// Notes implements the notes service.
type Notes struct {
	mu      sync.Mutex // serializes writes to files and the index
	gitMu   sync.Mutex // serializes syncs with git
	store   *store.Store
	index   *index.Index
	history *history.Store // earlier versions of notes; nil keeps none (see SetHistory)
	events  *broker
	now     func() time.Time
	nbState string // last known notebook list, for notebooks events
	// previewCSS is the path of the CSS file for the preview of the web UI
	// (see SetPreviewCSS).
	previewCSS string
}

var _ gennotes.Service = (*Notes)(nil)

// Event types of NoteEvent.
const (
	eventReady     = "ready"
	eventCreated   = "created"
	eventUpdated   = "updated"
	eventDeleted   = "deleted"
	eventNotebooks = "notebooks"
)

var errVersionMismatch = errors.New("note was changed since it was read")

// New returns a Notes service.
func New(s *store.Store, ix *index.Index) *Notes {
	return &Notes{
		store:  s,
		index:  ix,
		events: newBroker(),
		now:    func() time.Time { return time.Now().UTC().Truncate(time.Second) },
	}
}

// Close ends all event streams. Call it when shutting down the server so that
// open streams do not block the shutdown.
func (s *Notes) Close() { s.events.close() }

func (s *Notes) List(ctx context.Context, p *gennotes.ListPayload) (*gennotes.NoteList, error) {
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	if slices.Contains(hidden, deref(p.Notebook)) {
		return &gennotes.NoteList{Notes: []*gennotes.NoteSummary{}}, nil // as if the notebook were empty
	}
	if p.TrashedNotebook != nil {
		if deref(p.Notebook) != store.Trash {
			return nil, gennotes.MakeBadRequest(errors.New("trashed_notebook needs notebook=trash"))
		}
		sort := deref(p.Sort)
		if sort == "" {
			m, _ := s.store.TrashedNotebookMeta(*p.TrashedNotebook) // a broken settings file gives the default
			sort = m.Sort
		}
		res, total, err := s.index.ListTrashedNotebook(ctx, *p.TrashedNotebook, deref(p.Tag), sort, p.Limit, p.Offset)
		if err != nil {
			return nil, err
		}
		return &gennotes.NoteList{Total: total, Notes: summaries(res, false)}, nil
	}
	sort := deref(p.Sort)
	if nb := deref(p.Notebook); sort == "" && nb != "" {
		m, _ := s.store.NotebookMeta(nb) // a broken settings file gives the default
		sort = m.Sort
	}
	res, total, err := s.index.List(ctx, deref(p.Notebook), deref(p.Tag), sort, p.Limit, p.Offset, hidden...)
	if err != nil {
		return nil, err
	}
	return &gennotes.NoteList{Total: total, Notes: summaries(res, false)}, nil
}

func (s *Notes) Get(ctx context.Context, p *gennotes.GetPayload) (*gennotes.Note, error) {
	n, err := s.store.Load(p.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	// A note the request cannot see is reported as missing, not forbidden, so
	// that AI clients do not learn that it exists.
	if ok, err := s.visible(ctx, n.Notebook); err != nil {
		return nil, err
	} else if !ok {
		return nil, mapErr(store.ErrNotFound)
	}
	res := toNote(n)
	if q := deref(p.Q); strings.TrimSpace(q) != "" {
		title, body := s.index.Highlight(q, n.Title, n.Body)
		res.TitleHighlights, res.BodyHighlights = textRanges(title), textRanges(body)
	}
	return res, nil
}

func (s *Notes) Create(ctx context.Context, p *gennotes.CreatePayload) (*gennotes.Note, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.create(ctx, p, "")
}

// create creates a note; createdBy is store.CreatedByAI for AI clients.
func (s *Notes) create(ctx context.Context, p *gennotes.CreatePayload, createdBy string) (*gennotes.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	n := &store.Note{
		ID:        deref(p.ID),
		Notebook:  deref(p.Notebook),
		Title:     p.Title,
		Tags:      tags(p.Tags),
		Body:      deref(p.Body),
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: createdBy,
	}
	if n.ID == "" {
		n.ID = store.NewID()
	}
	if n.Notebook == "" {
		n.Notebook = store.Inbox
	}
	if n.Notebook == store.Trash {
		return nil, gennotes.MakeBadRequest(errors.New("cannot create a note in the trash"))
	}
	if err := s.store.Create(n); err != nil {
		return nil, mapErr(err)
	}
	if err := s.reindex(ctx, n); err != nil {
		return nil, err
	}
	s.events.publish(eventCreated, n.ID)
	return toNote(n), nil
}

func (s *Notes) Update(ctx context.Context, p *gennotes.UpdatePayload) (*gennotes.Note, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.updateNote(ctx, p)
}

// updateNote updates a note, for people and for AI clients after their checks.
func (s *Notes) updateNote(ctx context.Context, p *gennotes.UpdatePayload) (*gennotes.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.load(p.ID, p.IfMatch)
	if err != nil {
		return nil, err
	}
	to := n.Notebook
	if p.Notebook != nil {
		to = *p.Notebook
	}
	if to == store.Trash && n.Notebook != store.Trash {
		return nil, gennotes.MakeBadRequest(errors.New("use delete to move a note to the trash"))
	}
	edited := p.Title != nil || p.Tags != nil || p.Body != nil
	if p.Title != nil {
		n.Title = *p.Title
	}
	if p.Tags != nil {
		n.Tags = tags(p.Tags)
	}
	if p.Body != nil {
		n.Body = *p.Body
	}
	if edited {
		n.UpdatedAt = s.now()
	}
	if n.Notebook == store.Trash && to != store.Trash {
		n.TrashedFrom = "" // moving out of the trash restores the note
		edited = true
	}
	if err := s.write(ctx, n, edited, to); err != nil {
		return nil, err
	}
	return toNote(n), nil
}

func (s *Notes) Delete(ctx context.Context, p *gennotes.DeletePayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	return s.deleteNote(ctx, p)
}

// deleteNote moves a note to the trash, or deletes it from the trash, for people and for AI clients after their checks.
func (s *Notes) deleteNote(ctx context.Context, p *gennotes.DeletePayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.store.Stat(p.ID)
	if err != nil {
		return mapErr(err)
	}
	if f.Notebook != store.Trash {
		n, err := s.load(p.ID, p.IfMatch)
		if err != nil {
			return err
		}
		n.TrashedFrom = n.Notebook
		return s.write(ctx, n, true, store.Trash)
	}
	if p.IfMatch != nil {
		v, err := s.store.Version(p.ID)
		if err != nil {
			return mapErr(err)
		}
		if !store.MatchVersion(*p.IfMatch, v) {
			return gennotes.MakePreconditionFailed(errVersionMismatch)
		}
	}
	if err := s.store.Delete(p.ID); err != nil {
		return mapErr(err)
	}
	if err := s.index.Delete(ctx, p.ID); err != nil {
		return err
	}
	s.forgetHistory(p.ID)
	s.events.publish(eventDeleted, p.ID)
	return nil
}

func (s *Notes) Restore(ctx context.Context, p *gennotes.RestorePayload) (*gennotes.Note, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.load(p.ID, p.IfMatch)
	if err != nil {
		return nil, err
	}
	if n.Notebook != store.Trash {
		return nil, gennotes.MakeBadRequest(errors.New("the note is not in the trash"))
	}
	to := n.TrashedFrom
	if n.TrashedNotebook != "" {
		// A note of a notebook in the trash goes back into that notebook,
		// which is recreated with its settings (above all its kind, so that a
		// private note stays hidden from AI) if it does not exist.
		to = store.RestoredNotebook(n.TrashedNotebook)
		if err := s.ensureNotebookFromTrash(n.TrashedNotebook); err != nil {
			return nil, err
		}
	}
	if !store.ValidNotebook(to) || to == store.Trash {
		to = store.Inbox
	}
	n.TrashedFrom = ""
	if err := s.write(ctx, n, true, to); err != nil {
		return nil, err
	}
	return toNote(n), nil
}

// MoveNotes moves notes to a notebook at once. The notes are read first, so
// that none moves when one cannot be read.
func (s *Notes) MoveNotes(ctx context.Context, p *gennotes.MoveNotesPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	if p.Notebook == store.Trash {
		return gennotes.MakeBadRequest(errors.New("use trash_notes to move notes to the trash"))
	}
	if !store.ValidNotebook(p.Notebook) {
		return gennotes.MakeBadRequest(store.ErrInvalidNotebook)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.loadNotes(p.Ids)
	if err != nil {
		return err
	}
	for _, n := range notes {
		if n.Notebook == p.Notebook {
			continue
		}
		edited := false
		if n.Notebook == store.Trash {
			n.TrashedFrom = "" // moving out of the trash restores the note
			edited = true
		}
		if err := s.write(ctx, n, edited, p.Notebook); err != nil {
			return err
		}
	}
	return nil
}

// TrashNotes moves notes to the trash at once. The notes are read first, so
// that none moves when one cannot be read or is in the trash already.
func (s *Notes) TrashNotes(ctx context.Context, p *gennotes.TrashNotesPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.loadNotes(p.Ids)
	if err != nil {
		return err
	}
	for _, n := range notes {
		if n.Notebook == store.Trash {
			return gennotes.MakeBadRequest(fmt.Errorf("note %s is in the trash already", n.ID))
		}
	}
	for _, n := range notes {
		n.TrashedFrom = n.Notebook
		if err := s.write(ctx, n, true, store.Trash); err != nil {
			return err
		}
	}
	return nil
}

// loadNotes reads the notes, each once. The caller holds s.mu.
func (s *Notes) loadNotes(ids []string) ([]*store.Note, error) {
	var notes []*store.Note
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		n, err := s.store.Load(id)
		if err != nil {
			return nil, mapErr(err)
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// sortStep is the gap between the keys of the manual order that Move leaves
// when it puts a note first or last, or spaces the notes again. Keys of notes
// never moved are their creation times in milliseconds, which are at least a
// second apart unless created in the same second.
const sortStep = 1000

// Move puts a note right after another note of its notebook in the manual
// order, or first. Only the moved note is written, with a key between those of
// its new neighbors, so that a move changes one file in the notes repository.
// When the neighbors leave no room, the notes of the notebook are spaced
// again, which writes them all; this is rare, as keys start seconds apart.
func (s *Notes) Move(ctx context.Context, p *gennotes.MovePayload) (*gennotes.Note, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.moveNote(ctx, p)
}

// moveNote reorders a note, for people and for AI clients after their checks.
func (s *Notes) moveNote(ctx context.Context, p *gennotes.MovePayload) (*gennotes.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.load(p.ID, nil)
	if err != nil {
		return nil, err
	}
	if n.Notebook == store.Trash {
		return nil, gennotes.MakeBadRequest(errors.New("notes in the trash have no manual order"))
	}
	keys, err := s.index.SortKeys(ctx, n.Notebook)
	if err != nil {
		return nil, err
	}
	keys = slices.DeleteFunc(keys, func(k index.SortKey) bool { return k.ID == n.ID })
	if len(keys) == 0 {
		return toNote(n), nil // the only note of the notebook
	}
	pos := 0 // the index in keys that the note goes to
	if after := deref(p.After); after != "" {
		i := slices.IndexFunc(keys, func(k index.SortKey) bool { return k.ID == after })
		if i < 0 {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("note %s is not another note of notebook %s", after, n.Notebook))
		}
		pos = i + 1
	}
	key, ok := keyAt(keys, pos)
	if !ok {
		if err := s.respace(ctx, keys, pos, n); err != nil {
			return nil, err
		}
		return toNote(n), nil
	}
	if n.Order != nil && *n.Order == key || n.Order == nil && n.SortKey() == key {
		return toNote(n), nil
	}
	n.Order = &key
	if err := s.write(ctx, n, true, n.Notebook); err != nil {
		return nil, err
	}
	return toNote(n), nil
}

// keyAt returns a key of the manual order for a note put at index pos of keys,
// which are highest first, or false if its neighbors leave no room.
func keyAt(keys []index.SortKey, pos int) (int64, bool) {
	switch {
	case pos == 0:
		return keys[0].Key + sortStep, true
	case pos == len(keys):
		return keys[pos-1].Key - sortStep, true
	}
	upper, lower := keys[pos-1].Key, keys[pos].Key
	if upper-lower < 2 {
		return 0, false
	}
	return lower + (upper-lower)/2, true
}

// respace writes the notes of keys, with n put at index pos, sortStep apart
// from the highest key down.
func (s *Notes) respace(ctx context.Context, keys []index.SortKey, pos int, n *store.Note) error {
	ids := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	ids = slices.Insert(ids, pos, n.ID)
	top := n.SortKey()
	if len(keys) > 0 {
		top = max(top, keys[0].Key)
	}
	for i, id := range ids {
		key := top - int64(i)*sortStep
		m := n
		if id != n.ID {
			var err error
			if m, err = s.store.Load(id); err != nil {
				return mapErr(err)
			}
		}
		if m.Order != nil && *m.Order == key {
			continue
		}
		m.Order = &key
		if err := s.write(ctx, m, true, m.Notebook); err != nil {
			return err
		}
	}
	return nil
}

func (s *Notes) Notebooks(ctx context.Context, _ *gennotes.NotebooksPayload) ([]*gennotes.Notebook, error) {
	nbs, err := s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(nbs, func(nb *gennotes.Notebook) bool {
		if !slices.Contains(hidden, nb.ID) {
			return false
		}
		if nb.Kind == store.KindDrop {
			nb.Count = 0 // listed so that AI clients can add notes, but nothing about its notes
			return false
		}
		return true
	}), nil
}

func (s *Notes) Tags(ctx context.Context, p *gennotes.TagsPayload) ([]*gennotes.TagCount, error) {
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	if slices.Contains(hidden, deref(p.Notebook)) {
		return []*gennotes.TagCount{}, nil // as if the notebook were empty
	}
	res, err := s.index.Tags(ctx, deref(p.Notebook), hidden...)
	if err != nil {
		return nil, err
	}
	tags := make([]*gennotes.TagCount, len(res))
	for i, t := range res {
		tags[i] = &gennotes.TagCount{Name: t.Name, Count: t.Count}
	}
	return tags, nil
}

func (s *Notes) SaveNotebook(ctx context.Context, p *gennotes.SaveNotebookPayload) (*gennotes.Notebook, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	// Making a private notebook visible to AI is an important operation.
	if p.Kind != nil && storeKind(*p.Kind) != store.KindPrivate {
		if m, err := s.store.NotebookMeta(p.ID); err == nil && m.Kind == store.KindPrivate {
			if err := requireVerified(ctx); err != nil {
				return nil, err
			}
		}
	}
	return s.saveNotebook(ctx, p)
}

// saveNotebook saves the settings of a notebook, for people and for AI clients after their checks.
func (s *Notes) saveNotebook(ctx context.Context, p *gennotes.SaveNotebookPayload) (*gennotes.Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reserved := p.ID == store.Inbox || p.ID == store.Trash
	if reserved && p.Order != nil {
		return nil, gennotes.MakeBadRequest(errors.New("the order of inbox and trash cannot be changed"))
	}
	m, err := s.store.NotebookMeta(p.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	if p.Name != nil {
		m.Name = strings.TrimSpace(*p.Name)
	}
	if p.Order != nil {
		m.Order = *p.Order
	}
	if p.Sort != nil {
		m.Sort = *p.Sort
	}
	if p.Kind != nil {
		switch _, sub := store.SplitNotebook(p.ID); {
		case sub != "":
			return nil, gennotes.MakeBadRequest(errors.New("a sub notebook takes the kind of its notebook; change the kind of that notebook instead"))
		case reserved && *p.Kind == store.KindCollab:
			return nil, gennotes.MakeBadRequest(errors.New("inbox and trash cannot be of the collab kind"))
		case p.ID == store.Trash && (*p.Kind == store.KindPrivate || *p.Kind == store.KindDrop):
			return nil, gennotes.MakeBadRequest(errors.New("the trash keeps the default kind; it is hidden from AI clients anyway"))
		}
		m.Kind = storeKind(*p.Kind)
	}
	if err := s.store.SaveNotebookMeta(p.ID, m); err != nil {
		return nil, mapErr(err)
	}
	nbs, err := s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	s.notifyNotebooks(nbs)
	for _, nb := range nbs {
		if nb.ID == p.ID {
			return nb, nil
		}
	}
	return nil, gennotes.MakeNotFound(errors.New("notebook not found"))
}

// CreateNotebook creates a notebook whose ID is derived from its display name
// (see store.NotebookID). A display name already in use is rejected, so that
// notebooks can be told apart.
func (s *Notes) CreateNotebook(ctx context.Context, p *gennotes.CreateNotebookPayload) (*gennotes.Notebook, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.createNotebook(ctx, p)
}

// createNotebook creates a notebook, for people and for AI clients after their checks.
func (s *Notes) createNotebook(ctx context.Context, p *gennotes.CreateNotebookPayload) (*gennotes.Notebook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(norm.NFC.String(p.Name))
	if name == "" {
		return nil, gennotes.MakeBadRequest(errors.New("the notebook name is empty"))
	}
	nbs, err := s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	parent := deref(p.Parent)
	if parent != "" {
		if !store.ValidNotebook(parent+"/x") || !slices.ContainsFunc(nbs, func(nb *gennotes.Notebook) bool { return nb.ID == parent }) {
			if store.ValidNotebook(parent + "/x") {
				return nil, gennotes.MakeNotFound(fmt.Errorf("notebook %s not found", parent))
			}
			return nil, gennotes.MakeBadRequest(fmt.Errorf("notebook %s cannot hold sub notebooks: only notebooks other than the inbox and the trash can, one level deep", parent))
		}
	}
	// A notebook hidden from the request does not block its name, so that AI
	// clients do not learn the names of private notebooks. Drop notebooks are
	// listed for them, so their names do.
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	// Names and IDs are told apart among the notebooks of the same parent.
	taken := map[string]bool{store.Inbox: true, store.Trash: true}
	order := 0
	for _, nb := range nbs {
		taken[nb.ID] = true
		if deref(nb.Parent) != parent {
			continue
		}
		if strings.EqualFold(norm.NFC.String(nb.Name), name) && (!slices.Contains(hidden, nb.ID) || nb.Kind == store.KindDrop) {
			return nil, gennotes.MakeAlreadyExists(fmt.Errorf("notebook %q already exists (%s)", nb.Name, nb.ID))
		}
		if !nb.Reserved {
			order = max(order, nb.Order)
		}
	}
	prefix := ""
	if parent != "" {
		prefix = parent + "/"
	}
	id := prefix + store.UniqueNotebookID(store.NotebookID(name), func(id string) bool { return taken[prefix+id] })
	m := store.NotebookMeta{Name: name, Order: order + 10, Kind: storeKind(p.Kind)} // after the others by default
	if p.Order != nil {
		m.Order = *p.Order
	}
	if err := s.store.SaveNotebookMeta(id, m); err != nil {
		return nil, mapErr(err)
	}
	if nbs, err = s.notebooks(ctx); err != nil {
		return nil, err
	}
	s.notifyNotebooks(nbs)
	for _, nb := range nbs {
		if nb.ID == id {
			return nb, nil
		}
	}
	return nil, gennotes.MakeNotFound(errors.New("notebook not found"))
}

// MoveNotebook moves a notebook into another notebook, as a sub notebook, or
// a sub notebook to the top or into another notebook. It goes after the
// notebooks already there. As a sub notebook takes the kind of its parent,
// moving a private notebook into one of another kind makes it visible to AI,
// which is an important operation, as with SaveNotebook.
func (s *Notes) MoveNotebook(ctx context.Context, p *gennotes.MoveNotebookPayload) (*gennotes.Notebook, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nbs, err := s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	find := func(id string) *gennotes.Notebook {
		for _, nb := range nbs {
			if nb.ID == id {
				return nb
			}
		}
		return nil
	}
	cur := find(p.ID)
	if cur == nil {
		return nil, gennotes.MakeNotFound(fmt.Errorf("notebook %s not found", p.ID))
	}
	if cur.Reserved {
		return nil, gennotes.MakeBadRequest(store.ErrReserved)
	}
	parent, kind := deref(p.Parent), cur.Kind
	if parent != "" {
		pn := find(parent)
		if pn == nil {
			return nil, gennotes.MakeNotFound(fmt.Errorf("notebook %s not found", parent))
		}
		if pn.Reserved || pn.Parent != nil || pn.ID == cur.ID {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("notebook %s cannot hold sub notebooks: only notebooks other than the inbox and the trash can, one level deep", parent))
		}
		kind = pn.Kind
	}
	to := store.MovedNotebook(cur.ID, parent)
	if to == cur.ID {
		return cur, nil
	}
	// Names are told apart among the notebooks of the same parent.
	order := 0
	for _, nb := range nbs {
		if deref(nb.Parent) != parent || nb.ID == cur.ID {
			continue
		}
		if strings.EqualFold(norm.NFC.String(nb.Name), norm.NFC.String(cur.Name)) {
			return nil, gennotes.MakeAlreadyExists(fmt.Errorf("notebook %q already exists (%s)", nb.Name, nb.ID))
		}
		if !nb.Reserved {
			order = max(order, nb.Order)
		}
	}
	if cur.Kind == store.KindPrivate && kind != store.KindPrivate {
		if err := requireVerified(ctx); err != nil {
			return nil, err
		}
	}
	id, err := s.store.MoveNotebook(cur.ID, parent)
	if err != nil {
		return nil, mapErr(err)
	}
	m, err := s.store.NotebookMeta(id)
	if err != nil {
		return nil, err
	}
	m.Order = order + 10
	if err := s.store.SaveNotebookMeta(id, m); err != nil {
		return nil, err
	}
	if _, err := s.sync(ctx); err != nil {
		return nil, err
	}
	nbs, err = s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	for _, nb := range nbs {
		if nb.ID == id {
			return nb, nil
		}
	}
	return nil, gennotes.MakeNotFound(errors.New("notebook not found"))
}

// DeleteNotebook moves a notebook with its notes and settings into the trash.
func (s *Notes) DeleteNotebook(ctx context.Context, p *gennotes.DeleteNotebookPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.TrashNotebook(p.ID); err != nil {
		return mapErr(err)
	}
	_, err := s.sync(ctx)
	return err
}

// TrashedNotebooks lists the notebooks in the trash. AI clients cannot see
// the trash.
func (s *Notes) TrashedNotebooks(ctx context.Context, _ *gennotes.TrashedNotebooksPayload) ([]*gennotes.TrashedNotebook, error) {
	res := []*gennotes.TrashedNotebook{}
	if !fromPerson(ctx) {
		return res, nil
	}
	ids, err := s.store.TrashedNotebooks()
	if err != nil {
		return nil, err
	}
	counts, err := s.index.TrashedNotebookCounts(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		m, err := s.store.TrashedNotebookMeta(id)
		if err != nil {
			m = store.NotebookMeta{}
		}
		nb := &gennotes.TrashedNotebook{ID: id, Name: cmp.Or(m.Name, store.NotebookName(id)), Kind: apiKind(m.Kind), Count: counts[id]}
		if parent, sub := store.SplitNotebook(id); sub != "" {
			nb.Parent = &parent
		}
		res = append(res, nb)
	}
	return res, nil
}

// RestoreNotebook moves a notebook out of the trash with its notes.
func (s *Notes) RestoreNotebook(ctx context.Context, p *gennotes.RestoreNotebookPayload) (*gennotes.Notebook, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.store.RestoreNotebook(p.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	if _, err := s.sync(ctx); err != nil {
		return nil, err
	}
	nbs, err := s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	for _, nb := range nbs {
		if nb.ID == id {
			return nb, nil
		}
	}
	return nil, gennotes.MakeNotFound(errors.New("notebook not found"))
}

// DeleteTrashedNotebook permanently deletes a notebook in the trash with its
// notes.
func (s *Notes) DeleteTrashedNotebook(ctx context.Context, p *gennotes.DeleteTrashedNotebookPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.DeleteTrashedNotebook(p.ID); err != nil {
		return mapErr(err)
	}
	_, err := s.sync(ctx)
	return err
}

// EmptyTrash permanently deletes everything in the trash. The sync after it
// drops the notes from the index, tells the clients and forgets their
// history, as for notes deleted elsewhere.
func (s *Notes) EmptyTrash(ctx context.Context, _ *gennotes.EmptyTrashPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	if err := requireVerified(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.EmptyTrash(); err != nil {
		return err
	}
	_, err := s.sync(ctx)
	return err
}

// ensureNotebookFromTrash makes sure the notebook that notebook id in the
// trash goes back to exists, for a note restored from id: it is recreated
// with the settings kept in the trash, and an existing notebook must have the
// same kind, so that a note never becomes visible to AI clients by being
// restored. A sub notebook is recreated only in its parent, whose kind it
// takes. The caller holds s.mu.
func (s *Notes) ensureNotebookFromTrash(id string) error {
	trashed, err := s.store.TrashedNotebookMeta(id)
	if err != nil {
		return mapErr(err)
	}
	all, err := s.store.Notebooks()
	if err != nil {
		return err
	}
	nb := store.RestoredNotebook(id)
	if !slices.Contains(all, nb) {
		if parent, sub := store.SplitNotebook(nb); sub != "" {
			if !slices.Contains(all, parent) {
				return mapErr(fmt.Errorf("%w: %s", store.ErrNoParent, parent))
			}
			pm, err := s.store.NotebookMeta(parent)
			if err != nil {
				return err
			}
			if pm.Kind != trashed.Kind {
				return gennotes.MakeBadRequest(fmt.Errorf("notebook %s has the kind %s, but the note comes from a sub notebook of the kind %s; change the kind back before restoring the note", parent, apiKind(pm.Kind), apiKind(trashed.Kind)))
			}
		}
		return mapErr(s.store.SaveNotebookMeta(nb, trashed))
	}
	cur, err := s.store.NotebookMeta(nb)
	if err != nil {
		return err
	}
	if cur.Kind != trashed.Kind {
		return gennotes.MakeBadRequest(fmt.Errorf("notebook %s exists with the kind %s, but the note comes from a notebook of the kind %s; restore the whole notebook from the trash after renaming or deleting the existing one", nb, apiKind(cur.Kind), apiKind(trashed.Kind)))
	}
	return nil
}

// Notebook kinds in the API; store.NotebookMeta keeps "" for the default.
const kindDefault = "default"

func storeKind(kind string) string {
	switch kind {
	case store.KindCollab, store.KindDrop, store.KindPrivate:
		return kind
	}
	return ""
}

func apiKind(kind string) string {
	switch kind {
	case store.KindCollab, store.KindDrop, store.KindPrivate:
		return kind
	}
	return kindDefault
}

// CreateAiNotebook creates a new notebook of the collab kind for an AI client. AI
// clients cannot turn existing notebooks into collab notebooks.
func (s *Notes) CreateAiNotebook(ctx context.Context, p *gennotes.CreateAiNotebookPayload) (*gennotes.Notebook, error) {
	return s.createNotebook(ctx, &gennotes.CreateNotebookPayload{Name: p.Name, Kind: store.KindCollab})
}

// MakeNotebookPrivate hides a notebook from AI clients. They may do it to any
// notebook in their notebook list, since it only takes access away from them;
// only people can undo it.
func (s *Notes) MakeNotebookPrivate(ctx context.Context, p *gennotes.MakeNotebookPrivatePayload) error {
	ok, err := s.listed(ctx, p.ID)
	if err != nil {
		return err
	}
	all, err := s.store.Notebooks()
	if err != nil {
		return err
	}
	if !ok || !slices.Contains(all, p.ID) {
		return gennotes.MakeNotFound(fmt.Errorf("notebook %s not found", p.ID))
	}
	if p.ID == store.Trash {
		return gennotes.MakeBadRequest(errors.New("the trash keeps the default kind; it is hidden from AI clients anyway"))
	}
	if parent, sub := store.SplitNotebook(p.ID); sub != "" {
		return gennotes.MakeBadRequest(fmt.Errorf("a sub notebook takes the kind of its notebook; make %s private instead", parent))
	}
	kind := store.KindPrivate
	_, err = s.saveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: p.ID, Kind: &kind})
	return err
}

// requireAINotebook returns forbidden unless nb is a notebook of the collab kind,
// the only notebooks whose notes AI clients may write.
func (s *Notes) requireAINotebook(nb string) error {
	return s.requireKind(nb, store.KindCollab)
}

// requireKind returns forbidden unless nb is a notebook of one of the kinds.
func (s *Notes) requireKind(nb string, kinds ...string) error {
	if !store.ValidNotebook(nb) {
		return gennotes.MakeBadRequest(store.ErrInvalidNotebook)
	}
	m, err := s.store.NotebookMeta(nb)
	if err != nil {
		return err
	}
	// The kind is read from the notebook's _notebook.yaml, so a missing
	// notebook is of the default kind too.
	if !slices.Contains(kinds, m.Kind) {
		return gennotes.MakeForbidden(fmt.Errorf("notebook %s is not shared with AI: AI clients may only write notes in notebooks of the collab kind, or add them to notebooks of the drop kind", nb))
	}
	return nil
}

// CreateAiNote creates a note for an AI client, marked as created by AI, in a
// notebook of the collab or drop kind. In a drop notebook the ID is always random:
// a chosen ID that is taken would tell AI clients that a note they cannot see
// exists.
func (s *Notes) CreateAiNote(ctx context.Context, p *gennotes.CreateAiNotePayload) (*gennotes.Note, error) {
	if err := s.requireKind(p.Notebook, store.KindCollab, store.KindDrop); err != nil {
		return nil, err
	}
	if p.ID != nil {
		if m, err := s.store.NotebookMeta(p.Notebook); err != nil {
			return nil, err
		} else if m.Kind == store.KindDrop {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("notebook %s is of the drop kind: omit the id, which is chosen at random", p.Notebook))
		}
	}
	return s.create(ctx, &gennotes.CreatePayload{ID: p.ID, Notebook: &p.Notebook, Title: p.Title, Tags: p.Tags, Body: p.Body}, store.CreatedByAI)
}

// UpdateAiNote updates a note for an AI client. The note must be in a notebook
// of the collab kind and may only move to another one, and the version must match.
func (s *Notes) UpdateAiNote(ctx context.Context, p *gennotes.UpdateAiNotePayload) (*gennotes.Note, error) {
	n, err := s.Get(ctx, &gennotes.GetPayload{ID: p.ID})
	if err != nil {
		return nil, err
	}
	if err := s.requireAINotebook(n.Notebook); err != nil {
		return nil, err
	}
	if p.Notebook != nil && *p.Notebook != n.Notebook {
		if err := s.requireAINotebook(*p.Notebook); err != nil {
			return nil, err
		}
	}
	return s.updateNote(ctx, &gennotes.UpdatePayload{ID: p.ID, Notebook: p.Notebook, Title: p.Title, Tags: p.Tags, Body: p.Body, IfMatch: &p.Version})
}

// DeleteAiNote moves a note in a notebook of the collab kind to the trash for an AI
// client, if the version matches. People can restore it from the trash.
func (s *Notes) DeleteAiNote(ctx context.Context, p *gennotes.DeleteAiNotePayload) error {
	n, err := s.Get(ctx, &gennotes.GetPayload{ID: p.ID})
	if err != nil {
		return err
	}
	if err := s.requireAINotebook(n.Notebook); err != nil {
		return err
	}
	return s.deleteNote(ctx, &gennotes.DeletePayload{ID: p.ID, IfMatch: &p.Version})
}

// MoveAiNote reorders a note in a notebook of the collab kind for an AI
// client, as Move does. Notes hidden from AI clients are not found, and notes
// of other kinds are forbidden, so that AI clients do not rearrange the notes
// of people. No version is needed: only the order of the note changes, on
// the note as read when it is written.
func (s *Notes) MoveAiNote(ctx context.Context, p *gennotes.MoveAiNotePayload) (*gennotes.Note, error) {
	n, err := s.Get(ctx, &gennotes.GetPayload{ID: p.ID})
	if err != nil {
		return nil, err
	}
	if err := s.requireAINotebook(n.Notebook); err != nil {
		return nil, err
	}
	return s.moveNote(ctx, &gennotes.MovePayload{ID: p.ID, After: p.After})
}

// notebooks lists the notebooks: inbox first, then the others by order, name
// and ID, each followed by its sub notebooks in the same order, and trash
// last.
func (s *Notes) notebooks(ctx context.Context) ([]*gennotes.Notebook, error) {
	ids, err := s.store.Notebooks()
	if err != nil {
		return nil, err
	}
	counts, err := s.index.Counts(ctx)
	if err != nil {
		return nil, err
	}
	var inbox, trash *gennotes.Notebook
	var others []*gennotes.Notebook
	subs := map[string][]*gennotes.Notebook{}
	for _, id := range ids {
		m, err := s.store.NotebookMeta(id)
		if err != nil {
			m = store.NotebookMeta{Kind: store.DefaultKind(id)} // a broken settings file falls back to the defaults
		}
		nb := &gennotes.Notebook{ID: id, Name: m.Name, Order: m.Order, Count: counts[id], Kind: apiKind(m.Kind), Sort: cmp.Or(m.Sort, store.SortOrder)}
		if parent, sub := store.SplitNotebook(id); sub != "" {
			nb.Parent = &parent
			if nb.Name == "" {
				nb.Name = store.NotebookName(id)
			}
			subs[parent] = append(subs[parent], nb)
			continue
		}
		switch id {
		case store.Inbox:
			nb.Reserved, nb.Order, inbox = true, 0, nb
			if nb.Name == "" {
				nb.Name = "Inbox"
			}
		case store.Trash:
			nb.Reserved, nb.Order, trash = true, 0, nb
			if nb.Name == "" {
				nb.Name = "Trash"
			}
		default:
			if nb.Name == "" {
				nb.Name = store.NotebookName(id) // the decoded name of a Punycode ID
			}
			others = append(others, nb)
		}
	}
	byOrder := func(a, b *gennotes.Notebook) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
	}
	slices.SortFunc(others, byOrder)
	res := make([]*gennotes.Notebook, 0, len(ids))
	if inbox != nil {
		res = append(res, inbox)
	}
	for _, nb := range others {
		res = append(res, nb)
		slices.SortFunc(subs[nb.ID], byOrder)
		res = append(res, subs[nb.ID]...)
	}
	if trash != nil {
		res = append(res, trash)
	}
	return res, nil
}

// notifyNotebooks publishes a notebooks event when the notebook list, names
// or order differ from the last known state. The first call only records it.
func (s *Notes) notifyNotebooks(nbs []*gennotes.Notebook) {
	var b strings.Builder
	for _, nb := range nbs {
		fmt.Fprintf(&b, "%s\x00%s\x00%d\x00%s\x00%s\n", nb.ID, nb.Name, nb.Order, nb.Kind, nb.Sort)
	}
	trashed, _ := s.store.TrashedNotebooks()
	fmt.Fprintf(&b, "trash\x00%s\n", strings.Join(trashed, "\x00"))
	state := b.String()
	if s.nbState != "" && state != s.nbState {
		s.events.publishAll(eventNotebooks)
	}
	s.nbState = state
}

func (s *Notes) Search(ctx context.Context, p *gennotes.SearchPayload) (*gennotes.SearchResult, error) {
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	if slices.Contains(hidden, deref(p.Notebook)) {
		return &gennotes.SearchResult{Mode: string(s.index.Mode()), Total: 0, Notes: []*gennotes.NoteSummary{}}, nil
	}
	res, total, err := s.index.Search(ctx, p.Q, deref(p.Notebook), deref(p.Tag), p.Limit, hidden...)
	if err != nil {
		return nil, err
	}
	return &gennotes.SearchResult{Mode: string(s.index.Mode()), Total: total, Notes: summaries(res, true)}, nil
}

func (s *Notes) Sync(ctx context.Context, _ *gennotes.SyncPayload) (*gennotes.SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sync(ctx)
}

// sync updates the index from the files and notifies subscribers. The caller
// holds s.mu.
func (s *Notes) sync(ctx context.Context) (*gennotes.SyncResult, error) {
	// Take in the copies of notes left by file sync services first.
	repairs, err := s.store.Repair()
	for _, r := range repairs {
		if r.Note == "" {
			slog.Info("removed a copy of a note, the same as the note", "file", r.Path)
		} else {
			slog.Info("made a note of a copy left by file sync", "file", r.Path, "note", r.Note)
		}
	}
	if err != nil {
		slog.Warn("cannot repair the copies of notes", "error", err)
	}
	r, err := s.index.Sync(ctx, s.store)
	if err != nil {
		return nil, err
	}
	s.events.publish(eventCreated, r.Added...)
	s.events.publish(eventUpdated, r.Updated...)
	s.events.publish(eventDeleted, r.Removed...)
	s.forgetHistory(r.Removed...) // deleted for good, here or elsewhere
	if nbs, err := s.notebooks(ctx); err == nil {
		s.notifyNotebooks(nbs) // picks up edits of _notebook.yaml and new or removed notebooks
	}
	return &gennotes.SyncResult{Added: len(r.Added), Updated: len(r.Updated), Removed: len(r.Removed), Errors: r.Errors}, nil
}

// load reads a note and checks ifMatch against its version.
func (s *Notes) load(id string, ifMatch *string) (*store.Note, error) {
	n, err := s.store.Load(id)
	if err != nil {
		return nil, mapErr(err)
	}
	if ifMatch != nil && !store.MatchVersion(*ifMatch, n.Version) {
		return nil, gennotes.MakePreconditionFailed(errVersionMismatch)
	}
	return n, nil
}

// write saves n if edited, moves it to the notebook to, updates the index and
// notifies subscribers.
func (s *Notes) write(ctx context.Context, n *store.Note, edited bool, to string) error {
	if edited {
		if err := s.store.Save(n); err != nil {
			return mapErr(err)
		}
	}
	if to != n.Notebook {
		if err := s.store.Move(n.ID, to); err != nil {
			return mapErr(err)
		}
		n.Notebook, n.TrashedNotebook = to, "" // out of any notebook in the trash
	}
	if err := s.reindex(ctx, n); err != nil {
		return err
	}
	s.events.publish(eventUpdated, n.ID)
	return nil
}

func (s *Notes) reindex(ctx context.Context, n *store.Note) error {
	f, err := s.store.Stat(n.ID)
	if err != nil {
		return err
	}
	return s.index.Upsert(ctx, n, f)
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrInvalidID):
		return gennotes.MakeNotFound(err)
	case errors.Is(err, store.ErrExists):
		return gennotes.MakeAlreadyExists(err)
	case errors.Is(err, store.ErrNotebookExists):
		return gennotes.MakeAlreadyExists(err)
	case errors.Is(err, store.ErrInvalidNotebook), errors.Is(err, store.ErrDuplicate), errors.Is(err, store.ErrReserved), errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNoParent), errors.Is(err, store.ErrHasSubs):
		return gennotes.MakeBadRequest(err)
	}
	return err
}

func toNote(n *store.Note) *gennotes.Note {
	res := &gennotes.Note{
		ID:        n.ID,
		Notebook:  n.Notebook,
		Title:     n.Title,
		Tags:      tags(n.Tags),
		Body:      n.Body,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
		Version:   n.Version,
	}
	if n.TrashedFrom != "" {
		res.TrashedFrom = &n.TrashedFrom
	}
	if n.TrashedNotebook != "" {
		res.TrashedNotebook = &n.TrashedNotebook
	}
	res.CreatedBy = createdBy(n.CreatedBy)
	return res
}

// createdBy returns the created_by of the API: only the known value.
func createdBy(v string) *string {
	if v != store.CreatedByAI {
		return nil
	}
	return &v
}

func summaries(ss []*index.Summary, withSnippet bool) []*gennotes.NoteSummary {
	res := make([]*gennotes.NoteSummary, 0, len(ss))
	for _, s := range ss {
		r := &gennotes.NoteSummary{
			ID:        s.ID,
			Notebook:  s.Notebook,
			Title:     s.Title,
			Tags:      tags(s.Tags),
			CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if s.TrashedNotebook != "" {
			r.TrashedNotebook = &s.TrashedNotebook
		}
		r.CreatedBy = createdBy(s.CreatedBy)
		if withSnippet {
			r.Snippet = &s.Snippet
			r.TitleHighlights = textRanges(s.TitleHighlights)
			r.SnippetHighlights = textRanges(s.SnippetHighlights)
		}
		res = append(res, r)
	}
	return res
}

func textRanges(spans []index.Span) []*gennotes.TextRange {
	res := make([]*gennotes.TextRange, len(spans))
	for i, sp := range spans {
		res[i] = &gennotes.TextRange{Start: sp.Start, End: sp.End}
	}
	return res
}

func tags(t []string) []string {
	if t == nil {
		return []string{}
	}
	return t
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
