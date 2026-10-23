package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	tea "charm.land/bubbletea/v2"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	genauth "github.com/ikawaha/ebira/gen/auth"
	assistantcli "github.com/ikawaha/ebira/gen/http/assistant/client"
	authcli "github.com/ikawaha/ebira/gen/http/auth/client"
	notescli "github.com/ikawaha/ebira/gen/http/notes/client"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/authclient"
)

// Clients holds the API clients of the services the UI uses.
type Clients struct {
	Notes     *gennotes.Client
	Assistant *genassistant.Client
	Auth      *genauth.Client
	// Web is the URL of the web UI, where people sign in with a passkey:
	// on localhost, which passkeys belong to.
	Web string
	// session is sent with every request once the TUI is signed in, through
	// a device flow approved in the web UI (see internal/authclient).
	session *authclient.Cookie
}

// SetSession makes the clients send session with every request.
func (c *Clients) SetSession(session string) {
	if c.session != nil {
		c.session.Set(session)
	}
}

// NewClient returns the API clients for the server at rawURL, such as
// http://localhost:13579.
func NewClient(rawURL string) (*Clients, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid server URL %q", rawURL)
	}
	session := authclient.NewCookie()
	hc := &http.Client{Transport: session}
	c := notescli.NewClient(u.Scheme, u.Host, hc, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	a := assistantcli.NewClient(u.Scheme, u.Host, hc, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	au := authcli.NewClient(u.Scheme, u.Host, hc, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	web := u.Scheme + "://localhost/"
	if p := u.Port(); p != "" {
		web = u.Scheme + "://localhost:" + p + "/"
	}
	return &Clients{
		Notes: gennotes.NewClient(c.List(), c.Get(), c.Create(), c.Update(), c.Delete(), c.MoveNotes(), c.TrashNotes(), c.Search(), c.Restore(), c.Move(), c.Notebooks(), c.Tags(), c.Marks(), c.SetMarks(), c.SaveMarkNames(), c.CreateNotebook(), c.SaveNotebook(), c.MoveNotebook(), c.DeleteNotebook(), c.TrashedNotebooks(), c.RestoreNotebook(), c.DeleteTrashedNotebook(), c.EmptyTrash(), c.Import(),
			nil, nil, nil, nil, nil, nil, nil, nil, // the methods for AI clients have no HTTP endpoints
			c.AddFile(), c.GetFile(), c.DeleteFile(), c.History(), c.HistoryVersion(), c.CheckFiles(), c.Events(), c.GitStatus(), c.GitSync(), c.PreviewStyle(), c.SetPreviewStyle(), c.Sync()),
		Assistant: genassistant.NewClient(a.Status(), a.Ask()),
		Auth:      genauth.NewClient(au.Status(), au.RegisterBegin(), au.RegisterFinish(), au.LoginBegin(), au.LoginFinish(), au.DeviceStart(), au.DeviceToken(), au.DeviceApprove(), au.Passkeys(), au.DeletePasskey(), au.RevokeSessions(), au.Logout()),
		Web:       web,
		session:   session,
	}, nil
}

// eventMsg carries a note event from the server.
type eventMsg struct{ ev *gennotes.NoteEvent }

// connMsg reports the state of the event stream.
type connMsg struct {
	live bool
	err  error
}

// WatchEvents subscribes to the server's event stream and forwards events to
// send until ctx is done, reconnecting with backoff when the stream breaks.
func WatchEvents(ctx context.Context, api *gennotes.Client, send func(tea.Msg)) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := readEvents(ctx, api, func(ev *gennotes.NoteEvent) {
			if ev.Type == "ready" {
				backoff = time.Second
				send(connMsg{live: true})
			}
			send(eventMsg{ev: ev})
		})
		if ctx.Err() != nil {
			return
		}
		send(connMsg{live: false, err: err})
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}

func readEvents(ctx context.Context, api *gennotes.Client, fn func(*gennotes.NoteEvent)) error {
	stream, err := api.Events(ctx, &gennotes.EventsPayload{})
	if err != nil {
		return err
	}
	if c, ok := stream.(io.Closer); ok {
		defer c.Close()
	}
	for {
		ev, err := stream.RecvWithContext(ctx)
		if err != nil {
			return err
		}
		fn(ev)
	}
}

// errorName returns the name of a Goa service error, such as
// "precondition_failed", or "" for other errors.
func errorName(err error) string {
	var se *goa.ServiceError
	if errors.As(err, &se) {
		return se.Name
	}
	return ""
}

// errorMessage returns the message of a service error, or err as text.
func errorMessage(err error) string {
	var se *goa.ServiceError
	if errors.As(err, &se) {
		return se.Message
	}
	return err.Error()
}
