package design

import (
	. "goa.design/goa/v3/dsl"
)

// Whiteboards: a text to understand (the source), such as what Claude Code
// wrote, with sticky notes on the passages a person asked about. This is the
// boards service, for people. AI clients open boards and read what the person
// gives back at the end with the tools of the notes service (open_ai_board,
// start_ai_board, get_ai_board, in design/notes.go). A board lives on this
// device until the person finishes it, and becomes a note only if they choose
// so.

var stickyIDDSL = func() {
	Pattern(`^s[0-9a-f]{8}$`)
	Example("s1a2b3c4d")
}

var BoardAnchor = Type("BoardAnchor", func() {
	Description("The passage a sticky note is on, as a W3C text quote selector: the passage, with some text before and after it to tell it apart from the same text elsewhere")
	RequiredAttribute("exact", String, "The passage", func() {
		MinLength(1)
	})
	Attribute("prefix", String, "Text just before the passage, at most 256 characters")
	Attribute("suffix", String, "Text just after the passage, at most 256 characters")
})

var BoardMessage = Type("BoardMessage", func() {
	RequiredAttribute("role", String, "Who wrote it: the person (user) or the assistant", func() {
		Enum("user", "assistant")
	})
	RequiredAttribute("text", String, "Message in Markdown")
	RequiredAttribute("at", String, "When it was written", timeDSL)
})

var BoardSticky = Type("BoardSticky", func() {
	Description("A sticky note on a passage of the source, or of the answer of another sticky note (its parent)")
	RequiredAttribute("id", String, "Sticky note ID", stickyIDDSL)
	Attribute("parent", String, "The sticky note this one is on, for one on a passage of an answer", stickyIDDSL)
	RequiredAttribute("anchor", BoardAnchor, "The passage the sticky note is on")
	RequiredAttribute("kind", String, "memo, written by the person; assistant, answered by the assistant; or fork, answered by a fork of the Claude Code session that opened the board", func() {
		Enum("memo", "assistant", "fork")
	})
	RequiredAttribute("messages", ArrayOf(BoardMessage), "The messages, oldest first")
	RequiredAttribute("state", String, "idle, waiting for an answer, or error when the last answer failed", func() {
		Enum("idle", "waiting", "error")
	})
	Attribute("error", String, "Why the last answer failed")
	RequiredAttribute("resolved", Boolean, "Whether the person marked the sticky note resolved: they understood it and are satisfied. Asking it again clears it")
	Attribute("session", String, "The Claude Code session that answers a sticky note of the fork kind, which claude --resume goes on with", func() {
		Format(FormatUUID)
	})
	RequiredAttribute("created_at", String, "Creation time", timeDSL)
	RequiredAttribute("updated_at", String, "Last update time", timeDSL)
})

var BoardResult = Type("BoardResult", func() {
	RequiredAttribute("text", String, "What the person gives back to the session, in Markdown")
	RequiredAttribute("stickies", ArrayOf(String, stickyIDDSL), "The sticky notes given back with the text")
	Attribute("note_id", String, "The note the minutes of the board were kept in, if they were", noteIDDSL)
	RequiredAttribute("appended", Boolean, "Whether a link to the minutes was added to the note the board was opened from")
	RequiredAttribute("finished_at", String, "When the board was finished", timeDSL)
	Attribute("claude", BoardClaudeReply, "What Claude made of the next points, when asked to take them up as the board was closed (ask_claude)")
})

var BoardClaudeReply = Type("BoardClaudeReply", func() {
	RequiredAttribute("state", String, "waiting while Claude works, idle once done, or error", func() {
		Enum("idle", "waiting", "error")
	})
	Attribute("text", String, "The reply of Claude to the person, in Markdown")
	RequiredAttribute("edited", Boolean, "Whether the changes of Claude were made to the note the board was opened from")
	Attribute("reason", String, "Why Claude did not reply, or why its changes were not made to the note: the server stopped (interrupted), no reply in time (timeout), Claude could not be asked (failed), the changes are not valid (invalid), the note changed after the board was closed (changed), the note is no longer in a notebook of the collab kind (not_collab), or the changes do not fit the note (not_made)", func() {
		Enum("interrupted", "timeout", "failed", "invalid", "changed", "not_collab", "not_made")
	})
	Attribute("error", String, "Why Claude did not reply, or why its changes were not made to the note, in words (reason)")
})

