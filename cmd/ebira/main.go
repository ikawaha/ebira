// Command ebira is the ebira note app: a terminal UI and a web UI, and the
// server behind them, which also serves the REST API and MCP.
//
//	ebira [flags]                 open the TUI, starting the server in the background if needed
//	ebira web [flags]             open the web UI in the browser, starting the server in the background if needed
//	ebira serve [flags]           run the server in the foreground
//	ebira server start [flags]    start the server in the background
//	ebira server stop [flags]     stop the server
//	ebira server restart [flags]  restart the server in the background
//	ebira server status [flags]   show the server's state
//	ebira files check [flags]     list the unused files of notes and the broken references to files
//	ebira import [flags] path...  import Markdown files as notes
//
// One server runs per data directory; the server started in the background
// keeps running after the TUI exits or the browser closes.
//
// The settings file (see internal/config) gives the defaults of the flags,
// such as the data directory.
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/ikawaha/ebira/internal/config"
)

// settings is the settings file, read by run before the flags.
var settings config.Config

// defaultDataDir returns the default of -data-dir: data_dir of the settings
// file, else the current directory.
func defaultDataDir() string { return cmp.Or(settings.DataDir, ".") }

// defaultAddr returns the default of -addr: addr of the settings file, else
// localhost:8080.
func defaultAddr() string { return cmp.Or(settings.Addr, "localhost:8080") }

const usage = `usage:
  ebira [flags]                 open the TUI, starting the server in the background if needed
  ebira web [flags]             open the web UI in the browser, starting the server in the background if needed
  ebira serve [flags]           run the server in the foreground
  ebira server start [flags]    start the server in the background (flags of serve)
  ebira server stop [flags]     stop the server
  ebira server restart [flags]  restart the server in the background (flags of serve;
                                without them, the flags of the running server)
  ebira server status [flags]   show the server's state
  ebira files check [flags]     list the unused files of notes, such as images, and the
                                broken references to files (-delete deletes the unused ones)
  ebira import [flags] path...  import Markdown files, and the images they refer to, as notes

Run a command with -h for its flags.

The settings file, ~/.config/ebira/config.yaml ($XDG_CONFIG_HOME/ebira, or the
file of $EBIRA_CONFIG), gives the defaults of the flags: data_dir, addr, watch,
provider and model. Without -data-dir, data_dir and notes/ in the current
directory, the commands that open the notes in a terminal ask where to keep
them and how to search them, and set them up. The index is kept in ~/.cache/ebira and the server's
state and log in ~/.local/state/ebira ($XDG_CACHE_HOME, $XDG_STATE_HOME).
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "ebira:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "help" {
		fmt.Print(usage)
		return nil
	}
	var err error
	if settings, err = config.Load(); err != nil {
		return err
	}
	if len(args) > 0 {
		switch args[0] {
		case "web":
			return runWeb(args[1:])
		case "serve":
			return runServe(args[1:])
		case "server":
			return runServer(args[1:])
		case "files":
			return runFiles(args[1:])
		case "import":
			return runImport(args[1:])
		}
	}
	return runTUI(args)
}
