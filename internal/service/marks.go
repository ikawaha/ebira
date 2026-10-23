package service

import (
	"context"
	"fmt"
	"maps"
	"slices"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/store"
)

// Marks of notes: a note has one at most for now, from store.MarkIDs, and the
// marks have the names people gave them in the settings of the notes. Setting
// marks changes no content: the update time stays and no earlier version is
// kept, as for the manual order.

// symbolMarks are the marks the UIs draw as emoji; the others are colors.
var symbolMarks = []string{"star", "important", "question", "done", "pin", "urgent", "idea"}

// SetMarkNames sets the names of the marks, read from the settings of the
// notes (store.Config.Marks), for the API and for searches by name. Call it
// before the service is used.
func (s *Notes) SetMarkNames(names map[string]string) {
	clean := map[string]string{}
	for id, name := range names {
		if store.ValidMark(id) && name != "" {
			clean[id] = name
		}
	}
	s.mu.Lock()
	s.markNames = clean
	s.mu.Unlock()
	s.index.SetMarkNames(clean)
}

// Marks lists the marks with their names.
func (s *Notes) Marks(ctx context.Context, _ *gennotes.MarksPayload) ([]*gennotes.MarkInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markInfos(), nil
}

func (s *Notes) markInfos() []*gennotes.MarkInfo {
	res := make([]*gennotes.MarkInfo, 0, len(store.MarkIDs))
	for _, id := range store.MarkIDs {
		m := &gennotes.MarkInfo{ID: id, Kind: "color"}
		if slices.Contains(symbolMarks, id) {
			m.Kind = "symbol"
		}
		if name := s.markNames[id]; name != "" {
			m.Name = &name
		}
		res = append(res, m)
	}
	return res
}

// SetMarks sets the marks of a note, for people.
func (s *Notes) SetMarks(ctx context.Context, p *gennotes.SetMarksPayload) (*gennotes.Note, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.setMarks(ctx, p.ID, p.Marks)
}

// setMarks sets the marks of note id, keeping its update time.
func (s *Notes) setMarks(ctx context.Context, id string, marks []string) (*gennotes.Note, error) {
	for _, m := range marks {
		if !store.ValidMark(m) {
			return nil, gennotes.MakeBadRequest(fmt.Errorf("unknown mark %q", m))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.load(id, nil)
	if err != nil {
		return nil, err
	}
	if n.Notebook == store.Trash {
		return nil, gennotes.MakeBadRequest(fmt.Errorf("notes in the trash have no marks to set"))
	}
	marks = store.CleanMarks(marks)
	if slices.Equal(marks, store.CleanMarks(n.Marks)) {
		return toNote(n), nil
	}
	n.Marks = marks
	if err := s.write(ctx, n, true, n.Notebook); err != nil {
		return nil, err
	}
	return toNote(n), nil
}

// SaveMarkNames names the marks, writing the names to the settings of the
// notes, for people.
func (s *Notes) SaveMarkNames(ctx context.Context, p *gennotes.SaveMarkNamesPayload) ([]*gennotes.MarkInfo, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	names := maps.Clone(p.Names)
	maps.DeleteFunc(names, func(id, name string) bool { return !store.ValidMark(id) || name == "" })
	s.mu.Lock()
	err := s.store.SaveMarkNames(names)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.SetMarkNames(names)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markInfos(), nil
}
