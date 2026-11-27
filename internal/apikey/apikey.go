// Package apikey reads the API keys of the assistant with commands the user
// writes in the settings file, such as one of a password manager:
// op read of 1Password, bw get or rbw get of Bitwarden. The key stays in the
// memory of the server: it is never written to a file or shown.
package apikey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Timeout bounds a command, long enough to confirm it with Touch ID or a
// master password, as 1Password and Bitwarden may ask.
const Timeout = 2 * time.Minute

// maxOutput bounds what a command prints.
const maxOutput = 64 << 10

// Run runs command, a line of the shell, without input, and returns the key
// it printed: the one line of its standard output without the spaces around
// it, or the value of the JSON object it printed, as bws, the command line
// tool of Bitwarden Secrets Manager, prints a secret.
//
// A command that fails is run again with the environment of the login shell
// of the user, when it adds to that of the server: its PATH, as a server
// started by the desktop app, from the Finder, lacks the directories of the
// shell, such as that of Homebrew, and the variables that the tools of the
// password managers take their sessions and tokens from (ShellVars), as the
// profile of the shell may set them.
func Run(ctx context.Context, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", errors.New("no command")
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	key, err := run(ctx, command, nil)
	if err != nil && ctx.Err() == nil {
		if env := shellEnv(ctx); len(env) > 0 {
			key, err = run(ctx, command, env)
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("the command did not end in %s", Timeout)
	}
	return key, err
}

// ShellVars are the variables of the login shell that a command is run
// again with, when the server lacks them: the sessions and the tokens of
// the command line tools of the password managers.
var ShellVars = []string{
	"BWS_ACCESS_TOKEN",         // bws, Bitwarden Secrets Manager
	"BW_SESSION",               // bw, Bitwarden
	"OP_SERVICE_ACCOUNT_TOKEN", // op, 1Password service accounts
}

// run runs command with the variables env added to the environment of this
// process.
func run(ctx context.Context, command string, env []string) (string, error) {
	cmd := shellCommand(ctx, command)
	// Bitwarden's bw would wait for a master password that nobody types.
	cmd.Env = append(append(os.Environ(), env...), "BW_NOINTERACTION=true")
	var out, stderr bytes.Buffer
	cmd.Stdout = &limited{w: &out, n: maxOutput}
	cmd.Stderr = &limited{w: &stderr, n: maxOutput}
	if err := cmd.Run(); err != nil {
		return "", errors.New(message(stderr.String(), err))
	}
	key := strings.TrimSpace(out.String())
	if strings.HasPrefix(key, "{") {
		var secret struct {
			Value *string `json:"value"`
		}
		if err := json.Unmarshal([]byte(key), &secret); err != nil || secret.Value == nil {
			return "", errors.New("the command printed JSON without a value: it should print the key alone, or a secret as bws does")
		}
		key = strings.TrimSpace(*secret.Value)
	}
	switch {
	case key == "":
		return "", fmt.Errorf("the command printed no key%s", detail(stderr.String()))
	case strings.ContainsAny(key, "\r\n"):
		return "", errors.New("the command printed more than one line: it should print the key alone")
	case !utf8.ValidString(key):
		return "", errors.New("the command printed what is not text")
	}
	return key, nil
}

// message tells why a command failed: the first line of what it printed to
// its standard error, else err.
func message(stderr string, err error) string {
	if line := firstLine(stderr); line != "" {
		return line
	}
	return err.Error()
}

// detail is the first line of stderr as an addition to a message, or "".
func detail(stderr string) string {
	if line := firstLine(stderr); line != "" {
		return ": " + line
	}
	return ""
}

// firstLine returns the first line of s that is not blank, cut short.
func firstLine(s string) string {
	for line := range strings.Lines(s) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > 200 {
			line = string(r[:200]) + "…"
		}
		return line
	}
	return ""
}

// limited writes up to n bytes to w, and drops the rest.
type limited struct {
	w io.Writer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	size := len(p)
	if l.n <= 0 {
		return size, nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	if _, err := l.w.Write(p); err != nil {
		return 0, err
	}
	return size, nil
}

var (
	loginEnv     map[string]string
	loginEnvOnce sync.Once
)

// shellEnv returns the variables of the login shell of the user that add to
// the environment of this process: PATH, with that of the shell first, and
// those of ShellVars this process lacks; nil for none.
func shellEnv(ctx context.Context) []string {
	loginEnvOnce.Do(func() { loginEnv = readShellEnv(ctx) })
	var env []string
	if p := loginEnv["PATH"]; p != "" && p != os.Getenv("PATH") {
		if cur := os.Getenv("PATH"); cur != "" {
			p += string(os.PathListSeparator) + cur
		}
		env = append(env, "PATH="+p)
	}
	for _, v := range ShellVars {
		if loginEnv[v] != "" && os.Getenv(v) == "" {
			env = append(env, v+"="+loginEnv[v])
		}
	}
	return env
}
