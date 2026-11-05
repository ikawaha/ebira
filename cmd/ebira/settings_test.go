package main

import (
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
	if want := []string{"addr", "git_sync", "lang", "notes_dir"}; !slices.Equal(got, want) {
		t.Errorf("overridden = %v, want %v", got, want)
	}
}
