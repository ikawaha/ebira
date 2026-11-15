package main

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ikawaha/ebira/internal/config"
)

func TestCheckSettings(t *testing.T) {
	if err := checkSettings(config.Config{Theme: "dark", Auth: "passkey", Lang: "ja", Provider: "gemini", TUIPreview: "source", GitSync: "10m"}); err != nil {
		t.Errorf("valid settings: %v", err)
	}
	for _, c := range []config.Config{
		{Theme: "pink"},
		{Auth: "password"},
		{Lang: "fr"},
		{Provider: "openai"},
		{TUIPreview: "raw"},
		{GitSync: "10s"},
	} {
		if err := checkSettings(c); err == nil {
			t.Errorf("%+v passed", c)
		}
	}
}

func TestOverriddenSettings(t *testing.T) {
	fs, _ := newServeFlags("serve")
	if err := fs.Parse([]string{"-notes", "/n", "-lang", "en", "-git-sync", "5m", "-addr", "localhost:1", "-search", "ja"}); err != nil {
		t.Fatal(err)
	}
	got := overriddenSettings(fs)
	slices.Sort(got)
	if want := []string{"addr", "git_sync", "lang", "notes_dir", "search"}; !slices.Equal(got, want) {
		t.Errorf("overridden = %v, want %v", got, want)
	}
}

// TestPageLook checks that the look of the page is read again from the
// settings file and the defaults in _ebira.yaml, but for the flags given.
func TestPageLook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	t.Setenv(config.Env, path)
	notes := filepath.Join(dir, "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args              []string
		file, defaults    string
		wantLang, wantThm string
	}{
		{nil, "lang: ja\n", "defaults:\n  theme: dark\n", "ja", "dark"},
		{nil, "theme: light\n", "defaults:\n  theme: dark\n", "", "light"}, // this machine wins
		{[]string{"-lang", "en"}, "lang: ja\n", "", "en", ""},              // the flag wins
	} {
		if err := os.WriteFile(path, []byte(tt.file), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(notes, "_ebira.yaml"), []byte(tt.defaults), 0o644); err != nil {
			t.Fatal(err)
		}
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		lang := fs.String("lang", "", "")
		theme := fs.String("theme", "", "")
		if err := fs.Parse(tt.args); err != nil {
			t.Fatal(err)
		}
		l := newPageLook(fs, notes, *lang, *theme)
		l.reload()
		if lang, theme := l.get(); lang != tt.wantLang || theme != tt.wantThm {
			t.Errorf("%v %q %q: look = %q, %q; want %q, %q", tt.args, tt.file, tt.defaults, lang, theme, tt.wantLang, tt.wantThm)
		}
	}
}
