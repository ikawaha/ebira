package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// A device flow signs in a client without a browser, such as the TUI, as in
// OAuth's device authorization grant (RFC 8628): the client asks for a
// request (StartDevice) and shows its user code and URL; people open the URL
// in the web UI, signed in with a passkey, compare the code and approve it
// (ApproveDevice); the client meanwhile polls with the device code
// (PollDevice) and gets a session once approved. Requests are kept in memory
// only, so a restart of the server drops them.

// Lifetimes and bounds of device requests.
const (
	// DeviceMaxAge is the time people have to approve a request.
	DeviceMaxAge = 10 * time.Minute
	// DeviceInterval is the time a client waits between polls.
	DeviceInterval = 2 * time.Second
	// maxDevices bounds the pending requests, which anyone may begin.
	maxDevices = 16
)

// ErrDeviceCode is a user code that is unknown or expired.
var ErrDeviceCode = errors.New("unknown or expired code")

// Statuses of a device request for its client.
const (
	DevicePending  = "pending"
	DeviceApproved = "approved"
	DeviceExpired  = "expired"
)

// device is a pending device request.
type device struct {
	userCode string
	expires  time.Time
	approved bool
}

// userCodeLetters are the letters of user codes: consonants only, so that
// codes do not spell words, and without letters easy to mistake.
const userCodeLetters = "BCDFGHJKLMNPQRSTVWXZ"

// newUserCode returns a code of 8 letters, shown as two groups of 4.
func newUserCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		b[i] = userCodeLetters[int(b[i])%len(userCodeLetters)]
	}
	return string(b[:4]) + "-" + string(b[4:])
}

// normalizeUserCode makes a code typed by people comparable: upper case,
// without the dash and spaces.
func normalizeUserCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(code))
}

// StartDevice begins a device request. It returns the secret device code the
// client polls with, the user code people approve, and the URL of the page of
// the web UI that approves it. It forgets expired requests, and the oldest
// when there are too many.
func (p *Passkeys) StartDevice() (deviceCode, userCode, uri string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	var oldest string
	for code, d := range p.devices {
		if now.After(d.expires) {
			delete(p.devices, code)
		} else if oldest == "" || d.expires.Before(p.devices[oldest].expires) {
			oldest = code
		}
	}
	if len(p.devices) >= maxDevices {
		delete(p.devices, oldest)
	}
	b := make([]byte, 32)
	rand.Read(b)
	deviceCode = base64.RawURLEncoding.EncodeToString(b)
	userCode = newUserCode()
	p.devices[deviceCode] = &device{userCode: userCode, expires: now.Add(DeviceMaxAge)}
	return deviceCode, userCode, p.origin + "/#device/" + userCode
}

// ApproveDevice approves the pending request with userCode. Call it only for
// a request signed in with a passkey.
func (p *Passkeys) ApproveDevice(userCode string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	want := normalizeUserCode(userCode)
	now := time.Now()
	for _, d := range p.devices {
		if normalizeUserCode(d.userCode) == want && now.Before(d.expires) {
			d.approved = true
			return nil
		}
	}
	return ErrDeviceCode
}

// PollDevice tells the status of the request with deviceCode. Once it is
// approved, it returns a new session, not verified, and forgets the request,
// so that the session is handed out once.
func (p *Passkeys) PollDevice(deviceCode string) (status, session string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.devices[deviceCode]
	switch {
	case !ok:
		return DeviceExpired, ""
	case time.Now().After(d.expires):
		delete(p.devices, deviceCode)
		return DeviceExpired, ""
	case !d.approved:
		return DevicePending, ""
	}
	delete(p.devices, deviceCode)
	u, err := p.load()
	if err != nil || len(u.Credentials) == 0 {
		return DeviceExpired, ""
	}
	// Not verified: important operations stay in the web UI, which asks
	// for the passkey again.
	return DeviceApproved, newSession(u.SessionKey, time.Now().Add(SessionMaxAge), time.Time{})
}
