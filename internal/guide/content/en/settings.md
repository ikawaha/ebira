---
title: Settings
tags: [guide, settings]
---
## The gear menu

- Settings…: open the settings dialog below
- Import Markdown (or drop files onto the page)
- Check files, Sync with git, Server status
- Connect AI (MCP)…: show the URL of ebira and the command for Claude Code; a button registers it with Claude Code ([Using AI](ai.md))
- Add the guide
- Report an issue: open the page to post a request or a bug on GitHub
- Manage passkeys… and Sign out, when signed in with a passkey
- At the bottom, the version of ebira; choose it to copy the details, to tell in a report

In the desktop app, the gear menu leaves out what the menus of the app have ([Desktop App](desktop.md)).

## The settings dialog

Settings… in the gear menu (⌘, in the desktop app) opens it. It has tabs:

- App (in the desktop app only): the notes folder and the address of the server that the app connects to. Change… opens the settings window of the app ([Desktop App](desktop.md)).
- Display: the language, the theme (auto, light or dark) and the text size of notes (− and +). They are kept in this browser and apply at once; the language and the theme take precedence over the settings of the server.
- This machine: those of the settings file below (the language, the theme, authentication, syncing with git and more), and the style of the preview (Band, Document, Compact or Lined).
- Synced with notes: the search mode and the names of the marks (`_ebira.yaml`).

Save writes This machine and Synced with notes together.

## The settings file

What you write in `~/.config/ebira/config.yaml` becomes the default of the commands.

```yaml
notes_dir: ~/notes   # the notes directory
addr: localhost:13579
lang: en             # language of the UI
theme: auto
git_sync: 10m        # how often to sync with git
auth: none           # passkey asks for a passkey
```

You can also change its settings one by one in the settings dialog, which keeps the comments of the file. The server reads the settings when it starts, so choose Save and restart to apply them. `notes_dir` and `addr`, though, apply only when you stop the server and start it again.

The settings synced with the notes are in `_ebira.yaml` of the notes directory, such as the search mode `search`.

## CSS of the preview

Put `preview.css` next to the settings file to style the preview with your own CSS.
