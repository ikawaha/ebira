---
title: Settings
tags: [guide, settings]
---
## The gear menu

- Settings…: open the settings dialog below
- Import Markdown (or drop files onto the page)
- Check files, Sync with git, Server status (its Restart starts the server again, with the same flags and environment)
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
- This machine: those of the settings file below (the language, the theme, authentication, syncing with git and more), and the style of the preview (Band, Document, Compact or Lined). A setting left at its first choice, or empty, follows the defaults shared with the notes ("As shared: …").
- Synced with notes: the search mode, the names of the marks and the defaults of each machine (`_ebira.yaml`).

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

## Sharing settings among machines

Write keys of the settings file under `defaults:` in `_ebira.yaml`, and they become the defaults of every machine that syncs the notes. A value in the settings file of a machine wins, so write there only what a machine does differently.

```yaml
search: ja
defaults:
  lang: en
  theme: auto
  git_sync: 10m
  model: claude-sonnet-5-5
```

- The order is: the flags of a command, then the settings file of the machine, then `defaults` of `_ebira.yaml`, then the built-in defaults.
- The settings file of a machine needs only the notes directory (`notes_dir`).
- `notes_dir`, `auth` (how to sign in) and an `addr` other than localhost belong to each machine. Written in `defaults`, they are left out with a warning, so that other machines, or people you share the notes with, cannot turn off passkeys or open the server to the network.
- When you make new notes, setup writes the language, the interval of syncing and the address to `defaults`. On another machine with the notes, setup does not ask what `defaults` has, and writes the answers to what it lacks to the settings file of that machine.
- `defaults` can also choose the provider and the model of the assistant, used with the API key of each machine, and turn on syncing with git. When you share the notes with other people, watch the changes to `_ebira.yaml`.
- An earlier ebira does not know `defaults`: it says "field defaults not found" and does not start. Update ebira on every machine.

## CSS of the preview

Put `preview.css` next to the settings file to style the preview with your own CSS.
