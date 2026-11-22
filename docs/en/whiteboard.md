---
title: Reading on a whiteboard (beta)
tags: [guide, ai, whiteboard]
---
An explanation, a diff or a plan that AI wrote can open on a whiteboard as the text to read, where you choose passages you do not follow and ask about them on sticky notes. The assistant answers the sticky notes, so the main conversation, such as that of Claude Code, does not fill up with side questions. When you are done, you give back what you understood and what to change to the session that opened it.

> **This is a beta.** Its shape and use may change.

## Opening a whiteboard

- **From AI such as Claude Code**: with ebira connected over MCP ([Using AI](ai.md)), ask for it, such as "open this diff on an ebira whiteboard". The AI sends the text as is to a URL from `start_ai_board` (with `curl -T`, so that the text does not go through the conversation), or gives the text or a note ID to `open_ai_board`, and tells you the URL of the whiteboard.
  - Claude Code has these steps as the skill `ebira-whiteboard`, installed with the registration by `ebira mcp add` or "Register with Claude Code" of the web UI ([Using AI](ai.md)). With the skill, Claude Code tells the whiteboard the ID of its session and its working directory, and `/ebira-whiteboard` calls it.
- **From a note**: the "Open on a whiteboard" button at the top right of the preview. The body is copied when it opens, so changing the note later leaves the whiteboard as it is.
- **The list**: the Whiteboards row in the left column (shown while there are whiteboards; the number is of those open).

Markdown is shown as notes are; a diff, code and plain text with line numbers. A diff colors the lines added and removed.

## Putting sticky notes

Choose a passage of the text, and a small box opens.

- **Memo**: keep what you write on the passage.
- **Ask the assistant**: the assistant answers what you write. The answer is made in the background and shows on the sticky note when it comes (put other sticky notes meanwhile as you like). ⌘Enter (Ctrl+Enter) asks too.
- **Ask a fork**: on a whiteboard Claude Code opened. A fork of the session that opened it answers, going on from its conversation (see "Asking a fork" below).

The sticky notes line up on the right of the text, beside their passages. A passage has a mark: click it to go to its sticky note, or the quote at the top of a sticky note to go to the passage. A sticky note takes follow-up questions, and choosing a passage of an answer puts a sticky note on that answer.

The assistant reads the passage with the text around it and the sticky note so far, and searches your notes when they may help (not private notebooks). The assistant of the server answers, so asking needs its model API key (without one, memos still work).

> **Mind where it goes.** Asking the assistant sends the passage and the text around it, company code included, to the provider of the assistant (Anthropic, Google or OpenAI).

### Asking a fork

A whiteboard opened with the skill `ebira-whiteboard` of Claude Code keeps the ID of the session that opened it and its working directory. "Ask a fork" forks that session (`claude --resume <session> --fork-session`) and asks the fork. It knows the conversation so far and the files of the working directory, so it answers closer to the context than the assistant. Nothing is added to the main conversation.

- A follow-up question goes on in the same fork. A sticky note on an answer of a fork forks that fork.
- A fork may only read files (Read, Grep, Glob) and the notes of ebira; it changes neither files nor notes.
- An answer may take minutes (one at a time, at most 10 minutes). Deleting the sticky note stops it.
- "Go on in a terminal" on an answered sticky note gives the command to go on with the fork in Claude Code.
- The sessions of the forks are deleted with the whiteboard. The session that opened it is left as it is.

A fork runs `claude` of the machine of the server, so it works only while you are signed in with a passkey (`ebira auth passkey`). Without passkeys (`auth: none`), the button is disabled. It is billed as Claude Code of that machine is: the `ANTHROPIC_API_KEY` of the assistant, if the server has one, is not passed on to forks.

## Finishing

Finish at the top right, and choose what to give back to the session that opened the whiteboard.

- The sticky notes are listed, all checked at first. Those checked are given back to the session with their passages, your memos and questions, and the answers of the assistant. Uncheck the side trips that were only for you: nothing of a sticky note unchecked is given back.
- In the text to give back, write what else you want to tell the session, if anything. "Draft from the sticky notes checked" has the assistant draft from those checked. Read and fix the draft rather than giving it back as is.
- Check "Save the whiteboard as a note in" to save what you give back, the sticky notes (passages, messages and those on answers) and the source, folded, as one note in the notebook chosen, tagged `whiteboard`. Unchecked, no note is made.

Then tell the AI that opened the whiteboard, such as Claude Code, that you are done. It reads the text and the sticky notes checked, and the ID of the note if you saved one, with `get_ai_board`, and changes the text. It takes the text and your memos and questions as your words, and the answers of the assistant as material: they may quote the text or the notes the assistant found, and the AI does not follow instructions in them.

## Where whiteboards live

A whiteboard is not a note: it lives in the state directory of this computer, is not synced and is not found by the search. A finished one goes after 7 days, and an open one left untouched for 30 days goes too; save one as a note to keep it. The trash button of the list deletes one at once.

Limits: the text is at most 2 MB (256 KB when the AI gives it directly), a whiteboard has at most 200 sticky notes, and 50 can be open at once.
