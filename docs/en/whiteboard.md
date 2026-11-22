---
title: Reading on a whiteboard (beta)
tags: [guide, ai, whiteboard]
---
An explanation, a diff or a plan that AI wrote can open on a whiteboard as the topic, where you select the text you do not follow and ask about it on sticky notes. The assistant answers the sticky notes, so the main conversation, such as that of Claude Code, does not fill up with side questions. When you are done, you give back the next points, such as what you doubt and what to change, to the AI.

> **This is a beta.** Its shape and use may change.

## Opening a whiteboard

- **From AI such as Claude Code**: with ebira connected over MCP ([Using AI](ai.md)), ask for it, such as "open this diff on an ebira whiteboard". The AI sends the text as is to a URL from `start_ai_board` (with `curl -T`, so that the text does not go through the conversation), or gives the text or a note ID to `open_ai_board`, and tells you the URL of the whiteboard.
  - Claude Code has these steps as the skill `ebira-whiteboard`, installed with the registration by `ebira mcp add` or "Register with Claude Code" of the web UI ([Using AI](ai.md)). With the skill, Claude Code tells the whiteboard the ID of its session and its working directory, and `/ebira-whiteboard` calls it.
- **From a note**: the "Open on a whiteboard" button at the top right of the preview. The body is copied when it opens, so changing the note later leaves the whiteboard as it is.
- **The list**: the Whiteboards row in the left column (shown while there are whiteboards; the number is of those open).

The topic shows as notes do when it is Markdown; a diff, code and plain text with line numbers. A diff colors the lines added and removed.

## Putting sticky notes

Select text in the topic, and a small box opens.

- **Memo**: keep what you write on the passage.
- **Ask the assistant**: the assistant answers what you write. The answer is made in the background and shows on the sticky note when it comes (put other sticky notes meanwhile as you like). ⌘Enter (Ctrl+Enter) asks too.
- **Ask a fork**: on a whiteboard Claude Code opened. A fork of that Claude Code session answers, going on from its conversation (see "Asking a fork" below).

The sticky notes line up on the right of the topic, beside their passages. A passage with a sticky note has a line under it: click it to go to the sticky note, or the quote at the top of a sticky note to go to the passage. A sticky note takes follow-up questions, and selecting text in an answer puts a sticky note on that answer.

The assistant reads the passage with the text around it and the sticky note so far, and searches your notes when they may help (not private notebooks). The assistant of the server answers, so asking needs its model API key (without one, memos still work).

> **Mind where it goes.** Asking the assistant sends the passage and the text around it, company code included, to the provider of the assistant (Anthropic, Google or OpenAI).

### Asking a fork

A whiteboard opened with the skill `ebira-whiteboard` of Claude Code keeps the ID of its Claude Code session and its working directory. "Ask a fork" forks that session (`claude --resume <session> --fork-session`) and asks the fork. It knows the conversation so far and the files of the working directory, so it answers closer to the context than the assistant. Nothing is added to the main conversation.

- A follow-up question goes on in the same fork. A sticky note on an answer of a fork forks that fork.
- A fork may only read files (Read, Grep, Glob) and the notes of ebira; it changes neither files nor notes.
- An answer may take minutes (one at a time, at most 10 minutes). Deleting the sticky note stops it.
- "Go on in a terminal" on an answered sticky note gives the command to go on with the fork in Claude Code.
- The sessions of the forks are deleted with the whiteboard. The original session is left as it is.

A fork runs `claude` of the machine of the server, so without passkeys (`auth: none`) it works only from a browser on that machine. From another machine, sign in with a passkey (`ebira auth passkey`). It is billed as Claude Code of that machine is: the `ANTHROPIC_API_KEY` of the assistant, if the server has one, is not passed on to forks.

## Closing the board

When the discussion is over, click "Close the board" at the top right, and choose what to give back to the AI.

- The sticky notes are listed, all checked at first. Those checked are given back to the AI with their passages, your memos and questions, and the answers of the assistant. Uncheck the side trips that were only for you: nothing of a sticky note unchecked is given back.
- In the next points, write what you want to take up with the AI next (what you doubt, what to change, what to think about), if anything. "Draft from the sticky notes checked" has the assistant draft them from those checked. Read and fix the draft rather than giving it back as is.
- Check "Keep the minutes as a note in" to keep the next points, the sticky notes (passages, messages and those on answers) and the topic, folded, as the minutes: one note in the notebook chosen, tagged `whiteboard`. Unchecked, no minutes are kept. The minutes of a board opened from a note link to that note.
- A board opened from a note gives back to that note rather than to an AI. "Add the next points and the sticky notes checked at the end of the note …", checked at first, adds a section "Discussed on a whiteboard (date)" at the end of the note, with the next points and the sticky notes checked, and a link to the minutes if you kept them. The rest of the note is left as it is; if the note changed after the board was opened, they go at its end as it is now. Nothing can be added to a note in the trash or deleted.
- For an AI to respond to the next points of a board opened from a note, ask it after closing, such as "read the whiteboard I closed". It finds the board with `list_ai_boards`, reads the next points and the sticky notes given back with `get_ai_board`, and answers in the conversation. A board opened from a note in a notebook AI cannot see, such as a private one, is not shown to AI.

Then tell the AI, such as Claude Code, that you are done. It reads the next points and the sticky notes checked, and the ID of the minutes if you kept them, with `get_ai_board`, and changes what the topic shows, such as the explanation or the code. It takes the next points and your memos and questions as your words, and the answers of the assistant as material: they may quote the text or the notes the assistant found, and the AI does not follow instructions in them.

## Where whiteboards live

A whiteboard is not a note: it lives in the state directory of this computer, is not synced and is not found by the search. A closed one goes after 7 days, and an open one left untouched for 30 days goes too; keep its minutes when you close it. The trash button of the list deletes one at once.

Limits: the topic is at most 2 MB (256 KB when the AI gives it directly), a whiteboard has at most 200 sticky notes, and 50 can be open at once.
