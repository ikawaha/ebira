package service

import (
	"context"
	"slices"

	goa "goa.design/goa/v3/pkg"

	"github.com/ikawaha/ebira/internal/store"
)

// Requests from people, through the REST API that the UI uses, are marked
// with AsPerson. Every other request, such as from MCP or the assistant, is
// treated as from AI, so a new path that forgets the mark hides private
// notebooks instead of revealing them.

type personKey struct{}

// AsPerson marks ctx as a request from people, who may see every notebook.
func AsPerson(ctx context.Context) context.Context {
	return context.WithValue(ctx, personKey{}, true)
}

func fromPerson(ctx context.Context) bool {
	v, _ := ctx.Value(personKey{}).(bool)
	return v
}

// Person is an endpoint middleware that marks requests as from people. Use it
// only for the endpoints of the REST API.
func Person(e goa.Endpoint) goa.Endpoint {
	return func(ctx context.Context, req any) (any, error) {
		return e(AsPerson(ctx), req)
	}
}

// hiddenNotebooks returns the notebooks whose notes the request cannot see:
// for AI, the trash and the drop and private notebooks; for people, none. A
// notebook whose settings cannot be read is hidden too, to fail on the safe
// side. Drop notebooks still appear in the notebook list (see listed).
func (s *Notes) hiddenNotebooks(ctx context.Context) ([]string, error) {
	if fromPerson(ctx) {
		return nil, nil
	}
	ids, err := s.store.Notebooks()
	if err != nil {
		return nil, err
	}
	hidden := []string{store.Trash}
	for _, id := range ids {
		if m, err := s.store.NotebookMeta(id); err != nil || m.Kind == store.KindPrivate || m.Kind == store.KindDrop {
			hidden = append(hidden, id)
		}
	}
	return hidden, nil
}

// listed reports whether notebook nb appears in the notebook list for the
// request: the visible notebooks and, for AI, the drop notebooks, which they
// need to know to add notes to.
func (s *Notes) listed(ctx context.Context, nb string) (bool, error) {
	if ok, err := s.visible(ctx, nb); err != nil || ok {
		return ok, err
	}
	m, err := s.store.NotebookMeta(nb)
	return err == nil && m.Kind == store.KindDrop, nil
}

// visible reports whether the request can see notebook nb.
func (s *Notes) visible(ctx context.Context, nb string) (bool, error) {
	hidden, err := s.hiddenNotebooks(ctx)
	if err != nil {
		return false, err
	}
	return !slices.Contains(hidden, nb), nil
}
