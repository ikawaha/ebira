//go:build !unix && !windows

package daemon

import (
	"context"
	"errors"
	"os"
	"os/signal"
)

var errUnsupported = errors.New("running the server in the background is supported on Unix and Windows only")

// Lock is not supported on this system; it never fails, so that the server
// runs without the single-server check.
func Lock(dir string) (*os.File, error) { return nil, nil }

// Running is not supported on this system.
func Running(dir string) (bool, error) { return false, errUnsupported }

// Spawn is not supported on this system.
func Spawn(exe string, args []string, logPath string) (int, <-chan struct{}, error) {
	return 0, nil, errUnsupported
}

// Alive is not supported on this system.
func Alive(pid int) bool { return false }

// Terminate is not supported on this system.
func Terminate(pid int) error { return errUnsupported }

// NotifyStop returns a context canceled by Ctrl+C.
func NotifyStop(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}

// Reexec is not supported on this system.
func Reexec(exe string, args []string, logPath string) error { return errUnsupported }
