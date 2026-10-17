package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ikawaha/ebira/internal/daemon"
	"github.com/ikawaha/ebira/internal/ui/tui"
)

// runTUI opens the TUI on the server of the data directory, starting it in
// the background when it is not running, or on the server given by -server.
func runTUI(args []string) error {
	fs := flag.NewFlagSet("ebira", flag.ContinueOnError)
	var (
		dataDir = fs.String("data-dir", ".", "data directory whose server the TUI uses, started in the background if needed")
		addr    = fs.String("addr", "localhost:8080", "address of a server started for the TUI")
		server  = fs.String("server", "", "URL of a server to use as is, without starting one, such as http://localhost:8080")
		light   = fs.Bool("light", false, "use colors for a light terminal background")
	)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage+"\nflags of the TUI:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown command %q (see ebira help)", fs.Arg(0))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := *server
	if url == "" {
		a, err := ensureServer(ctx, *dataDir, *addr, os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		url = "http://" + a
	}
	api, err := tui.NewClient(url)
	if err != nil {
		return err
	}
	p := tea.NewProgram(tui.New(ctx, api, *light))
	go tui.WatchEvents(ctx, api.Notes, p.Send)
	_, err = p.Run()
	return err
}

// ensureServer returns the address of the server of dataDir, starting it on
// addr when it is not running. A server of another build is restarted if the
// user agrees, so that the TUI does not talk to an outdated API.
func ensureServer(ctx context.Context, dataDir, addr string, in io.Reader, out io.Writer) (string, error) {
	dir, err := absDir(dataDir)
	if err != nil {
		return "", err
	}
	running, err := daemon.Running(stateDir(dir))
	if err != nil {
		return "", err
	}
	if !running {
		s, err := startServer(ctx, dir, addr, []string{"-data-dir", dir, "-addr", addr}, out)
		if err != nil {
			return "", err
		}
		return s.Addr, nil
	}
	s, err := waitServer(ctx, dir, out)
	if err != nil {
		return "", err
	}
	if s.Version == daemon.Version() {
		return s.Addr, nil
	}
	fmt.Fprintf(out, "The server (pid %d) is from another build (server %s, this ebira %s).\nRestart it with this build? Its flags are kept; the environment, such as API keys, is the one of this ebira. [Y/n] ", s.PID, s.Version, daemon.Version())
	answer, _ := bufio.NewReader(in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "" && a != "y" && a != "yes" {
		return s.Addr, nil
	}
	if err := stopServer(ctx, dir, out); err != nil {
		return "", err
	}
	s, err = startServer(ctx, dir, s.Addr, append([]string{"-data-dir", dir}, keptFlags(s)...), out)
	if err != nil {
		return "", err
	}
	return s.Addr, nil
}
