package main

import (
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestVersionText(t *testing.T) {
	at := time.Date(2026, 10, 22, 1, 2, 3, 0, time.UTC)
	bi := &debug.BuildInfo{
		GoVersion: "go1.26.0",
		Main:      debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "4cf9701c0123456789abcdef"},
			{Key: "vcs.time", Value: at.Format(time.RFC3339)},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	got := versionText(bi, "9063f7980e96")
	for _, want := range []string{
		"version:   v0.1.0\n",
		"commit:    4cf9701c0123 (" + at.Local().Format(time.DateTime) + ", with changes not committed)\n",
		"build:     9063f7980e96\n",
		"go:        go1.26.0 ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("versionText() = %q, want %q in it", got, want)
		}
	}

	// Without the build information, such as in a stripped binary.
	got = versionText(nil, "unknown")
	if !strings.Contains(got, "version:   unknown\ncommit:    unknown\n") {
		t.Errorf("versionText(nil) = %q", got)
	}
}
