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
	RequiredAttribute("id", String, "Notebook ID, which is also its directory name. It is derived from the display name when the notebook is created: an ASCII name is lower-cased with dashes, and a name with other characters, such as Japanese, is Punycode-encoded into an ID that starts with xn--, as in internationalized domain names. Pass the ID as is wherever a notebook is asked for, and show people the name instead", notebookIDDSL)
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

// TagCount is a tag with the number of notes that have it.
var TagCount = Type("TagCount", func() {
	RequiredAttribute("name", String, "Tag")
	RequiredAttribute("count", Int, "Number of notes with the tag")
})

// NoteFile is a file of a note, such as an image.
var NoteFile = Type("NoteFile", func() {
	RequiredAttribute("name", String, "File name", func() {
		Example("screenshot.png")
	})
	RequiredAttribute("path", String, "Path of the file relative to the note file, to refer to it from the body, such as ![](<path>)", func() {
		Example("be4c1193735ee1.assets/screenshot.png")
	})
})

// UnusedFile is a file of a note that no note body refers to.
var UnusedFile = Type("UnusedFile", func() {
	RequiredAttribute("note_id", String, "ID of the note holding the file", noteIDDSL)
	RequiredAttribute("title", String, "Title of the note holding the file")
	RequiredAttribute("name", String, "File name", func() {
		Example("screenshot.png")
	})
	RequiredAttribute("path", String, "Path of the file relative to the note file", func() {
		Example("be4c1193735ee1.assets/screenshot.png")
	})
	RequiredAttribute("size", Int64, "Size in bytes")
	RequiredAttribute("added_at", String, "Last modification time of the file, usually when it was added", timeDSL)
	RequiredAttribute("recent", Boolean, "Whether the file was added within the last hour, such as to a note still being edited, whose body does not refer to it until saved")
})

// BrokenFileRef is a reference in a note body to a file that does not exist.
var BrokenFileRef = Type("BrokenFileRef", func() {
	RequiredAttribute("note_id", String, "ID of the note whose body has the reference", noteIDDSL)
	RequiredAttribute("title", String, "Title of the note whose body has the reference")
	RequiredAttribute("path", String, "The path referred to", func() {
		Example("be4c1193735ee1.assets/screenshot.png")
	})
})

// FileCheck is the result of checking the files of notes against the bodies.
var FileCheck = Type("FileCheck", func() {
	RequiredAttribute("unused", ArrayOf(UnusedFile), "Files that no note body, including those in the trash, refers to, sorted by note and name")
	RequiredAttribute("broken", ArrayOf(BrokenFileRef), "References to files that do not exist, sorted by note and path")
})

// ImportFile is an image of a Markdown file to import, referred to from it by
// a relative path.
var ImportFile = Type("ImportFile", func() {
	RequiredAttribute("ref", String, "Path of the image as the Markdown file refers to it, one of the paths the file refers to", func() {
		Example("images/photo.png")
	})
	RequiredAttribute("name", String, "File name", func() {
		MinLength(1)
		MaxLength(255)
		Example("photo.png")
	})
	RequiredAttribute("content", Bytes, "The image: PNG, JPEG, GIF or WebP, at most 10 MB")
})

// ImportResult is the outcome of importing a Markdown file.
var ImportResult = Type("ImportResult", func() {
	RequiredAttribute("status", String, "created, or skipped when a file with the same content was imported before", func() {
		Enum("created", "skipped")
	})
	Attribute("note", Note, "The note created")
	Attribute("duplicate_of", String, "When skipped: the note imported from the same content before", noteIDDSL)
	RequiredAttribute("warnings", ArrayOf(String), "What could not be converted as is, such as images left out")
})

// importContent is the Markdown file of an import.
var importContent = func() {
	RequiredAttribute("name", String, "File name of the Markdown file, the title when the file gives none", func() {
		MinLength(1)
		MaxLength(255)
		Example("meeting.md")
	})
	RequiredAttribute("content", String, "The Markdown file, at most 5 MB. A YAML front matter gives the title, tags (a list or comma-separated), created (or date) and updated times; its other keys are kept in the note file. Without a title, the first heading of level 1 is the title, else the file name", func() {
		MaxLength(5 << 20)
	})
}

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

var GitSyncResult = Type("GitSyncResult", func() {
	RequiredAttribute("committed", Int, "Number of changed files committed")
	Attribute("upstream", String, "Upstream branch merged and pushed to; absent when the branch has none, and then the changes are only committed", func() {
		Example("origin/main")
	})
	RequiredAttribute("merged", Int, "Number of files changed by the merge of the upstream branch")
	RequiredAttribute("pushed", Boolean, "Whether commits were pushed")
	RequiredAttribute("conflicts", ArrayOf(GitConflict), "Files changed on both sides of the merge, or deleted on one side and changed on the other")
	RequiredAttribute("errors", ArrayOf(String), "Files that could not be read when the index was updated after the sync")
})

