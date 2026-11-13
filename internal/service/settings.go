package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"strings"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/store"
	"gopkg.in/yaml.v3"
)

// Settings is what the command running the server tells about its settings:
// those of the machine and user in the settings file at config.Path, and
// those synced with the notes in _ebira.yaml, which the service reads and
// writes.
type Settings struct {
	// Check fails for settings of the machine and user that the commands do
	// not take, such as an unknown language; nil takes any.
	Check func(config.Config) error
	// Overridden are the keys whose values the server does not take, as it
	// was started with their flags.
	Overridden []string
	// Addr is the address the server listens on.
	Addr string
	// Passkeys tells whether a passkey is registered, which auth: passkey
	// needs to sign in; nil for none.
	Passkeys func() bool
	// DefaultModels are the default models of the assistant by provider.
	DefaultModels map[string]string
	// Restart starts the server again, with its flags; nil when it cannot.
	Restart func() error
	// Restartable tells why Restart cannot start the server now, as one of
	// the Restart reasons, or "" when it can; nil when it always can.
	Restartable func() string
}

// Reasons why the server cannot restart itself (restart_unavailable), which
// the UI tells in its language.
const (
	// RestartExecutableGone: the executable of the server was removed, as go
	// run does when it ends while the server it started still runs.
	RestartExecutableGone = "executable_gone"
	// RestartUnsupported: the server has no way to restart itself.
	RestartUnsupported = "unsupported"
)

// restartErrors are the errors of restart_server by the reasons.
var restartErrors = map[string]string{
	RestartExecutableGone: "the executable of this server is gone, such as one built by go run: restart it with ebira server restart",
	RestartUnsupported:    "this server cannot start itself again: restart it with ebira server restart",
}

// Uses of a stop list (StopList).
const (
	stopDefault = "default"
	stopNone    = "none"
	stopList    = "list"
)

// SetSettings sets how the service handles the settings.
func (s *Notes) SetSettings(st Settings) { s.settings = st }

// Settings reads the settings. For people only.
func (s *Notes) Settings(ctx context.Context, _ *gennotes.SettingsPayload) (*gennotes.Settings, error) {
	if err := peopleOnly(ctx); err != nil {
		return nil, err
	}
	return s.readSettings()
}

// SaveSettings writes the settings, when they are valid and were not changed
// since they were read, keeping the rest of the files with their comments.
// For people only, signed in with a passkey recently while passkeys are
// required, as the settings decide how the server authenticates people.
func (s *Notes) SaveSettings(ctx context.Context, p *gennotes.SaveSettingsPayload) (*gennotes.Settings, error) {
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
		return nil, gennotes.MakePreconditionFailed(errors.New("the settings were changed since they were read"))
	}
	m := machineConfig(p.Machine)
	if s.settings.Check != nil {
		if err := s.settings.Check(m); err != nil {
			return nil, gennotes.MakeBadRequest(err)
		}
	}
	// Without a passkey, nobody could sign in once the server restarts.
	if m.Auth == "passkey" && !cur.Passkeys {
		return nil, gennotes.MakeBadRequest(errors.New("register a passkey with ebira auth passkey before auth: passkey, or nobody can sign in"))
	}
	n := notesConfig(p.Notes)
	var defaults *config.Config
	if p.Notes != nil && p.Notes.Defaults != nil {
		d := machineConfig(p.Notes.Defaults)
		d.Auth = "" // a machine's own
		if s.settings.Check != nil {
			if err := s.settings.Check(d); err != nil {
				return nil, gennotes.MakeBadRequest(fmt.Errorf("defaults: %w", err))
			}
		}
		defaults = &d
	}
	if _, err := config.Update(m); err != nil {
		return nil, err
	}
	if err := s.store.SaveSearch(n); err != nil {
		return nil, err
	}
	if defaults != nil {
		if err := s.store.SaveDefaults(func(m *yaml.Node) { config.SetKeys(m, *defaults, config.DefaultKeys) }); err != nil {
			return nil, err
		}
	}
	return s.readSettings()
}

// RestartServer starts the server again, which stops this one. For people
// only, signed in with the passkey recently: the server starts with the
// settings written, which decide how it authenticates people.
func (s *Notes) RestartServer(ctx context.Context, _ *gennotes.RestartServerPayload) error {
	if err := peopleOnly(ctx); err != nil {
		return err
	}
	if err := requireVerified(ctx); err != nil {
		return err
	}
	if why := s.restartReason(); why != "" {
		return gennotes.MakeUnavailable(errors.New(restartErrors[why]))
	}
	return s.settings.Restart()
}

