package apikey

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

// createNoWindow keeps the command from opening a console window.
const createNoWindow = 0x08000000

// shellCommand returns the command that runs command with cmd.exe. The line
// is given as it is, as cmd.exe parses it in its own way.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd.exe /d /s /c "` + command + `"`,
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

// notFound tells whether err is that of cmd.exe not finding a command: exit
// status 9009.
func notFound(_ *exec.Cmd, err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 9009
}

// readShellPath returns "": a program started on Windows has the PATH of the
// user already.
func readShellPath(context.Context) string { return "" }
