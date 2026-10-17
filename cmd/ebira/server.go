package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ikawaha/ebira/internal/config"
	"github.com/ikawaha/ebira/internal/daemon"
)

// How long to wait for a server to answer after starting, which includes
// indexing the notes, and to stop.
const (
	startTimeout = 5 * time.Minute
	stopTimeout  = 15 * time.Second
)

// runServer runs ebira server start, stop, restart or status.
func runServer(args []string) error {
	if len(args) == 0 {
		return errors.New("ebira server needs start, stop, restart or status")
	}
	ctx := context.Background()
	switch args[0] {
	case "start":
		return serverStart(ctx, args[1:])
	case "restart":
		return serverRestart(ctx, args[1:])
	case "stop", "status":
		fs := flag.NewFlagSet("server "+args[0], flag.ContinueOnError)
		dataDir := fs.String("data-dir", defaultDataDir(), "data directory of the server")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		dir, err := absDir(*dataDir)
		if err != nil {
			return err
		}
		if args[0] == "stop" {
			return stopServer(ctx, dir, os.Stdout)
		}
		return serverStatus(ctx, dir, os.Stdout)
	}
	return fmt.Errorf("unknown server command %q (start, stop, restart or status)", args[0])
}

// serverStart starts ebira serve in the background with the flags of serve.
func serverStart(ctx context.Context, args []string) error {
	fs, o := newServeFlags("server start")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	dir, err := absDir(*o.dataDir)
	if err != nil {
		return err
	}
	if _, running, err := serverDir(dir); err != nil {
		return err
	} else if running {
		fmt.Println("The server is already running.")
		return serverStatus(ctx, dir, os.Stdout)
	}
	return startWithFlags(ctx, dir, setFlags(fs))
}

// serverRestart stops the server of the data directory, if it runs, and
// starts it again in the background. Without flags other than -data-dir, the
// flags of the stopped server are used again; the environment, such as the
// API keys of the assistant, is the one of this command.
func serverRestart(ctx context.Context, args []string) error {
	fs, o := newServeFlags("server restart")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	dir, err := absDir(*o.dataDir)
	if err != nil {
		return err
	}
	flags := setFlags(fs)
	sd, running, err := serverDir(dir)
	if err != nil {
		return err
	}
	if running {
		// server.json appears once the server listens.
		st, err := daemon.ReadState(sd)
		if err != nil {
			if st, err = waitServer(ctx, dir, os.Stdout); err != nil {
				return err
			}
		}
		if len(flags) == 0 {
			flags = keptFlags(st)
		}
		if err := stopServer(ctx, dir, os.Stdout); err != nil {
			return err
		}
	} else {
		fmt.Println("The server is not running; starting it.")
	}
	return startWithFlags(ctx, dir, flags)
}

// keptFlags returns the flags to start the server of st again with. The
// address is kept even when the flags do not name it, such as for a server of
// an earlier build, which did not record its flags.
func keptFlags(st daemon.State) []string {
	flags := slices.Clone(st.Flags)
	if !slices.ContainsFunc(flags, func(f string) bool { return strings.HasPrefix(f, "-addr=") }) {
		flags = append(flags, "-addr="+st.Addr)
	}
	return flags
}

// startWithFlags starts the server of dir in the background with flags of
// serve other than -data-dir, and prints where it runs.
func startWithFlags(ctx context.Context, dir string, flags []string) error {
	fs, o := newServeFlags("serve")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	// The data directory goes as an absolute path.
	s, err := startServer(ctx, dir, *o.addr, append([]string{"-data-dir", dir}, flags...), os.Stdout)
	if err != nil {
		return err
	}
	fmt.Printf("The server is running: http://%s (pid %d)\n", s.Addr, s.PID)
	return nil
}

