// Package claudecode registers ebira as an MCP server of Claude Code, with its
// command claude, for ebira mcp add and the button of the web UI.
//
// It registers ebira in the user scope, so that every project can use it, and
// leaves the registrations of the local and project scopes alone: they belong
// to a project.
package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Name is the name of ebira among the MCP servers of Claude Code.
const Name = "ebira"

// Scope is the scope ebira registers in.
const Scope = "user"

// ErrNotFound is the error of Find without claude.
var ErrNotFound = errors.New("claude (Claude Code) is not found")

// commandTimeout bounds each run of claude; claude mcp get connects to the
// server it shows, to tell its state.
const commandTimeout = 30 * time.Second

// URL returns the URL of MCP of a server listening on addr. A server that
// listens on every address takes requests to localhost, which its check of
// the Host header lets in.
func URL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/mcp"
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/mcp"
}

// AddArgs returns the arguments of claude that register url.
func AddArgs(url string) []string {
	return []string{"mcp", "add", "--transport", "http", "--scope", Scope, Name, url}
}

// Command returns the command line that registers url, to show.
func Command(url string) string {
	return "claude " + strings.Join(AddArgs(url), " ")
}

// Client runs claude.
type Client struct {
	// Bin is the path of claude.
	Bin string
	// Path is the PATH to run it with, when it differs from that of this
	// process: claude may be a script that needs node from there.
	Path string
}

// Find finds claude: in PATH, else where its installers put it, else in the
// PATH of the user's login shell. A server started by the desktop app, from
// the Finder, has a PATH without the directories of the shell.
func Find(ctx context.Context) (Client, error) {
	if bin, err := exec.LookPath("claude"); err == nil {
		return Client{Bin: bin}, nil
	}
	if runtime.GOOS != "windows" {
		if c, err := fromShell(ctx); err == nil {
			return c, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range []string{
			filepath.Join(home, ".local", "bin", "claude"),
			filepath.Join(home, ".claude", "local", "claude"),
			"/opt/homebrew/bin/claude",
			"/usr/local/bin/claude",
		} {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return Client{Bin: p}, nil
			}
		}
	}
	return Client{}, ErrNotFound
}

// fromShell asks the login shell of the user where claude is, and for its
// PATH.
func fromShell(ctx context.Context) (Client, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-lc", `command -v claude; printf '%s\n' "$PATH"`).Output()
	if err != nil {
		return Client{}, ErrNotFound
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 || !filepath.IsAbs(lines[0]) {
		return Client{}, ErrNotFound
	}
	return Client{Bin: lines[0], Path: lines[len(lines)-1]}, nil
}

// Registration is ebira as Claude Code has it.
type Registration struct {
	// Scope is local, project or user.
	Scope string `json:"scope"`
	URL   string `json:"url"`
}

// Get returns the registration of ebira that every project sees, or nil
// without one. It runs claude in an empty directory, outside every project.
func (c Client) Get(ctx context.Context) (*Registration, error) {
	out, err := c.run(ctx, "mcp", "get", Name)
	if err != nil {
		if strings.Contains(out, "No MCP server named") {
			return nil, nil
		}
		return nil, err
	}
	return parseGet(out), nil
}

// parseGet reads the output of claude mcp get, such as
//
//	ebira:
//	  Scope: User config (available in all your projects)
//	  Type: http
//	  URL: http://localhost:13579/mcp
func parseGet(out string) *Registration {
	var r Registration
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Scope":
			switch lower := strings.ToLower(value); {
			case strings.HasPrefix(lower, "user"):
				r.Scope = "user"
			case strings.HasPrefix(lower, "project"):
				r.Scope = "project"
			case strings.HasPrefix(lower, "local"):
				r.Scope = "local"
			default:
				r.Scope = value
			}
		case "URL":
			r.URL = value
		}
	}
	return &r
}

// Result is what Add did.
type Result struct {
	// Status is added, replaced (another URL was registered) or unchanged.
	Status string `json:"status"`
	// Previous is the URL that was registered, when it was replaced.
	Previous string `json:"previous,omitempty"`
}

// Add registers url as ebira in the user scope. It replaces a registration
// of another URL there, and leaves one of the same URL as it is.
func (c Client) Add(ctx context.Context, url string) (Result, error) {
	reg, err := c.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	var res Result
	switch {
	case reg == nil:
		res.Status = "added"
	case reg.URL == url:
		return Result{Status: "unchanged"}, nil
	case reg.Scope != "" && reg.Scope != Scope:
		// Outside every project, only the user scope should show.
		return Result{}, fmt.Errorf("claude has %s in the %s scope, not in the %s scope", Name, reg.Scope, Scope)
	default:
		res = Result{Status: "replaced", Previous: reg.URL}
		if _, err := c.run(ctx, "mcp", "remove", Name, "--scope", Scope); err != nil {
			return Result{}, err
		}
	}
	if _, err := c.run(ctx, AddArgs(url)...); err != nil {
		return Result{}, err
	}
	return res, nil
}

// run runs claude with args in an empty directory, and returns its output.
func (c Client) run(ctx context.Context, args ...string) (string, error) {
	dir, err := os.MkdirTemp("", "ebira-claude-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Dir = dir
	if c.Path != "" {
		cmd.Env = append(os.Environ(), "PATH="+c.Path)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(out.String())
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("claude %s: %s", strings.Join(args[:min(len(args), 2)], " "), text)
	}
	return out.String(), nil
}
