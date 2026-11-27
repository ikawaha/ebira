// Package apikey reads the API keys of the assistant with commands the user
// writes in the settings file, such as one of a password manager:
// op read of 1Password, bw get or rbw get of Bitwarden. The key stays in the
// memory of the server: it is never written to a file or shown.
package apikey

import (
	"bytes"
	"context"
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
// it printed, the one line of its standard output without the spaces around
// it. A command the shell cannot find is run again with the PATH of the
// login shell of the user: a server started by the desktop app, from the
// Finder, has a PATH without the directories of the shell, such as that of
// Homebrew.
func Run(ctx context.Context, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", errors.New("no command")
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	key, err := run(ctx, command, "")
	if errors.Is(err, errNotFound) {
		if p := shellPath(ctx); p != "" {
			key, err = run(ctx, command, p)
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("the command did not end in %s", Timeout)
	}
	return key, err
}

// errNotFound is the error of a command the shell did not find.
var errNotFound = errors.New("command not found")

// run runs command with the PATH path, or that of this process for "".
func run(ctx context.Context, command, path string) (string, error) {
	cmd := shellCommand(ctx, command)
	cmd.Env = os.Environ()
	if path != "" {
		cmd.Env = append(cmd.Env, "PATH="+path)
	}
	// Bitwarden's bw would wait for a master password that nobody types.
	cmd.Env = append(cmd.Env, "BW_NOINTERACTION=true")
	var out, stderr bytes.Buffer
	cmd.Stdout = &limited{w: &out, n: maxOutput}
	cmd.Stderr = &limited{w: &stderr, n: maxOutput}
	if err := cmd.Run(); err != nil {
		if notFound(cmd, err) {
			return "", fmt.Errorf("%w: %s", errNotFound, message(stderr.String(), err))
		}
		return "", errors.New(message(stderr.String(), err))
	}
	key := strings.TrimSpace(out.String())
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
	loginPath     string
	loginPathOnce sync.Once
)

// shellPath returns the PATH of the login shell of the user joined with
// that of this process, or "" when it adds nothing.
func shellPath(ctx context.Context) string {
	loginPathOnce.Do(func() { loginPath = readShellPath(ctx) })
	if loginPath == "" {
		return ""
	}
	cur := os.Getenv("PATH")
	if cur == "" {
		return loginPath
	}
	return loginPath + string(os.PathListSeparator) + cur
}