// restartReason tells why RestartServer cannot start the server again, or ""
// when it can.
func (s *Notes) restartReason() string {
	if s.settings.Restart == nil {
		return RestartUnsupported
	}
	if s.settings.Restartable != nil {
		if why := s.settings.Restartable(); why != "" {
			if _, ok := restartErrors[why]; !ok {
				return RestartUnsupported
			}
			return why
		}
	}
	return ""
}

func (s *Notes) readSettings() (*gennotes.Settings, error) {
	mp, err := config.Path()
	if err != nil {
		return nil, err
	}
	mb, err := os.ReadFile(mp)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	m, err := config.Parse(mb, mp)
	if err != nil {
		return nil, err
	}
	np := s.store.ConfigPath()
	nb, err := os.ReadFile(np)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	n, err := s.store.Config()
	if err != nil {
		return nil, err
	}
	// The keys left out of the defaults are told when a command reads them.
	d, _, err := config.ParseDefaults(&n.Defaults, np)
	if err != nil {
		return nil, err
	}
	sum := sha256.New()
	sum.Write(mb)
	sum.Write([]byte{0})
	sum.Write(nb)
	var why *string
	if r := s.restartReason(); r != "" {
		why = &r
	}
	models := map[string]string{}
	maps.Copy(models, s.settings.DefaultModels)
	return &gennotes.Settings{
		Machine:            machineSettings(m),
		Notes:              notesSettings(n, d),
		MachinePath:        mp,
		NotesPath:          np,
		NotesDir:           s.store.Dir(),
		Addr:               s.settings.Addr,
		Version:            hex.EncodeToString(sum.Sum(nil)[:8]),
		Overridden:         append([]string{}, s.settings.Overridden...),
		Restart:            why == nil,
		RestartUnavailable: why,
		Passkeys:           s.settings.Passkeys != nil && s.settings.Passkeys(),
		DefaultModels:      models,
	}, nil
}

// machineSettings returns the settings of c that the web UI changes.
func machineSettings(c config.Config) *gennotes.MachineSettings {
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	return &gennotes.MachineSettings{
		Lang: opt(c.Lang), Theme: opt(c.Theme), Auth: opt(c.Auth), GitSync: opt(c.GitSync),
		Watch: c.Watch, Provider: opt(c.Provider), Model: opt(c.Model), TuiPreview: opt(c.TUIPreview), TuiSearch: opt(c.TUISearch),
	}
}

// machineConfig returns the settings of m as those of the settings file,
// without spaces around the values.
func machineConfig(m *gennotes.MachineSettings) config.Config {
	if m == nil {
		return config.Config{}
	}
	v := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	return config.Config{
		Lang: v(m.Lang), Theme: v(m.Theme), Auth: v(m.Auth), GitSync: v(m.GitSync),
		Watch: m.Watch, Provider: v(m.Provider), Model: v(m.Model), TUIPreview: v(m.TuiPreview), TUISearch: v(m.TuiSearch),
	}
}

func notesSettings(c store.Config, defaults config.Config) *gennotes.NotesSettings {
	list := func(words []string) *gennotes.StopList {
		switch {
		case words == nil:
			return &gennotes.StopList{Use: stopDefault}
		case len(words) == 0:
			return &gennotes.StopList{Use: stopNone}
		}
		return &gennotes.StopList{Use: stopList, Words: words}
	}
	var search *string
	if c.Search != "" {
		search = &c.Search
	}
	return &gennotes.NotesSettings{Search: search, StopWords: list(c.StopWords), StopTags: list(c.StopTags), Defaults: machineSettings(defaults)}
}

// notesConfig returns the search settings of n. A list without words is
// none; blank words are left out.
func notesConfig(n *gennotes.NotesSettings) store.Config {
	var c store.Config
	if n == nil {
		return c
	}
	if n.Search != nil {
		c.Search = *n.Search
	}
	list := func(l *gennotes.StopList) []string {
		if l == nil || l.Use == stopDefault {
			return nil
		}
		words := []string{}
		if l.Use == stopList {
			for _, w := range l.Words {
				if w = strings.TrimSpace(w); w != "" {
					words = append(words, w)
				}
			}
		}
		return words
	}
	c.StopWords, c.StopTags = list(n.StopWords), list(n.StopTags)
	return c
}