var BoardSource = Type("BoardSource", func() {
	RequiredAttribute("from", String, "Where the source came from: sent to an upload URL, given as text, or the body of a note", func() {
		Enum("upload", "text", "note")
	})
	Attribute("note_id", String, "The note the source was copied from", noteIDDSL)
	Attribute("note_title", String, "The title of that note now, if it is there")
	Attribute("note_notebook", String, "The notebook of that note now, if it is there, where its minutes go by default", notebookIDDSL)
	Attribute("note_state", String, "The state of that note now: ok; changed since the board was opened; trash, in the trash; or missing, deleted. Closing the board may add to it unless it is in the trash or missing", func() {
		Enum("ok", "changed", "trash", "missing")
	})
	RequiredAttribute("format", String, "How to show the source", func() {
		Enum("markdown", "text", "diff", "code")
	})
	Attribute("lang", String, "Language of code")
	RequiredAttribute("size", Int, "Size of the source, in bytes")
})

var boardTrait = func() {
	RequiredAttribute("id", String, "Board ID", boardIDDSL)
	RequiredAttribute("title", String, "Title")
	RequiredAttribute("status", String, "open, or done once finished", func() {
		Enum("open", "done")
	})
	RequiredAttribute("created_at", String, "Creation time", timeDSL)
	RequiredAttribute("updated_at", String, "Last update time", timeDSL)
}

var BoardSummary = Type("BoardSummary", func() {
	boardTrait()
	RequiredAttribute("format", String, "How the source is shown", func() {
		Enum("markdown", "text", "diff", "code")
	})
	RequiredAttribute("stickies", Int, "Number of sticky notes")
	RequiredAttribute("waiting", Int, "Number of sticky notes waiting for an answer")
})

var Board = Type("Board", func() {
	boardTrait()
	RequiredAttribute("source", BoardSource, "Where the source came from and how to show it")
	RequiredAttribute("text", String, "The source, copied when the board was opened")
	RequiredAttribute("stickies", ArrayOf(BoardSticky), "The sticky notes, in the order they were added")
	Attribute("result", BoardResult, "What was given back, once finished")
	RequiredAttribute("from_session", Boolean, "Whether an AI client opened the board, and waits for what is given back")
	RequiredAttribute("unchanged", Boolean, "Whether the sticky notes are as they were when a person last opened the board from its note: closing it as it is then keeps no minutes by default")
	RequiredAttribute("fork", BoardFork, "Whether sticky notes of the fork kind can be put on the board")
})

var BoardFork = Type("BoardFork", func() {
	RequiredAttribute("available", Boolean, "Whether Claude can answer sticky notes: forks of the Claude Code session that opened the board, or of one ebira makes to read the topic")
	Attribute("reason", String, "Why not: auth_none (a request from another machine without passkeys: forks need passkeys there), session_missing (Claude Code no longer has the session that opened the board), not_found (claude is not found on the machine of the server) or done (the board is finished)", func() {
		Enum("auth_none", "session_missing", "not_found", "done")
	})
	Attribute("cwd", String, "The working directory of the session, where claude --resume goes on with the session of a sticky note")
})

