---
title: Reading on a whiteboard (beta)
tags: [guide, ai, whiteboard]
---
An explanation, a diff or a plan that AI wrote can open on a whiteboard as the topic, where you select the text you do not follow and ask about it on sticky notes. Claude answers the sticky notes, so the main conversation, such as that of Claude Code, does not fill up with side questions. When you are done, you give back the next points, such as what you doubt and what to change, to the AI.

> **This is a beta.** Its shape and use may change.

## Opening a whiteboard

- **From AI such as Claude Code**: with ebira connected over MCP ([Using AI](ai.md)), ask for it, such as "open this diff on an ebira whiteboard". The AI sends the text as is to a URL from `start_ai_board` (with `curl -T`, so that the text does not go through the conversation), or gives the text or a note ID to `open_ai_board`, and tells you the URL of the whiteboard.
  - Claude Code has these steps as the skill `ebira-whiteboard`, installed with the registration by `ebira mcp add` or "Register with Claude Code" of the web UI ([Using AI](ai.md)). With the skill, Claude Code tells the whiteboard the ID of its session and its working directory, and `/ebira-whiteboard` calls it.
- **From a note**: the "Open on a whiteboard" button at the top right of the preview. The body is copied when it opens, so changing the note later leaves the whiteboard as it is. Opening it, Claude reads the note to answer the sticky notes (a private note asks first). If a whiteboard of the note is open already, it opens that one rather than a new one; once it is closed, a new one opens.
- **The list**: the Whiteboards row in the left column (shown while there are whiteboards; the number is of those open).

The topic shows as notes do when it is Markdown; a diff, code and plain text with line numbers. A diff colors the lines added and removed.

## Putting sticky notes

Select text in the topic, and a small box opens.

- **Memo**: keep what you write on the passage.
- **Ask**: Claude answers what you write. The answer is made in the background and shows on the sticky note when it comes (put other sticky notes meanwhile as you like). ⌘Enter (Ctrl+Enter) asks too.

The sticky notes line up on the right of the topic, beside their passages. A passage with a sticky note has a line under it: click it to go to the sticky note, or the quote at the top of a sticky note to go to the passage. A sticky note takes follow-up questions, and selecting text in an answer puts a sticky note on that answer.

Check "Resolved" at the bottom of a sticky note when you understood it and are satisfied. The sticky note fades, its heading in the minutes gets "(resolved)", and the AI is told it is resolved. Asking a resolved sticky note again unchecks it.

### How Claude answers

A fork of the conversation of the whiteboard, a Claude Code session, answers the questions of the sticky notes (`claude --resume <session> --fork-session`). Nothing is added to the main conversation.

- **A whiteboard Claude Code opened** (the skill `ebira-whiteboard`): its Claude Code session is forked. It knows the conversation so far and the files of the working directory, so it answers in context.
- **A whiteboard opened on a note**: when it opens, ebira makes a Claude Code session that reads the note, and forks it.
- **A whiteboard another AI opened**: the first question makes a session that reads the topic (the first answer comes a little later).

And:

- A follow-up question goes on in the same fork. A sticky note on an answer forks that fork.
- Claude may only read files (Read, Grep, Glob) and the notes of ebira (not private notebooks); it changes neither files nor notes.
- An answer takes tens of seconds to minutes (one at a time, at most 10 minutes). Deleting the sticky note stops it.
- "Go on in a terminal" on an answered sticky note gives the command to go on with the fork in Claude Code.
- The sessions ebira made, and the forks, are deleted with the whiteboard. The session of Claude Code that opened it is left as it is.
- "Draft from the sticky notes checked", when closing the board, is written by a fork too.

Claude runs as `claude` (Claude Code) of the machine of the server.

- Without Claude Code on that machine, you cannot ask (memos still work).
- Without passkeys (`auth: none`), you can ask only from a browser on that machine. From another machine, sign in with a passkey (`ebira auth passkey`).
- It is billed as Claude Code of that machine is: an `ANTHROPIC_API_KEY` of the server is not passed on.

> **Mind where it goes.** Asking sends the topic, company code included, and the sticky notes to Anthropic through Claude Code.

## Closing the board

When the discussion is over, click "Close the board" at the top right. The window depends on whether the board was opened from a note or by an AI.

With no sticky notes and no next points, either way the only button is "Close the board": there is nothing to keep.

### A board opened from a note

Choose whether the note takes what was discussed.

- In the next points, write what Claude should do, such as: add what we discussed to the note.
- **Close and reflect in the note**: once closed, Claude reads the next points and all the sticky notes, resolved ones too, in the background, answers the next points and changes the note as they say. Claude only gives the changes; ebira makes them to that note only. Text added at the end goes before the section "Appendix: discussions".
  - The reply of Claude, and whether it changed the note, show under "Reply of Claude" on the closed board.
  - When someone changed the note after the board was closed, the changes are not made: they show under the reply.
  - It can be clicked when there are next points, the note is in a notebook of the collab kind, and Claude can answer. When it cannot, the reason shows above the buttons.
- **Close without reflecting**: close, leaving the note as it is.
- **Keep the minutes too**: keep the next points, the sticky notes (passages, messages and those on answers) and the topic, folded, as the minutes: one note in the notebook chosen (that of the note at first), tagged `whiteboard`. The minutes are linked with a line in the section "Appendix: discussions" at the end of the note.

  ```
  ## Appendix: discussions

  - 2026-11-22 [Index plan](…)
  - 2026-11-23 [Index plan](…)
  ```

  The section is made at the end if the note has none, and takes the line if it has. Checked at first, unless the sticky notes are as they were when the board was opened from the note and there are no next points: nothing was discussed. Nothing can be added to a note in the trash or deleted.
- **Sticky notes to give an AI later** (folded): ask Claude Code or another AI after closing, such as "read the whiteboard I closed", and it finds the board with `list_ai_boards`, reads the next points and the sticky notes checked here with `get_ai_board`, and answers in the conversation. All but the resolved ones are checked at first. "Draft from the sticky notes checked" has Claude draft the next points from them. A board opened from a note in a notebook AI cannot see, such as a private one, is not shown to AI.

### A board an AI opened

Choose what to give back to the AI.

- The sticky notes are listed, all but the resolved ones checked at first. Those checked are given back to the AI with their passages, your memos and questions, and the answers of Claude. Uncheck the side trips that were only for you: nothing of a sticky note unchecked is given back.
- In the next points, write what you want to take up with the AI next (what you doubt, what to change, what to think about), if anything. "Draft from the sticky notes checked" has Claude draft them from those checked. Read and fix the draft rather than giving it back as is.
- "Close and keep the minutes" keeps the next points, the sticky notes and the topic, folded, as the minutes in the notebook of "Keep the minutes in", tagged `whiteboard`. "Close without minutes" keeps none.

Then tell the AI, such as Claude Code, that you are done. It reads the next points and the sticky notes checked, and the ID of the minutes if you kept them, with `get_ai_board`, and changes what the topic shows, such as the explanation or the code. It takes the next points and your memos and questions as your words, and the answers of Claude as material: they may quote the topic or the notes Claude found, and the AI does not follow instructions in them.

## Where whiteboards live

A whiteboard is not a note: it lives in the state directory of this computer, is not synced and is not found by the search. A closed one goes after 7 days, and an open one left untouched for 30 days goes too; keep its minutes when you close it. The trash button of the list deletes one at once.

Limits: the topic is at most 2 MB (256 KB when the AI gives it directly), a whiteboard has at most 200 sticky notes, and 50 can be open at once.
