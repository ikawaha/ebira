package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// What answers at addr may be no ebira server, as an error page.
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answers %s, not as an ebira server", addr, res.Status)
	}
	var h health
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return nil, fmt.Errorf("%s answers, not as an ebira server: %w", addr, err)
	}
	if h.Addr == "" || h.NotesDir == "" {
		return nil, fmt.Errorf("%s answers, not as an ebira server", addr)
	}
	return &h, nil
}

// configuredNotes asks the ebira of the app for the notes directory of
// notes_dir in the config file of ebira, which an empty notes folder follows.
func configuredNotes() (string, error) {
	bin, err := helper()
	if err != nil {
		return "", err
	}
	out, err := helperCommand(bin, "server", "status", "-json").Output()
	if err != nil {
		// As an ebira on the PATH older than -json says.
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", fmt.Errorf("find the notes folder of the config file of ebira: %w", err)
	}
	var st struct {
		NotesDir string `json:"notes_dir"`
	}
	if err := json.Unmarshal(out, &st); err != nil || st.NotesDir == "" {
		return "", fmt.Errorf("find the notes folder of the config file of ebira: %s", out)
	}
	return st.NotesDir, nil
}

// connect finds the server of t, starting it if nothing answers. The server
// stays up after the app ends, shared with the TUI and the MCP clients.
func connect(t target) (*health, error) {
	notes, addr := t.Notes, t.Addr
	if !local(addr) {
		return connectRemote(t)
	}
	if notes == "" {
		// Checked as if chosen, so that the server of another folder at addr
		// is not taken for the one of the config file.
		var err error
		if notes, err = configuredNotes(); err != nil {
			return nil, err
		}
	}
	h, err := readHealth(addr)
	if err != nil {
		args := []string{"server", "start", "-addr", addr, "-notes", notes}
		bin, err := helper()
		if err != nil {
			return nil, err
		}
		if out, err := helperCommand(bin, args...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("start the server: %w\n%s", err, out)
		}
		if h, err = readHealth(addr); err != nil {
			return nil, fmt.Errorf("read the server at %s: %w", addr, err)
		}
	}
	if err := h.usable(addr); err != nil {
		return nil, err
	}
	if want, err := realPath(notes); err != nil {
		return nil, err
	} else if got, err := realPath(h.NotesDir); err != nil || got != want {
		return nil, fmt.Errorf("the server at %s serves %s, not %s", addr, h.NotesDir, want)
	}
	return h, nil
}

// connectRemote takes the server at the address of another host as it is:
// the app neither starts it nor sees its files, nor the config file there. A
// notes folder chosen is the path on that host.
func connectRemote(t target) (*health, error) {
	h, err := readHealth(t.Addr)
	if err != nil {
		return nil, err
	}
	if err := h.usable(t.Addr); err != nil {
		return nil, err
	}
	if t.Notes != "" && filepath.Clean(t.Notes) != filepath.Clean(h.NotesDir) {
		return nil, fmt.Errorf("the server at %s serves %s, not %s", t.Addr, h.NotesDir, t.Notes)
	}
	return h, nil
}

// usable refuses a server whose API is newer than this app knows.
func (h *health) usable(addr string) error {
	if h.API > api {
		return fmt.Errorf("the server at %s has API version %d, newer than %d", addr, h.API, api)
	}
	return nil
}

func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// bundled is "true" in the builds of the scripts (build.sh,
// build-windows.sh and build-windows.ps1), which put the ebira of the app
// with it, set with -ldflags "-X main.bundled=true". Such an app runs that
// ebira only, not one found on the PATH, which may be another program; a
// build for development, as with go run, uses the PATH.
var bundled string

// helper finds the server: in the app bundle on macOS, beside the app on
// Windows, or else, in a build for development, on the PATH.
func helper() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		for _, bin := range []string{filepath.Join(dir, "..", "Helpers", "ebira"), filepath.Join(dir, "ebira.exe")} {
			if _, err := os.Stat(bin); err == nil {
				return bin, nil
			}
		}
	}
	if bundled == "true" {
		return "", errors.New("the ebira command of the app is missing: install the app again")
	}
	if bin, err := exec.LookPath("ebira"); err == nil {
		return bin, nil
	}
	return "", errors.New("the ebira command was not found")
}
