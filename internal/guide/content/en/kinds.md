---
title: Kinds of notebooks and AI
tags: [guide, safety, ai]
---
Each notebook tells what AI clients (MCP clients and the assistant) may do with it. Change the kind in the "…" menu of the notebook.

| Kind | AI clients |
|---|---|
| default | read |
| collab | read, write and delete |
| drop | only add notes, and cannot read them |
| private | do nothing, and do not see the notebook |

The Inbox starts as drop. This guide is default, so an AI asked how to use ebira can read it to answer.

- Notes an AI made have the mark of a plover (✦ in the TUI).
- Making a private notebook visible asks first.
- The limits hold for AI clients that go through the API of ebira, not for those that read the note files directly. Claude Code can be denied the notes directory in its settings ([Using AI](ai.md)).
