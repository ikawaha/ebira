package design

import (
	. "goa.design/goa-ai/dsl"
	. "goa.design/goa/v3/dsl"
)

// NoteTrait is the attributes that Note and NoteSummary share.
var NoteTrait = func() {
	RequiredAttribute("id", String, "Note ID: the file name without the extension (notes/<notebook>/<id>.yaml), unique across notebooks", noteIDDSL)
	RequiredAttribute("notebook", String, "Notebook (directory) the note is in", notebookIDDSL)
	RequiredAttribute("title", String, "Title", func() {
		Example("Get the CREATE TABLE statement")
	})
	RequiredAttribute("tags", ArrayOf(String), "Tags", func() {
		Example([]string{"DB", "postgres"})
	})
	RequiredAttribute("created_at", String, "Creation time", timeDSL)
	RequiredAttribute("updated_at", String, "Last update time", timeDSL)
	Attribute("trashed_notebook", String, "For a note of a notebook deleted as a whole: the ID of that notebook, which is in the trash. notebook is then trash, and restoring the note puts it back into that notebook", notebookIDDSL)
	Attribute("created_by", String, "Who created the note: ai for a note created by an AI client (create_ai_note); absent for people", func() {
		Enum("ai")
	})
}

// Note is a single note.
var Note = Type("Note", func() {
	NoteTrait()
	RequiredAttribute("body", String, "Body in Markdown")
	RequiredAttribute("version", String, "Strong entity tag of the note file. Sent as the ETag header over HTTP", func() {
		Example(`"87ef7c76b82c5b1d299b08b474f6a7bb"`)
	})
	Attribute("trashed_from", String, "Notebook the note was in before it was moved to the trash", notebookIDDSL)
	Attribute("title_highlights", ArrayOf(TextRange), "Ranges of the title matched by the query of get, sorted and not overlapping")
	Attribute("body_highlights", ArrayOf(TextRange), "Ranges of the body matched by the query of get, sorted and not overlapping")
})

// TextRange is a range of a string, used to highlight matched terms.
var TextRange = Type("TextRange", func() {
	Description("Range of characters, in Unicode code point offsets")
	RequiredAttribute("start", Int, "Offset of the first character")
	RequiredAttribute("end", Int, "Offset just past the last character")
})

// NoteSummary is an entry of a list or search result.
var NoteSummary = Type("NoteSummary", func() {
	NoteTrait()
	Attribute("snippet", String, "Excerpt around the matched terms (search only)")
	Attribute("title_highlights", ArrayOf(TextRange), "Ranges of the title matched by the query, sorted and not overlapping (search only)")
	Attribute("snippet_highlights", ArrayOf(TextRange), "Ranges of the snippet matched by the query, sorted and not overlapping (search only)")
})

// Notebook is a directory of notes. inbox and trash always exist. The display
// name and order are kept in notes/<id>/_notebook.yaml.
var Notebook = Type("Notebook", func() {
	RequiredAttribute("id", String, "Notebook ID, which is also its directory name", notebookIDDSL)
	RequiredAttribute("name", String, "Display name. Defaults to the ID (Inbox and Trash for the reserved notebooks)")
	RequiredAttribute("order", Int, "Sort key among the notebooks other than inbox and trash; lower comes first")
	RequiredAttribute("count", Int, "Number of notes")
	RequiredAttribute("reserved", Boolean, "Whether this is a system notebook (inbox or trash)")
	RequiredAttribute("kind", String, "Kind of the notebook. "+kindDescription, func() {
		Enum(notebookKinds...)
	})
	RequiredAttribute("sort", String, sortDescription, func() {
		Enum(sortKeys...)
	})
})

// TrashedNotebook is a notebook deleted as a whole, kept in the trash with its
// notes and settings.
var TrashedNotebook = Type("TrashedNotebook", func() {
	RequiredAttribute("id", String, "Notebook ID", notebookIDDSL)
	RequiredAttribute("name", String, "Display name")
	RequiredAttribute("kind", String, "Kind of the notebook, kept for when it is restored", func() {
		Enum(notebookKinds...)
	})
	RequiredAttribute("count", Int, "Number of notes")
})