// The whiteboards for people: the web UI reads them, puts sticky notes on them
// and finishes them. Not MCP tools: the boards hold what the person asked
// about, and asking the assistant spends their API key. While passkeys are
// required, only people signed in may call them.
var _ = Service("boards", func() {
	Description("Whiteboards for people: a text to read with sticky notes on its passages, answered by the assistant. AI clients open them, and read what is given back, with the tools of the notes service.")

	Security(SessionAuth)
	Error("unauthorized", ErrorResult, "The session is invalid or expired: sign in with a passkey again")
	HTTP(func() {
		Cookie("session:" + sessionCookie)
		Response("unauthorized", StatusUnauthorized)
	})

	Method("list", func() {
		Description("List the whiteboards, last updated first.")
		Payload(sessionDSL)
		Result(ArrayOf(BoardSummary))
		HTTP(func() {
			GET("/boards")
			Response(StatusOK)
		})
	})

	Method("get", func() {
		Description("Get a whiteboard with its source and sticky notes.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
		})
		Result(Board)
		Error("not_found", ErrorResult, "The board does not exist")
		HTTP(func() {
			GET("/boards/{id}")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
		})
	})

	Method("open", func() {
		Description("Open a whiteboard on the body of a note, copied as it is now; or the whiteboard of the note open already, if a person opened one and has not closed it.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("note_id", String, "The note", noteIDDSL)
			Attribute("title", String, "Title of the board; the note's title when omitted", func() {
				MaxLength(200)
			})
		})
		Result(Board)
		Error("not_found", ErrorResult, "The note does not exist")
		Error("bad_request", ErrorResult, "The body of the note is empty, or too many boards are open")
		HTTP(func() {
			POST("/boards")
			Response(StatusCreated)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("save", func() {
		Description("Rename a whiteboard.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
			RequiredAttribute("title", String, "Title", func() {
				MinLength(1)
				MaxLength(200)
			})
		})
		Result(Board)
		Error("not_found", ErrorResult, "The board does not exist")
		Error("bad_request", ErrorResult, "The title is empty")
		HTTP(func() {
			PATCH("/boards/{id}")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("delete", func() {
		Description("Delete a whiteboard, open or finished; a session waiting for it reads that it does not exist.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
		})
		Error("not_found", ErrorResult, "The board does not exist")
		HTTP(func() {
			DELETE("/boards/{id}")
			Response(StatusNoContent)
			Response("not_found", StatusNotFound)
		})
	})

	Method("add_sticky", func() {
		Description("Put a sticky note on a passage of the source, or of an answer of another sticky note (parent). A memo keeps the text as written; a sticky note of the assistant kind asks it the text as a question, and one of the fork kind asks a fork of the Claude Code session that opened the board, which needs a session signed in with a passkey: they come back waiting, and the answer comes later, told by an event of type board on /events.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
			Attribute("parent", String, "The sticky note whose answer the passage is in", stickyIDDSL)
			RequiredAttribute("anchor", BoardAnchor, "The passage")
			RequiredAttribute("kind", String, "memo; assistant to ask the assistant; fork to ask a fork of the Claude Code session that opened the board", func() {
				Enum("memo", "assistant", "fork")
			})
			Attribute("text", String, "The memo, or the question; required for the assistant and a fork", func() {
				MaxLength(10 << 10)
			})
		})
		Result(BoardSticky)
		Error("not_found", ErrorResult, "The board, or the parent sticky note, does not exist")
		Error("bad_request", ErrorResult, "The passage or the question is empty or too long, the board is finished, or it has too many sticky notes")
		Error("unavailable", ErrorResult, "The assistant is not configured, such as when no model API key is set, or a fork cannot answer (see fork of the board)")
		HTTP(func() {
			POST("/boards/{id}/stickies")
			Response(StatusCreated)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("unavailable", StatusServiceUnavailable)
		})
	})

	Method("save_sticky", func() {
		Description("Change the text of a memo, or mark a sticky note resolved or not.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
			RequiredAttribute("sticky", String, "Sticky note ID", stickyIDDSL)
			Attribute("text", String, "The new text of a memo; empty to leave only the mark on the passage", func() {
				MaxLength(10 << 10)
			})
			Attribute("resolved", Boolean, "Whether the sticky note is resolved: the person understood it and is satisfied")
		})
		Result(BoardSticky)
		Error("not_found", ErrorResult, "The board or the sticky note does not exist")
		Error("bad_request", ErrorResult, "The text is too long, is given for a sticky note that is not a memo, or the board is finished")
		HTTP(func() {
			PATCH("/boards/{id}/stickies/{sticky}")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("delete_sticky", func() {
		Description("Delete a sticky note, with the sticky notes on its answer.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
			RequiredAttribute("sticky", String, "Sticky note ID", stickyIDDSL)
		})
		Error("not_found", ErrorResult, "The board or the sticky note does not exist")
		Error("bad_request", ErrorResult, "The board is finished")
		HTTP(func() {
			DELETE("/boards/{id}/stickies/{sticky}")
			Response(StatusNoContent)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("ask_sticky", func() {
		Description("Add a message to a sticky note: a follow-up question to the assistant or a fork, which comes back waiting as with add_sticky, or a line of a memo. Asking again after an error asks the question that failed when text is omitted.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
			RequiredAttribute("sticky", String, "Sticky note ID", stickyIDDSL)
			Attribute("text", String, "The message", func() {
				MaxLength(10 << 10)
			})
		})
		Result(BoardSticky)
		Error("not_found", ErrorResult, "The board or the sticky note does not exist")
		Error("bad_request", ErrorResult, "The message is empty or too long, the sticky note waits for an answer, the board is finished, or the sticky note has too many messages")
		Error("unavailable", ErrorResult, "The assistant is not configured, such as when no model API key is set, or a fork cannot answer (see fork of the board)")
		HTTP(func() {
			POST("/boards/{id}/stickies/{sticky}/messages")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("unavailable", StatusServiceUnavailable)
		})
	})

	Method("draft", func() {
		Description("Have the assistant draft what to give back to the session, from the sticky notes chosen when finishing. The draft is for the person to check and change before finishing: the sticky notes hold text from outside, such as a pull request, which is not to reach the session unchecked.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
			RequiredAttribute("stickies", ArrayOf(String, stickyIDDSL), "The sticky notes to draft from", func() {
				MinLength(1)
				MaxLength(200)
			})
		})
		Result(func() {
			RequiredAttribute("text", String, "The draft, in Markdown")
		})
		Error("not_found", ErrorResult, "The board does not exist")
		Error("bad_request", ErrorResult, "No sticky note is chosen, a chosen one does not exist, or the board is finished")
		Error("unavailable", ErrorResult, "The assistant is not configured, or failed to answer")
		HTTP(func() {
			POST("/boards/{id}/draft")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("unavailable", StatusServiceUnavailable)
		})
	})

	Method("finish", func() {
		Description("Finish a whiteboard: keep what to give back to the session, the text and the sticky notes chosen, for get_ai_board, and save the board as a note in notebook if given. A finished board takes no more sticky notes and is deleted after 7 days.")
		Payload(func() {
			sessionDSL()
			RequiredAttribute("id", String, boardIDDesc, boardIDDSL)
			RequiredAttribute("text", String, "What to give back, in Markdown; may be empty", func() {
				MaxLength(64 << 10)
			})
			Attribute("stickies", ArrayOf(String, stickyIDDSL), "The sticky notes to give back: the session reads their passages and messages, those of the assistant too. The others are not given back", func() {
				MaxLength(200)
			})
			Attribute("notebook", String, "Notebook to keep the minutes of the board in, as a note; not kept when omitted, nor for a board with no sticky notes and no next points", notebookIDDSL)
			Attribute("append_to_note", Boolean, "Add a line linking to the minutes, which are then to be kept (notebook), in the section of discussions at the end of the note the board was opened from (Appendix: discussions)")
			Attribute("ask_claude", Boolean, "Ask Claude, in the background, to take up the next points of a board opened on a note of a notebook of the collab kind: it replies, and the changes it gives are made to that note. The next points must not be empty, and Claude must be able to answer (see fork of the board)")
			Attribute("lang", String, "Language of the headings of the note, that of the UI", func() {
				Enum("en", "ja")
				Default("en")
			})
		})
		Result(Board)
		Error("not_found", ErrorResult, "The board does not exist")
		Error("bad_request", ErrorResult, "The board is finished, a sticky note waits for an answer or does not exist, the notebook is invalid, the note to add to is not there or the minutes are not kept (append_to_note), or Claude cannot take up the next points (ask_claude)")
		HTTP(func() {
			POST("/boards/{id}/finish")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})
})
