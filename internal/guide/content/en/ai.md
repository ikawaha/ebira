---
title: Using AI
tags: [guide, ai]
---
## MCP

ebira is an MCP server. Register its URL with an AI client, and the AI can search and read your notes, and write in collab notebooks ([Kinds of notebooks and AI](kinds.md)). Private notebooks stay hidden.

"Connect AI (MCP)…" in the settings menu (the Help menu in the desktop app) shows the URL of the server and the command that registers it with Claude Code, with the address of the server. The copy icons copy them.

AI can connect only while the ebira server runs. If you change the address of the server (`addr` of the settings file), register it again.

## Registering with Claude Code

Each of these registers the same thing, in the user scope (`--scope user`), so that every project can use it.

- **Web UI**: "Register with Claude Code" in "Connect AI (MCP)…" of the settings menu (the Help menu in the desktop app).
- **Terminal**: `ebira mcp add`. It shows the command and runs it once you agree (`-yes` runs it without asking, `-print` only prints it).
- **By hand**: this command, with your address if you changed it.

```
claude mcp add --transport http --scope user ebira http://localhost:13579/mcp
```

Sessions of Claude Code started after that can use it. `claude mcp list`, or `/mcp` in Claude Code, shows whether it is registered. After you change the address, the button and `ebira mcp add` replace the registration of the old address.

### Without the button

The button shows only when the ebira server finds Claude Code (`claude`): the server runs `claude` of its machine to register it.

- Claude Code is not installed: install it, and open the dialog again.
- The browser is on another machine: the button would register ebira with the Claude Code of the server's machine, so it does not show. Run the command in a terminal of the machine you use.

### Registrations of a project

If you registered ebira in the local scope of a project before (`--scope local`, the default of `claude mcp add`), that project uses that one. Run `claude mcp remove ebira -s local` in the directory of the project to use the one of the user scope.

## Keeping Claude Code off the note files

The kinds of notebooks (private and the others) hold only for AI that goes through the MCP of ebira. Claude Code also has tools that read and write files, and a shell: reading the notes directory directly, it would see private notes too. To have it handle notes only through MCP, deny it the notes directory in the settings of Claude Code.

Written in your user settings, `~/.claude/settings.json`, this holds in every project (put your notes directory, `notes_dir` of the settings file, in place of `~/notes`):

```json
{
  "permissions": {
    "allow": ["mcp__ebira__*"],
    "deny": ["Read(~/notes/**)", "Edit(~/notes/**)"],
    "disableBypassPermissionsMode": "disable"
  },
  "sandbox": {
    "enabled": true,
    "allowUnsandboxedCommands": false,
    "failIfUnavailable": true,
    "filesystem": {
      "denyRead": ["~/notes"],
      "denyWrite": ["~/notes"]
    }
  }
}
```

- `permissions.deny` keeps the tools that read files (Read, Grep, Glob, and common shell commands such as `cat`) and those that write them (Edit, Write and others) out of the notes directory. Deny rules come before the allow rules of any settings. Write paths with `~/` or `//` (absolute); a single leading `/` is relative to the settings file.
- `permissions.allow` lets it use the MCP tools of ebira without asking each time (leave it out to be asked).
- `sandbox` runs shell commands in a sandbox of the OS, which keeps them out of the notes directory. Deny rules do not hold for commands that read files without naming them (`grep -r` in the notes directory), or for scripts that open files themselves (Python and the like); the sandbox does. `allowUnsandboxedCommands: false` stops retries outside the sandbox, and `failIfUnavailable` stops running commands outside it when the sandbox cannot start.
- `disableBypassPermissionsMode` turns off the mode that skips the questions and the rules (`--dangerously-skip-permissions`).

The MCP server of ebira is another process, outside Claude Code, so it handles the notes whatever these rules say, as it is meant to.

- The sandbox runs on macOS, Linux and WSL2. On Windows, shell commands run without it, and only the deny rules hold.
- Commands you type yourself after `!` at the prompt of Claude Code run outside the sandbox.
- Do not start Claude Code in the notes directory, which would be its working directory.
- How to write the settings may change between versions of Claude Code; see its documentation on permissions and sandboxing.

## Other clients

Clients that keep MCP servers in a JSON settings file (such as `.mcp.json` of a project in Claude Code) take this form; it differs a little between clients. `ebira mcp add -print` prints it too, with the address of the server.

```json
{
  "mcpServers": {
    "ebira": {
      "type": "http",
      "url": "http://localhost:13579/mcp"
    }
  }
}
```

## The assistant

The column `a` opens answers questions, searching and reading your notes. It works when the server starts with the environment variable `ANTHROPIC_API_KEY` (Claude) or `GEMINI_API_KEY` (Gemini). Answers link to the notes they used.

## Pointing an AI to a note

The chain button after the title of the preview (or `Y`) copies the URL of the note, or a Markdown link to it. The part after `#note/` in the URL is the ID of the note, by which an AI can read it.
