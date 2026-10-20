// Package auth authenticates people with passkeys (WebAuthn) for the REST
// API, so that AI clients on the same machine, which call it without a
// browser, are not taken for people.
//
// The files are kept in a directory of the state directory of the notes
// directory, readable by the user only:
//
//   - passkeys.json: the user handle, the registered passkeys (their public
//     keys, with when they were added and last used) and the key that signs
//     sessions, so that sessions survive a restart of the server and end
//     with the passkeys (Reset). Earlier builds kept the key in session.key,
//     which is moved into it.
//   - register.json: the hash of the one-time token of a pending
//     registration, which ebira auth passkey writes.
//
// A session is a signed expiry time, kept in a cookie by the browser; the
// server keeps no sessions. It also tells until when the sign-in counts as a
// fresh verification with a passkey, which important operations ask for
// (Verified).
package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Modes of authenticating people.
const (
	// ModeNone treats every REST request as from people.
	ModeNone = "none"
	// ModePasskey treats a REST request as from people only with a session
	// of a sign-in with a passkey.
	ModePasskey = "passkey"
)

// Modes are the modes, the default first.
var Modes = []string{ModeNone, ModePasskey}

// ValidMode reports whether mode is one of Modes.
func ValidMode(mode string) bool {
	for _, m := range Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// Dir returns the directory of the passkeys in the state directory of a notes
// directory.
func Dir(stateDir string) string { return filepath.Join(stateDir, "auth") }

// Files in the directory.
const (
	passkeysFile = "passkeys.json"
	legacyKey    = "session.key" // the key of sessions of earlier builds
	registerFile = "register.json"
)

// Lifetimes.
const (
	// SessionMaxAge is the lifetime of a session; the design gives the
	// cookie the same.
	SessionMaxAge = 30 * 24 * time.Hour
	// VerifyMaxAge is how long a sign-in with a passkey counts as a fresh
	// verification, which important operations ask for.
	VerifyMaxAge = 5 * time.Minute
	// RegisterTokenMaxAge is the lifetime of a registration token.
	RegisterTokenMaxAge = 10 * time.Minute
	// ceremonyMaxAge is the time a browser has to finish a ceremony.
	ceremonyMaxAge = 5 * time.Minute
	// maxCeremonies bounds the pending ceremonies, which anyone may begin.
	maxCeremonies = 32
)

// Errors.
var (
	// ErrToken is a registration token that is unknown, used or expired.
	ErrToken = errors.New("unknown, used or expired registration token")
	// ErrCeremony is a ceremony that is unknown or expired.
	ErrCeremony = errors.New("unknown or expired ceremony")
	// ErrNoPasskey is a sign-in without registered passkeys.
	ErrNoPasskey = errors.New("no passkey is registered: run ebira auth passkey to register one")
	// ErrPasskeyNotFound is a passkey that is not registered.
	ErrPasskeyNotFound = errors.New("the passkey is not registered")
	// ErrLastPasskey is the deletion of the only passkey, which would lock
	// people out of the web UI.
	ErrLastPasskey = errors.New("the only passkey cannot be deleted: register another one first, or run ebira auth reset")
)

// Passkeys registers passkeys, signs people in with them and checks their
// sessions.
type Passkeys struct {
	dir    string
	origin string
	wa     *webauthn.WebAuthn

	mu         sync.Mutex
	ceremonies map[string]*ceremony
	devices    map[string]*device // by device code
}

// ceremony is a pending registration or sign-in.
type ceremony struct {
	register bool
	data     *webauthn.SessionData
}

// New returns the passkeys kept in dir for the web UI at origin, such as
// http://localhost:13579. Passkeys belong to the host name of origin, which
// must be a domain name such as localhost, not an IP address.
func New(dir, origin, host string) (*Passkeys, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          host,
		RPDisplayName: "ebira",
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, err
	}
	return &Passkeys{dir: dir, origin: origin, wa: wa, ceremonies: map[string]*ceremony{}, devices: map[string]*device{}}, nil
}

