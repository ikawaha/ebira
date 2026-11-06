package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/config"
)

func TestSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv(config.Env, path)
	if err := os.WriteFile(path, []byte("# mine\nnotes_dir: ~/notes\nlang: en # the UI\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	registered := false
	restarted := 0
	svc.SetSettings(Settings{
		Check: func(c config.Config) error {
			if c.Lang == "fr" {
				return errors.New("unknown lang")
			}
			return nil
		},
		Overridden:    []string{"notes_dir", "addr"},
		Addr:          "localhost:1",
		Passkeys:      func() bool { return registered },
		DefaultModels: map[string]string{"gemini": "g"},
		Restart:       func() error { restarted++; return nil },
	})

	got, err := svc.Settings(person, &gennotes.SettingsPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Machine.Lang == nil || *got.Machine.Lang != "en" || got.Notes.Search != nil || got.Notes.StopWords.Use != "default" ||
		got.MachinePath != path || got.Addr != "localhost:1" || !got.Restart || got.Passkeys || got.DefaultModels["gemini"] != "g" {
		t.Errorf("settings = %+v, machine %+v, notes %+v", got, got.Machine, got.Notes)
	}

	// Saving writes only the keys of the settings, keeping the rest with the
	// comments, and the settings synced with the notes.
	ja, dark, search := "ja", "dark", "ja"
	saved, err := svc.SaveSettings(person, &gennotes.SaveSettingsPayload{
		Machine: &gennotes.MachineSettings{Lang: &ja, Theme: &dark},
		Notes:   &gennotes.NotesSettings{Search: &search, StopWords: &gennotes.StopList{Use: "list", Words: []string{" の ", ""}}, StopTags: &gennotes.StopList{Use: "none"}},
		Version: got.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{"# mine", "notes_dir: ~/notes", "lang: ja # the UI", "theme: dark"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings file lacks %q:\n%s", want, b)
		}
	}
	if saved.Notes.Search == nil || *saved.Notes.Search != "ja" || saved.Notes.StopWords.Use != "list" || !slices.Equal(saved.Notes.StopWords.Words, []string{"の"}) || saved.Notes.StopTags.Use != "none" {
		t.Errorf("notes settings = %+v", saved.Notes)
	}
	if saved.Version == got.Version {
		t.Error("the version did not change")
	}

	// An earlier version fails, as the settings were changed since.
	if _, err := svc.SaveSettings(person, &gennotes.SaveSettingsPayload{Machine: &gennotes.MachineSettings{}, Notes: &gennotes.NotesSettings{}, Version: got.Version}); errorName(err) != "precondition_failed" {
		t.Errorf("stale version = %v, want precondition_failed", err)
	}
	// Invalid settings are refused, and nothing is written.
	fr, passkey := "fr", "passkey"
	for name, m := range map[string]*gennotes.MachineSettings{"lang": {Lang: &fr}, "passkey without one": {Auth: &passkey}} {
		if _, err := svc.SaveSettings(person, &gennotes.SaveSettingsPayload{Machine: m, Notes: &gennotes.NotesSettings{}, Version: saved.Version}); errorName(err) != "bad_request" {
			t.Errorf("%s = %v, want bad_request", name, err)
		}
	}
	if b2, _ := os.ReadFile(path); string(b2) != string(b) {
		t.Errorf("the file changed:\n%s", b2)
	}
	// With a passkey registered, auth: passkey is taken.
	registered = true
	if _, err := svc.SaveSettings(person, &gennotes.SaveSettingsPayload{Machine: &gennotes.MachineSettings{Auth: &passkey}, Notes: &gennotes.NotesSettings{}, Version: saved.Version}); err != nil {
		t.Errorf("passkey with one = %v", err)
	}

	if err := svc.RestartServer(person, &gennotes.RestartServerPayload{}); err != nil || restarted != 1 {
		t.Errorf("restart = %v, %d times", err, restarted)
	}
	// A server that cannot start again says so before it is asked to.
	svc.SetSettings(Settings{Restart: func() error { restarted++; return nil }, Restartable: func() string { return RestartExecutableGone }})
	if f, err := svc.Settings(person, &gennotes.SettingsPayload{}); err != nil || f.Restart || f.RestartUnavailable == nil || *f.RestartUnavailable != RestartExecutableGone {
		t.Errorf("not restartable = %+v, %v", f, err)
	}
	if err := svc.RestartServer(person, &gennotes.RestartServerPayload{}); errorName(err) != "unavailable" || restarted != 1 || strings.Contains(err.Error(), "/") {
		t.Errorf("restart while not restartable = %v, %d times", err, restarted)
	}
	// An unknown reason is told as unsupported, which the UI knows.
	svc.SetSettings(Settings{Restart: func() error { return nil }, Restartable: func() string { return "/some/path" }})
	if f, _ := svc.Settings(person, &gennotes.SettingsPayload{}); f.RestartUnavailable == nil || *f.RestartUnavailable != RestartUnsupported {
		t.Errorf("unknown reason = %v", f.RestartUnavailable)
	}
	svc.SetSettings(Settings{})
	if err := svc.RestartServer(person, &gennotes.RestartServerPayload{}); errorName(err) != "unavailable" {
		t.Errorf("restart without a way = %v, want unavailable", err)
	}
}

// TestSaveSettingsVerification checks that saving the settings, which decide
// how the server authenticates people, asks for a session that signed in
// with a passkey recently while passkeys are required.
func TestSaveSettingsVerification(t *testing.T) {
	t.Setenv(config.Env, filepath.Join(t.TempDir(), "config.yaml"))
	svc, _ := newTestService(t)
	ctx := context.Background()
	for _, verified := range []bool{false, true} {
		rest := gennotes.NewEndpoints(REST{Notes: svc, Session: func(string) (bool, bool) { return true, verified }})
		session := "s"
		f, err := rest.Settings(ctx, &gennotes.SettingsPayload{Session: &session})
		if err != nil {
			t.Fatal(err)
		}
		_, err = rest.SaveSettings(ctx, &gennotes.SaveSettingsPayload{Session: &session, Machine: &gennotes.MachineSettings{}, Notes: &gennotes.NotesSettings{}, Version: f.(*gennotes.Settings).Version})
		if want := !verified; (errorName(err) == "verification_required") != want {
			t.Errorf("verified %v: err = %v", verified, err)
		}
	}
}
