---
title: Installing and setting up
tags: [guide, basics]
---
## Installing

The `ebira` command, with the server, the web UI and the TUI, runs on macOS and Linux. It needs Go 1.27 or later:

```sh
go install github.com/ikawaha/ebira/cmd/ebira@latest
```

On macOS and Windows there is also the desktop app, which carries the server; on Windows, it is the way to run ebira. How to install it is in [Desktop app](desktop.md).

## Setting up

Start ebira. The first time, it asks where to keep your notes, the git repository to sync them with (if any), how to search them and the language of the web UI and the TUI, and saves the answers to `~/.config/ebira/config.yaml`, with the address of the server (`localhost:13579`), so that the URL you give to MCP clients stays the same.

```console
$ ebira web
No notes found: no -notes, no notes_dir in ~/.config/ebira/config.yaml, and no notes/ in ~/work.
Let's set up ebira.

Directory to keep the notes in [~/notes]:
...
```

`ebira web` starts the server in the background and opens the web UI at <http://localhost:13579/>. `ebira` opens the TUI instead. The server keeps running when you close them.

The desktop app takes the same settings, so open it once ebira is set up this way; or choose the notes folder in its Settings… (Cmd+,; the File menu on Windows), as on Windows without the `ebira` command.

A new notes directory starts with this guide, in the language you chose. Read it from the top, try things on its notes, and move it to the trash when you are done; it does not come back by itself, but "Add the guide" in the gear menu adds it again.

## Next

- To share the notes among machines, answer a folder synced by Dropbox or the like, such as `~/Dropbox/notes`, or give the same git repository on every machine ([Syncing](sync.md)).
- Bring your Markdown files with `ebira import`, or move from Quiver ([Commands](cli.md)).
- Let your AI in, as far as you like ([Using AI](ai.md)).