// NoteList is a page of a note list.
var NoteList = Type("NoteList", func() {
	RequiredAttribute("total", Int, "Number of notes in the whole list, regardless of limit and offset")
	RequiredAttribute("notes", ArrayOf(NoteSummary), "Notes from offset, at most limit; fewer than total when the list goes on")
})

var SearchResult = Type("SearchResult", func() {
	RequiredAttribute("mode", String, "Search mode used", func() {
		Enum("like", "bigram", "ja")
	})
	RequiredAttribute("total", Int, "Number of matched notes")
	RequiredAttribute("notes", ArrayOf(NoteSummary), "Matched notes")
})

// NoteEvent notifies that a note was changed by the API, a file edit or a git pull.
var NoteEvent = Type("NoteEvent", func() {
	RequiredAttribute("type", String, "Kind of change. ready is sent once when the stream starts; clients should reload the note list on it. notebooks means the notebook list, names or order changed", func() {
		Enum("ready", "created", "updated", "deleted", "notebooks")
	})
	Attribute("id", String, "Note ID (absent for ready and notebooks)", noteIDDSL)
})

var SyncResult = Type("SyncResult", func() {
	RequiredAttribute("added", Int, "Number of notes added to the index")
	RequiredAttribute("updated", Int, "Number of notes updated in the index")
	RequiredAttribute("removed", Int, "Number of notes removed from the index")
	RequiredAttribute("errors", ArrayOf(String), "Files that could not be read")
})

// sortKeys are the orders of a note list. order is the manual order: notes
// moved with move come where they were put, and the others by creation time,
// newest first.
var sortKeys = []any{"order", "created", "updated", "title"}

const sortDescription = "Order of the note list: order is the manual order (notes not moved with move come by creation time, newest first), created and updated are newest first, and title is by code point, ignoring case and width"

// notebookKinds are the kinds of notebooks, which decide what AI clients may do
// with their notes.
var notebookKinds = []any{"default", "collab", "drop", "private"}

const kindDescription = "AI clients may only read notes in default notebooks, may also create, update and delete notes in collab notebooks, which people share with AI, may only add notes to drop notebooks, whose notes they cannot see, and cannot see private notebooks at all. In the notebook list, AI clients see drop notebooks with a count of 0. A notebook without a kind set is default, except the inbox, which is drop"

var notebookFilter = func() {
	Attribute("notebook", String, "Only notes in this notebook. Omit for all notebooks except the trash", notebookIDDSL)
}

