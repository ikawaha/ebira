//go:build !windows

package apikey

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// shellCommand returns the command that runs command with sh.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

// readShellEnv asks the login shell of the user for PATH and the variables
// of ShellVars, those it has.
func readShellEnv(ctx context.Context) map[string]string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// After a mark, past whatever the profile of the shell prints, the
	// values separated by NUL.
	names := append([]string{"PATH"}, ShellVars...)
	script := `printf '\nebira-env'`
	for _, n := range names {
		script += `; printf '\0%s' "$` + n + `"`
	}
	out, err := exec.CommandContext(ctx, shell, "-lc", script).Output()
	if err != nil {
		return nil
	}
	s := string(out)
	i := strings.LastIndex(s, "\nebira-env\x00")
	if i < 0 {
		return nil
	}
	values := strings.Split(s[i+len("\nebira-env\x00"):], "\x00")
	if len(values) != len(names) {
		return nil
	}
	env := map[string]string{}
	for k, n := range names {
		if v := strings.TrimSpace(values[k]); v != "" && !strings.ContainsAny(v, "\r\n") {
			env[n] = v
		}
	}
	return env
}
