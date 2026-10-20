package service

import (
	"context"
	"errors"
	"slices"

	"goa.design/goa/v3/security"

	gennotes "github.com/ikawaha/ebira/gen/notes"
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

// peopleOnly fails a request that is not from people, for the methods only
// people may call. They are not MCP tools, but they check the mark
// themselves too, so that a request that reaches them some other way as AI
// cannot change what AI clients may not.
func peopleOnly(ctx context.Context) error {
	if fromPerson(ctx) {
		return nil
	}
	return gennotes.MakeUnauthorized(errors.New("only people can do this: sign in with a passkey"))
}

type verifiedKey struct{}

// requireVerified fails an important operation, such as making a private
// notebook visible to AI, reading the history of a note or emptying the
// trash, for a request with a session that did not sign in with a passkey
// recently: a session left in a browser, or that of the TUI, which signs in
// through the web UI. Without passkeys (auth: none) the request has no
// session, and passes.
func requireVerified(ctx context.Context) error {
	if v, ok := ctx.Value(verifiedKey{}).(bool); ok && !v {
		return gennotes.MakeVerificationRequired(errors.New("sign in with the passkey again to do this"))
	}
	return nil
}

// APIKeyAuth checks the session of a request on the endpoints made from Notes
// itself, which MCP and the assistant use: their requests are from AI, even
// with the session cookie of a browser, which MCP clients pass on. The REST
// API uses REST instead.
func (s *Notes) APIKeyAuth(ctx context.Context, _ string, _ *security.APIKeyScheme) (context.Context, error) {
	return ctx, nil
}

// REST is the service for the endpoints of the REST API, whose requests are
// from people as Session tells.
type REST struct {
	*Notes
	// Session checks the session of a request signed in with a passkey. With
	// it, only a request with a valid session passes, as from people; one
	// without a session or with an invalid one fails, so that AI agents that
	// call the REST API, such as with curl, get nothing. AI clients use MCP,
	// whose endpoints do not check sessions. Without it (auth: none), every
	// request is from people. verified tells whether the session signed in
	// with a passkey recently, which important operations ask for
	// (requireVerified).
	Session func(session string) (valid, verified bool)
}

// APIKeyAuth marks the request as from people by its session.
func (r REST) APIKeyAuth(ctx context.Context, session string, _ *security.APIKeyScheme) (context.Context, error) {
	if r.Session == nil {
		return AsPerson(ctx), nil
	}
	if session != "" {
		if valid, verified := r.Session(session); valid {
			return context.WithValue(AsPerson(ctx), verifiedKey{}, verified), nil
		}
	}
	return ctx, gennotes.MakeUnauthorized(errors.New("sign in with a passkey: the session is missing, invalid or expired"))
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
