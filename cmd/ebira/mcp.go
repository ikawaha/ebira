package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ikawaha/ebira/internal/claudecode"
	"github.com/ikawaha/ebira/internal/daemon"
)

// runMCP runs ebira mcp.
func runMCP(args []string) error {
	if len(args) > 0 && args[0] == "add" {
		return mcpAdd(context.Background(), args[1:], os.Stdin, os.Stdout)
	}
	return fmt.Errorf("usage: ebira mcp add [flags]")
}

// mcpAdd runs ebira mcp add: it registers the MCP server of the notes
// directory with Claude Code, in the user scope, or shows how to register it
// with other clients.
func mcpAdd(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	var (
		notesDir  = fs.String("notes", defaultNotesDir(), "notes directory whose server to register")
		addr      = fs.String("addr", defaultAddr(), "address of the server, when it is not running")
		printOnly = fs.Bool("print", false, "only print the URL, the command of Claude Code and the settings of other clients")
		yes       = fs.Bool("yes", false, "register it without asking")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `usage: ebira mcp add [flags]

Register ebira as an MCP server of Claude Code, in the user scope, so that
every project can use it:

  claude mcp add --transport http --scope user ebira http://<addr>/mcp

The address is that of the running server of the notes directory, else of
-addr. A registration of another address is replaced. Without claude, or with
-print, it prints the command, and the settings for clients that keep MCP
servers in a JSON file. AI clients can connect only while the server runs.`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	dir, err := absDir(*notesDir)
	if err != nil {
		return err
	}
	// The address may be a default shared with the notes.
	if isDir(dir) {
		if err := applyDefaults(fs, dir); err != nil {
			return err
		}
	}
	a, running := *addr, false
	if sd, ok, err := serverDir(dir); err == nil && ok {
		if st, err := daemon.ReadState(sd); err == nil {
			a, running = st.Addr, true
		}
	}
	url := claudecode.URL(a)
	fmt.Fprintln(out, "MCP of ebira:", url)
	if !running {
		fmt.Fprintln(out, "The server is not running: AI clients can connect once it runs (ebira server start).")
	}
	command := claudecode.Command(url)
	if *printOnly {
		printMCP(out, url, command)
		return nil
	}
	c, err := claudecode.Find(ctx)
	if errors.Is(err, claudecode.ErrNotFound) {
		fmt.Fprintln(out, "Claude Code (claude) is not found.")
		printMCP(out, url, command)
		return nil
	}
	reg, err := c.Get(ctx)
	if err != nil {
		return err
	}
	switch {
	case reg != nil && reg.URL == url:
		fmt.Fprintln(out, "Claude Code has it already.")
		return nil
	case reg != nil:
		fmt.Fprintf(out, "Claude Code has ebira at %s; it will be replaced.\n", reg.URL)
	}
	fmt.Fprintln(out, "Register it with Claude Code:")
	fmt.Fprintln(out, "  "+command)
	if !*yes {
		if !interactive() {
			return errors.New("add -yes to register it without a terminal")
		}
		ok, err := confirm(bufio.NewReader(in), out, "Run it", true)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Nothing registered.")
			return nil
		}
	}
	res, err := c.Add(ctx, url)
	if err != nil {
		return err
	}
	switch res.Status {
	case "unchanged":
		fmt.Fprintln(out, "Claude Code has it already.")
	case "replaced":
		fmt.Fprintf(out, "Registered, in place of %s.\n", res.Previous)
	default:
		fmt.Fprintln(out, "Registered. Claude Code uses it from its next session; claude mcp list shows it.")
	}
	return nil
}

// printMCP prints the command of Claude Code and the settings of the clients
// that keep MCP servers in a JSON file.
func printMCP(out io.Writer, url, command string) {
	fmt.Fprintln(out, "\nClaude Code:")
	fmt.Fprintln(out, "  "+command)
	fmt.Fprintln(out, "\nClients with a JSON settings file (the form differs a little between clients):")
	data, _ := json.MarshalIndent(mcpConfig(url), "  ", "  ")
	fmt.Fprintln(out, "  "+string(data))
}

// mcpConfig is the settings of ebira for the clients that keep MCP servers in
// a JSON file, such as .mcp.json of Claude Code.
func mcpConfig(url string) any {
	return map[string]any{"mcpServers": map[string]any{claudecode.Name: map[string]string{"type": "http", "url": url}}}
}
