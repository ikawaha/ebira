// Package daemon runs the ebira server in the background, one per notes
// directory: the TUI starts it when it is not running, and it keeps running
// after the TUI exits, so that MCP clients and the web UI can use it.
//
// The files are in the state directory of the notes directory (see
// config.StateDir):
//   - server.lock: held with flock by the running server, so that a second
//     server for the same notes directory fails to start. The OS releases it
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
	"net"
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
var ErrRunning = errors.New("a server is already running for this notes directory")

// State describes a running server. It is written to server.json and served
// at GET /health.
type State struct {
	PID       int       `json:"pid"`
	Addr      string    `json:"addr"`
	Version   string    `json:"version"`
	NotesDir  string    `json:"notes_dir"`
	StartedAt time.Time `json:"started_at"`
	// Flags are the flags of ebira serve other than -notes, so that
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

// ErrNotSelf reports that a server listens on its address, but another
// program answers requests sent there.
var ErrNotSelf = errors.New("another program answers on the address")

// CheckSelf asks GET /health at addr, the address a server listens on, until
// the server of pid answers, for up to wait. Listening does not show that
// requests reach the server: on some systems, another program listening on
// the same port, such as on all addresses, can take them. CheckSelf then
// returns ErrNotSelf with what answered.
func CheckSelf(ctx context.Context, addr string, pid int, wait time.Duration) error {
	addr = dialAddr(addr)
	deadline := time.Now().Add(wait)
	for {
		s, err := Health(ctx, addr)
		if err == nil && s.PID == pid {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("pid %d answers", s.PID)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w %s (%v)", ErrNotSelf, addr, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// dialAddr returns addr to connect to: a server listening on all addresses,
// such as :13579 or 0.0.0.0:13579, is asked at localhost.
func dialAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		return net.JoinHostPort("localhost", port)
	}
	return addr
}

// HealthHandler serves s as JSON, for GET /health.
func HealthHandler(s State) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(s)
	})
}
