package main

import (
	"strings"
	"testing"
	"time"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestPrintSync(t *testing.T) {
	up, id, cp := "origin/main", "a1", "b2"
	var out strings.Builder
	printSync(&gennotes.GitSyncResult{
		Committed: 2, Upstream: &up, Merged: 3, Pushed: true,
		Conflicts: []*gennotes.GitConflict{
			{Path: "inbox/a1.yaml", NoteID: &id, CopyID: &cp},
			{Path: "dev/_notebook.yaml"},
		},
		Errors: []string{"bad.yaml: broken"},
	}, &out)
	for _, want := range []string{
		"Files committed: 2\n",
		"Files merged from origin/main: 3\n",
		"Pushed to origin/main\n",
		"Files changed on both sides: 2\n",
		"  inbox/a1.yaml: kept this version; the other is the new note b2\n",
		"  dev/_notebook.yaml: kept this or the changed version",
		"Cannot read: bad.yaml: broken\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in\n%s", want, out.String())
		}
	}

	out.Reset()
	printSync(&gennotes.GitSyncResult{Conflicts: []*gennotes.GitConflict{}, Errors: []string{}}, &out)
	if !strings.Contains(out.String(), "No upstream branch") {
		t.Errorf("no upstream:\n%s", out.String())
	}
}

func TestGitSyncInterval(t *testing.T) {
	for s, want := range map[string]time.Duration{"": 0, "0": 0, "0s": 0, "10m": 10 * time.Minute, "1h": time.Hour} {
		if d, err := gitSyncInterval(s); err != nil || d != want {
			t.Errorf("%q: %v, %v", s, d, err)
		}
	}
	for _, s := range []string{"30s", "10", "often", "-5m"} {
		if _, err := gitSyncInterval(s); err == nil {
			t.Errorf("%q: no error", s)
		}
	}
}
