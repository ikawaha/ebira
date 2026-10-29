package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/ikawaha/ebira/internal/authclient"
	"github.com/ikawaha/ebira/internal/index"
)

// stats is the answer of GET /debug/stats: the state of the server process
// and of its index.
type stats struct {
	PID       int         `json:"pid"`
	StartedAt time.Time   `json:"started_at"`
	Uptime    string      `json:"uptime"`
	Go        string      `json:"go"`
	Process   procStats   `json:"process"`
	Index     index.Stats `json:"index"`
}

// procStats is the memory of the process. The C code of SQLite, translated
// to Go, takes its memory outside the Go heap, so it shows in MaxRSS but not
// in the heap.
type procStats struct {
	// MaxRSS is the largest resident set size so far, in bytes (0 where it
	// cannot be read).
	MaxRSS       int64  `json:"max_rss"`
	Goroutines   int    `json:"goroutines"`
	HeapAlloc    uint64 `json:"heap_alloc"`
	HeapInuse    uint64 `json:"heap_inuse"`
	HeapIdle     uint64 `json:"heap_idle"`
	HeapReleased uint64 `json:"heap_released"`
	HeapObjects  uint64 `json:"heap_objects"`
	StackInuse   uint64 `json:"stack_inuse"`
	// Sys is all the memory that the Go runtime got from the OS.
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"num_gc"`
	GCPauseTotal uint64 `json:"gc_pause_total_ns"`
	NextGC       uint64 `json:"next_gc"`
}

// statsHandler serves GET /debug/stats. With passkeys (check is not nil), it
// asks for a session signed in with a passkey, as the REST API does.
func statsHandler(ix *index.Index, startedAt time.Time, check func(session string) (valid, verified bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if check != nil {
			c, err := r.Cookie(authclient.CookieName)
			if err != nil || !first(check(c.Value)) {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"name": "unauthorized", "message": "sign in with a passkey"})
				return
			}
		}
		is, err := ix.Stats(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "index stats failed", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "internal", "message": err.Error()})
			return
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		s := stats{
			PID:       os.Getpid(),
			StartedAt: startedAt,
			Uptime:    time.Since(startedAt).Truncate(time.Second).String(),
			Go:        runtime.Version(),
			Process: procStats{
				MaxRSS:       maxRSS(),
				Goroutines:   runtime.NumGoroutine(),
				HeapAlloc:    m.HeapAlloc,
				HeapInuse:    m.HeapInuse,
				HeapIdle:     m.HeapIdle,
				HeapReleased: m.HeapReleased,
				HeapObjects:  m.HeapObjects,
				StackInuse:   m.StackInuse,
				Sys:          m.Sys,
				NumGC:        m.NumGC,
				GCPauseTotal: m.PauseTotalNs,
				NextGC:       m.NextGC,
			},
			Index: is,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(s)
	})
}

func first(a, _ bool) bool { return a }
