package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

func TestLocal(t *testing.T) {
	for addr, want := range map[string]bool{
		"localhost:13579":   true,
		"127.0.0.1:13579":   true,
		"[::1]:13579":       true,
		"192.168.0.2:13579": false,
		"example.com:13579": false,
		"localhost":         false, // no port: not an address of a server
	} {
		if got := local(addr); got != want {
			t.Errorf("local(%q) = %v, want %v", addr, got, want)
		}
	}
}

// healthServer answers GET /health with status and body.
func healthServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestReadHealth(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		ok     bool
	}{
		{200, `{"addr":"localhost:1","notes_dir":"/n","api":1}`, true},
		{404, `{"error":"unavailable"}`, false},        // an error, as JSON
		{200, `{"error":"unavailable"}`, false},        // no ebira server
		{200, `<html>not found</html>`, false},         // a page
		{200, `{"addr":"localhost:1","api":1}`, false}, // no notes directory
	} {
		_, err := readHealth(healthServer(t, tc.status, tc.body))
		if (err == nil) != tc.ok {
			t.Errorf("readHealth of %d %s: %v, want ok %v", tc.status, tc.body, err, tc.ok)
		}
	}
}

// TestOwnServer checks that what answers at a local address is taken for the
// server of the notes folder only as the state of this user's server tells.
func TestOwnServer(t *testing.T) {
	state := func(pid int) *struct {
		PID int `json:"pid"`
	} {
		return &struct {
			PID int `json:"pid"`
		}{pid}
	}
	h := &health{PID: 4242, Addr: "localhost:13579", NotesDir: "/n"}
	for _, tc := range []struct {
		name string
		st   status
		ok   bool
	}{
		{"its server", status{NotesDir: "/n", Running: true, State: state(4242)}, true},
		{"not running", status{NotesDir: "/n"}, false},
		{"another process", status{NotesDir: "/n", Running: true, State: state(1)}, false},
		{"no state yet", status{NotesDir: "/n", Running: true}, false},
	} {
		if err := ownServer("localhost:13579", &tc.st, h); (err == nil) != tc.ok {
			t.Errorf("%s: ownServer = %v, want ok %v", tc.name, err, tc.ok)
		}
	}
	if err := ownServer("localhost:13579", &status{NotesDir: "/n", Running: true, State: state(0)}, &health{NotesDir: "/n"}); err == nil {
		t.Error("ownServer took a server without a PID")
	}
}

// TestConnectSquatter checks connect against a program that answers at the
// address as a server of the user's folder while the user's server is not
// running, with an ebira that tells the state as the real one does.
func TestConnectSquatter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ebira of the test is a shell script")
	}
	notes := t.TempDir()
	addr := healthServer(t, 200, `{"pid":4242,"addr":"localhost:13579","notes_dir":"`+notes+`","api":1}`)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	ebira := func(running bool, pid int) {
		t.Helper()
		st := fmt.Sprintf(`{"notes_dir":%q,"running":%v,"state":{"pid":%d}}`, notes, running, pid)
		if err := os.WriteFile(filepath.Join(bin, "ebira"), []byte("#!/bin/sh\necho '"+st+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ebira(false, 0)
	if _, err := connect(target{Notes: notes, Addr: addr}); err == nil {
		t.Error("connect took the program for the server, which is not running")
	}
	ebira(true, 7)
	if _, err := connect(target{Notes: notes, Addr: addr}); err == nil {
		t.Error("connect took the program for the server, which is another process")
	}
	ebira(true, 4242)
	if _, err := connect(target{Notes: notes, Addr: addr}); err != nil {
		t.Errorf("connect to the server: %v", err)
	}
}

func TestConnectRemote(t *testing.T) {
	addr := healthServer(t, 200, `{"addr":"other:13579","notes_dir":"/srv/notes","api":1}`)
	for _, tc := range []struct {
		notes string
		ok    bool
	}{
		{"", true},            // as the server is
		{"/srv/notes/", true}, // the path on that host
		{"/Users/me/notes", false},
	} {
		_, err := connectRemote(target{Notes: tc.notes, Addr: addr})
		if (err == nil) != tc.ok {
			t.Errorf("connectRemote with notes %q: %v, want ok %v", tc.notes, err, tc.ok)
		}
	}
}
