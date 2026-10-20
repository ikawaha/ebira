// Package authclient signs in the clients of the server that have no
// browser, the TUI and the commands, while the server requires a passkey
// (auth: passkey): people approve them in the web UI, signed in with a
// passkey (a device flow, see internal/auth), and the session they get is
// kept in memory only, so that other programs, such as AI agents, cannot
// read it from a file.
package authclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	genauth "github.com/ikawaha/ebira/gen/auth"
)

// CookieName is the name of the session cookie (see the design).
const CookieName = "ebira_session"

// Cookie is an http.RoundTripper that adds the session cookie to the
// requests, once set.
type Cookie struct {
	Base  http.RoundTripper
	value atomic.Pointer[string]
}

// NewCookie returns a Cookie over http.DefaultTransport.
func NewCookie() *Cookie { return &Cookie{Base: http.DefaultTransport} }

// Set makes the requests carry session.
func (c *Cookie) Set(session string) { c.value.Store(&session) }

func (c *Cookie) RoundTrip(r *http.Request) (*http.Response, error) {
	if v := c.value.Load(); v != nil {
		r = r.Clone(r.Context())
		r.AddCookie(&http.Cookie{Name: CookieName, Value: *v})
	}
	return c.Base.RoundTrip(r)
}

// ErrNoPasskey is a server that requires a passkey without one registered,
// so that nobody can approve a sign-in.
var ErrNoPasskey = errors.New("the server requires a passkey (auth: passkey), but none is registered: register one with ebira auth passkey, or restart the server with -auth=none")

// SignIn signs in through the web UI when the server requires a passkey: it
// prints the URL and the code to approve to out, and waits until people
// approve it, or ctx is done. It returns the session, or "" when the server
// does not require one.
func SignIn(ctx context.Context, api *genauth.Client, out io.Writer) (string, error) {
	st, err := api.Status(ctx, &genauth.StatusPayload{})
	if err != nil {
		return "", nil // an earlier server, without passkeys
	}
	if st.Mode != "passkey" || st.SignedIn {
		return "", nil
	}
	if !st.Registered {
		return "", ErrNoPasskey
	}
	d, err := api.DeviceStart(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot begin the sign-in: %w", err)
	}
	fmt.Fprintf(out, "The server requires a passkey. To run this command, open this URL in the web UI, signed in with a passkey, and approve the code:\n  %s\nCode (check that the web UI shows the same): %s\nWaiting for the approval… (Ctrl+C to stop)\n", d.VerificationURI, d.UserCode)
	every := time.Duration(d.Interval) * time.Second
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(every):
		}
		res, err := api.DeviceToken(ctx, &genauth.DeviceTokenPayload{DeviceCode: d.DeviceCode})
		switch {
		case err != nil:
			continue // the server may be restarting
		case res.Status == "approved" && res.Session != nil:
			fmt.Fprintln(out, "Signed in.")
			return *res.Session, nil
		case res.Status == "expired":
			return "", errors.New("the sign-in expired before it was approved; run the command again")
		}
	}
}
