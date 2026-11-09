---
title: Passkeys
tags: [guide, safety]
---
By default, anyone who opens the web UI on this computer can read the notes. To guard them with a passkey:

1. Run `ebira auth passkey` to open the page that registers a passkey
2. Write `auth: passkey` in the settings file, and restart the server

The web UI then asks you to sign in with the passkey. The TUI and the commands connect when you approve them in the web UI.

- Important operations, such as making a notebook visible to AI, reading the history, emptying the trash or connecting to a git remote, ask for the passkey again.
- Add or remove passkeys with "Manage passkeys…" in the gear menu.
- If you lose your passkey, remove it with `ebira auth reset` and register again.
- MCP is the way in for AI clients and asks for no passkey; they cannot see private notebooks.
