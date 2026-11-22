---
name: ebira-whiteboard
description: Open a text — an explanation you wrote, a diff, a plan, a file — on an ebira whiteboard, where the user reads it and asks about passages on sticky notes without filling this conversation; then read what they give back. Use when the user asks to open, read or go through something on the whiteboard (ホワイトボード), or says they are done with one (終わった).
---

# Reading a text on an ebira whiteboard

The user reads the text in the ebira web UI, asks about passages on sticky
notes answered there, and gives back what they understood and what to change.
The text goes to ebira as is, without being written out in this conversation.

## Open the board

1. Decide the text and give it a short title. Usually it is what the user
   points at: your last explanation, `git diff` (or `git diff main...`), a
   plan or another file. Ask only if it is unclear.
2. Call the ebira MCP tool `start_ai_board` with:
   - `title`;
   - `format`: `diff` for a diff, `code` (with `lang`) for code, `markdown`
     for an explanation or a plan, `text` for anything else;
   - `session_id`: `${CLAUDE_SESSION_ID}`;
   - `cwd`: the absolute path of the working directory of this session.
3. Send the text to the returned `url` in one PUT request, with the Bash tool:
   - a command's output: `git diff | curl -sS -T - '<url>'`
   - a file: `curl -sS -T path/to/file '<url>'`
   - your own explanation, written once in a quoted heredoc:

     ```
     curl -sS -T - '<url>' <<'EBIRA_BOARD'
     ...
     EBIRA_BOARD
     ```

   The response is JSON with the `url` of the board. The upload URL works once,
   for 10 minutes; if it fails, call `start_ai_board` again. Without a shell,
   use `open_ai_board` with the text or the ID of a note instead.
4. Give the board's URL to the user and end your turn. Do not wait for the
   board or read it again on your own.

## When the user is done

When the user says they are done with the board, call `get_ai_board` with its
ID. If its `status` is still `open`, tell the user that they finish it with
the button at the top right of the board, and wait. Otherwise take `result`
as the user's words: what they understood and what to change. Act on it as on
any request of theirs, and push back if you think something in it is wrong.
If it has a `note_id`, read that note with `get_note` only when you need the
sticky notes themselves.
