---
title: Settings
tags: [guide, settings]
---
## The gear menu

- Settings…: open the settings dialog below
- Import Markdown (or drop files onto the page)
- Candidates for the archive…: list the notes not updated for a while, to choose and put in the archive ([Notes and notebooks](notes.md))
- Check files, Sync with git, Server status
- Connect AI (MCP)…: show the URL of ebira and the command for Claude Code; a button registers it with Claude Code ([Using AI](ai.md))
- Add the guide
- Report an issue: open the page to post a request or a bug on GitHub
- Manage passkeys… and Sign out, when signed in with a passkey
- At the bottom, the version of ebira; choose it to copy the details, to tell in a report. The GitHub mark at its right opens the repository of ebira

In the desktop app, the gear menu leaves out what the menus of the app have ([Desktop App](desktop.md)).

## The settings dialog

Settings… in the gear menu (⌘, in the desktop app) opens it. The column on the left chooses the settings by what they are about:

- Display: the language, the theme (auto, light or dark), the text size of notes (− and +), the style of the preview (Band, the default, Document, Compact, Lined or Plain) and the names of the marks.
- Search: the search mode, and the stop words and stop tags of ja.
- Sync: how often to sync with git, and watching the files.
- Integrations: the repository, the branch and the user name for Zenn (beta) ([Writing to Zenn](zenn.md)).
- Assistant: the provider and the model.
- TUI: the preview and the search box of the TUI.
- Server: the notes directory and the address, authentication, and where the settings files are. In the desktop app it shows the notes folder and the address of the server that the app connects to, and Change… opens the settings window of the app ([Desktop App](desktop.md)).

Each setting has one value. The language, the theme and the settings of sync, integrations, the assistant and the TUI have a check box, "Share with other machines":

- Unchecked, the value is written to the settings file of this machine (see The settings file below), for this machine alone.
- Checked, it is written to `defaults` in `_ebira.yaml` of the notes directory, as the default of every machine that syncs these notes (see Sharing settings among machines below), and taken out of the settings file of this machine.
- When this machine has a value of its own and the other machines share another, the dialog shows this machine's, with the shared one below it. Emptied, this machine's value follows the shared one ("As shared: …").
- The search settings and the names of the marks are always shared. The text size of notes alone is kept in this browser: − and + try a size at once, Save keeps it and Cancel goes back (the − and + of the settings menu, the gear at the bottom of the left column, change it too, and keep it at once; in the desktop app, so do Larger Text, Smaller Text and Default Text Size in the View menu, ⌘+, ⌘- and ⌘0, or Ctrl+, Ctrl+- and Ctrl+0 on Windows. Only the body of notes changes, not the list or the titles). The style of the preview is tried on the sample beside it and on the note open as soon as it is chosen; Save writes it to `preview.css`, next to the settings file, and Cancel goes back. Without `preview.css`, the preview is drawn in Band (choosing Band removes `preview.css`). For the preview with nothing added, choose Plain.

Save writes the settings file of this machine and `_ebira.yaml` together. The language and the theme apply as soon as they are saved (a new language reloads the page). The server reads the other settings when it starts; after saving, restart the server to apply them (see Restarting the server below).

## Restarting the server

The ↻ beside the connection status at the top right ("live") restarts the server with the same flags and environment, and it reads the settings file again. For the few seconds of the restart, no client can use the server. The page reloads after it.

The mark is faint. While settings read at the start are saved but the server has not restarted yet, it is colored. A server that cannot restart itself, such as one started by `go run`, has no mark (Server status tells why). When the desktop app shows its own ↻ for a server older than the app, only that one shows ([Desktop App](desktop.md)).

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

The keys are `notes_dir`, `addr`, `watch`, `provider`, `model`, `lang`, `theme` (`auto`, `light` or `dark`; `light` makes the TUI use its light colors too), `git_sync`, `auth` (`none` or `passkey`), `tui_preview` (`rendered` or `source`, which `v` in the TUI switches and writes) `tui_search` (`search` or `filter`, which Tab in the search box of the TUI switches and writes), and `zenn_repo`, `zenn_branch` and `zenn_user` ([Writing to Zenn](zenn.md)). Each is the default of a flag of the commands ([Commands](cli.md)), and a key ebira does not know is an error. API keys never go in it. The index is kept in `~/.cache/ebira`, and the server's state and log in `~/.local/state/ebira` (the XDG variables are honored).

You can also change its settings one by one in the settings dialog, which keeps the comments of the file. But for the language and the theme, the server reads the settings when it starts, so save them, then restart the server to apply them. `notes_dir` and `addr`, though, apply only when you stop the server and start it again.

The settings synced with the notes are in `_ebira.yaml` of the notes directory, such as the search mode `search`.

## Safe mode

With a mistake in the settings file or in `_ebira.yaml` of the notes directory, such as an error of YAML or a key ebira does not know, the commands stop telling it, and the server does not start. `_ebira.yaml` is synced with the notes, so a mistake on one machine reaches them all. To use the notes until it is fixed, set the environment variable `EBIRA_SAFE_MODE=1`, and they start without the file with the mistake (safe mode).

```sh
EBIRA_SAFE_MODE=1 ebira server start
```

- Of the settings file, only the line of `notes_dir` is taken, when it reads; the rest are the defaults. As the settings of authentication are not known, the server starts only on an address of this computer, such as `localhost:13579`.
- Of `_ebira.yaml`, only the line of the search mode `search` is taken, when it reads, so that the index is not made again; `defaults`, the stop lists and the names of the marks are the defaults.
- The web UI tells above its panes that it is in safe mode, and the mistake. Fix the file, then press "Restart" there to run with the settings fixed.
- With no mistake in either file, `EBIRA_SAFE_MODE` changes nothing.

In the desktop app, "Start in Safe Mode" in its settings window does the same ([Desktop App](desktop.md)).

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

Put `preview.css` next to the settings file to style the preview with your own CSS. Your CSS takes the place of Band: to start from the preview with nothing added, choose Plain in the settings and add to the `preview.css` it writes.

The CSS applies to the body of notes only: write `h1 { … }` and the rest of the page is left alone. It is read on each page load. The colors of the web UI, such as `var(--accent)`, `var(--fg)` and `var(--fg-muted)`, follow the light and dark themes:

```css
h1 { border-bottom: 2px solid var(--accent); color: var(--accent); }
h2 { background: var(--accent); color: #fff; border-radius: 4px; padding: 2px 8px; }
hr { border: none; border-top: 2px dotted var(--fg-muted); }
strong { color: #c0392b; }
:root[data-theme="dark"] & strong { color: #f7dc6f; }  /* the dark theme chosen in the menu */
@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) & strong { color: #f7dc6f; } }  /* the system's */
```

So that the CSS cannot send the content of notes anywhere, what would make the browser fetch something is dropped: `@import`, and `url()` and the like except with a `data:` URL. Fonts installed on the machine work; web fonts do not.
