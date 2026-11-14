---
title: Desktop App
tags: [guide]
---
An app to use the web UI in a window of its own, with menus, instead of a browser tab. It runs on macOS and Windows. Inside it is the web UI, and it shares the notes directory and the server with the TUI, the commands and MCP.

## Installing

It is not distributed yet, so an install script builds it from the source of ebira and installs it. It needs Go 1.27 or later and git. To update the app, run the same again: a running app is asked to quit first (it asks about a note being edited), and the server the installed app started is started again with the new build.

**macOS** (macOS 13 or later, with the Xcode Command Line Tools), in the Terminal:

```sh
curl -fsSL https://raw.githubusercontent.com/ikawaha/ebira/main/apps/desktop/install.sh | sh
```

This installs `/Applications/ebira.app` (with `sh -s -- -user`, `~/Applications/ebira.app`), with the server (`ebira`) inside, signed for this Mac only, and opens it.

**Windows**, in PowerShell:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/ikawaha/ebira/main/apps/desktop/install-windows.ps1)))
```

This installs the app in `%LOCALAPPDATA%\Programs\ebira`, adds "ebira" to the Start menu, and opens it. Go is at https://go.dev/dl/, and git at https://git-scm.com/. Windows 10 may need the WebView2 runtime installed.

The scripts build the latest release. To build another version, give its tag or a branch: `sh -s -- v0.8.1` on macOS, and on Windows the tag after the closing parenthesis.

In a clone of the repository (or, on Windows, the source from "Code" → "Download ZIP" on the GitHub page, which needs no git), the scripts build that source: `apps/desktop/install.sh`, or `powershell -ExecutionPolicy Bypass -File apps\desktop\install-windows.ps1`.

To build without installing, run `apps/desktop/build.sh`, which makes `apps/desktop/build/ebira.app`, or `powershell -ExecutionPolicy Bypass -File apps\desktop\build-windows.ps1`, which makes `apps\desktop\build\windows-amd64\ebira\` (for Windows on ARM, set `$env:GOARCH = "arm64"` first); the folder works when copied elsewhere or to another PC too. On macOS or Linux, `apps/desktop/build-windows.sh` makes the folder for Windows as well.

## On start

The app uses the server at the address of its settings (`localhost:13579` by default) if it runs for the same notes directory, and starts the server inside it otherwise. The server keeps running when you quit the app (`ebira server stop` stops it).

When it cannot connect, the settings window tells why.

## Settings

"Settings…" in the ebira menu (Cmd+,; the File menu on Windows) opens the settings dialog of the web UI ([Settings](settings.md)). Change… in its App tab opens the settings window of the app, which sets the notes folder and the address of the server; with no server answering, "Settings…" opens this window. With the notes folder empty, it follows `notes_dir` of the config file ([Settings](settings.md)). "Apply" connects again, to the server of those notes.

With the address of a server on another computer, the app opens that server's page as it is, and lets it write to the clipboard and open links in the browser. Choose only a server you trust, such as one you run yourself.

The gear menu leaves out what the menus of the app have, and shows only when you are signed in with a passkey.

## Menus

Editing, saving, finding, marks, history, copying and more are in the menus too, doing what the keys of the web UI do. "Put in Archive" in the Note and Notebook menus puts the note and the notebook shown in the archive, and "Candidates for the Archive…" in the File menu opens the candidates ([Notes and notebooks](notes.md)). "Filter the List" in the View menu switches the search box between searching and filtering, checked while it filters ([How to search](search.md)). The Help menu opens this guide, [how to search](search.md), and the commands to connect AI ([Using AI](ai.md)).

When the server is older than the app, a ↻ mark shows beside the connection status at the top right. Click it to restart the server with the one inside the app.

"About ebira" shows the version of the app, and the version, address and notes directory of the server it is connected to.

## Logs

When something goes wrong, look at the log of the app and the log of the server.

- The app: `~/Library/Logs/ebira/desktop.log` on macOS, `%LocalAppData%\ebira\desktop.log` on Windows. Past 1 MiB it moves to `desktop.log.1` and starts over, so the two stay at about 2 MiB.
- The server: `server.log` in the state directory of the notes directory (`ebira server status` tells where).