var _ = Service("notes", func() {
	Description("CRUD and search for notes.")

	// MCP tools are what AI clients can do, so they follow the access rules,
	// which the server checks:
	//   - AI clients read notes except in drop and private notebooks and the
	//     trash.
	//   - They write only notes in notebooks of the collab kind (*_ai_note), and
	//     may add notes to notebooks of the drop kind (create_ai_note), whose
	//     notes they cannot see.
	//   - They may create a new notebook of the collab kind (create_ai_notebook),
	//     but not turn an existing notebook into one.
	//   - They may make any notebook they can see private at any time
	//     (make_notebook_private), which only takes access away from them.
	// Never add Tool to create_notebook or save_notebook, which set any kind,
	// or to create, update and delete, which are for people. TestMCPTools in
	// internal/service lists the allowed tools.
	MCP("ebira", "0.1.0")
	JSONRPC(func() { POST("/mcp") })

	// Each method declares the errors it returns, with their HTTP statuses, so
	// that the API description and the MCP tools list only those. An error a
	// method returns without declaring it becomes a 500.

	Method("list", func() {
		Description("List notes in the order of sort, a page at a time: total tells how many there are, and offset reads the next page.")
		Payload(func() {
			notebookFilter()
			Attribute("tag", String, "Only notes with this tag")
			Attribute("limit", Int, "Maximum number of notes", func() {
				Default(50)
				Minimum(1)
				Maximum(500)
			})
			Attribute("offset", Int, "Number of notes to skip", func() {
				Default(0)
				Minimum(0)
			})
			Attribute("trashed_notebook", String, "With notebook=trash, list the notes of this notebook in the trash instead of the notes deleted on their own", notebookIDDSL)
			Attribute("sort", String, sortDescription+". Omit for the sort setting of the notebook (order without a notebook)", func() {
				Enum(sortKeys...)
			})
		})
		Result(NoteList)
		Error("bad_request", ErrorResult, "trashed_notebook is given without notebook=trash")
		HTTP(func() {
			GET("/notes")
			Param("notebook")
			Param("tag")
			Param("limit")
			Param("offset")
			Param("trashed_notebook")
			Param("sort")
			Response("bad_request", StatusBadRequest)
		})
		Tool("list_notes", "List notes, in the notebook's order unless sort is given. total is the number of notes; use offset for more", func() {
			ReadOnlyHint(true)
		})
	})

	Method("get", func() {
		Description("Get a note. With q, the result also has the ranges of the title and body matched by the query, as search matches them.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			Attribute("q", String, "Search query to highlight")
		})
		Result(Note)
		Error("not_found", ErrorResult, "The note does not exist, or is hidden from the client")
		Error("bad_request", ErrorResult, "The ID is used in more than one notebook")
		HTTP(func() {
			GET("/notes/{id}")
			Param("q")
			Response(StatusOK, func() { Header("version:ETag") })
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
		Tool("get_note", "Get the full content of a note by ID", func() {
			ReadOnlyHint(true)
		})
	})

	Method("create", func() {
		Description("Create a note. A random ID is assigned when id is omitted, and the note goes to the inbox when notebook is omitted. A new notebook is created on demand.")
		Payload(func() {
			Attribute("id", String, "Note ID", noteIDDSL)
			Attribute("notebook", String, "Notebook to put the note in (default inbox; not trash)", notebookIDDSL)
			RequiredAttribute("title", String, "Title", func() {
				MinLength(1)
			})
			Attribute("tags", ArrayOf(String), "Tags")
			Attribute("body", String, "Body in Markdown")
		})
		Result(Note)
		Error("bad_request", ErrorResult, "The notebook is the trash or invalid")
		Error("already_exists", ErrorResult, "A note with the ID exists (in any notebook, including the trash)")
		HTTP(func() {
			POST("/notes")
			Response(StatusCreated, func() { Header("version:ETag") })
			Response("bad_request", StatusBadRequest)
			Response("already_exists", StatusConflict)
		})
	})

	Method("update", func() {
		Description("Update a note. Omitted attributes are left unchanged. Setting notebook moves the note; use delete to move it to the trash.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			Attribute("notebook", String, "Notebook to move the note to (not trash)", notebookIDDSL)
			Attribute("title", String, "Title", func() {
				MinLength(1)
			})
			Attribute("tags", ArrayOf(String), "Tags")
			Attribute("body", String, "Body in Markdown")
			Attribute("if_match", String, "Update only if the note's current version matches. Omit to overwrite unconditionally")
		})
		Result(Note)
		Error("not_found", ErrorResult, "The note does not exist")
		Error("bad_request", ErrorResult, "The notebook to move the note to is the trash or invalid")
		Error("precondition_failed", ErrorResult, "If-Match does not match the note's current version")
		HTTP(func() {
			PATCH("/notes/{id}")
			Header("if_match:If-Match")
			Response(StatusOK, func() { Header("version:ETag") })
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("precondition_failed", StatusPreconditionFailed)
		})
	})

	Method("delete", func() {
		Description("Move a note to the trash, or delete it permanently if it is already in the trash.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			Attribute("if_match", String, "Delete only if the note's current version matches. Omit to delete unconditionally")
		})
		Error("not_found", ErrorResult, "The note does not exist")
		Error("bad_request", ErrorResult, "The note cannot be deleted, such as when its ID is used in more than one notebook")
		Error("precondition_failed", ErrorResult, "If-Match does not match the note's current version")
		HTTP(func() {
			DELETE("/notes/{id}")
			Header("if_match:If-Match")
			Response(StatusNoContent)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("precondition_failed", StatusPreconditionFailed)
		})
	})

	Method("search", func() {
		Description("Search notes. The search mode (like, bigram or ja, Japanese morphological analysis) is set by the server configuration.")
		Payload(func() {
			RequiredAttribute("q", String, "Search terms separated by spaces, combined with AND", func() {
				MinLength(1)
			})
			notebookFilter()
			Attribute("tag", String, "Only notes with this tag")
			Attribute("limit", Int, "Maximum number of notes", func() {
				Default(20)
				Minimum(1)
				Maximum(200)
			})
		})
		Result(SearchResult)
		HTTP(func() {
			GET("/search")
			Param("q")
			Param("notebook")
			Param("tag")
			Param("limit")
		})
		Tool("search_notes", "Full-text search over notes. Japanese terms are supported", func() {
			ReadOnlyHint(true)
		})
	})

	Method("restore", func() {
		Description("Move a note from the trash back to the notebook it was in.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			Attribute("if_match", String, "Restore only if the note's current version matches")
		})
		Result(Note)
		Error("not_found", ErrorResult, "The note does not exist")
		Error("bad_request", ErrorResult, "The note is not in the trash, or its notebook exists with another kind")
		Error("precondition_failed", ErrorResult, "If-Match does not match the note's current version")
		HTTP(func() {
			POST("/notes/{id}/restore")
			Header("if_match:If-Match")
			Response(StatusOK, func() { Header("version:ETag") })
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("precondition_failed", StatusPreconditionFailed)
		})
	})

	// move is for people, who arrange notes by hand; it is not an MCP tool.
	Method("move", func() {
		Description("Put a note right after another note of the same notebook in the manual order (sort order), or first when after is omitted. Only the moved note is written, with its new order; updated_at is kept, as the content does not change.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			Attribute("after", String, "ID of the note of the same notebook to put the note after; omit to put it first", noteIDDSL)
		})
		Result(Note)
		Error("not_found", ErrorResult, "The note does not exist")
		Error("bad_request", ErrorResult, "The note is in the trash, or after is not another note of its notebook")
		HTTP(func() {
			POST("/notes/{id}/move")
			Response(StatusOK, func() { Header("version:ETag") })
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("notebooks", func() {
		Description("List notebooks with their note counts: inbox first, then the others by order and name, trash last.")
		Result(ArrayOf(Notebook))
		HTTP(func() {
			GET("/notebooks")
		})
		Tool("list_notebooks", "List notebooks with their kinds and note counts", func() {
			ReadOnlyHint(true)
		})
	})

	// The notebook methods set the kinds, which decide what AI clients may
	// write, so they are for people only and must not become MCP tools.
	Method("create_notebook", func() {
		Description("Create a notebook from its display name. The ID, which is also the directory name, is derived from the name with only the characters an ID allows: an ASCII name is lower-cased with dashes (\"My Notes\" gives my-notes), and any other name is Punycode-encoded as in internationalized domain names, giving an ID that starts with xn--. A suffix such as -2 is added when the ID is taken.")
		Payload(func() {
			RequiredAttribute("name", String, "Display name", func() {
				MinLength(1)
				MaxLength(100)
			})
			Attribute("order", Int, "Sort key; lower comes first. Omit to put the notebook after the others")
			Attribute("kind", String, "Kind of the notebook. "+kindDescription, func() {
				Enum(notebookKinds...)
				Default("default")
			})
		})
		Result(Notebook)
		Error("bad_request", ErrorResult, "The name is empty")
		Error("already_exists", ErrorResult, "A notebook with the name exists")
		HTTP(func() {
			POST("/notebooks")
			Response(StatusCreated)
			Response("bad_request", StatusBadRequest)
			Response("already_exists", StatusConflict)
		})
	})

	Method("save_notebook", func() {
		Description("Set the display name, order, kind or note sort of a notebook, creating the notebook if it does not exist. The order of inbox and trash cannot be changed.")
		Payload(func() {
			RequiredAttribute("id", String, "Notebook ID", notebookIDDSL)
			Attribute("name", String, "Display name; empty to reset it to the default")
			Attribute("order", Int, "Sort key; lower comes first")
			Attribute("kind", String, "Kind of the notebook. "+kindDescription+". Inbox cannot be collab, and the trash keeps the default kind (it is hidden from AI clients anyway)", func() {
				Enum(notebookKinds...)
			})
			Attribute("sort", String, "Default order of the note list. "+sortDescription, func() {
				Enum(sortKeys...)
			})
		})
		Result(Notebook)
		Error("not_found", ErrorResult, "The notebook does not exist")
		Error("bad_request", ErrorResult, "The change is not allowed for the notebook, such as an order for inbox or trash")
		HTTP(func() {
			PUT("/notebooks/{id}")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("delete_notebook", func() {
		Description("Move a notebook with its notes and settings into the trash. Inbox and trash cannot be deleted. Fails with already_exists if the trash holds a notebook with the same ID.")
		Payload(func() {
			RequiredAttribute("id", String, "Notebook ID", notebookIDDSL)
		})
		Error("not_found", ErrorResult, "The notebook does not exist")
		Error("bad_request", ErrorResult, "The notebook is inbox or trash")
		Error("already_exists", ErrorResult, "The trash holds a notebook with the same ID")
		HTTP(func() {
			DELETE("/notebooks/{id}")
			Response(StatusNoContent)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("already_exists", StatusConflict)
		})
	})

	Method("trashed_notebooks", func() {
		Description("List the notebooks in the trash. List their notes with list, notebook=trash and trashed_notebook.")
		Result(ArrayOf(TrashedNotebook))
		HTTP(func() {
			GET("/trash/notebooks")
			Response(StatusOK)
		})
	})

	Method("restore_notebook", func() {
		Description("Move a notebook out of the trash with its notes and settings. Fails with already_exists if a notebook with the same ID exists.")
		Payload(func() {
			RequiredAttribute("id", String, "Notebook ID", notebookIDDSL)
		})
		Result(Notebook)
		Error("not_found", ErrorResult, "The notebook is not in the trash")
		Error("bad_request", ErrorResult, "The notebook ID is invalid")
		Error("already_exists", ErrorResult, "A notebook with the same ID exists")
		HTTP(func() {
			POST("/trash/notebooks/{id}/restore")
			Response(StatusOK)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("already_exists", StatusConflict)
		})
	})

	Method("delete_trashed_notebook", func() {
		Description("Permanently delete a notebook in the trash with its notes.")
		Payload(func() {
			RequiredAttribute("id", String, "Notebook ID", notebookIDDSL)
		})
		Error("not_found", ErrorResult, "The notebook is not in the trash")
		Error("bad_request", ErrorResult, "The notebook ID is invalid")
		HTTP(func() {
			DELETE("/trash/notebooks/{id}")
			Response(StatusNoContent)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	// The methods for AI clients write only notes in notebooks of the collab kind,
	// so that notes written by people stay read-only for them, and add notes to
	// notebooks of the drop kind. They are exposed as MCP tools only; people
	// edit notes with create, update and delete.
	Method("create_ai_note", func() {
		Description("Create a note in a notebook of the collab kind, which people share with AI, or add one to a notebook of the drop kind, such as an inbox, whose notes AI clients cannot see and therefore cannot update or delete. Other notebooks are read-only for AI clients: use list_notebooks to find the notebooks whose kind is collab or drop. The note is marked as created by AI (created_by).")
		Payload(func() {
			Attribute("id", String, "Note ID; random if omitted. Must be omitted for a drop notebook", noteIDDSL)
			RequiredAttribute("notebook", String, "ID of a notebook of the collab or drop kind", notebookIDDSL)
			RequiredAttribute("title", String, "Title", func() {
				MinLength(1)
			})
			Attribute("tags", ArrayOf(String), "Tags")
			Attribute("body", String, "Body in Markdown")
		})
		Result(Note)
		Error("bad_request", ErrorResult, "The ID is given for a drop notebook, or the notebook is invalid")
		Error("forbidden", ErrorResult, "The notebook is not of the collab or drop kind")
		Error("already_exists", ErrorResult, "A note with the ID exists")
		Tool("create_ai_note", "Create a note in a notebook shared with AI (kind collab), or add one to a notebook of the drop kind, whose notes you cannot see afterwards. Notes in other notebooks are read-only")
	})

	Method("update_ai_note", func() {
		Description("Update a note in a notebook of the collab kind. Omitted attributes are left unchanged. The version from get_note is required, so that changes made by people meanwhile are not overwritten. Setting notebook moves the note to another notebook of the collab kind.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			RequiredAttribute("version", String, "Version of the note as returned by get_note")
			Attribute("notebook", String, "ID of a notebook of the collab kind to move the note to", notebookIDDSL)
			Attribute("title", String, "Title", func() {
				MinLength(1)
			})
			Attribute("tags", ArrayOf(String), "Tags")
			Attribute("body", String, "Body in Markdown")
		})
		Result(Note)
		Error("not_found", ErrorResult, "The note does not exist, or is hidden from AI clients")
		Error("bad_request", ErrorResult, "The notebook to move the note to is invalid")
		Error("forbidden", ErrorResult, "The note, or the notebook to move it to, is not of the collab kind")
		Error("precondition_failed", ErrorResult, "The version does not match the note's current version")
		Tool("update_ai_note", "Update a note in a notebook shared with AI (kind collab); pass the version from get_note")
	})

	Method("delete_ai_note", func() {
		Description("Move a note in a notebook of the collab kind to the trash, where people can restore it. The version from get_note is required.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			RequiredAttribute("version", String, "Version of the note as returned by get_note")
		})
		Error("not_found", ErrorResult, "The note does not exist, or is hidden from AI clients")
		Error("bad_request", ErrorResult, "The note cannot be deleted")
		Error("forbidden", ErrorResult, "The note is not in a notebook of the collab kind")
		Error("precondition_failed", ErrorResult, "The version does not match the note's current version")
		Tool("delete_ai_note", "Move a note in a notebook shared with AI (kind collab) to the trash; pass the version from get_note")
	})

	Method("create_ai_notebook", func() {
		Description("Create a new notebook of the collab kind, where AI clients may write notes. Existing notebooks cannot be turned into collab notebooks by AI clients; people do that.")
		Payload(func() {
			RequiredAttribute("name", String, "Display name", func() {
				MinLength(1)
				MaxLength(100)
			})
		})
		Result(Notebook)
		Error("bad_request", ErrorResult, "The name is empty")
		Error("already_exists", ErrorResult, "A notebook with the name exists")
		Tool("create_ai_notebook", "Create a new notebook shared with AI (kind collab), where you may write notes")
	})

	Method("make_notebook_private", func() {
		Description("Make a notebook private, so that AI clients can no longer see it or its notes. Only people can undo this.")
		Payload(func() {
			RequiredAttribute("id", String, "Notebook ID", notebookIDDSL)
		})
		Error("not_found", ErrorResult, "The notebook does not exist, or is not in the notebook list of the client")
		Error("bad_request", ErrorResult, "The notebook is the trash")
		Tool("make_notebook_private", "Make a notebook private so that AI clients can no longer see it; only people can undo this")
	})

	Method("events", func() {
		Description("Stream note changes as Server-Sent Events. Events missed while disconnected are not replayed; the ready event sent on every (re)connection tells clients to reload the note list.")
		StreamingResult(NoteEvent)
		HTTP(func() {
			GET("/events")
			ServerSentEvents(func() {
				SSEEventType("type")
			})
		})
	})

	Method("sync", func() {
		Description("Scan the notes directory and bring the SQLite index in line with the YAML files. Not needed while the server watches files.")
		Result(SyncResult)
		HTTP(func() {
			POST("/sync")
		})
	})
})
