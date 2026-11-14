---
title: Notes and notebooks
tags: [guide, basics]
---
## Notes

- New: `c`, or "+ New" above the list. Write the title, tags and body, and save with ⌘S (Ctrl+S) or "Save". Esc stops editing.
- Edit: `e`, or the pencil button at the top right of the preview.
- Delete: the trash button of the preview. The note goes to the Trash, from which you can restore it (see "The trash" below).

## Notebooks

A notebook is a folder of notes. Make one with "+" under the side column. With its "…" or a right click, rename it, change its kind, or move it to the trash.

Besides the notebooks, the side column has these rows:

- **All**: all the notes outside the trash and the archive.
- **Inbox**: where notes made without a notebook go. Notes made while viewing All, a tag, the archive or the trash go here, and so does Markdown imported there. It is always at the top. It can be renamed and change its kind, but cannot go to the trash or the archive, nor hold sub notebooks. Its kind starts as drop and cannot be collab ([Kinds of notebooks and AI](kinds.md)).
- **Archive**: not a notebook, but the row to view the notes and notebooks in the archive. It shows only when there is something there (see "Archive" below).
- **Trash**: the trash, always at the bottom (see "The trash" below).

The count of a notebook in the side column takes in the notes of its sub notebooks. The counts of All and of the notebooks leave out the archive and the trash.

A notebook can hold sub notebooks, one level deep ("New sub notebook…" in its menu). A sub notebook shows as a cell at the top of the list, which opens and closes when clicked. "Terminal" of this guide is one. "+ New" makes a note in the sub notebook whose cell was clicked last, or of the note chosen when that note is in a cell, and opens its cell; else in the parent. The sort above the list is the parent's: to sort the notes of a sub notebook, use "Sort" in the menu of its cell, which also has "New note here". Drag a cell onto the upper or lower half of another to put it before or after.

## Moving

- Drag a note onto a notebook in the side column to move it there, onto the archive to put it there, or onto the Trash to delete it. Notebook rows can be dropped the same way.
- Click with ⌘ (Ctrl) to choose more notes, or with Shift for a range, to move them together. A right click on one of them opens a menu whose marks, archive and trash are for all of them.

## Archive

Notes and notebooks no longer in use but worth keeping can go to the archive. What is there is left out of lists, searches and tag counts. The notebooks and the files of the notes stay where they are, and links to the notes still open them.

- Notes: the box button of the preview, the right-click menu of the list, "Put in the archive" for notes chosen together, or drag them onto the archive in the side column. Notes in the Inbox wait to go to notebooks, so they cannot go to the archive; a note in the archive moved to the Inbox leaves the archive.
- Notebooks: "Put in the archive" in the menu of a row. Its notes and sub notebooks go with it. The Inbox and the Trash cannot go there.
- Viewing: "Archive" in the side column. The archive is a mark, so the notes stay in their notebooks: the archive lists the notebooks with notes there as cells, which open and close under them when clicked, as sub notebooks do. A notebook put there as a whole has a box on its cell and holds all its notes; the cell of another notebook holds its notes put there one by one. Sub notebooks are in the cells of their parents. Notes in the archive can be edited, and stay there.
- Viewing with the other notes: the box at the right of the search box (struck out while the archive is hidden, as it is at first) shows the notes in the archive in the lists and searches too, marked with a box and paler. Click it again to hide them. This browser remembers it.
- Taking out: on a note in the archive, the same button shows a struck-out box, which takes it out of the archive. For a notebook, "Take out of the archive" in its menu. The menus of the cells in the archive only open, take out and delete; rename a notebook, change its kind or move it once it is out.
- TUI: `x` puts in and takes out. The notebooks with notes in the archive are listed under "Archive" in the notebook column ([TUI](tui.md)).
- Candidates: "Candidates for the archive…" in the gear menu lists the notes not updated for a while, oldest first, to choose and put there at once (but those in the Inbox).
- Searching: when a search shows "N more in the archive", click it to search the archive. Writing `in:archive` in the search does the same ([How to search](search.md)).

## The trash

- Deleting: for a note, the trash button of the preview, the right-click menu of the list, or a drag onto the Trash. For a notebook, "Move to the trash" in the menu of its row, or a drag onto the Trash; its notes and sub notebooks go with it.
- Viewing: choose the Trash for the notes deleted one by one, under the notebooks deleted, shown as cells that open and close when clicked. A search with the Trash chosen searches the trash.
- Notes in the trash cannot be edited. They can be restored (to their notebook, made again if it is gone), moved to another notebook, or deleted for good. A deleted notebook can be restored or deleted for good from the menu of its cell.
- Emptying: "Empty trash…" in the menu of the Trash row deletes all there for good. What is deleted for good cannot come back.
- Notes in the trash are left out of the other lists, searches and tag counts, and AI cannot see them. They cannot go to the archive.

## Undo

After a move, a delete or a reorder, a bar with "Undo" shows at the bottom. Click it, or press ⌘Z (Ctrl+Z).