var GitStatus = Type("GitStatus", func() {
	RequiredAttribute("repository", Boolean, "Whether the notes are in a git repository, so that git_sync can sync them; git is optional")
	Attribute("upstream", String, "Upstream branch that git_sync merges and pushes to; absent when there is none", func() {
		Example("origin/main")
	})
})

var GitConflict = Type("GitConflict", func() {
	RequiredAttribute("path", String, "File, relative to the notes directory; this side's version is kept, or the changed version when the other side deleted it", func() {
		Example("inbox/3f9a1c0e7b2d4a.yaml")
	})
	Attribute("note_id", String, "ID of the note of the file, for a note's file", noteIDDSL)
	Attribute("copy_id", String, "ID of the new note made from the other side's version of the note, when both sides changed it", noteIDDSL)
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
	//     may add notes to notebooks of the drop kind (create_ai_note,
	//     import_ai_note), whose notes they cannot see.
	//   - They may create a new notebook of the collab kind (create_ai_notebook),
	//     but not turn an existing notebook into one.
	//   - They may make any notebook they can see private at any time
	//     (make_notebook_private), which only takes access away from them.
	// Never add Tool to create_notebook or save_notebook, which set any kind,
	// or to create, update, delete and import, which are for people. The files of
	// notes (add_file, get_file, delete_file, check_files) are for people too
	// for now. TestMCPTools in
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
		Tool("list_notebooks", "List notebooks with their kinds and note counts; other tools take a notebook by its id, and people know it by its name", func() {
			ReadOnlyHint(true)
		})
	})

	Method("tags", func() {
		Description("List the tags of the notes with their note counts, by name (case-insensitive, full-width and half-width forms alike).")
		Payload(func() {
			notebookFilter()
		})
		Result(ArrayOf(TagCount))
		HTTP(func() {
			GET("/tags")
			Param("notebook")
		})
		Tool("list_tags", "List tags with their note counts; filter list_notes or search_notes by one of them", func() {
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

	Method("import", func() {
		Description("Import a Markdown file as a new note with a random ID. A file whose content was imported before, even if the note is in the trash, is skipped unless force is set. Images the file refers to by relative paths can be sent with it; they are added to the note and the body refers to them.")
		Payload(func() {
			Attribute("notebook", String, "Notebook to put the note in (default inbox; not trash)", notebookIDDSL)
			importContent()
			Attribute("modified_at", String, "Modification time of the file, for the times the front matter does not give", timeDSL)
			Attribute("files", ArrayOf(ImportFile), "Images the file refers to")
			Attribute("force", Boolean, "Import even if the same content was imported before")
		})
		Result(ImportResult)
		Error("bad_request", ErrorResult, "The notebook is the trash or invalid, or the file is too large")
		HTTP(func() {
			POST("/import")
			Response(StatusOK)
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

	Method("import_ai_note", func() {
		Description("Import a Markdown file as a new note in a notebook of the collab or drop kind, as create_ai_note does, converting it as people's imports do: the front matter gives the title, tags and times, and its other keys are kept. Images cannot be imported; their links are left as they are. A file whose content was imported before into a notebook visible to AI clients is skipped. The note is marked as created by AI.")
		Payload(func() {
			RequiredAttribute("notebook", String, "ID of a notebook of the collab or drop kind", notebookIDDSL)
			importContent()
		})
		Result(ImportResult)
		Error("bad_request", ErrorResult, "The notebook is invalid, or the file is too large")
		Error("forbidden", ErrorResult, "The notebook is not of the collab or drop kind")
		Tool("import_ai_note", "Import a Markdown file, with its front matter, as a new note in a notebook shared with AI (kind collab) or of the drop kind")
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
		Description("Create a new notebook of the collab kind, where AI clients may write notes. Existing notebooks cannot be turned into collab notebooks by AI clients; people do that. The ID of the notebook is derived from the name: a name with characters other than ASCII gives an ID that starts with xn-- (Punycode), which is expected.")
		Payload(func() {
			RequiredAttribute("name", String, "Display name, in any language; the ID is derived from it", func() {
				MinLength(1)
				MaxLength(100)
			})
		})
		Result(Notebook)
		Error("bad_request", ErrorResult, "The name is empty")
		Error("already_exists", ErrorResult, "A notebook with the name exists")
		Tool("create_ai_notebook", "Create a new notebook shared with AI (kind collab), where you may write notes. Its ID is derived from the name; a name with characters other than ASCII, such as Japanese, gives an ID that starts with xn-- (Punycode), which is expected: pass the id of the result to other tools as is, and call the notebook by its name to people")
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

	// The files of a note, such as images, are kept next to the note file in
	// notes/<notebook>/<id>.assets/ and move with the note.
	Method("add_file", func() {
		Description("Add an image to a note. The request body is the file: a PNG, JPEG, GIF or WebP image of at most 10 MB, told by its content. The name is cleaned up (letters, digits, - and _), gets the extension of the type, and a suffix such as -2 when it is taken. Refer to the file from the body with the returned path.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			RequiredAttribute("name", String, "File name, such as the name of the uploaded file", func() {
				MinLength(1)
				MaxLength(255)
				Example("Screen Shot.png")
			})
		})
		Result(NoteFile)
		Error("not_found", ErrorResult, "The note does not exist")
		Error("bad_request", ErrorResult, "The file is not a supported image, or the note ID is used in more than one notebook")
		Error("too_large", ErrorResult, "The file is larger than 10 MB")
		HTTP(func() {
			POST("/notes/{id}/files")
			Param("name")
			SkipRequestBodyEncodeDecode()
			Response(StatusCreated)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
			Response("too_large", StatusRequestEntityTooLarge)
		})
	})

	Method("get_file", func() {
		Description("Get a file of a note. The response body is the file. Only files of the accepted image types are served, told by their content.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			RequiredAttribute("name", String, "File name", func() {
				Example("screenshot.png")
			})
		})
		Result(func() {
			RequiredAttribute("content_type", String, "Content type of the file", func() {
				Example("image/png")
			})
			RequiredAttribute("length", Int64, "Size of the file in bytes")
		})
		Error("not_found", ErrorResult, "The note or the file does not exist, or the file is not of an accepted type")
		Error("bad_request", ErrorResult, "The note ID is used in more than one notebook")
		HTTP(func() {
			GET("/notes/{id}/files/{name}")
			SkipResponseBodyEncodeDecode()
			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("length:Content-Length")
			})
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("delete_file", func() {
		Description("Delete a file of a note. The body may still refer to it.")
		Payload(func() {
			RequiredAttribute("id", String, "Note ID", noteIDDSL)
			RequiredAttribute("name", String, "File name", func() {
				Example("screenshot.png")
			})
		})
		Error("not_found", ErrorResult, "The note or the file does not exist")
		Error("bad_request", ErrorResult, "The note ID is used in more than one notebook")
		HTTP(func() {
			DELETE("/notes/{id}/files/{name}")
			Response(StatusNoContent)
			Response("not_found", StatusNotFound)
			Response("bad_request", StatusBadRequest)
		})
	})

	Method("check_files", func() {
		Description("Check the files of notes against the note bodies: list the files that no body refers to, which can be deleted with delete_file, and the references to files that do not exist. A reference is <id>.assets/<name> anywhere in a body, including in code, so that a file counts as used rather than not. Files added within the last hour are marked recent, as the note may still be being edited.")
		Result(FileCheck)
		HTTP(func() {
			GET("/files/check")
		})
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

	Method("git_status", func() {
		Description("Tell whether the notes are in a git repository, and its upstream branch, such as to offer git_sync. For people only; others are told there is none.")
		Result(GitStatus)
		HTTP(func() {
			GET("/git/status")
		})
	})

	Method("git_sync", func() {
		Description("Sync the notes with their git repository: commit the changes of the notes directory, then, when the branch has an upstream branch, fetch it, merge it and push. A merge never stops in conflict: a note changed on both sides keeps this side's version, and the other side's version becomes a new note in the same notebook, with the conflict in its title; another file keeps this side's version; a file deleted on one side and changed on the other keeps the changed version. The index is updated afterwards. For people only.")
		Payload(func() {
			Attribute("remote", String, "URL of a remote repository to connect the notes to first, as the remote origin: the notes directory becomes a repository unless it is one, and the notes are pushed, setting the upstream branch; the notes already on the remote, such as from another machine, are merged first", func() {
				Example("git@github.com:you/notes.git")
			})
		})
		Result(GitSyncResult)
		Error("forbidden", ErrorResult, "The caller is not a person")
		Error("bad_request", ErrorResult, "The notes are not in a git repository, the repository is in the middle of an operation such as a merge, a file outside the notes changed on both sides, the remote origin is another repository, or a git command failed, such as a push")
		HTTP(func() {
			POST("/git/sync")
			Body(func() {
				Attribute("remote")
			})
			Response("forbidden", StatusForbidden)
			Response("bad_request", StatusBadRequest)
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
