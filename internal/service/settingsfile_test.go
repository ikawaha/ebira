package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/config"
)

func TestSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv(config.Env, path)
	person := AsPerson(context.Background())
	svc, _ := newTestService(t)
	restarted := 0
	svc.SetSettingsFile(SettingsFile{
		Check: func(b []byte) error {
			if c, _ := config.Parse(b, path); c.Theme == "pink" {
				return errors.New("unknown theme")
			}
			return nil
		},
		Overridden: []string{"notes_dir", "addr"},
		Restart:    func() error { restarted++; return nil },
	})

	// A missing file reads as empty.
	f, err := svc.SettingsFile(person, &gennotes.SettingsFilePayload{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Exists || f.Content != "" || f.Path != path || len(f.Overridden) != 2 {
		t.Errorf("missing file = %+v", f)
	}

	// Saving writes the content as it is, with its comments.
	content := "# mine\nlang: ja\n"
	saved, err := svc.SaveSettingsFile(person, &gennotes.SaveSettingsFilePayload{Content: content, Version: f.Version})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != content || !saved.Exists || saved.Version == f.Version {
		t.Errorf("saved %q, result %+v", b, saved)
	}

	// The version of an earlier read fails, as the file was changed since.
	if _, err := svc.SaveSettingsFile(person, &gennotes.SaveSettingsFilePayload{Content: "lang: en\n", Version: f.Version}); errorName(err) != "precondition_failed" {
		t.Errorf("stale version = %v, want precondition_failed", err)
	}
	// Invalid settings are refused, and the file is kept.
	for _, bad := range []string{"thme: dark\n", "theme: pink\n", "lang: [\n"} {
		if _, err := svc.SaveSettingsFile(person, &gennotes.SaveSettingsFilePayload{Content: bad, Version: saved.Version}); errorName(err) != "bad_request" {
			t.Errorf("%q = %v, want bad_request", bad, err)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != content {
		t.Errorf("the file changed: %q", b)
	}

	if err := svc.RestartServer(person, &gennotes.RestartServerPayload{}); err != nil || restarted != 1 {
		t.Errorf("restart = %v, %d times", err, restarted)
	}
	svc.SetSettingsFile(SettingsFile{})
	if err := svc.RestartServer(person, &gennotes.RestartServerPayload{}); errorName(err) != "unavailable" {
		t.Errorf("restart without a way = %v, want unavailable", err)
	}
}

// TestSaveSettingsFileVerification checks that saving the settings file,
// which decides how the server authenticates people, asks for a session that
// signed in with a passkey recently while passkeys are required.
func TestSaveSettingsFileVerification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv(config.Env, path)
	svc, _ := newTestService(t)
	ctx := context.Background()
	for _, verified := range []bool{false, true} {
		rest := gennotes.NewEndpoints(REST{Notes: svc, Session: func(string) (bool, bool) { return true, verified }})
		session := "s"
		f, err := rest.SettingsFile(ctx, &gennotes.SettingsFilePayload{Session: &session})
		if err != nil {
			t.Fatal(err)
		}
		_, err = rest.SaveSettingsFile(ctx, &gennotes.SaveSettingsFilePayload{Session: &session, Content: "auth: none\n", Version: f.(*gennotes.SettingsFile).Version})
		if want := !verified; (errorName(err) == "verification_required") != want {
			t.Errorf("verified %v: err = %v", verified, err)
		}
	}
}
