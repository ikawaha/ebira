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
