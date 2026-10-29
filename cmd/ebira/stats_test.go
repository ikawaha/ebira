package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/authclient"
	"github.com/ikawaha/ebira/internal/index"
)

func TestStatsHandler(t *testing.T) {
	ix, err := index.Open(context.Background(), filepath.Join(t.TempDir(), "index.sqlite"), index.Config{Mode: index.ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	started := time.Now().Add(-time.Minute)
	check := func(session string) (bool, bool) { return session == "good", false }

	tests := []struct {
		name    string
		check   func(string) (bool, bool)
		session string
		want    int
	}{
		{name: "no passkeys", want: http.StatusOK},
		{name: "signed in", check: check, session: "good", want: http.StatusOK},
		{name: "bad session", check: check, session: "bad", want: http.StatusUnauthorized},
		{name: "no session", check: check, want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/debug/stats", nil)
			if tt.session != "" {
				req.AddCookie(&http.Cookie{Name: authclient.CookieName, Value: tt.session})
			}
			rec := httptest.NewRecorder()
			statsHandler(ix, started, tt.check).ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			if tt.want != http.StatusOK {
				return
			}
			var s stats
			if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
				t.Fatal(err)
			}
			if s.PID == 0 || s.Process.HeapAlloc == 0 || s.Process.MaxRSS == 0 || s.Index.Mode != index.ModeBigram {
				t.Errorf("stats = %+v", s)
			}
		})
	}
}
