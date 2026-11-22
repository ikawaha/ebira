package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ikawaha/ebira/internal/claudecode"
)

// fakeClaude puts a claude in PATH that keeps the URL of ebira in a file,
// with a configuration directory of its own for the skills.
func fakeClaude(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a shell script")
	}
	dir := t.TempDir()
	state := filepath.Join(dir, "url")
	script := `#!/bin/sh
case "$2" in
get)
  if [ -f '` + state + `' ]; then
    printf 'ebira:\n  Scope: User config (available in all your projects)\n  Type: http\n  URL: %s\n' "$(cat '` + state + `')"
  else
    echo 'No MCP server named "ebira".' >&2
    exit 1
  fi ;;
add) printf '%s' "$8" > '` + state + `' ;;
remove) rm '` + state + `' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "config"))
	return state
}

func TestClaudeHandler(t *testing.T) {
	state := fakeClaude(t)
	h := claudeHandler("0.0.0.0:13579", claudecode.Find, nil)
	do := func(method, remote, host string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+"/claude/mcp", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	get := func(remote, host string) claudeState {
		t.Helper()
		w := do(http.MethodGet, remote, host)
		var st claudeState
		if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}

	if st := get("127.0.0.1:50000", "localhost:13579"); !st.Available || st.Registered != "" || st.URL != "http://localhost:13579/mcp" || st.Skill != claudecode.SkillMissing {
		t.Errorf("before registering: %+v", st)
	}
	if w := do(http.MethodPost, "127.0.0.1:50000", "localhost:13579"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"added"`) || !strings.Contains(w.Body.String(), `"skill":"missing"`) {
		t.Errorf("POST = %d %s", w.Code, w.Body)
	}
	if data, _ := os.ReadFile(state); string(data) != "http://localhost:13579/mcp" {
		t.Errorf("registered %q", data)
	}
	if st := get("[::1]:50000", "[::1]:13579"); st.Registered != "http://localhost:13579/mcp" || st.Skill != claudecode.SkillCurrent || st.SkillPath != filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "skills", "ebira-whiteboard", "SKILL.md") {
		t.Errorf("after registering: %+v", st)
	}
	// Once registered, POST installs a skill that is missing.
	if err := os.Remove(claudecode.SkillPath(os.Getenv("CLAUDE_CONFIG_DIR"), claudecode.SkillName)); err != nil {
		t.Fatal(err)
	}
	if w := do(http.MethodPost, "127.0.0.1:50000", "localhost:13579"); !strings.Contains(w.Body.String(), `"status":"unchanged"`) || !strings.Contains(w.Body.String(), `"skill":"missing"`) {
		t.Errorf("POST again = %d %s", w.Code, w.Body)
	}
	if st := get("127.0.0.1:50000", "localhost:13579"); st.Skill != claudecode.SkillCurrent {
		t.Errorf("after POST again: %+v", st)
	}

	// Another machine, or a proxy on this machine for another machine.
	for _, c := range [][2]string{{"192.168.1.5:50000", "notes.local:13579"}, {"127.0.0.1:50000", "notes.example.ts.net"}} {
		if st := get(c[0], c[1]); st.Available || st.Reason != "remote" {
			t.Errorf("GET from %s to %s: %+v", c[0], c[1], st)
		}
		if w := do(http.MethodPost, c[0], c[1]); w.Code != http.StatusForbidden {
			t.Errorf("POST from %s to %s = %d", c[0], c[1], w.Code)
		}
	}
}

func TestClaudeHandlerNotFound(t *testing.T) {
	h := claudeHandler("localhost:13579", func(context.Context) (claudecode.Client, error) {
		return claudecode.Client{}, claudecode.ErrNotFound
	}, nil)
	r := httptest.NewRequest(http.MethodGet, "http://localhost:13579/claude/mcp", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `"reason":"not_found"`) || strings.Contains(w.Body.String(), `"available":true`) {
		t.Errorf("GET = %s", w.Body)
	}
}

func TestClaudeHandlerSession(t *testing.T) {
	h := claudeHandler("localhost:13579", claudecode.Find, func(string) (bool, bool) { return false, false })
	r := httptest.NewRequest(http.MethodPost, "http://localhost:13579/claude/mcp", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("POST without a session = %d", w.Code)
	}
}

func TestMCPAdd(t *testing.T) {
	state := fakeClaude(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	notes := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := mcpAdd(context.Background(), append([]string{"-notes", notes}, args...), strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if out := run("-addr", "localhost:13600", "-print"); !strings.Contains(out, "claude mcp add --transport http --scope user ebira http://localhost:13600/mcp") || !strings.Contains(out, `"mcpServers"`) {
		t.Errorf("-print:\n%s", out)
	}
	if _, err := os.Stat(state); err == nil {
		t.Error("-print registered it")
	}
	skill := claudecode.SkillPath(os.Getenv("CLAUDE_CONFIG_DIR"), claudecode.SkillName)
	if out := run("-addr", "localhost:13600", "-yes", "-skill=false"); !strings.Contains(out, "Registered.") || strings.Contains(out, "skill") {
		t.Errorf("-skill=false:\n%s", out)
	}
	if _, err := os.Stat(skill); err == nil {
		t.Error("-skill=false installed the skill")
	}
	if out := run("-addr", "localhost:13600", "-yes"); !strings.Contains(out, "has it already") || !strings.Contains(out, "Installed the skill ebira-whiteboard") {
		t.Errorf("with the skill:\n%s", out)
	}
	if out := run("-addr", "localhost:13600", "-yes"); !strings.Contains(out, "has it already") || !strings.Contains(out, "has the skill ebira-whiteboard already") || strings.Contains(out, "Installed") {
		t.Errorf("again:\n%s", out)
	}
	mine := []byte("---\nname: ebira-whiteboard\ndescription: mine\n---\n")
	if err := os.WriteFile(skill, mine, 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run("-addr", "localhost:13600", "-yes"); !strings.Contains(out, "changed by hand") {
		t.Errorf("a skill changed by hand:\n%s", out)
	}
	if data, _ := os.ReadFile(skill); !bytes.Equal(data, mine) {
		t.Error("the skill changed by hand was replaced")
	}
	if out := run("-addr", "localhost:13700", "-yes"); !strings.Contains(out, "in place of http://localhost:13600/mcp") {
		t.Errorf("another address:\n%s", out)
	}
	if data, _ := os.ReadFile(state); string(data) != "http://localhost:13700/mcp" {
		t.Errorf("registered %q", data)
	}
}
