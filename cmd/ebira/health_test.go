package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ikawaha/ebira/internal/auth"
	"github.com/ikawaha/ebira/internal/daemon"
)

// TestHealthState checks what GET /health tells without a session: with
// passkeys, not the flags and the start time of the server.
func TestHealthState(t *testing.T) {
	st := daemon.State{PID: 42, Addr: "localhost:13579", API: 1, APIRev: 1, NotesDir: "/n", StartedAt: time.Now().UTC(), Instance: "i", Flags: []string{"-addr=localhost:13579", "-git-sync=5m"}}
	for mode, hidden := range map[string]bool{auth.ModePasskey: true, auth.ModeNone: false} {
		w := httptest.NewRecorder()
		daemon.HealthHandler(healthState(st, mode)).ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
		body := w.Body.String()
		for _, key := range []string{`"flags"`, `"started_at"`} {
			if strings.Contains(body, key) == hidden {
				t.Errorf("%s: /health = %s, want %s shown %v", mode, body, key, !hidden)
			}
		}
		var got daemon.State
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.PID != 42 || got.Addr != st.Addr || got.NotesDir != "/n" || got.API != 1 || got.Instance != "i" {
			t.Errorf("%s: /health = %+v, without what the clients need", mode, got)
		}
	}
}
