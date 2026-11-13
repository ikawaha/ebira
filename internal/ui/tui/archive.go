package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// The archive keeps notes and notebooks no longer in use out of the lists and
// searches. The notebook pane has a row of it above the trash, followed by
// the notebooks there, as the trash is by the notebooks in it: the archive
// lists the notes put there on their own, and a notebook there all its notes.

// archivePrefix marks the archive in the notebook filter and the rows of the
// notebook pane: alone, the archive; followed by an ID, a notebook in it.
// Notebook IDs cannot start with it.
const archivePrefix = "@"

// archivedID returns the notebook in the archive that filter names.
func archivedID(filter string) (string, bool) {
	id, ok := strings.CutPrefix(filter, archivePrefix)
	return id, ok && id != ""
}

// inArchive reports whether filter is the archive or a notebook in it.
func inArchive(filter string) bool { return strings.HasPrefix(filter, archivePrefix) }

// archived reports whether notebook nb is in the archive, put there or with
// its parent.
func archived(nb *gennotes.Notebook) bool { return nb.Archived != nil && *nb.Archived }

// splitArchived returns the notebooks nbs but those in the archive, and those
// in the archive, which have rows of their own under the archive (see
// archiveRows).
func splitArchived(nbs []*gennotes.Notebook) (rest, archive []*gennotes.Notebook) {
	for _, nb := range nbs {
		if archived(nb) {
			archive = append(archive, nb)
		} else {
			rest = append(rest, nb)
		}
	}
	return rest, archive
}

// archivedNotebook returns notebook id in the archive, or nil.
func (m Model) archivedNotebook(id string) *gennotes.Notebook {
	for _, nb := range m.archived {
		if nb.ID == id {
			return nb
		}
	}
	return nil
}

// archiveCount is the number of notes in the archive: those put there on
// their own and those of the notebooks there.
func (m *Model) archiveCount() int {
	n := 0
	for _, nb := range m.notebooks {
		n += nb.ArchivedCount
	}
	for _, nb := range m.archived {
		n += nb.Count + nb.ArchivedCount
	}
	return n
}

// archiveRows returns the rows of the archive and of the notebooks there, or
// none when the archive is empty.
func (m *Model) archiveRows() []notebookRow {
	count := m.archiveCount()
	if count == 0 && len(m.archived) == 0 {
		return nil
	}
	rows := []notebookRow{{id: archivePrefix, count: count}}
	for _, nb := range m.archived {
		rows = append(rows, notebookRow{id: archivePrefix + nb.ID, count: nb.Count + nb.ArchivedCount, kind: nb.Kind})
	}
	return rows
}

// archiveParam returns the archived parameter of a list or a search of the
// notebook filter: only the archive in the archive, all the notes of a
// notebook there, and the default, which leaves the archive out, otherwise.
func archiveParam(filter string) string {
	switch {
	case filter == archivePrefix:
		return "only"
	case inArchive(filter):
		return "include"
	}
	return "exclude"
}

// archivedMsg reports putting notes or a notebook in the archive or taking
// them out; show is the notebook filter to show afterwards, "" to stay.
type archivedMsg struct {
	on   bool
	show string
	note bool
	err  error
}

// toggleNoteArchive puts the note in the preview in the archive, or takes it
// out. A note in a notebook in the archive goes in and out with it, and one
// in the inbox goes to a notebook instead.
func (m *Model) toggleNoteArchive() tea.Cmd {
	n := m.note
	switch {
	case n == nil:
		return nil
	case n.Notebook == trashNotebook:
		m.setStatus(statusWarn, "status.archiveTrash")
		return nil
	case n.Notebook == inboxNotebook:
		// The inbox holds no notes in the archive: its notes go to notebooks.
		m.setStatus(statusWarn, "status.archiveInbox")
		return nil
	case m.archivedNotebook(n.Notebook) != nil:
		m.setStatus(statusWarn, "status.archivedWithNotebook")
		return nil
	}
	on := n.ArchivedAt == nil
	ctx, api, id := m.ctx, m.api, n.ID
	return func() tea.Msg {
		err := api.ArchiveNotes(ctx, &gennotes.ArchiveNotesPayload{Ids: []string{id}, Archived: on})
		return archivedMsg{on: on, note: true, err: err}
	}
}

// toggleNotebookArchive puts the notebook under the cursor of the notebook
// pane in the archive, or takes it out of there. A sub notebook of a notebook
// in the archive goes in and out with its parent.
func (m *Model) toggleNotebookArchive() tea.Cmd {
	row := m.selectedRow()
	var nb *gennotes.Notebook
	on := true
	if id, ok := archivedID(row.id); ok {
		nb, on = m.archivedNotebook(id), false
		if nb != nil && nb.ArchivedAt == nil {
			m.setStatus(statusWarn, "status.archivedWithParent")
			return nil
		}
	} else if row.id != archivePrefix {
		nb = m.selectedNotebook()
	}
	if nb == nil || nb.Reserved {
		if _, tag := tagOf(row.id); !tag && row.id != "" && row.id != archivePrefix {
			m.setStatus(statusWarn, "status.archiveReserved")
		}
		return nil
	}
	ctx, api, id := m.ctx, m.api, nb.ID
	show := archivePrefix + id
	if !on {
		show = id
	}
	return func() tea.Msg {
		_, err := api.SaveNotebook(ctx, &gennotes.SaveNotebookPayload{ID: id, Archived: &on})
		return archivedMsg{on: on, show: show, err: err}
	}
}

// finishArchive shows the result of archivedMsg.
func (m *Model) finishArchive(msg archivedMsg) tea.Cmd {
	if msg.err != nil {
		m.setStatus(statusError, "status.archiveFailed", "err", m.errText(msg.err))
		return nil
	}
	switch {
	case msg.note && msg.on:
		m.setStatus(statusInfo, "status.noteArchived")
	case msg.note:
		m.setStatus(statusInfo, "status.noteUnarchived")
	case msg.on:
		m.setStatus(statusInfo, "status.notebookArchived")
	default:
		m.setStatus(statusInfo, "status.notebookUnarchived")
	}
	if msg.show != "" {
		m.notebook, m.sel, m.top = msg.show, 0, 0
	}
	cmds := []tea.Cmd{m.loadNotebooks(), m.loadList()}
	if m.note != nil {
		cmds = append(cmds, m.loadNote(m.note.ID))
	}
	return tea.Batch(cmds...)
}
