package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// api is the newest version of the REST API this app knows (daemon.API, and
// Server.api in the Swift app). The page comes from the server itself, so older
// servers are fine: earlier builds report no version, read as 0.
const api = 1

// apiRev is the revision within api this app relies on (daemon.APIRev): an
// older server lacks some of window.ebira, and the app offers to start it again.
const apiRev = 1

type health struct {
	Addr         string `json:"addr"`
	NotesDir     string `json:"notes_dir"`
	API          int    `json:"api"`
	APIRev       int    `json:"api_rev"`
	Version      string `json:"version"`       // the build: a hash of the executable
	VersionLabel string `json:"version_label"` // as the web UI shows it
}

func readHealth(addr string) (*health, error) {
	client := http.Client{Timeout: 2 * time.Second}
	res, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var h health
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

// connect finds the server of t, starting it if nothing answers. The server
// stays up after the app ends, shared with the TUI and the MCP clients.
func connect(t target) (*health, error) {
	notes, addr := t.Notes, t.Addr
	h, err := readHealth(addr)
	if err != nil {
		args := []string{"server", "start", "-addr", addr}
		if notes != "" {
			args = append(args, "-notes", notes)
		}
		bin, err := helper()
		if err != nil {
			return nil, err
		}
		if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("start the server: %w\n%s", err, out)
		}
		if h, err = readHealth(addr); err != nil {
			return nil, fmt.Errorf("read the server at %s: %w", addr, err)
		}
	}
	if h.API > api {
		return nil, fmt.Errorf("the server at %s has API version %d, newer than %d", addr, h.API, api)
	}
	if notes != "" {
		if want, err := realPath(notes); err != nil {
			return nil, err
		} else if got, err := realPath(h.NotesDir); err != nil || got != want {
			return nil, fmt.Errorf("the server at %s serves %s, not %s", addr, h.NotesDir, want)
		}
	}
	return h, nil
}

func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// helper finds the server: in the app bundle, or else on the PATH.
func helper() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		bin := filepath.Join(filepath.Dir(exe), "..", "Helpers", "ebira")
		if _, err := os.Stat(bin); err == nil {
			return bin, nil
		}
	}
	if bin, err := exec.LookPath("ebira"); err == nil {
		return bin, nil
	}
	return "", errors.New("the ebira command was not found")
}