// startServer starts ebira serve with serveArgs in the background and waits
// until it answers on addr.
func startServer(ctx context.Context, dir, addr string, serveArgs []string, out io.Writer) (daemon.State, error) {
	sd, err := stateDir(dir)
	if err != nil {
		return daemon.State{}, err
	}
	if err := config.Prepare(sd, dir); err != nil {
		return daemon.State{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return daemon.State{}, err
	}
	logPath := filepath.Join(sd, daemon.LogFile)
	pid, exited, err := daemon.Spawn(exe, append([]string{"serve"}, serveArgs...), logPath)
	if err != nil {
		return daemon.State{}, err
	}
	fmt.Fprintf(out, "Starting the server for %s (pid %d, log %s)…\n", dir, pid, logPath)
	deadline := time.Now().Add(startTimeout)
	notice := time.Now().Add(5 * time.Second)
	for {
		if s, err := daemon.Health(ctx, addr); err == nil && s.PID == pid {
			return s, nil
		}
		select {
		case <-exited:
			return daemon.State{}, fmt.Errorf("the server exited:\n%s", tail(logPath, 10))
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return daemon.State{}, fmt.Errorf("the server did not answer in %s; see %s", startTimeout, logPath)
		}
		if time.Now().After(notice) {
			fmt.Fprintln(out, "Still starting, such as indexing the notes…")
			notice = time.Now().Add(15 * time.Second)
		}
	}
}

// waitServer waits until the running server of dir answers, as it may still
// be starting, and returns its state.
func waitServer(ctx context.Context, dir string, out io.Writer) (daemon.State, error) {
	deadline := time.Now().Add(startTimeout)
	noticed := false
	for {
		sd, running, err := serverDir(dir)
		if err != nil {
			return daemon.State{}, err
		}
		// server.json appears once the server listens.
		if st, err := daemon.ReadState(sd); err == nil {
			if s, err := daemon.Health(ctx, st.Addr); err == nil && s.PID == st.PID {
				return s, nil
			}
		}
		if !running {
			return daemon.State{}, errors.New("the server stopped")
		}
		if time.Now().After(deadline) {
			return daemon.State{}, fmt.Errorf("the server does not answer; see %s", filepath.Join(sd, daemon.LogFile))
		}
		if !noticed {
			fmt.Fprintln(out, "Waiting for the server to start…")
			noticed = true
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// stopServer stops the server of dir and waits until it has shut down.
func stopServer(ctx context.Context, dir string, out io.Writer) error {
	sd, running, err := serverDir(dir)
	if err != nil {
		return err
	}
	if !running {
		fmt.Fprintln(out, "The server is not running.")
		return nil
	}
	st, err := daemon.ReadState(sd)
	if err != nil {
		return fmt.Errorf("the server is running, but %s cannot be read (still starting?): %w", daemon.StateFile, err)
	}
	if err := daemon.Terminate(st.PID); err != nil {
		return err
	}
	deadline := time.Now().Add(stopTimeout)
	for {
		if running, err := daemon.Running(sd); err != nil {
			return err
		} else if !running {
			fmt.Fprintf(out, "Stopped the server (pid %d).\n", st.PID)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the server (pid %d) did not stop in %s", st.PID, stopTimeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// serverStatus prints the state of the server of dir.
func serverStatus(ctx context.Context, dir string, out io.Writer) error {
	sd, running, err := serverDir(dir)
	if err != nil {
		return err
	}
	if !running {
		fmt.Fprintf(out, "The server for %s is not running.\n", dir)
		return nil
	}
	st, err := daemon.ReadState(sd)
	if err != nil {
		fmt.Fprintf(out, "The server for %s is starting.\n", dir)
		return nil
	}
	answers := "yes"
	if _, err := daemon.Health(ctx, st.Addr); err != nil {
		answers = "no (" + err.Error() + ")"
	}
	build := "same as this ebira"
	if st.Version != daemon.Version() {
		build = "another build than this ebira (" + daemon.Version() + ")"
	}
	fmt.Fprintf(out, "data dir:  %s\naddress:   http://%s\npid:       %d\nstarted:   %s\nversion:   %s, %s\nanswers:   %s\nlog:       %s\n",
		st.DataDir, st.Addr, st.PID, st.StartedAt.Local().Format(time.DateTime), st.Version, build, answers, filepath.Join(sd, daemon.LogFile))
	return nil
}

// absDir returns the absolute path of the data directory.
func absDir(dataDir string) (string, error) {
	return filepath.Abs(dataDir)
}

// stateDir returns the state directory of the data directory dir: the
// server's lock, server.json and log.
func stateDir(dir string) (string, error) { return config.StateDir(dir) }

// legacyDir returns the directory in the data directory dir where earlier
// builds kept the state and the index.
func legacyDir(dir string) string { return filepath.Join(dir, ".ebira") }

// legacyFiles are the files that earlier builds kept in legacyDir.
var legacyFiles = []string{
	daemon.LockFile, daemon.StateFile, daemon.LogFile,
	"index.sqlite", "index.sqlite-wal", "index.sqlite-shm", "index.sqlite-journal",
}

// removeLegacyDir removes legacyDir of the data directory dir when it holds
// only the files of earlier builds; the index is built again in the cache
// directory. It fails while a server of an earlier build runs there, so that
// two servers do not run for one data directory.
func removeLegacyDir(dir string) error {
	old := legacyDir(dir)
	entries, err := os.ReadDir(old)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if running, err := daemon.Running(old); err != nil {
		return err
	} else if running {
		return fmt.Errorf("%w (%s, by an earlier build); stop it with ebira server stop", daemon.ErrRunning, dir)
	}
	for _, e := range entries {
		if !slices.Contains(legacyFiles, e.Name()) {
			slog.Warn("not removing the directory of earlier builds, which has other files", "dir", old, "file", e.Name())
			return nil
		}
	}
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	slog.Info("removed the directory of earlier builds; the index is now in the cache directory", "dir", old)
	return nil
}

// serverDir returns the state directory of the server of the data directory
// dir and whether the server runs. A server of an earlier build, which kept
// its state in legacyDir, is found too, so that it can be stopped or
// restarted with this build.
func serverDir(dir string) (string, bool, error) {
	sd, err := stateDir(dir)
	if err != nil {
		return "", false, err
	}
	if running, err := daemon.Running(sd); err != nil || running {
		return sd, running, err
	}
	old := legacyDir(dir)
	if running, err := daemon.Running(old); err != nil {
		return "", false, err
	} else if running {
		return old, true, nil
	}
	return sd, false, nil
}

// tail returns the last n lines of the file at path.
func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}
