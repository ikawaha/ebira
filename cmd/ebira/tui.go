package main

import (
	"bufio"
	"cmp"
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

// runTUI opens the TUI on the server of the notes directory, starting it in
// the background when it is not running, or on the server given by -server.
func runTUI(args []string) error {
	fs := flag.NewFlagSet("ebira", flag.ContinueOnError)
	var (
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory whose server the TUI uses, started in the background if needed")
		addr     = fs.String("addr", defaultAddr(), "address of a server started for the TUI")
		server   = fs.String("server", "", "URL of a server to use as is, without starting one, such as http://localhost:13579")
		light    = fs.Bool("light", settings.Theme == "light", "use colors for a light terminal background (default: true when theme of the settings file is light)")
		lang     = fs.String("lang", settings.Lang, "language of the TUI: "+strings.Join(tui.Langs, " or ")+" (default: lang of the settings file, which the web UI shares, else ja when $LANG is Japanese, else en)")
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
	if *lang != "" && !tui.ValidLang(*lang) {
		return fmt.Errorf("unknown language %q of -lang or lang in the settings file (%s)", *lang, strings.Join(tui.Langs, ", "))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := *server
	if url == "" {
		dir, err := notesDirOf(fs, *notesDir, interactive(), os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		a, err := ensureServer(ctx, dir, *addr, os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		url = "http://" + a
	}
	api, err := tui.NewClient(url)
	if err != nil {
		return err
	}
	if !flagSet(fs, "lang") {
		*lang = cmp.Or(settings.Lang, setupLang) // as chosen in setup, if it ran
	}
	p := tea.NewProgram(tui.New(ctx, api, *light, tui.Lang(*lang)))
	go tui.WatchEvents(ctx, api.Notes, p.Send)
	_, err = p.Run()
	return err
}

// ensureServer returns the address of the server of notesDir, starting it on
// addr when it is not running. A server of another build is restarted if the
// user agrees, so that the TUI does not talk to an outdated API.
func ensureServer(ctx context.Context, notesDir, addr string, in io.Reader, out io.Writer) (string, error) {
	dir, err := absDir(notesDir)
	if err != nil {
		return "", err
	}
	_, running, err := serverDir(dir)
	if err != nil {
		return "", err
	}
	if !running {
		s, err := startServer(ctx, dir, addr, append([]string{"-notes", dir, "-addr", addr}, setupFlags...), out)
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
	s, err = startServer(ctx, dir, s.Addr, append([]string{"-notes", dir}, keptFlags(s)...), out)
	if err != nil {
		return "", err
	}
	return s.Addr, nil
}
