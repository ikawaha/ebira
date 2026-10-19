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
	assistantcli "github.com/ikawaha/ebira/gen/http/assistant/client"
	notescli "github.com/ikawaha/ebira/gen/http/notes/client"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// Clients holds the API clients of the services the UI uses.
type Clients struct {
	Notes     *gennotes.Client
	Assistant *genassistant.Client
}

// NewClient returns the API clients for the server at rawURL, such as
// http://localhost:8080.
func NewClient(rawURL string) (*Clients, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid server URL %q", rawURL)
	}
	c := notescli.NewClient(u.Scheme, u.Host, http.DefaultClient, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	a := assistantcli.NewClient(u.Scheme, u.Host, http.DefaultClient, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	return &Clients{
		Notes: gennotes.NewClient(c.List(), c.Get(), c.Create(), c.Update(), c.Delete(), c.Search(), c.Restore(), c.Move(), c.Notebooks(), c.Tags(), c.CreateNotebook(), c.SaveNotebook(), c.DeleteNotebook(), c.TrashedNotebooks(), c.RestoreNotebook(), c.DeleteTrashedNotebook(), c.Import(),
			nil, nil, nil, nil, nil, nil, nil, // the methods for AI clients have no HTTP endpoints
			c.AddFile(), c.GetFile(), c.DeleteFile(), c.History(), c.HistoryVersion(), c.CheckFiles(), c.Events(), c.GitStatus(), c.GitSync(), c.Sync()),
		Assistant: genassistant.NewClient(a.Ask()),
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
	stream, err := api.Events(ctx)
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
