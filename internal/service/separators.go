package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/store"
)

// Separators are bars between the notes of a notebook in the manual order,
// kept in the notebook's _separators.yaml (see store.Separator). Their keys
// are in the same order as those of the notes, so Move and the methods here
// put notes and separators among each other. People arrange them with the
// methods here, and AI clients in notebooks of the collab kind with the
// *_ai_separator tools.

func (s *Notes) CreateSeparator(ctx context.Context, p *gennotes.CreateSeparatorPayload) (*gennotes.Separator, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.createSeparator(ctx, p.Notebook, sepText{p.Title, p.Date, p.Color}, p.After)
}

func (s *Notes) UpdateSeparator(ctx context.Context, p *gennotes.UpdateSeparatorPayload) (*gennotes.Separator, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.updateSeparator(ctx, p.Notebook, p.ID, sepText{p.Title, p.Date, p.Color})
}

func (s *Notes) DeleteSeparator(ctx context.Context, p *gennotes.DeleteSeparatorPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	return s.deleteSeparator(ctx, p.Notebook, p.ID)
}

func (s *Notes) MoveSeparator(ctx context.Context, p *gennotes.MoveSeparatorPayload) (*gennotes.Separator, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.moveSeparator(ctx, p.Notebook, p.ID, p.After)
}

func (s *Notes) CreateAiSeparator(ctx context.Context, p *gennotes.CreateAiSeparatorPayload) (*gennotes.Separator, error) {
	if err := s.requireAISeparators(ctx, p.Notebook); err != nil {
		return nil, err
	}
	return s.createSeparator(ctx, p.Notebook, sepText{p.Title, p.Date, p.Color}, p.After)
}

func (s *Notes) UpdateAiSeparator(ctx context.Context, p *gennotes.UpdateAiSeparatorPayload) (*gennotes.Separator, error) {
	if err := s.requireAISeparators(ctx, p.Notebook); err != nil {
		return nil, err
	}
	return s.updateSeparator(ctx, p.Notebook, p.ID, sepText{p.Title, p.Date, p.Color})
}

func (s *Notes) DeleteAiSeparator(ctx context.Context, p *gennotes.DeleteAiSeparatorPayload) error {
	if err := s.requireAISeparators(ctx, p.Notebook); err != nil {
		return err
	}
	return s.deleteSeparator(ctx, p.Notebook, p.ID)
}

func (s *Notes) MoveAiSeparator(ctx context.Context, p *gennotes.MoveAiSeparatorPayload) (*gennotes.Separator, error) {
	if err := s.requireAISeparators(ctx, p.Notebook); err != nil {
		return nil, err
	}
	return s.moveSeparator(ctx, p.Notebook, p.ID, p.After)
}

// requireAISeparators returns not_found unless notebook nb exists and AI
// clients can see it, and forbidden unless it is of the collab kind, as for
// the notes in it.
func (s *Notes) requireAISeparators(ctx context.Context, nb string) error {
	if err := s.requireNotebook(nb); err != nil {
		return err
	}
	if ok, err := s.visible(ctx, nb); err != nil {
		return err
	} else if !ok {
		return gennotes.MakeNotFound(fmt.Errorf("notebook %s not found", nb))
	}
	return s.requireAINotebook(nb)
}

// requireNotebook returns not_found unless notebook nb exists.
func (s *Notes) requireNotebook(nb string) error {
	ids, err := s.store.Notebooks()
	if err != nil {
		return err
	}
	if !slices.Contains(ids, nb) {
		return gennotes.MakeNotFound(fmt.Errorf("notebook %s not found", nb))
	}
	return nil
}

// sepText is what a payload gives a separator to show.
type sepText struct{ title, date, color *string }

func (s *Notes) createSeparator(ctx context.Context, nb string, text sepText, after *string) (*gennotes.Separator, error) {
	if nb == store.Trash {
		return nil, gennotes.MakeBadRequest(errors.New("notes in the trash have no manual order"))
	}
	if err := s.requireNotebook(nb); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, seps, err := s.manualOrder(ctx, nb)
	if err != nil {
		return nil, err
	}
	pos, err := afterPos(items, deref(after), nb)
	if err != nil {
		return nil, err
	}
	sep := store.Separator{ID: store.NewSeparatorID(), Title: strings.TrimSpace(deref(text.title)), Date: deref(text.date), Color: deref(text.color), Order: s.now().UnixMilli()}
	for slices.ContainsFunc(items, func(k index.SortKey) bool { return k.ID == sep.ID }) {
		sep.ID = store.NewSeparatorID()
	}
	// Saved even when it keeps the key it starts with: put first a second
	// after the first note was made, both are the time of this second.
	seps = append(seps, sep)
	if err := s.applyKeys(ctx, nb, seps, place(items, pos, sep.ID, sep.Order), true); err != nil {
		return nil, err
	}
	return s.separator(ctx, nb, sep.ID)
}

func (s *Notes) updateSeparator(ctx context.Context, nb, id string, text sepText) (*gennotes.Separator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seps, i, err := s.findSeparator(nb, id)
	if err != nil {
		return nil, err
	}
	seps[i].Title, seps[i].Date, seps[i].Color = strings.TrimSpace(deref(text.title)), deref(text.date), deref(text.color)
	if err := s.saveSeparators(ctx, nb, seps); err != nil {
		return nil, err
	}
	return s.separator(ctx, nb, id)
}

