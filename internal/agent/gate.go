package agent

import (
	"context"
	"errors"

	"goa.design/goa/v3/security"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
)

// Gate is the assistant service of the API, letting in only the requests
// that Session accepts. Unlike the REST API of the notes, it does not mark
// the request as from people: the assistant reads the notes as AI. The
// runtime runs the tools with the context of the request, so a mark there
// would send private notes to the model (TestGateReadsAsAI).
type Gate struct {
	genassistant.Service
	// Session checks the session of a request signed in with a passkey, as
	// service.REST does. Without it (auth: none), every request passes.
	Session func(session string) (valid, verified bool)
}

// APIKeyAuth fails a request without a valid session while passkeys are
// required, so that no one else spends the model API key. It returns ctx as
// it is.
func (g Gate) APIKeyAuth(ctx context.Context, session string, _ *security.APIKeyScheme) (context.Context, error) {
	if g.Session == nil {
		return ctx, nil
	}
	if session != "" {
		if valid, _ := g.Session(session); valid {
			return ctx, nil
		}
	}
	return ctx, genassistant.MakeUnauthorized(errors.New("sign in with a passkey: the session is missing, invalid or expired"))
}
