---
title: TUI
tags: [guide, terminal]
---
Run `ebira` in a terminal to open a screen for the same notes as the web UI: the TUI. It starts the server in the background unless it runs.

The bottom line shows the keys that work. The main ones:

| Key | Action |
|---|---|
| `j` / `k` | Move |
| `/` | Search |
| Enter | Open |
| `n` | New note |
| `e` / `E` | Edit / edit in `$EDITOR` |
| `v` | Switch between the preview and the Markdown source |
| `z` | Zoom the document column |
| `f` | Find in the note |
| `T` | Switch between notebooks and tags |
| `x` | Put in the archive or take out of it (the note in the list, or the notebook in the notebook column) |
| `?` | Ask the assistant |
| `q` | Quit |

## Special rows of the notebook column

- The Inbox is at the top and the Trash at the bottom. "📦 Archive" between them shows when there is something in the archive, with the notebooks with notes there under it, marked `↳`.
- The notebooks deleted are listed under the Trash, marked `↳`. `d` on the Trash row empties the trash, after asking.
- Notes in the trash cannot be edited: `u` restores one, `m` moves it to another notebook, and `d` deletes it for good. `u` and `d` work on the rows of deleted notebooks too.
- When a search finds notes in the archive as well, the title of the list says "N more in the archive". Choose the archive row and search again to search there.

In the source mode (`v`), the document column is the editor itself, and the note is saved when you leave it.
