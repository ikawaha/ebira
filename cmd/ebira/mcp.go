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
		skill     = fs.Bool("skill", true, "also install the skill "+claudecode.SkillName+" of Claude Code, which opens whiteboards")
	)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `usage: ebira mcp add [flags]

Register ebira as an MCP server of Claude Code, in the user scope, so that
every project can use it:

  claude mcp add --transport http --scope user ebira http://<addr>/mcp

The address is that of the running server of the notes directory, else of
-addr. A registration of another address is replaced. Without claude, or with
-print, it prints the command, and the settings for clients that keep MCP
servers in a JSON file. AI clients can connect only while the server runs.

It also installs the skill ebira-whiteboard, with which Claude Code opens
whiteboards, into the skills of its configuration directory
($CLAUDE_CONFIG_DIR, else ~/.claude), or updates it after ebira is updated.
A skill changed by hand is left as it is.`)
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
	register := reg == nil || reg.URL != url
	switch {
	case !register:
		fmt.Fprintln(out, "Claude Code has it already.")
	case reg != nil:
		fmt.Fprintf(out, "Claude Code has ebira at %s; it will be replaced.\n", reg.URL)
	}
	var configDir, skillPath string
	install := false
	if *skill {
		if configDir, err = claudecode.ConfigDir(ctx); err != nil {
			return err
		}
		skillPath = claudecode.SkillPath(configDir, claudecode.SkillName)
		st, err := claudecode.SkillStatus(configDir, claudecode.SkillName)
		if err != nil {
			return err
		}
		switch st {
		case claudecode.SkillCurrent:
			fmt.Fprintf(out, "Claude Code has the skill %s already.\n", claudecode.SkillName)
		case claudecode.SkillModified:
			fmt.Fprintf(out, "The skill %s was changed by hand, so it is left as it is: %s\nRemove it and run this again to install the one of this version.\n", claudecode.SkillName, skillPath)
		default:
			install = true
		}
	}
	if !register && !install {
		return nil
	}
	if register {
		fmt.Fprintln(out, "Register it with Claude Code:")
		fmt.Fprintln(out, "  "+command)
	}
	if install {
		fmt.Fprintf(out, "Install the skill %s, with which Claude Code opens whiteboards:\n  %s\n", claudecode.SkillName, skillPath)
	}
	if !*yes {
		if !interactive() {
			return errors.New("add -yes to register it without a terminal")
		}
		ok, err := confirm(bufio.NewReader(in), out, "Go on", true)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Nothing done.")
			return nil
		}
	}
	if register {
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
	}
	if install {
		if _, err := claudecode.InstallSkill(configDir, claudecode.SkillName); err != nil {
			return err
		}
		fmt.Fprintf(out, "Installed the skill %s. Claude Code uses it from its next session.\n", claudecode.SkillName)
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
