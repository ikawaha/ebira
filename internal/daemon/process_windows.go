//go:build windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code of a process that has not exited.
const stillActive = 259

// Lock takes server.lock in dir, creating dir if needed. It returns
// ErrRunning when another server holds it. The lock lasts until the file is
// closed or the process ends.
func Lock(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, LockFile), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	// Lock the first byte, which may be past the end of the empty file.
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err != nil {
		f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrRunning
		}
		return nil, err
	}
	return f, nil
}

// Running reports whether a server holds the lock in dir.
func Running(dir string) (bool, error) {
	if _, err := os.Stat(filepath.Join(dir, LockFile)); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	f, err := Lock(dir)
	if errors.Is(err, ErrRunning) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, f.Close()
}

// Spawn starts exe with args detached from the console, so that it survives
// the caller and its console, with its output appended to logPath. It
// returns the PID and a channel closed when the process exits while the
// caller runs; the caller may exit any time without stopping it.
func Spawn(exe string, args []string, logPath string) (int, <-chan struct{}, error) {
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return 0, nil, err
	}
	defer log.Close() // the child has its own copy
	start := func(flags uint32) (*exec.Cmd, error) {
		cmd := exec.Command(exe, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, log, log
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		return cmd, cmd.Start()
	}
	// No console, and no Ctrl+C from the caller's. Breaking away from the
	// caller's job keeps the server when a terminal closes the job of its
	// processes; a job that does not allow it fails the start.
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	cmd, err := start(flags | windows.CREATE_BREAKAWAY_FROM_JOB)
	if err != nil {
		if cmd, err = start(flags); err != nil {
			return 0, nil, err
		}
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return cmd.Process.Pid, exited, nil
}

// Alive reports whether process pid exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// A process of another user cannot be opened, but it exists.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActive
}

// stopEvent returns the name of the event that asks process pid to shut
// down. Windows cannot send a signal to another process, so the server
// waits on this event (see NotifyStop).
func stopEvent(pid int) (*uint16, error) {
	return windows.UTF16PtrFromString(fmt.Sprintf(`Local\ebira-stop-%d`, pid))
}

// Terminate asks process pid to shut down, by the event it waits on. A
// process without the event, such as one still starting, is killed.
func Terminate(pid int) error {
	name, err := stopEvent(pid)
	if err != nil {
		return err
	}
	if h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name); err == nil {
		defer windows.CloseHandle(h)
		return windows.SetEvent(h)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// NotifyStop returns a context canceled when the process is asked to shut
// down: by Ctrl+C, or by Terminate.
func NotifyStop(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt)
	name, err := stopEvent(os.Getpid())
	if err != nil {
		return ctx, stop
	}
	h, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		return ctx, stop
	}
	go func() {
		defer windows.CloseHandle(h)
		for ctx.Err() == nil {
			if ev, _ := windows.WaitForSingleObject(h, 500); ev == windows.WAIT_OBJECT_0 {
				stop()
				return
			}
		}
	}()
	return ctx, stop
}
