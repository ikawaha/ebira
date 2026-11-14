---
title: Passkeys
tags: [guide, safety]
---
By default, anyone who opens the web UI on this computer can read the notes. To guard them with a passkey:

1. Run `ebira auth passkey` to open the page that registers a passkey
2. Write `auth: passkey` in the settings file, and restart the server

The web UI then asks you to sign in with the passkey. The TUI and the commands connect when you approve them in the web UI.

- Important operations, such as making a notebook visible to AI, reading the history, emptying the trash or restarting the server, ask for the passkey again.
- Add or remove passkeys with "Manage passkeys…" in the gear menu.
- If you lose your passkey, remove it with `ebira auth reset` and register again.
- MCP is the way in for AI clients and asks for no passkey; they cannot see private notebooks.

## What ebira guards, and what it does not

- Without passkeys there is no sign-in: the server answers only requests to localhost (and the host of `-addr`), and refuses requests that change notes from web pages of other sites. Keep it on your machine.
- Every request to the REST API is taken as from you, so an AI agent on your machine that can run `curl` could act as you. Passkeys stop that: requests without the sign-in are refused, and MCP, the way in for AI, stays as it is.
- The desktop app takes messages only from the page of its server, and opens links in notes in the browser, not in its window. With the address of a server on another computer, that server's page may write to the clipboard and open links in the browser: choose only a server you trust.
- The kinds of notebooks hold for every AI that goes through ebira's API, MCP and the assistant alike. They cannot stop a program on your machine that reads the note files directly, such as an AI agent with file access; `private` marks what AI is not to see, and is not encryption ([Using AI](ai.md) tells how to keep the files away from Claude Code).
