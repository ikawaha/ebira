package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/store"
)

// TestApplyDefaultsSafe checks that _ebira.yaml that cannot be read stops a
// command with the hint of the safe mode, and in the safe mode is left out,
// with its error kept to tell.
func TestApplyDefaultsSafe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, store.ConfigFile), []byte("search: ja\ndefaults:\n  lang: [ja\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { notesSettingsError, settingsError = nil, nil }()
	newFS := func() *flag.FlagSet {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("lang", "", "")
		return fs
	}

	t.Setenv(config.SafeEnv, "")
	err := applyDefaults(newFS(), dir)
	if err == nil || !strings.Contains(err.Error(), config.SafeEnv) {
		t.Errorf("applyDefaults outside the safe mode = %v, want the error with the hint", err)
	}
	if notesSettingsError != nil {
		t.Errorf("an error kept outside the safe mode: %v", notesSettingsError)
	}

	t.Setenv(config.SafeEnv, "1")
	if err := applyDefaults(newFS(), dir); err != nil {
		t.Errorf("applyDefaults in the safe mode = %v, want none", err)
	}
	if notesSettingsError == nil || !strings.Contains(safeModeErrors(), store.ConfigFile) {
		t.Errorf("the error of %s is not kept to tell: %q", store.ConfigFile, safeModeErrors())
	}
	if err := notesSettingsCheck(dir); err == nil {
		t.Error("notesSettingsCheck read a broken file")
	}
	if err := notesSettingsCheck(t.TempDir()); err != nil {
		t.Errorf("notesSettingsCheck of a folder without the file = %v", err)
	}
}
