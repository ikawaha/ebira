//go:build !windows

package apikey

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// shellCommand returns the command that runs command with sh.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

// notFound tells whether err is that of sh not finding a command: exit
// status 127.
func notFound(_ *exec.Cmd, err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 127
}

// readShellPath asks the login shell of the user for its PATH.
func readShellPath(ctx context.Context) string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// The last line, after whatever the profile of the shell prints.
	out, err := exec.CommandContext(ctx, shell, "-lc", `printf '\n%s' "$PATH"`).Output()
	if err != nil {
		return ""
	}
	s := string(out)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}