func (s *Notes) deleteSeparator(ctx context.Context, nb, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seps, i, err := s.findSeparator(nb, id)
	if err != nil {
		return err
	}
	return s.saveSeparators(ctx, nb, slices.Delete(seps, i, i+1))
}

func (s *Notes) moveSeparator(ctx context.Context, nb, id string, after *string) (*gennotes.Separator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.findSeparator(nb, id); err != nil {
		return nil, err
	}
	items, seps, err := s.manualOrder(ctx, nb)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(items, func(k index.SortKey) bool { return k.ID == id })
	cur := items[i].Key
	items = slices.Delete(items, i, i+1)
	pos, err := afterPos(items, deref(after), nb)
	if err != nil {
		return nil, err
	}
	if err := s.applyKeys(ctx, nb, seps, place(items, pos, id, cur), false); err != nil {
		return nil, err
	}
	return s.separator(ctx, nb, id)
}

// findSeparator reads the separators of notebook nb and returns them with the
// index of the one with ID id, or not_found.
func (s *Notes) findSeparator(nb, id string) ([]store.Separator, int, error) {
	notFound := gennotes.MakeNotFound(fmt.Errorf("separator %s not found in notebook %s", id, nb))
	if nb == store.Trash || !store.ValidNotebook(nb) {
		return nil, 0, notFound
	}
	seps, err := s.store.Separators(nb)
	if err != nil {
		return nil, 0, err
	}
	i := slices.IndexFunc(seps, func(sep store.Separator) bool { return sep.ID == id })
	if i < 0 {
		return nil, 0, notFound
	}
	return seps, i, nil
}

// saveSeparators writes the separators of notebook nb and notifies
// subscribers. The caller holds s.mu.
func (s *Notes) saveSeparators(ctx context.Context, nb string, seps []store.Separator) error {
	if err := s.store.SaveSeparators(nb, seps); err != nil {
		return mapErr(err)
	}
	if nbs, err := s.notebooks(ctx); err == nil {
		s.notifyNotebooks(nbs) // the separators are part of the state
	}
	return nil
}

// separator returns the separator with ID id of notebook nb, with the note it
// comes before.
func (s *Notes) separator(ctx context.Context, nb, id string) (*gennotes.Separator, error) {
	seps, err := s.separatorsOf(ctx, nb, index.Archive{Mode: index.ArchiveInclude}, "")
	if err != nil {
		return nil, err
	}
	for _, sep := range seps {
		if sep.ID == id {
			return sep, nil
		}
	}
	return nil, gennotes.MakeNotFound(fmt.Errorf("separator %s not found in notebook %s", id, nb))
}

// separatorsOf returns the separators of notebook nb in the manual order,
// each with the note it comes right before among those listed by a and the
// filter q (see index.Filter).
func (s *Notes) separatorsOf(ctx context.Context, nb string, a index.Archive, q string) ([]*gennotes.Separator, error) {
	// The notes listed: the separators go before them, or last.
	var hidden func(id string) bool
	switch {
	case a.Mode == index.ArchiveOnly, a.Mode != index.ArchiveInclude && slices.Contains(a.Notebooks, nb):
		return []*gennotes.Separator{}, nil // a list of the archive is not in the manual order
	case strings.TrimSpace(q) != "":
		// Every separator stays, before the next note listed.
		shown, err := s.index.Matching(ctx, q, nb, a)
		if err != nil {
			return nil, err
		}
		hidden = func(id string) bool { return !shown[id] }
	case a.Mode != index.ArchiveInclude:
		archived, err := s.index.ArchivedIn(ctx, nb)
		if err != nil {
			return nil, err
		}
		hidden = func(id string) bool { return archived[id] }
	default:
		hidden = func(string) bool { return false }
	}
	items, seps, err := s.manualOrder(ctx, nb)
	if err != nil || len(seps) == 0 {
		return []*gennotes.Separator{}, err
	}
	isSep := map[string]store.Separator{}
	for _, sep := range seps {
		isSep[sep.ID] = sep
	}
	res := []*gennotes.Separator{}
	var pending []*gennotes.Separator // separators before the next note
	for _, k := range items {
		sep, ok := isSep[k.ID]
		if !ok && hidden(k.ID) {
			continue
		}
		if !ok {
			for _, p := range pending {
				p.Before = ptr(k.ID)
			}
			pending = nil
			continue
		}
		g := &gennotes.Separator{ID: sep.ID, Notebook: nb}
		if sep.Title != "" {
			g.Title = ptr(sep.Title)
		}
		if sep.Date != "" {
			g.Date = ptr(sep.Date)
		}
		if sep.Color != "" {
			g.Color = ptr(sep.Color)
		}
		res = append(res, g)
		pending = append(pending, g)
	}
	return res, nil
}

// separatorsState describes the separators of the notebooks nbs, so that a
// change of them made elsewhere, as in the file, gives a notebooks event.
func (s *Notes) separatorsState(nbs []*gennotes.Notebook) string {
	var b strings.Builder
	for _, nb := range nbs {
		seps, _ := s.store.Separators(nb.ID)
		for _, sep := range seps {
			fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00%s\x00%d\n", nb.ID, sep.ID, sep.Title, sep.Date, sep.Color, sep.Order)
		}
	}
	return b.String()
}
