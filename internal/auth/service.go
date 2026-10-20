package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	genauth "github.com/ikawaha/ebira/gen/auth"
)

// Service implements the auth service of the API.
type Service struct {
	mode     string
	passkeys *Passkeys
}

var _ genauth.Service = (*Service)(nil)

// NewService returns the auth service of the passkeys, for the server
// authenticating people by mode. Passkeys can be registered and used to sign
// in whatever the mode, so that they are ready before requiring them.
func NewService(mode string, passkeys *Passkeys) *Service {
	return &Service{mode: mode, passkeys: passkeys}
}

// Status tells the mode, and whether a passkey is registered and the request
// is signed in.
func (s *Service) Status(_ context.Context, p *genauth.StatusPayload) (*genauth.StatusResult, error) {
	r := &genauth.StatusResult{Mode: s.mode, Registered: s.passkeys.Registered()}
	r.SignedIn, _ = s.check(p.Session)
	return r, nil
}

// check checks a session cookie.
func (s *Service) check(session *string) (valid, verified bool) {
	if session == nil {
		return false, false
	}
	return s.passkeys.Check(*session)
}

// verify fails unless the session is signed in and verified with a passkey.
func (s *Service) verify(session *string) error {
	valid, verified := s.check(session)
	switch {
	case !valid:
		return genauth.MakeUnauthorized(errors.New("sign in with a passkey"))
	case !verified:
		return genauth.MakeVerificationRequired(errors.New("sign in with the passkey again to do this"))
	}
	return nil
}

// RegisterBegin begins registering a passkey: with a registration token, or
// from a session verified with another passkey.
func (s *Service) RegisterBegin(_ context.Context, p *genauth.RegisterBeginPayload) (*genauth.Ceremony, error) {
	token := ""
	if p.Token != nil {
		token = *p.Token
	}
	if token == "" {
		if _, verified := s.check(p.Session); !verified {
			return nil, genauth.MakeVerificationRequired(errors.New("sign in with a passkey again to add another, or run ebira auth passkey"))
		}
	}
	id, options, err := s.passkeys.BeginRegistration(token)
	if errors.Is(err, ErrToken) {
		return nil, genauth.MakeForbidden(err)
	}
	if err != nil {
		return nil, err
	}
	return &genauth.Ceremony{Ceremony: id, Options: options}, nil
}

// RegisterFinish finishes registering a passkey.
func (s *Service) RegisterFinish(_ context.Context, p *genauth.CeremonyFinish) error {
	cred, err := json.Marshal(p.Credential)
	if err != nil {
		return genauth.MakeBadRequest(err)
	}
	if err := s.passkeys.FinishRegistration(p.Ceremony, cred); err != nil {
		slog.Warn("passkey registration failed", "error", err)
		return genauth.MakeBadRequest(err)
	}
	slog.Info("passkey registered")
	return nil
}

// LoginBegin begins signing in with a passkey.
func (s *Service) LoginBegin(context.Context) (*genauth.Ceremony, error) {
	id, options, err := s.passkeys.BeginLogin()
	if errors.Is(err, ErrNoPasskey) {
		return nil, genauth.MakeBadRequest(err)
	}
	if err != nil {
		return nil, err
	}
	return &genauth.Ceremony{Ceremony: id, Options: options}, nil
}

// LoginFinish finishes signing in, returning the session for the cookie.
func (s *Service) LoginFinish(_ context.Context, p *genauth.CeremonyFinish) (*genauth.LoginFinishResult, error) {
	cred, err := json.Marshal(p.Credential)
	if err != nil {
		return nil, genauth.MakeUnauthorized(err)
	}
	session, err := s.passkeys.FinishLogin(p.Ceremony, cred)
	if err != nil {
		slog.Warn("passkey sign-in failed", "error", err)
		return nil, genauth.MakeUnauthorized(errors.New("sign-in with the passkey failed"))
	}
	return &genauth.LoginFinishResult{Session: session}, nil
}

// Logout clears the session cookie. Sessions are not kept by the server, so
// a copy of the cookie stays valid until it expires.
func (s *Service) Logout(context.Context) (*genauth.LogoutResult, error) {
	return &genauth.LogoutResult{Session: ""}, nil
}

// DeviceStart begins signing in a client without a browser.
func (s *Service) DeviceStart(context.Context) (*genauth.DeviceStartResult, error) {
	deviceCode, userCode, uri := s.passkeys.StartDevice()
	return &genauth.DeviceStartResult{
		DeviceCode:      deviceCode,
		UserCode:        userCode,
		VerificationURI: uri,
		ExpiresIn:       int(DeviceMaxAge / time.Second),
		Interval:        int(DeviceInterval / time.Second),
	}, nil
}

// DeviceToken tells the client whether its request is approved, with the
// session once it is.
func (s *Service) DeviceToken(_ context.Context, p *genauth.DeviceTokenPayload) (*genauth.DeviceTokenResult, error) {
	status, session := s.passkeys.PollDevice(p.DeviceCode)
	r := &genauth.DeviceTokenResult{Status: status}
	if session != "" {
		r.Session = &session
	}
	return r, nil
}

// DeviceApprove approves a request of a client, for people signed in with a
// passkey.
func (s *Service) DeviceApprove(_ context.Context, p *genauth.DeviceApprovePayload) error {
	if valid, _ := s.check(p.Session); !valid {
		return genauth.MakeUnauthorized(errors.New("sign in with a passkey to approve the request"))
	}
	if err := s.passkeys.ApproveDevice(p.UserCode); err != nil {
		return genauth.MakeNotFound(err)
	}
	slog.Info("approved a sign-in of a client without a browser")
	return nil
}

// Passkeys lists the registered passkeys.
func (s *Service) Passkeys(_ context.Context, p *genauth.PasskeysPayload) ([]*genauth.PasskeyInfo, error) {
	if valid, _ := s.check(p.Session); !valid {
		return nil, genauth.MakeUnauthorized(errors.New("sign in with a passkey"))
	}
	list, err := s.passkeys.List()
	if err != nil {
		return nil, err
	}
	res := []*genauth.PasskeyInfo{}
	for _, k := range list {
		info := &genauth.PasskeyInfo{ID: k.ID}
		if !k.CreatedAt.IsZero() {
			info.CreatedAt = k.CreatedAt.UTC().Format(time.RFC3339)
		}
		if k.UsedAt != nil {
			used := k.UsedAt.UTC().Format(time.RFC3339)
			info.UsedAt = &used
		}
		res = append(res, info)
	}
	return res, nil
}

// DeletePasskey deletes a passkey, for a verified session.
func (s *Service) DeletePasskey(_ context.Context, p *genauth.DeletePasskeyPayload) error {
	if err := s.verify(p.Session); err != nil {
		return err
	}
	switch err := s.passkeys.Delete(p.ID); {
	case errors.Is(err, ErrPasskeyNotFound):
		return genauth.MakeNotFound(err)
	case errors.Is(err, ErrLastPasskey):
		return genauth.MakeBadRequest(err)
	case err != nil:
		return err
	}
	slog.Info("passkey deleted")
	return nil
}

// RevokeSessions ends every session, for a verified session, and gives the
// request a new one.
func (s *Service) RevokeSessions(_ context.Context, p *genauth.RevokeSessionsPayload) (*genauth.RevokeSessionsResult, error) {
	if err := s.verify(p.Session); err != nil {
		return nil, err
	}
	session, err := s.passkeys.RevokeSessions()
	if err != nil {
		return nil, err
	}
	slog.Info("ended every session")
	return &genauth.RevokeSessionsResult{Session: session}, nil
}
