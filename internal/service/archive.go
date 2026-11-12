package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/store"
)

// The archive holds notes and notebooks kept but no longer in use. They are
// marked, not moved: a note has archived_at in its file, and a notebook in
// its settings, which takes its notes and sub notebooks in. So the notes keep
// their notebooks, and with them their kinds, which decide what AI clients
// see, and their IDs, files and links. Lists, searches and tags leave the
// archive out unless they ask for it.

// archivedNotebooks returns the notebooks in the archive: those put there,
// and the sub notebooks of those.
func (s *Notes) archivedNotebooks() ([]string, error) {
	ids, err := s.store.Notebooks()
	if err != nil {
		return nil, err
	}
	var res []string
	for _, id := range ids {
		parent, sub := store.SplitNotebook(id)
		if sub != "" && slices.Contains(res, parent) {
			res = append(res, id)
			continue
		}
		if m, err := s.store.NotebookMeta(id); err == nil && m.ArchivedAt != nil && id != store.Inbox && id != store.Trash {
			res = append(res, id)
		}
	}
	return res, nil
}

// archive returns how a list, a search or the tags take the archive, by the
// archived attribute of their payload ("" for the default).
func (s *Notes) archive(mode string) (index.Archive, error) {
	nbs, err := s.archivedNotebooks()
	if err != nil {
		return index.Archive{}, err
	}
	return index.Archive{Mode: mode, Notebooks: nbs}, nil
}

// note returns n for the API, telling whether it is in the archive with its
// notebook too.
func (s *Notes) note(n *store.Note) *gennotes.Note {
	res := toNote(n)
	if res.Archived == nil && n.Notebook != store.Trash {
		if nbs, err := s.archivedNotebooks(); err == nil && slices.Contains(nbs, n.Notebook) {
			res.Archived = ptr(true)
		}
	}
	return res
}

// summaries returns ss for the API, telling which are in the archive by
// their notebooks too (archived, see archivedNotebooks).
func (s *Notes) summaries(ss []*index.Summary, withSnippet bool, archived []string) []*gennotes.NoteSummary {
	res := summaries(ss, withSnippet)
	for _, r := range res {
		if r.Archived == nil && slices.Contains(archived, r.Notebook) {
			r.Archived = ptr(true)
		}
	}
	return res
}

// ArchiveNotes puts notes in the archive or takes them out, checking them all
// first. Like marks, it changes nothing of their content.
func (s *Notes) ArchiveNotes(ctx context.Context, p *gennotes.ArchiveNotesPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	notes := make([]*store.Note, 0, len(p.Ids))
	for _, id := range p.Ids {
		n, err := s.load(id, nil)
		if err != nil {
			return err
		}
		if n.Notebook == store.Trash {
			return gennotes.MakeBadRequest(fmt.Errorf("note %s is in the trash", id))
		}
		notes = append(notes, n)
	}
	now := s.now()
	for _, n := range notes {
		if err := s.setArchived(ctx, n, p.Archived, now); err != nil {
			return err
		}
	}
	return nil
}

// archiveNote puts note id in the archive, or takes it out.
func (s *Notes) archiveNote(ctx context.Context, id string, archived bool) (*gennotes.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.load(id, nil)
	if err != nil {
		return nil, err
	}
	if n.Notebook == store.Trash {
		return nil, gennotes.MakeBadRequest(fmt.Errorf("note %s is in the trash", id))
	}
	if err := s.setArchived(ctx, n, archived, s.now()); err != nil {
		return nil, err
	}
	return s.note(n), nil
}

// setArchived puts note n in the archive at now, or takes it out, if it is
// not so already. The caller holds s.mu.
func (s *Notes) setArchived(ctx context.Context, n *store.Note, archived bool, now time.Time) error {
	if (n.ArchivedAt != nil) == archived {
		return nil
	}
	n.ArchivedAt = nil
	if archived {
		n.ArchivedAt = &now
	}
	return s.write(ctx, n, true, n.Notebook)
}

// ArchiveCandidates lists the notes not updated for days, oldest first, but
// those in the archive and the trash.
func (s *Notes) ArchiveCandidates(ctx context.Context, p *gennotes.ArchiveCandidatesPayload) (*gennotes.NoteList, error) {
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.archive("")
	if err != nil {
		return nil, err
	}
	before := s.now().AddDate(0, 0, -p.Days)
	res, total, err := s.index.Stale(ctx, before, a, p.Limit, p.Offset, hidden...)
	if err != nil {
		return nil, err
	}
	return &gennotes.NoteList{Total: total, Notes: summaries(res, false)}, nil
}

// ArchiveAiNotebook puts a notebook of the collab kind in the archive, or
// takes it out, for AI clients.
func (s *Notes) ArchiveAiNotebook(ctx context.Context, p *gennotes.ArchiveAiNotebookPayload) (*gennotes.Notebook, error) {
	ok, err := s.listed(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	all, err := s.store.Notebooks()
	if err != nil {
		return nil, err
	}
	if !ok || !slices.Contains(all, p.ID) {
		return nil, gennotes.MakeNotFound(fmt.Errorf("notebook %s not found", p.ID))
	}
	if err := s.requireAINotebook(p.ID); err != nil {
		return nil, err
	}
	return s.archiveNotebook(ctx, p.ID, p.Archived)
}

// archiveNotebook puts notebook nb in the archive, or takes it out.
func (s *Notes) archiveNotebook(ctx context.Context, nb string, archived bool) (*gennotes.Notebook, error) {
	if nb == store.Inbox || nb == store.Trash {
		return nil, gennotes.MakeBadRequest(errors.New("the inbox and the trash cannot be put in the archive"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.store.NotebookMeta(nb)
	if err != nil {
		return nil, mapErr(err)
	}
	if (m.ArchivedAt != nil) != archived {
		m.ArchivedAt = nil
		if archived {
			now := s.now()
			m.ArchivedAt = &now
		}
		if err := s.store.SaveNotebookMeta(nb, m); err != nil {
			return nil, mapErr(err)
		}
	}
	nbs, err := s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	s.notifyNotebooks(nbs)
	for _, n := range nbs {
		if n.ID == nb {
			return n, nil
		}
	}
	return nil, gennotes.MakeNotFound(errors.New("notebook not found"))
}
