---
title: Using AI
tags: [guide, ai]
---
## MCP

ebira is an MCP server. Register this URL with an AI client, and the AI can search and read your notes, and write in collab notebooks ([Kinds of notebooks and AI](kinds.md)).

```
http://localhost:13579/mcp
```

For Claude Code, register it with:

```
claude mcp add --transport http ebira http://localhost:13579/mcp
```

Use your address if you changed it.

## The assistant

The column `a` opens answers questions, searching and reading your notes. It works when the server starts with the environment variable `ANTHROPIC_API_KEY` (Claude) or `GEMINI_API_KEY` (Gemini). Answers link to the notes they used.

## Pointing an AI to a note

The chain button after the title of the preview (or `Y`) copies the URL of the note, or a Markdown link to it. The part after `#note/` in the URL is the ID of the note, by which an AI can read it.
