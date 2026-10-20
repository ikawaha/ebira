package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func newTest(t *testing.T) (*Passkeys, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "auth")
	p, err := New(dir, "http://localhost:13579", "localhost")
	if err != nil {
		t.Fatal(err)
	}
	return p, dir
}

// register stores a passkey as if registered, without a browser.
func register(t *testing.T, p *Passkeys, id ...string) {
	t.Helper()
	u, err := p.load()
	if err != nil {
		t.Fatal(err)
	}
	cid := "credential"
	if len(id) > 0 {
		cid = id[0]
	}
	u.Credentials = append(u.Credentials, webauthn.Credential{ID: []byte(cid)})
	u.info([]byte(cid)).CreatedAt = time.Now().UTC()
	if err := p.save(u); err != nil {
		t.Fatal(err)
	}
}

// session returns a session signed with the key of p.
func session(t *testing.T, p *Passkeys, exp, verified time.Time) string {
	t.Helper()
	u, err := p.load()
	if err != nil {
		t.Fatal(err)
	}
	return newSession(u.SessionKey, exp, verified)
}

func TestSession(t *testing.T) {
	p, dir := newTest(t)
	register(t, p)
	hour := time.Now().Add(time.Hour)
	s := session(t, p, hour, time.Time{})
	if valid, verified := p.Check(s); !valid || verified {
		t.Errorf("a new session: valid %v, verified %v; want valid, not verified", valid, verified)
	}
	if _, verified := p.Check(session(t, p, hour, time.Now().Add(time.Minute))); !verified {
		t.Error("a session signed in a minute ago is not verified")
	}
	if valid, verified := p.Check(session(t, p, hour, time.Now().Add(-time.Second))); !valid || verified {
		t.Error("the verification of a session does not end")
	}
	for name, bad := range map[string]string{
		"empty":    "",
		"bad mac":  s + "x",
		"garbage":  "abc.def",
		"expired":  session(t, p, time.Now().Add(-time.Second), time.Time{}),
		"no point": "abcdef",
	} {
		if valid, _ := p.Check(bad); valid {
			t.Errorf("%s session is valid", name)
		}
	}

	// The key is kept, so sessions survive a restart; another key rejects them.
	again, err := New(dir, "http://localhost:13579", "localhost")
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := again.Check(s); !valid {
		t.Error("a session is invalid after a restart")
	}
	other, _ := newTest(t)
	register(t, other)
	if valid, _ := other.Check(s); valid {
		t.Error("a session is valid with another key")
	}
	if fi, err := os.Stat(filepath.Join(dir, passkeysFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("%s: %v, %v; want mode 0600", passkeysFile, fi.Mode(), err)
	}

	// Without passkeys, no session is valid.
	if err := Reset(dir); err != nil {
		t.Fatal(err)
	}
	if valid, _ := p.Check(s); valid || p.Registered() {
		t.Error("a session is valid after a reset")
	}
}

// TestLegacyKey checks that the key of sessions of earlier builds, in
// session.key, is moved into passkeys.json, so that their sessions stay valid.
func TestLegacyKey(t *testing.T) {
	p, dir := newTest(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	u, _ := p.load()
	u.SessionKey = nil
	u.Credentials = []webauthn.Credential{{ID: []byte("c")}}
	b, _ := json.Marshal(u)
	if err := writeFile(filepath.Join(dir, passkeysFile), b); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, legacyKey), key); err != nil {
		t.Fatal(err)
	}
	s := newSession(key, time.Now().Add(time.Hour), time.Time{})
	if valid, _ := p.Check(s); !valid {
		t.Fatal("a session of the legacy key is invalid")
	}
	if err := p.Delete("x"); !errors.Is(err, ErrPasskeyNotFound) {
		t.Fatal(err)
	}
	register(t, p, "d") // saves
	if _, err := os.Stat(filepath.Join(dir, legacyKey)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is left: %v", legacyKey, err)
	}
	if valid, _ := p.Check(s); !valid {
		t.Error("a session of the legacy key is invalid after the move")
	}
}

