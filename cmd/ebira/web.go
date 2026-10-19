package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ikawaha/ebira/internal/ui/web"
)

// runWeb opens the web UI in the browser on the server of the notes directory,
// starting the server in the background when it is not running.
func runWeb(args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	var (
		notesDir = fs.String("notes", defaultNotesDir(), "notes directory whose server the web UI uses, started in the background if needed")
		addr     = fs.String("addr", defaultAddr(), "address of a server started for the web UI")
		noOpen   = fs.Bool("no-open", false, "only print the URL, without opening the browser")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	dir, err := notesDirOf(fs, *notesDir, interactive(), os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	a, err := ensureServer(context.Background(), dir, *addr, os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	url := "http://" + a + web.Prefix
	fmt.Println("Web UI:", url)
	if *noOpen {
		return nil
	}
	return openBrowser(url)
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot open the browser (%v): %s; open %s", err, strings.TrimSpace(string(out)), url)
	}
	return nil
}
