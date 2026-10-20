package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	goa "goa.design/goa/v3/pkg"

	genauth "github.com/ikawaha/ebira/gen/auth"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// TestPasskeySignIn checks that the TUI signs in through a device flow when
// the server requires a passkey: it shows the URL and the code of the request
// in the note list, instead of the refused requests as errors, asks the
// server until the request is approved, then sends the session it got and
// loads the notes.
func TestPasskeySignIn(t *testing.T) {
	status := &genauth.StatusResult{Mode: "passkey"}
	token := &genauth.DeviceTokenResult{Status: "pending"}
	starts := 0
	api := &Clients{
		Notes: &gennotes.Client{},
		Auth: &genauth.Client{
			StatusEndpoint: func(context.Context, any) (any, error) { return status, nil },
			DeviceStartEndpoint: func(context.Context, any) (any, error) {
				starts++
				return &genauth.DeviceStartResult{DeviceCode: "secret", UserCode: "BCDF-GHJK", VerificationURI: "http://localhost:13579/#device/BCDF-GHJK", ExpiresIn: 600, Interval: 2}, nil
			},
			DeviceTokenEndpoint: func(_ context.Context, p any) (any, error) {
				if p.(*genauth.DeviceTokenPayload).DeviceCode != "secret" {
					t.Errorf("polled with %+v", p)
				}
				return token, nil
			},
		},
	}
	var session string
	m := New(context.Background(), api, false, "en")
	m.signIn = func(s string) { session = s }
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = tm.(Model)
	step := func(msg tea.Msg) tea.Cmd {
		t.Helper()
		tm, cmd := m.Update(msg)
		m = tm.(Model)
		return cmd
	}
	list := func() string { return ansi.Strip(m.render()) }

	// Without a passkey, nobody can approve a request: the list tells how to
	// register one.
	refused := &goa.ServiceError{Name: "unauthorized", Message: "sign in with a passkey"}
	cmd := step(notebooksMsg{err: refused})
	if cmd == nil {
		t.Fatal("unauthorized: no command to check the server")
	}
	if cmd = step(cmd()); cmd != nil || starts != 0 {
		t.Fatalf("without a passkey: began a request (%d)", starts)
	}
	for _, want := range []string{"requires a passkey", "ebira auth passkey", "-auth=none"} {
		if !strings.Contains(list(), want) {
			t.Errorf("the note list does not tell %q:\n%s", want, list())
		}
	}
	if strings.Contains(m.status, "load failed") {
		t.Errorf("status = %q, want no load error", m.status)
	}
	// The event stream refused does not show as disconnected, and asks again.
	if cmd = step(connMsg{err: refused}); cmd == nil || strings.Contains(m.status, "disconnected") {
		t.Errorf("event stream refused: status %q, asks again %v", m.status, cmd != nil)
	}

	// With a passkey registered, the TUI begins a request and shows it.
	status.Registered = true
	cmd = step(step(cmd())()) // authMsg, then deviceStartMsg
	if starts != 1 || cmd == nil {
		t.Fatalf("began %d requests, tick %v", starts, cmd != nil)
	}
	for _, want := range []string{"http://localhost:13579/#device/BCDF-GHJK", "BCDF-GHJK", "Waiting"} {
		if !strings.Contains(list(), want) {
			t.Errorf("the note list does not show %q:\n%s", want, list())
		}
	}
	if strings.Contains(list(), "secret") {
		t.Error("the note list shows the device code")
	}

	// Pending: ask again later.
	if cmd = step(step(deviceTickMsg{code: "secret"})()); cmd == nil || session != "" {
		t.Fatalf("pending: tick %v, session %q", cmd != nil, session)
	}
	// Approved: send the session and load the notes.
	token = &genauth.DeviceTokenResult{Status: "approved", Session: ptr("the session")}
	cmd = step(step(deviceTickMsg{code: "secret"})())
	if session != "the session" || m.passkey != nil || cmd == nil {
		t.Fatalf("approved: session %q, guide %v, reload %v", session, m.passkey, cmd != nil)
	}
	if strings.Contains(list(), "requires a passkey") {
		t.Errorf("the note list still tells to sign in:\n%s", list())
	}
}

// TestPasskeyExpired checks that an expired request is begun again.
func TestPasskeyExpired(t *testing.T) {
	starts := 0
	api := &Clients{Notes: &gennotes.Client{}, Auth: &genauth.Client{
		StatusEndpoint: func(context.Context, any) (any, error) {
			return &genauth.StatusResult{Mode: "passkey", Registered: true}, nil
		},
		DeviceStartEndpoint: func(context.Context, any) (any, error) {
			starts++
			return &genauth.DeviceStartResult{DeviceCode: "c", UserCode: "BCDF-GHJK", Interval: 2}, nil
		},
		DeviceTokenEndpoint: func(context.Context, any) (any, error) {
			return &genauth.DeviceTokenResult{Status: "expired"}, nil
		},
	}}
	m := New(context.Background(), api, false, "en")
	tm, _ := m.Update(m.checkAuth()())
	m = tm.(Model)
	tm, _ = m.Update(m.startDevice()())
	m = tm.(Model)
	tm, cmd := m.Update(m.pollDevice("c")())
	m = tm.(Model)
	if cmd == nil || m.passkey.device != nil {
		t.Fatal("expired: the request is not begun again")
	}
	if cmd(); starts != 2 {
		t.Errorf("began %d requests, want 2", starts)
	}
}

func ptr[T any](v T) *T { return &v }

// TestWrapText checks that a line of Japanese text does not begin with a
// character such as a comma, which takes the last character of the line
// before with it, and that no line is wider than asked.
func TestWrapText(t *testing.T) {
	s := "この TUI でログインするには、パスキーでログインした Web UI でこの URL を開き、コードを承認してください:"
	for w := 10; w <= 60; w++ {
		lines := wrapText(s, w)
		if got := strings.Join(lines, ""); got != s {
			t.Fatalf("width %d: the text changed: %q", w, got)
		}
		for i, l := range lines {
			if ansi.StringWidth(l) > w {
				t.Errorf("width %d: line %d is %d wide: %q", w, i, ansi.StringWidth(l), l)
			}
			if i > 0 && strings.ContainsAny(l[:min(len(l), 3)], "、。:") && strings.IndexAny(l, "、。:") == 0 {
				t.Errorf("width %d: line %d begins with %q", w, i, l)
			}
		}
	}
	if got := wrapText("Sign in this TUI: open this URL", 12); strings.Join(got, "|") != "Sign in this|TUI: open|this URL" {
		t.Errorf("English: %q", got)
	}
}