func TestManagePasskeys(t *testing.T) {
	p, _ := newTest(t)
	register(t, p, "a")
	if err := p.Delete(credentialID([]byte("a"))); !errors.Is(err, ErrLastPasskey) {
		t.Errorf("deleted the only passkey: %v", err)
	}
	register(t, p, "b")
	list, err := p.List()
	if err != nil || len(list) != 2 || list[0].ID != credentialID([]byte("a")) {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := p.Delete(credentialID([]byte("a"))); err != nil {
		t.Fatal(err)
	}
	if list, _ := p.List(); len(list) != 1 || list[0].ID != credentialID([]byte("b")) {
		t.Errorf("after the deletion: %+v", list)
	}

	// Revoking sessions ends the others and gives a new verified one.
	old := session(t, p, time.Now().Add(time.Hour), time.Time{})
	s, err := p.RevokeSessions()
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := p.Check(old); valid {
		t.Error("an old session is valid after revoking sessions")
	}
	if valid, verified := p.Check(s); !valid || !verified {
		t.Errorf("the new session: valid %v, verified %v", valid, verified)
	}
}

func TestRegisterToken(t *testing.T) {
	p, dir := newTest(t)
	if _, _, err := p.BeginRegistration("x"); !errors.Is(err, ErrToken) {
		t.Errorf("without a token: %v, want ErrToken", err)
	}
	token, err := NewRegisterToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.BeginRegistration(token + "x"); !errors.Is(err, ErrToken) {
		t.Errorf("wrong token: %v, want ErrToken", err)
	}
	id, options, err := p.BeginRegistration(token)
	if err != nil {
		t.Fatal(err)
	}
	var o struct {
		RP struct {
			ID string `json:"id"`
		} `json:"rp"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(options, &o); err != nil || o.RP.ID != "localhost" || o.Challenge == "" {
		t.Errorf("options %s: %v", options, err)
	}
	if _, _, err := p.BeginRegistration(token); !errors.Is(err, ErrToken) {
		t.Errorf("token used twice: %v, want ErrToken", err)
	}
	// A registration is not a sign-in, and a ceremony is finished once.
	if _, err := p.FinishLogin(id, []byte("{}")); !errors.Is(err, ErrCeremony) {
		t.Errorf("sign-in with a registration: %v, want ErrCeremony", err)
	}

	// An expired token is refused.
	token, _ = NewRegisterToken(dir)
	b, _ := json.Marshal(registration{Hash: hashToken(token), Expires: time.Now().Add(-time.Second)})
	if err := writeFile(filepath.Join(dir, registerFile), b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.BeginRegistration(token); !errors.Is(err, ErrToken) {
		t.Errorf("expired token: %v, want ErrToken", err)
	}
}

func TestLogin(t *testing.T) {
	p, _ := newTest(t)
	if _, _, err := p.BeginLogin(); !errors.Is(err, ErrNoPasskey) {
		t.Errorf("without passkeys: %v, want ErrNoPasskey", err)
	}
	register(t, p)
	id, _, err := p.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.FinishRegistration(id, []byte("{}")); !errors.Is(err, ErrCeremony) {
		t.Errorf("registration with a sign-in: %v, want ErrCeremony", err)
	}
	if _, err := p.FinishLogin("unknown", []byte("{}")); !errors.Is(err, ErrCeremony) {
		t.Errorf("unknown ceremony: %v, want ErrCeremony", err)
	}

	// Pending ceremonies are bounded.
	for range 2 * maxCeremonies {
		if _, _, err := p.BeginLogin(); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(p.ceremonies); n > maxCeremonies {
		t.Errorf("%d pending ceremonies, want at most %d", n, maxCeremonies)
	}
}

func TestDevice(t *testing.T) {
	p, _ := newTest(t)
	register(t, p)
	code, user, uri := p.StartDevice()
	if len(user) != 9 || user[4] != '-' || uri != "http://localhost:13579/#device/"+user {
		t.Errorf("user code %q, URI %q", user, uri)
	}
	if st, s := p.PollDevice(code); st != DevicePending || s != "" {
		t.Errorf("before the approval: %s %q", st, s)
	}
	if err := p.ApproveDevice("BCDF-GHJK-X"); !errors.Is(err, ErrDeviceCode) {
		t.Errorf("unknown code: %v, want ErrDeviceCode", err)
	}
	// People may type the code in lower case and without the dash.
	if err := p.ApproveDevice(strings.ToLower(strings.ReplaceAll(user, "-", ""))); err != nil {
		t.Fatal(err)
	}
	st, s := p.PollDevice(code)
	if valid, verified := p.Check(s); st != DeviceApproved || !valid || verified {
		t.Fatalf("after the approval: %s, session valid %v, verified %v; want valid, not verified", st, valid, verified)
	}
	// The session is handed out once.
	if st, s := p.PollDevice(code); st != DeviceExpired || s != "" {
		t.Errorf("polled again: %s %q", st, s)
	}
	if st, _ := p.PollDevice("unknown"); st != DeviceExpired {
		t.Errorf("unknown device code: %s", st)
	}

	// An expired request is gone, and cannot be approved.
	code, user, _ = p.StartDevice()
	p.devices[code].expires = time.Now().Add(-time.Second)
	if err := p.ApproveDevice(user); !errors.Is(err, ErrDeviceCode) {
		t.Errorf("expired code approved: %v", err)
	}
	if st, _ := p.PollDevice(code); st != DeviceExpired {
		t.Errorf("expired request: %s", st)
	}

	// Pending requests are bounded.
	for range 2 * maxDevices {
		p.StartDevice()
	}
	if n := len(p.devices); n > maxDevices {
		t.Errorf("%d pending requests, want at most %d", n, maxDevices)
	}
}