// Reset removes the passkeys, the key of sessions, which ends every session,
// and a pending registration in dir.
func Reset(dir string) error {
	for _, f := range []string{passkeysFile, legacyKey, registerFile} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// user is the only user, the owner of the notes.
type user struct {
	ID          []byte                  `json:"id"`
	SessionKey  []byte                  `json:"session_key,omitempty"`
	Credentials []webauthn.Credential   `json:"credentials"`
	Info        map[string]*passkeyInfo `json:"info,omitempty"` // by the ID of the credential, in base64url
}

// passkeyInfo is what ebira keeps about a passkey besides the credential.
type passkeyInfo struct {
	CreatedAt time.Time  `json:"created_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

func (u *user) WebAuthnID() []byte                         { return u.ID }
func (u *user) WebAuthnName() string                       { return "ebira" }
func (u *user) WebAuthnDisplayName() string                { return "ebira" }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

func credentialID(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

func (u *user) info(id []byte) *passkeyInfo {
	if u.Info == nil {
		u.Info = map[string]*passkeyInfo{}
	}
	k := credentialID(id)
	if u.Info[k] == nil {
		u.Info[k] = &passkeyInfo{}
	}
	return u.Info[k]
}

// load reads the user, with a new handle and session key if missing (the key
// of earlier builds is taken from session.key). Save it to keep them.
func (p *Passkeys) load() (*user, error) {
	var u user
	b, err := os.ReadFile(filepath.Join(p.dir, passkeysFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &u); err != nil {
			return nil, fmt.Errorf("%s: %w", passkeysFile, err)
		}
	}
	if len(u.ID) == 0 {
		u.ID = make([]byte, 32)
		rand.Read(u.ID)
	}
	if len(u.SessionKey) == 0 {
		if k, err := os.ReadFile(filepath.Join(p.dir, legacyKey)); err == nil && len(k) == 32 {
			u.SessionKey = k
		} else {
			u.SessionKey = make([]byte, 32)
			rand.Read(u.SessionKey)
		}
	}
	return &u, nil
}

func (p *Passkeys) save(u *user) error {
	b, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(p.dir, passkeysFile), b); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(p.dir, legacyKey)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Registered reports whether a passkey is registered.
func (p *Passkeys) Registered() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, err := p.load()
	return err == nil && len(u.Credentials) > 0
}

// Passkey is a registered passkey, as people see it.
type Passkey struct {
	ID        string // the ID of the credential, in base64url
	CreatedAt time.Time
	UsedAt    *time.Time
}

// List returns the registered passkeys, the oldest first.
func (p *Passkeys) List() ([]Passkey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, err := p.load()
	if err != nil {
		return nil, err
	}
	var res []Passkey
	for _, c := range u.Credentials {
		in := u.info(c.ID)
		res = append(res, Passkey{ID: credentialID(c.ID), CreatedAt: in.CreatedAt, UsedAt: in.UsedAt})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].CreatedAt.Before(res[j].CreatedAt) })
	return res, nil
}

// Delete deletes the passkey with id, unless it is the only one.
func (p *Passkeys) Delete(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, err := p.load()
	if err != nil {
		return err
	}
	i := -1
	for j, c := range u.Credentials {
		if credentialID(c.ID) == id {
			i = j
		}
	}
	if i < 0 {
		return ErrPasskeyNotFound
	}
	if len(u.Credentials) == 1 {
		return ErrLastPasskey
	}
	u.Credentials = append(u.Credentials[:i], u.Credentials[i+1:]...)
	delete(u.Info, id)
	return p.save(u)
}

// RevokeSessions replaces the key of sessions, which ends every session, and
// returns a new session for the request that asked for it, still verified.
func (p *Passkeys) RevokeSessions() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, err := p.load()
	if err != nil {
		return "", err
	}
	if len(u.Credentials) == 0 {
		return "", ErrNoPasskey
	}
	u.SessionKey = make([]byte, 32)
	rand.Read(u.SessionKey)
	if err := p.save(u); err != nil {
		return "", err
	}
	now := time.Now()
	return newSession(u.SessionKey, now.Add(SessionMaxAge), now.Add(VerifyMaxAge)), nil
}

// NewRegisterToken creates the one-time token of a registration in dir,
// replacing an earlier one, and returns it.
func NewRegisterToken(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b := make([]byte, 24)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	r := registration{Hash: hashToken(token), Expires: time.Now().Add(RegisterTokenMaxAge).UTC()}
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return token, writeFile(filepath.Join(dir, registerFile), data)
}

// registration is a pending registration token.
type registration struct {
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// useToken checks the registration token and removes it, so that it is used
// only once.
func (p *Passkeys) useToken(token string) error {
	path := filepath.Join(p.dir, registerFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrToken
	}
	if err != nil {
		return err
	}
	var r registration
	if err := json.Unmarshal(b, &r); err != nil {
		return ErrToken
	}
	if !hmac.Equal([]byte(r.Hash), []byte(hashToken(token))) {
		return ErrToken
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if time.Now().After(r.Expires) {
		return ErrToken
	}
	return nil
}

// BeginRegistration begins registering a passkey: with a registration token
// from ebira auth passkey, or, with an empty token, for a request signed in
// and verified with another passkey, which its caller checks. It returns the
// ID of the ceremony and the options for the browser.
func (p *Passkeys) BeginRegistration(token string) (string, json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if token != "" {
		if err := p.useToken(token); err != nil {
			return "", nil, err
		}
	}
	u, err := p.load()
	if err != nil {
		return "", nil, err
	}
	// Keep the user handle, which load makes up before the first passkey,
	// for the registration to finish with.
	if err := p.save(u); err != nil {
		return "", nil, err
	}
	creation, data, err := p.wa.BeginRegistration(u,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(webauthn.Credentials(u.Credentials).CredentialDescriptors()),
	)
	if err != nil {
		return "", nil, err
	}
	return p.begin(true, data, creation.Response)
}

// FinishRegistration finishes the registration of ceremony id with the
// credential the browser created, in the JSON form of the browser.
func (p *Passkeys) FinishRegistration(id string, credential []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, err := p.finish(id, true)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return err
	}
	u, err := p.load()
	if err != nil {
		return err
	}
	if !bytes.Equal(u.ID, c.data.UserID) {
		return ErrCeremony
	}
	cred, err := p.wa.CreateCredential(u, *c.data, parsed)
	if err != nil {
		return err
	}
	u.Credentials = append(u.Credentials, *cred)
	u.info(cred.ID).CreatedAt = time.Now().UTC()
	return p.save(u)
}

// BeginLogin begins signing in with any registered passkey. It returns the
// ID of the ceremony and the options for the browser.
func (p *Passkeys) BeginLogin() (string, json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, err := p.load()
	if err != nil {
		return "", nil, err
	}
	if len(u.Credentials) == 0 {
		return "", nil, ErrNoPasskey
	}
	assertion, data, err := p.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return "", nil, err
	}
	return p.begin(false, data, assertion.Response)
}

// FinishLogin finishes the sign-in of ceremony id with the credential the
// browser used, in the JSON form of the browser, and returns a new session,
// verified for VerifyMaxAge.
func (p *Passkeys) FinishLogin(id string, credential []byte) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, err := p.finish(id, false)
	if err != nil {
		return "", err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credential)
	if err != nil {
		return "", err
	}
	u, err := p.load()
	if err != nil {
		return "", err
	}
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		if !bytes.Equal(userHandle, u.ID) {
			return nil, errors.New("unknown user")
		}
		return u, nil
	}
	_, cred, err := p.wa.ValidatePasskeyLogin(handler, *c.data, parsed)
	if err != nil {
		return "", err
	}
	if cred.Authenticator.CloneWarning {
		return "", errors.New("the signature counter of the passkey went back: it may be cloned")
	}
	now := time.Now()
	for i := range u.Credentials {
		if bytes.Equal(u.Credentials[i].ID, cred.ID) {
			u.Credentials[i].Authenticator = cred.Authenticator
			u.Credentials[i].Flags = cred.Flags
			used := now.UTC()
			u.info(cred.ID).UsedAt = &used
		}
	}
	if err := p.save(u); err != nil {
		return "", err
	}
	return newSession(u.SessionKey, now.Add(SessionMaxAge), now.Add(VerifyMaxAge)), nil
}

// begin keeps a new ceremony and returns its ID with the options as JSON.
// It forgets expired ceremonies, and the oldest when there are too many.
func (p *Passkeys) begin(register bool, data *webauthn.SessionData, options any) (string, json.RawMessage, error) {
	b, err := json.Marshal(options)
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	var oldest string
	for id, c := range p.ceremonies {
		if now.After(c.data.Expires) {
			delete(p.ceremonies, id)
		} else if oldest == "" || c.data.Expires.Before(p.ceremonies[oldest].data.Expires) {
			oldest = id
		}
	}
	if len(p.ceremonies) >= maxCeremonies {
		delete(p.ceremonies, oldest)
	}
	if data.Expires.IsZero() || data.Expires.After(now.Add(ceremonyMaxAge)) {
		data.Expires = now.Add(ceremonyMaxAge)
	}
	idb := make([]byte, 16)
	rand.Read(idb)
	id := base64.RawURLEncoding.EncodeToString(idb)
	p.ceremonies[id] = &ceremony{register: register, data: data}
	return id, b, nil
}

// finish takes the ceremony id of the kind out of the pending ones.
func (p *Passkeys) finish(id string, register bool) (*ceremony, error) {
	c, ok := p.ceremonies[id]
	if !ok || c.register != register {
		return nil, ErrCeremony
	}
	delete(p.ceremonies, id)
	if time.Now().After(c.data.Expires) {
		return nil, ErrCeremony
	}
	return c, nil
}

// sessionSize is the size of the signed part of a session: the expiry time,
// the end of the verification and a random nonce.
const sessionSize = 8 + 8 + 16

// newSession returns a session signed with key that expires at exp and
// counts as verified until verified (zero for never).
func newSession(key []byte, exp, verified time.Time) string {
	payload := make([]byte, sessionSize)
	binary.BigEndian.PutUint64(payload, uint64(exp.Unix()))
	if !verified.IsZero() {
		binary.BigEndian.PutUint64(payload[8:], uint64(verified.Unix()))
	}
	rand.Read(payload[16:])
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sign(key, payload))
}

func sign(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	return m.Sum(nil)
}

// Check reports whether session is a valid session that has not expired,
// while a passkey is registered, and whether it is verified: signed in with
// a passkey within VerifyMaxAge.
func (p *Passkeys) Check(session string) (valid, verified bool) {
	enc, sig, ok := strings.Cut(session, ".")
	if !ok {
		return false, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil || len(payload) != sessionSize {
		return false, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false, false
	}
	p.mu.Lock()
	u, err := p.load()
	p.mu.Unlock()
	if err != nil || len(u.Credentials) == 0 || !hmac.Equal(mac, sign(u.SessionKey, payload)) {
		return false, false
	}
	now := time.Now().Unix()
	if now >= int64(binary.BigEndian.Uint64(payload)) {
		return false, false
	}
	return true, now < int64(binary.BigEndian.Uint64(payload[8:]))
}

// writeFile writes a file readable by the user only, replacing it at once.
func writeFile(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
