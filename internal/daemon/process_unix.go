//go:build unix

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

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
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
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

// Spawn starts exe with args in a new session, detached from the terminal so
// that it survives the caller and its terminal, with its output appended to
// logPath. It returns the PID and a channel closed when the process exits
// while the caller runs; the caller may exit any time without stopping it.
func Spawn(exe string, args []string, logPath string) (int, <-chan struct{}, error) {
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return 0, nil, err
	}
	defer log.Close() // the child has its own copy
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // no terminal, no SIGHUP
	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}
	// Waiting reaps the process if it exits early, such as when the address is
	// taken; a zombie would still look alive.
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return cmd.Process.Pid, exited, nil
}

// Alive reports whether process pid exists.
func Alive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// Terminate asks process pid to shut down (SIGTERM).
func Terminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
