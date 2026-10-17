// Package daemon runs the ebira server in the background, one per data
// directory: the TUI starts it when it is not running, and it keeps running
// after the TUI exits, so that MCP clients and the web UI can use it.
//
// The files are in the state directory, <data-dir>/.ebira:
//   - server.lock: held with flock by the running server, so that a second
//     server for the same data directory fails to start. The OS releases it
//     when the process ends, so a stale file needs no cleaning.
//   - server.json: the State of the running server, to find and stop it.
//   - server.log: the output of a server started in the background.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Files in the state directory.
const (
	LockFile  = "server.lock"
	StateFile = "server.json"
	LogFile   = "server.log"
)

// ErrRunning is returned by Lock when a server holds the lock.
var ErrRunning = errors.New("a server is already running for this data directory")

// State describes a running server. It is written to server.json and served
// at GET /health.
type State struct {
	PID       int       `json:"pid"`
	Addr      string    `json:"addr"`
	Version   string    `json:"version"`
	DataDir   string    `json:"data_dir"`
	StartedAt time.Time `json:"started_at"`
	// Flags are the flags of ebira serve other than -data-dir, so that
	// ebira server restart starts the server again in the same way.
	Flags []string `json:"flags,omitempty"`
}

// WriteState writes s to server.json in dir.
func WriteState(dir string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, StateFile), append(b, '\n'), 0o644)
}

// ReadState reads server.json in dir.
func ReadState(dir string) (State, error) {
	var s State
	b, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// RemoveState removes server.json in dir.
func RemoveState(dir string) error {
	err := os.Remove(filepath.Join(dir, StateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

var (
	versionOnce sync.Once
	version     string
)

// Version identifies the build of the running binary: the first 12 hex digits
// of the SHA-256 of the executable. A rebuilt binary gets another version, so
// that a server left running from an older build can be noticed.
func Version() string {
	versionOnce.Do(func() {
		version = "unknown"
		exe, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.Open(exe)
		if err != nil {
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return
		}
		version = hex.EncodeToString(h.Sum(nil))[:12]
	})
	return version
}

// Health asks the server at addr for its State (GET /health).
func Health(ctx context.Context, addr string) (State, error) {
	var s State
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health", nil)
	if err != nil {
		return s, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return s, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return s, fmt.Errorf("GET /health: %s", res.Status)
	}
	return s, json.NewDecoder(res.Body).Decode(&s)
}

// HealthHandler serves s as JSON, for GET /health.
func HealthHandler(s State) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(s)
	})
}
