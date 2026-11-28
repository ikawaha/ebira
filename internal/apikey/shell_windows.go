package apikey

import (
	"context"
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

// readShellEnv returns nil: a program started on Windows has the
// environment of the user already.
func readShellEnv(context.Context) map[string]string { return nil }
