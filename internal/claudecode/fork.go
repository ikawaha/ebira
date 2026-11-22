package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Fork answers the questions of sticky notes in forks of the Claude Code
// session that opened a whiteboard: claude resumes the session, or forks it
// into a new one, and answers in print mode with tools that only read.
type Fork struct {
	Client Client
	// ConfigDir is the configuration directory of Claude Code, whose
	// projects hold the sessions.
	ConfigDir string
	// MCPURL is the MCP server of ebira, the only one the fork gets.
	MCPURL string
	// Allowed and Disallowed are the tools of ebira that the fork may and
	// may not call, by their names in ebira: those that only read, and the
	// others.
	Allowed, Disallowed []string
}

// ForkAsk is a question for a fork.
type ForkAsk struct {
	// Cwd is the working directory of the session.
	Cwd string
	// Resume is the session to resume.
	Resume string
	// Session, when set, forks Resume into a new session of that ID; else
	// the question goes on in Resume.
	Session string
	// Question is the prompt.
	Question string
}

// sessionID is the form of the ID of a session of Claude Code.
var sessionID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// builtin are the tools of Claude Code the fork gets: they only read.
const builtin = "Read,Grep,Glob"

// mcpTools names the tools of ebira as Claude Code calls them.
func mcpTools(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "mcp__" + Name + "__" + n
	}
	return strings.Join(out, ",")
}

// Args returns the arguments of claude for a; the question goes on its
// standard input. The flags hold for one run only, so every run has them all.
func (f Fork) Args(a ForkAsk) []string {
	args := []string{"-p", "--resume", a.Resume}
	if a.Session != "" {
		args = append(args, "--fork-session", "--session-id", a.Session)
	}
	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{Name: map[string]string{"type": "http", "url": f.MCPURL}}})
	args = append(args,
		"--restricted", "--tools", builtin,
		"--strict-mcp-config", "--mcp-config", string(config),
	)
	allowed := builtin
	if len(f.Allowed) > 0 {
		allowed += "," + mcpTools(f.Allowed)
	}
	args = append(args, "--allowedTools", allowed)
	if len(f.Disallowed) > 0 {
		args = append(args, "--disallowedTools", mcpTools(f.Disallowed))
	}
	return append(args, "--effort", "low", "--output-format", "json")
}

// Ask asks a fork the question of a, and returns its answer. ctx bounds the
// run: claude is killed when it ends.
func (f Fork) Ask(ctx context.Context, a ForkAsk) (string, error) {
	if !sessionID.MatchString(a.Resume) || a.Session != "" && !sessionID.MatchString(a.Session) {
		return "", errors.New("not a session of Claude Code")
	}
	if !filepath.IsAbs(a.Cwd) {
		return "", fmt.Errorf("the working directory %q is not absolute", a.Cwd)
	}
	cmd := exec.CommandContext(ctx, f.Client.Bin, f.Args(a)...)
	cmd.Dir = a.Cwd
	// claude is killed when ctx ends; its children may hold its output.
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = forkEnv(os.Environ())
	if f.Client.Path != "" {
		cmd.Env = append(cmd.Env, "PATH="+f.Client.Path)
	}
	if f.ConfigDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+f.ConfigDir)
	}
	cmd.Stdin = strings.NewReader(a.Question)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var res struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if jerr := json.Unmarshal(stdout.Bytes(), &res); jerr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && err != nil {
			msg = err.Error()
		}
		if msg == "" {
			msg = jerr.Error()
		}
		return "", fmt.Errorf("claude: %s", cut(msg, 500))
	}
	if res.IsError || err != nil {
		return "", fmt.Errorf("claude: %s", cut(firstOf(res.Result, strings.TrimSpace(stderr.String()), "failed"), 500))
	}
	return res.Result, nil
}

// forkEnv returns env without the API key of Anthropic, which the server may
// have for its assistant: with it, claude would bill the key rather than the
// plan Claude Code is signed in with.
func forkEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			out = append(out, kv)
		}
	}
	return out
}

// sessionFiles returns the files of session id in the projects of the
// configuration: Claude Code names the directory of a project after its
// path, shortened when it is long, so the file is looked for in all of them.
func (f Fork) sessionFiles(id string) []string {
	if !sessionID.MatchString(id) || f.ConfigDir == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(f.ConfigDir, "projects", "*", id+".jsonl"))
	return files
}

// Has reports whether Claude Code keeps session id.
func (f Fork) Has(id string) bool {
	return len(f.sessionFiles(id)) > 0
}

// Remove deletes session id, one that ebira made with Ask, and is done with.
func (f Fork) Remove(id string) error {
	var errs []error
	for _, p := range f.sessionFiles(id) {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		// Claude Code may keep the files of a session in a directory of its
		// name next to it.
		dir := strings.TrimSuffix(p, ".jsonl")
		if fi, err := os.Lstat(dir); err == nil && fi.IsDir() {
			errs = append(errs, os.RemoveAll(dir))
		}
	}
	return errors.Join(errs...)
}

// cut cuts s to at most n bytes, at a character boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// firstOf returns the first of ss that is not empty.
func firstOf(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
