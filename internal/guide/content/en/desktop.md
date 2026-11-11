---
title: Desktop App
tags: [guide]
---
An app to use the web UI in a window of its own, with menus, instead of a browser tab. It runs on macOS and Windows. Inside it is the web UI, and it shares the notes directory and the server with the TUI, the commands and MCP.

## Building

It is not distributed yet, so you build it from the ebira repository. It needs Go 1.27 or later.

**macOS** (macOS 26 or later, with the Xcode Command Line Tools):

```sh
git clone https://github.com/ikawaha/ebira.git
cd ebira
apps/desktop/build.sh
open apps/desktop/build/ebira.app
```

This makes `apps/desktop/build/ebira.app`, with the server (`ebira`) inside, signed for this Mac only. Move it to the Applications folder if you like.

**Windows** (built with PowerShell):

1. Install Go from https://go.dev/dl/
2. Get the source of ebira (`git clone https://github.com/ikawaha/ebira.git`; without git, "Code" → "Download ZIP" on the GitHub page, and extract it)
3. In PowerShell, go to that folder and run

```powershell
powershell -ExecutionPolicy Bypass -File apps\desktop\build-windows.ps1
```

This makes `apps\desktop\build\windows-amd64\ebira\` (for Windows on ARM, set `$env:GOARCH = "arm64"` first). Open `ebira-desktop.exe` in it. The folder works when copied elsewhere or to another PC too. Windows 10 may need the WebView2 runtime installed.

On macOS or Linux, `apps/desktop/build-windows.sh` makes the folder for Windows as well.

## On start

The app uses the server at the address of its settings (`localhost:13579` by default) if it runs for the same notes directory, and starts the server inside it otherwise. The server keeps running when you quit the app (`ebira server stop` stops it).

When it cannot connect, the settings window tells why.

## Settings

"Settings…" in the ebira menu (Cmd+,; the File menu on Windows) opens the settings dialog of the web UI ([Settings](settings.md)). Change… in its App tab opens the settings window of the app, which sets the notes folder and the address of the server; with no server answering, "Settings…" opens this window. With the notes folder empty, it follows `notes_dir` of the config file ([Settings](settings.md)). "Apply" connects again, to the server of those notes.

With the address of a server on another computer, the app opens that server's page as it is, and lets it write to the clipboard and open links in the browser. Choose only a server you trust, such as one you run yourself.

The gear menu leaves out what the menus of the app have, and shows only when you are signed in with a passkey.

## Menus

Editing, saving, finding, marks, history, copying and more are in the menus too, doing what the keys of the web UI do. The Help menu opens this guide, [how to search](search.md), and the commands to connect AI ([Using AI](ai.md)).

When the server is older than the app, a ↻ mark shows beside the connection status at the top right. Click it to restart the server with the one inside the app.

"About ebira" shows the version of the app, and the version, address and notes directory of the server it is connected to.

## Logs

When something goes wrong, look at the log of the app and the log of the server.

- The app: `~/Library/Logs/ebira/desktop.log` on macOS, `%LocalAppData%\ebira\desktop.log` on Windows. Past 1 MiB it moves to `desktop.log.1` and starts over, so the two stay at about 2 MiB.
- The server: `server.log` in the state directory of the notes directory (`ebira server status` tells where).
