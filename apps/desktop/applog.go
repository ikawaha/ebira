package main

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// maxLog is the size the log of the app grows to before it is moved to
// <log>.1, which keeps one older log: two files of this size at most.
const maxLog = 1 << 20

// logFile is the log of the app, kept to maxLog with one older file.
type logFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

// openLog opens the log at path, moving it aside first if it is full.
func openLog(path string) (*logFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if st, err := os.Stat(path); err == nil && st.Size() >= maxLog {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	l := &logFile{path: path}
	if err := l.open(0); err != nil {
		return nil, err
	}
	return l, nil
}

// open opens the file of the log to append to it, with flag, as os.O_TRUNC.
func (l *logFile) open(flag int) error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|flag, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, st.Size()
	return nil
}

// Write writes p, starting a new file first when p would not fit. When the
// file cannot be moved aside, as while another program holds it open on
// Windows, it starts the file over, so that the log never grows past the
// limit.
func (l *logFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > maxLog {
		l.f.Close()
		flag := 0
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			flag = os.O_TRUNC
		}
		if err := l.open(flag); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// setupLog sends the log of the app and of Wails to the log file of the
// platform (logPath) as well as to the standard error, which an app opened
// from the Finder or the Explorer has nowhere to show. Without a log file,
// it logs to the standard error alone.
func setupLog() *slog.Logger {
	var w io.Writer = os.Stderr
	if path, err := logPath(); err != nil {
		log.Print("the log of the app: ", err)
	} else if f, err := openLog(path); err != nil {
		log.Print("the log of the app: ", err)
	} else {
		// The file first: MultiWriter stops at the first writer that fails, and
		// the standard error of an app built with -H windowsgui has no handle.
		w = io.MultiWriter(f, os.Stderr)
	}
	log.SetOutput(w)
	log.Printf("ebira-desktop %s, pid %d", appVersion(), os.Getpid())
	return slog.New(slog.NewTextHandler(w, nil))
}
