---
title: Commands
tags: [guide, terminal]
---
| Command | Action |
|---|---|
| `ebira` | Open the TUI |
| `ebira web` | Open the web UI in the browser |
| `ebira server start` / `stop` / `restart` / `status` | Run the server in the background |
| `ebira serve` | Run the server in the foreground |
| `ebira import path…` | Import Markdown files |
| `ebira files check` | Find images no note uses |
| `ebira sync` | Sync with git |
| `ebira auth passkey` | Register a passkey |
| `ebira version` | Print the version |

Each command tells its usage with `-h`. One server runs for each notes directory, shared by the TUI, the web UI and MCP.
