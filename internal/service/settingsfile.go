package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/config"
)

// SettingsFile is what the command running the server tells about the
// settings file, which the service reads and writes at config.Path.
type SettingsFile struct {
	// Check fails for content that is not valid settings, as the commands
	// read them; nil takes any content the settings file can be.
	Check func(content []byte) error
	// Overridden are the keys whose values the server does not take, as it
	// was started with their flags.
	Overridden []string
	// Restart starts the server again in the background, with its flags; nil
	// when it cannot.
	Restart func() error
}

// SetSettingsFile sets how the service handles the settings file.
func (s *Notes) SetSettingsFile(f SettingsFile) { s.settings = f }

// SettingsFile reads the settings file. For people only.
func (s *Notes) SettingsFile(ctx context.Context, _ *gennotes.SettingsFilePayload) (*gennotes.SettingsFile, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.readSettings()
}

// SaveSettingsFile writes the settings file, when its content is valid and
// the file was not changed since it was read. For people only, signed in with
// a passkey recently while passkeys are required, as the file decides how the
// server authenticates people.
func (s *Notes) SaveSettingsFile(ctx context.Context, p *gennotes.SaveSettingsFilePayload) (*gennotes.SettingsFile, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	if err := requireVerified(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.readSettings()
	if err != nil {
		return nil, err
	}
	if p.Version != cur.Version {
		return nil, gennotes.MakePreconditionFailed(errors.New("the settings file was changed since it was read"))
	}
	b := []byte(p.Content)
	if _, err := config.Parse(b, cur.Path); err != nil {
		return nil, gennotes.MakeBadRequest(err)
	}
	if s.settings.Check != nil {
		if err := s.settings.Check(b); err != nil {
			return nil, gennotes.MakeBadRequest(err)
		}
	}
	if _, err := config.Write(b); err != nil {
		return nil, err
	}
	return s.readSettings()
}

// RestartServer starts the server again, which stops this one. For people
// only.
func (s *Notes) RestartServer(ctx context.Context, _ *gennotes.RestartServerPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	if s.settings.Restart == nil {
		return gennotes.MakeUnavailable(errors.New("this server cannot start itself again: restart it with ebira server restart"))
	}
	return s.settings.Restart()
}

func (s *Notes) readSettings() (*gennotes.SettingsFile, error) {
	p, err := config.Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return &gennotes.SettingsFile{
		Path:       p,
		Content:    string(b),
		Exists:     exists,
		Version:    hex.EncodeToString(sum[:8]),
		Overridden: append([]string{}, s.settings.Overridden...),
	}, nil
}
