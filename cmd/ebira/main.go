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
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

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
		case "help":
			fmt.Print(usage)
			return nil
		}
	}
	return runTUI(args)
}
