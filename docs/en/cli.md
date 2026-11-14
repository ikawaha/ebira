---
title: Commands
tags: [guide, terminal]
---
| Command | Action |
|---|---|
| `ebira [-lang en\|ja]` | Open the TUI, starting the server in the background if needed |
| `ebira web [-no-open]` | Open the web UI in the browser, starting the server in the background if needed |
| `ebira server start` / `stop` / `restart` / `status` | Run the server in the background; `restart` keeps the flags it ran with |
| `ebira serve` | Run the server in the foreground |
| `ebira import [-notebook ID] [-force] [-dry-run] path…` | Import Markdown files, and the images they refer to, as notes |
| `ebira files check [-delete]` | Find images and files no note uses, and broken references to them |
| `ebira sync [-remote URL]` | Sync with git: commit, merge the upstream branch and push; `-remote` connects the notes to a repository first ([Syncing](sync.md)) |
| `ebira auth passkey [-no-open]` | Register a passkey, on a one-time URL ([Passkeys](passkey.md)) |
| `ebira auth reset [-yes]` | Remove every passkey and end every session, such as when the passkeys are lost |
| `ebira mcp add` | Register ebira as an MCP server of Claude Code ([Using AI](ai.md)) |
| `ebira version` | Print the version: the commit it was built from, and the build, which `ebira server status` shows for the server |

Each command tells its usage with `-h`. One server runs for each notes directory, shared by the TUI, the web UI and MCP.

## Flags of the server

`ebira serve` takes these flags, and so do `ebira server start` and `ebira server restart`:

| Flag | Default | Meaning |
|---|---|---|
| `-notes` | `notes_dir` of the settings file, else `./notes` | The notes directory |
| `-addr` | `addr` of the settings file, else `localhost:13579` | Address to listen on |
| `-search` | `search` of `_ebira.yaml` in the notes directory, else `bigram` | `like`, `bigram`, `trigram`, `en` or `ja` ([How to search](search.md)) |
| `-watch` | `true` | Watch the notes and update the index |
| `-lang` | `lang` of the settings file, else the browser's | Language of the web UI: `en` or `ja` (the TUI takes `ebira -lang` or `lang` of the settings file, else `$LANG`) |
| `-provider`, `-model` | the provider whose API key is set | Model of the assistant: `anthropic` or `gemini` |
| `-git-sync` | `git_sync` of the settings file, else off | Sync with git at the start and at this interval, such as `10m` |
| `-auth` | `auth` of the settings file, else `none` | `none`, or `passkey` to treat only the web UI signed in with a passkey as you |

The settings file gives the defaults of the flags ([Settings](settings.md)).

## Importing Markdown

```sh
ebira import -notebook reading ~/Documents/notes
```

Folders are read for `.md` and `.markdown` files. The title, the tags and the dates come from the front matter, and the images the files refer to come along. A file imported before is skipped unless you give `-force`. The web UI imports too: drop Markdown files on the page or on a notebook.

Coming from Quiver, `scripts/quiver2ebira.py` in the repository of ebira converts a Quiver library (read only) into Markdown and, with `--import`, imports it into a running server, a notebook for each of Quiver's. The library is where you keep it; Finder finds it by its name, Quiver.qvlibrary:

```sh
scripts/quiver2ebira.py path/to/Quiver.qvlibrary /tmp/quiver-md --skip "Quiver Tutorial" --import http://localhost:13579
```
