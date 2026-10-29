//go:build unix

package main

import (
	"runtime"
	"syscall"
)

// maxRSS returns the largest resident set size of the process so far, in
// bytes.
func maxRSS() int64 {
	var u syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
		return 0
	}
	// macOS gives bytes, and Linux and the BSDs KiB.
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return int64(u.Maxrss)
	}
	return int64(u.Maxrss) * 1024
}
