package tui

import (
	"os/exec"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"

	genauth "github.com/ikawaha/ebira/gen/auth"
)

// While the server requires a passkey (auth: passkey), the REST API refuses
// the TUI until it signs in. Passkeys are a browser's, so the TUI signs in
// through the web UI with a device flow: it asks the server for a request,
// shows its URL and code in the note list, and asks the server every few
// seconds until people approve the code in the web UI, signed in with a
// passkey; it then sends the session it gets with every request. The session
// is kept in memory only, so the TUI signs in again each time it starts.

// passkeyGuide is what the note list tells while the server requires a
// passkey and the TUI is not signed in.
type passkeyGuide struct {
	registered bool                       // whether a passkey is registered
	device     *genauth.DeviceStartResult // the request people approve, once begun
	starting   bool                       // whether the request is being begun
}

// Messages of signing in.
type (
	// authMsg tells how the server authenticates people.
	authMsg struct {
		status *genauth.StatusResult
		err    error
	}
	// deviceStartMsg carries a new device request.
	deviceStartMsg struct {
		res *genauth.DeviceStartResult
		err error
	}
	// deviceTickMsg asks the server about the request again.
	deviceTickMsg struct{ code string }
	// deviceTokenMsg tells whether people approved the request.
	deviceTokenMsg struct {
		code string
		res  *genauth.DeviceTokenResult
		err  error
	}
)

// checkAuth asks the server how it authenticates people.
func (m *Model) checkAuth() tea.Cmd {
	if m.auth == nil {
		return nil
	}
	ctx, api := m.ctx, m.auth
	return func() tea.Msg {
		res, err := api.Status(ctx, &genauth.StatusPayload{})
		return authMsg{status: res, err: err}
	}
}

func (m *Model) startDevice() tea.Cmd {
	m.passkey.starting = true
	ctx, api := m.ctx, m.auth
	return func() tea.Msg {
		res, err := api.DeviceStart(ctx)
		return deviceStartMsg{res: res, err: err}
	}
}

func deviceTick(code string, every time.Duration) tea.Cmd {
	return tea.Tick(every, func(time.Time) tea.Msg { return deviceTickMsg{code: code} })
}

func (m *Model) pollDevice(code string) tea.Cmd {
	ctx, api := m.ctx, m.auth
	return func() tea.Msg {
		res, err := api.DeviceToken(ctx, &genauth.DeviceTokenPayload{DeviceCode: code})
		return deviceTokenMsg{code: code, res: res, err: err}
	}
}

// waiting reports whether the TUI waits for the approval of the request with
// the device code.
func (m *Model) waiting(code string) bool {
	return m.passkey != nil && m.passkey.device != nil && m.passkey.device.DeviceCode == code
}

func (m *Model) handleAuth(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case authMsg:
		if msg.err != nil {
			return nil // an earlier server, without passkeys
		}
		if msg.status.Mode != "passkey" || msg.status.SignedIn {
			return nil
		}
		if m.passkey == nil {
			m.passkey = &passkeyGuide{}
			m.setStatus(statusWarn, "status.passkeyRequired")
		}
		m.passkey.registered = msg.status.Registered
		// Without a passkey, nobody can approve the request.
		if m.passkey.registered && m.passkey.device == nil && !m.passkey.starting {
			return m.startDevice()
		}

	case deviceStartMsg:
		if m.passkey == nil {
			return nil
		}
		m.passkey.starting = false
		if msg.err != nil {
			m.setStatus(statusError, "status.signInFailed", "err", errorMessage(msg.err))
			return nil
		}
		m.passkey.device = msg.res
		return deviceTick(msg.res.DeviceCode, time.Duration(msg.res.Interval)*time.Second)

	case browserOpenedMsg:
		if msg.err != nil {
			m.setStatus(statusError, "status.browserFailed", "err", msg.err)
		}

	case deviceTickMsg:
		if m.waiting(msg.code) {
			return m.pollDevice(msg.code)
		}

	case deviceTokenMsg:
		if !m.waiting(msg.code) {
			return nil
		}
		every := time.Duration(m.passkey.device.Interval) * time.Second
		switch {
		case msg.err != nil:
			return deviceTick(msg.code, 5*every) // the server may be restarting
		case msg.res.Status == "approved" && msg.res.Session != nil:
			if m.signIn != nil {
				m.signIn(*msg.res.Session)
			}
			m.passkey = nil
			m.setStatus(statusInfo, "status.signedIn")
			return tea.Batch(m.loadList(), m.loadNotebooks())
		case msg.res.Status == "expired":
			m.passkey.device = nil
			return m.startDevice()
		}
		return deviceTick(msg.code, every)
	}
	return nil
}

// browserOpenedMsg tells that the browser could not open a URL.
type browserOpenedMsg struct{ err error }

// openURL opens url in the default browser.
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		name, args := "xdg-open", []string{url}
		switch runtime.GOOS {
		case "darwin":
			name = "open"
		case "windows":
			name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
		}
		return browserOpenedMsg{err: exec.Command(name, args...).Run()}
	}
}
