package service

import (
	"context"
	"errors"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/guide"
)

// AddGuide adds the guide of ebira as a new notebook, leaving the notebooks
// there as they are. For people only.
func (s *Notes) AddGuide(ctx context.Context, p *gennotes.AddGuidePayload) (*gennotes.Notebook, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := guide.Install(s.store, deref(p.Lang), s.now())
	if err != nil {
		return nil, err
	}
	if _, err := s.sync(ctx); err != nil {
		return nil, err
	}
	nbs, err := s.notebooks(ctx)
	if err != nil {
		return nil, err
	}
	for _, nb := range nbs {
		if nb.ID == r.Notebook {
			return nb, nil
		}
	}
	return nil, errors.New("the guide was added, but its notebook is not found")
}
