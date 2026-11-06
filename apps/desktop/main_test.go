package main

import (
	"io"
	"net/http"
	"net/http/httptest"
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
