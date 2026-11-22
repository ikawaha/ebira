package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	parentSession = "11111111-2222-4333-8444-555555555555"
	forkSession   = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
)

// fakeFork writes a claude that logs its arguments, working directory and
// standard input, makes the file of the session it forks into, and answers
// as answer says: ok, error (is_error) or hang.
func fakeFork(t *testing.T, answer string) (Fork, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a shell script")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	project := filepath.Join(config, "projects", "-work")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, parentSession+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
{ echo "args: $*"; echo "cwd: $(pwd)"; echo "config: $CLAUDE_CONFIG_DIR"; echo "key: [$ANTHROPIC_API_KEY]"; echo "stdin: $(cat)"; } >> '` + log + `'
while [ $# -gt 0 ]; do
  if [ "$1" = --session-id ]; then echo '{}' > "$CLAUDE_CONFIG_DIR/projects/-work/$2.jsonl"; fi
  shift
done
case '` + answer + `' in
ok) echo '{"type":"result","is_error":false,"result":"a trie finds prefixes fast"}' ;;
error) echo '{"type":"result","is_error":true,"result":"No conversation found"}'; exit 1 ;;
hang) sleep 30 ;;
esac
`
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Fork{
		Client: Client{Bin: bin}, ConfigDir: config, MCPURL: "http://localhost:13579/mcp",
		Allowed: []string{"search_notes", "get_note"}, Disallowed: []string{"update_ai_note"},
	}, log
}

func TestForkAsk(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-of-the-assistant")
	f, log := fakeFork(t, "ok")
	work := t.TempDir()
	if f.Has(forkSession) {
		t.Fatal("the fork is there before it is made")
	}
	got, err := f.Ask(context.Background(), ForkAsk{Cwd: work, Resume: parentSession, Session: forkSession, Question: "why a trie?"})
	if err != nil || got != "a trie finds prefixes fast" {
		t.Fatalf("Ask = %q, %v", got, err)
	}
	data, _ := os.ReadFile(log)
	s := string(data)
	for _, want := range []string{
		"-p --resume " + parentSession + " --fork-session --session-id " + forkSession,
		"--restricted --tools Read,Grep,Glob --strict-mcp-config --mcp-config {\"mcpServers\":{\"ebira\":{\"type\":\"http\",\"url\":\"http://localhost:13579/mcp\"}}}",
		"--allowedTools Read,Grep,Glob,mcp__ebira__search_notes,mcp__ebira__get_note",
		"--disallowedTools mcp__ebira__update_ai_note",
		"--effort low --output-format json",
		"config: " + f.ConfigDir,
		"stdin: why a trie?",
		// The key of the assistant is not passed on.
		"key: []",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("claude was not run with %q:\n%s", want, s)
		}
	}
	// The working directory may be a link, such as /tmp on macOS.
	if real, _ := filepath.EvalSymlinks(work); !strings.Contains(s, "cwd: "+real) && !strings.Contains(s, "cwd: "+work) {
		t.Errorf("claude did not run in %s:\n%s", work, s)
	}
	if !f.Has(forkSession) {
		t.Fatal("the fork is not there")
	}

	// Going on in the fork, without forking again.
	if _, err := f.Ask(context.Background(), ForkAsk{Cwd: work, Resume: forkSession, Question: "and memory?"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(log)
	if last := string(data)[len(s):]; !strings.Contains(last, "-p --resume "+forkSession+" --restricted") {
		t.Errorf("going on:\n%s", last)
	}

	// A new session, not resumed.
	const fresh = "cccccccc-dddd-4eee-8fff-000000000000"
	if _, err := f.Ask(context.Background(), ForkAsk{Cwd: work, Session: fresh, Question: "read this"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(log)
	if !strings.Contains(string(data), "args: -p --session-id "+fresh+" --restricted") || !f.Has(fresh) {
		t.Errorf("new session:\n%s", data)
	}

	// Removing it leaves the session it was forked from.
	if err := f.Remove(forkSession); err != nil || f.Has(forkSession) || !f.Has(parentSession) {
		t.Errorf("Remove = %v; fork there %v, parent there %v", err, f.Has(forkSession), f.Has(parentSession))
	}
}

func TestForkAskFails(t *testing.T) {
	f, _ := fakeFork(t, "error")
	work := t.TempDir()
	if _, err := f.Ask(context.Background(), ForkAsk{Cwd: work, Resume: parentSession, Question: "?"}); err == nil || !strings.Contains(err.Error(), "No conversation found") {
		t.Errorf("Ask of a failing claude = %v", err)
	}
	for name, a := range map[string]ForkAsk{
		"session":  {Cwd: work, Resume: "../../etc/passwd", Question: "?"},
		"neither":  {Cwd: work, Question: "?"},
		"new":      {Cwd: work, Resume: parentSession, Session: "x", Question: "?"},
		"relative": {Cwd: "work", Resume: parentSession, Question: "?"},
	} {
		if _, err := f.Ask(context.Background(), a); err == nil {
			t.Errorf("%s: Ask passed", name)
		}
	}
	if f.Has("../" + parentSession) {
		t.Error("Has looked outside the projects")
	}

	h, _ := fakeFork(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := h.Ask(ctx, ForkAsk{Cwd: work, Resume: parentSession, Question: "?"}); err == nil || time.Since(start) > 10*time.Second {
		t.Errorf("Ask of a claude that hangs = %v after %v", err, time.Since(start))
	}
}
