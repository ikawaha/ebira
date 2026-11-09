package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitBodies(t *testing.T) {
	read := limitBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		}
	}))
	for _, tc := range []struct {
		method, path string
		size         int
		ok           bool
	}{
		{"PATCH", "/notes/a", 1 << 20, true},
		{"PATCH", "/notes/a", maxBody + 1, false},
		{"POST", "/mcp", maxBody, true},
		{"POST", "/mcp", maxBody + 1, false},
		{"POST", "/import", maxBody + 1, true},
		{"POST", "/notes/a/files", maxFileBody + 1, false},
		{"POST", "/notes/a/files", maxFileBody, true},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(strings.Repeat("a", tc.size)))
		w := httptest.NewRecorder()
		read.ServeHTTP(w, r)
		if ok := w.Code == http.StatusOK; ok != tc.ok {
			t.Errorf("%s %s of %d bytes: %d, want ok %v", tc.method, tc.path, tc.size, w.Code, tc.ok)
		}
	}
}
