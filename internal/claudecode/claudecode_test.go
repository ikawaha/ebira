package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestURL(t *testing.T) {
	for addr, want := range map[string]string{
		"localhost:13579":    "http://localhost:13579/mcp",
		"127.0.0.1:8080":     "http://127.0.0.1:8080/mcp",
		":13579":             "http://localhost:13579/mcp",
		"0.0.0.0:13579":      "http://localhost:13579/mcp",
		"[::]:13579":         "http://localhost:13579/mcp",
		"[::1]:13579":        "http://[::1]:13579/mcp",
		"notes.example:1234": "http://notes.example:1234/mcp",
	} {
		if got := URL(addr); got != want {
			t.Errorf("URL(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestCommand(t *testing.T) {
	want := "claude mcp add --transport http --scope user ebira http://localhost:13579/mcp"
	if got := Command("http://localhost:13579/mcp"); got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
}

func TestParseGet(t *testing.T) {
	out := `ebira:
  Scope: User config (available in all your projects)
  Status: ✔ Connected
  Type: http
  URL: http://localhost:13579/mcp

To remove this server, run: claude mcp remove ebira -s user
`
	got := parseGet(out)
	if got.Scope != "user" || got.URL != "http://localhost:13579/mcp" {
		t.Errorf("parseGet = %+v", got)
	}
	if got := parseGet("ebira:\n  Scope: Local config (private to you in this project)\n"); got.Scope != "local" {
		t.Errorf("scope = %q, want local", got.Scope)
	}
}

// fakeClaude writes a claude that keeps the URL of ebira in a file, and logs
// its arguments.
func fakeClaude(t *testing.T) (Client, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a shell script")
	}
	dir := t.TempDir()
	state := filepath.Join(dir, "url")
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$@" >> '` + log + `'
case "$2" in
get)
  if [ -f '` + state + `' ]; then
    printf 'ebira:\n  Scope: User config (available in all your projects)\n  Type: http\n  URL: %s\n' "$(cat '` + state + `')"
  else
    echo 'No MCP server named "ebira". Configured servers: none' >&2
    exit 1
  fi ;;
add) printf '%s' "$8" > '` + state + `' ;;
remove) rm '` + state + `' ;;
esac
`
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Client{Bin: bin}, log
}

func TestAdd(t *testing.T) {
	c, log := fakeClaude(t)
	ctx := context.Background()
	if reg, err := c.Get(ctx); err != nil || reg != nil {
		t.Fatalf("Get = %v, %v; want nothing", reg, err)
	}
	steps := []struct {
		url  string
		want Result
	}{
		{"http://localhost:13579/mcp", Result{Status: "added"}},
		{"http://localhost:13579/mcp", Result{Status: "unchanged"}},
		{"http://localhost:13600/mcp", Result{Status: "replaced", Previous: "http://localhost:13579/mcp"}},
	}
	for _, s := range steps {
		got, err := c.Add(ctx, s.url)
		if err != nil {
			t.Fatal(err)
		}
		if got != s.want {
			t.Errorf("Add(%s) = %+v, want %+v", s.url, got, s.want)
		}
	}
	reg, err := c.Get(ctx)
	if err != nil || reg == nil || reg.URL != "http://localhost:13600/mcp" || reg.Scope != "user" {
		t.Errorf("Get = %+v, %v", reg, err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mcp remove ebira --scope user") {
		t.Errorf("claude was not asked to remove the old URL:\n%s", data)
	}
}
