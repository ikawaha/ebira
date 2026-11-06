package main

import (
	"slices"
	"testing"
)

func TestAppArgs(t *testing.T) {
	for _, tc := range []struct {
		args, want []string
	}{
		{nil, nil},
		{[]string{"-notes", "/tmp/n", "-addr", "localhost:1"}, []string{"-notes", "/tmp/n", "-addr", "localhost:1"}},
		{[]string{"-ApplePersistenceIgnoreState", "YES", "-addr", "localhost:1"}, []string{"-addr", "localhost:1"}},
		{[]string{"-notes", "/tmp/n", "-AppleLanguages", "(ja)", "-NSDocumentRevisionsDebugMode", "YES"}, []string{"-notes", "/tmp/n"}},
		{[]string{"-psn_0_12345", "-addr", "localhost:1"}, []string{"-addr", "localhost:1"}},
	} {
		if got := appArgs(tc.args); !slices.Equal(got, tc.want) {
			t.Errorf("appArgs(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
