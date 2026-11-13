package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// The archive keeps notes and notebooks no longer in use out of the lists and
// searches. It is a mark, so its notes stay in their notebooks. The notebook
// pane has a row of it above the trash, followed by the notebooks with notes
// there, sub notebooks under their parents: the archive lists the notes put
// there on their own, a notebook there all its notes, and another notebook
// its notes put there on their own.

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

// anyNotebook returns notebook id, in the archive or not, or nil.
func (m Model) anyNotebook(id string) *gennotes.Notebook {
	if nb := m.notebookOf(id); nb != nil {
		return nb
	}
	return m.archivedNotebook(id)
}

// allNotebooks returns the notebooks, in the archive or not, in their order.
func (m *Model) allNotebooks() []*gennotes.Notebook {
	if m.order == nil {
		return append(slices.Clone(m.notebooks), m.archived...)
	}
	var res []*gennotes.Notebook
	for _, id := range m.order {
		if nb := m.anyNotebook(id); nb != nil {
			res = append(res, nb)
		}
	}
	return res
}

// archivedIn is the number of notes of notebook nb alone in the archive.
func archivedIn(nb *gennotes.Notebook) int {
	if archived(nb) {
		return nb.Count + nb.ArchivedCount
	}
	return nb.ArchivedCount
}

// archiveSubs returns the sub notebooks of notebook id with notes in the
// archive, or all of them for a notebook there.
func (m *Model) archiveSubs(id string) []*gennotes.Notebook {
	parent := m.archivedNotebook(id) != nil
	var res []*gennotes.Notebook
	for _, nb := range m.allNotebooks() {
		if nb.Parent != nil && *nb.Parent == id && (parent || archived(nb) || nb.ArchivedCount > 0) {
			res = append(res, nb)
		}
	}
	return res
}

// archiveCount is the number of notes in the archive: those put there on
// their own and those of the notebooks there.
func (m *Model) archiveCount() int {
	n := 0
	for _, nb := range m.allNotebooks() {
		n += archivedIn(nb)
	}
	return n
}

// archiveRows returns the rows of the archive and of the notebooks with notes
// there, sub notebooks under their parents, or none when the archive is
// empty.
func (m *Model) archiveRows() []notebookRow {
	var rows []notebookRow
	for _, nb := range m.allNotebooks() {
		if nb.Parent != nil || nb.Reserved {
			continue
		}
		subs := m.archiveSubs(nb.ID)
		count := archivedIn(nb)
		for _, s := range subs {
			count += archivedIn(s)
		}
		if !archived(nb) && count == 0 && len(subs) == 0 {
			continue
		}
		rows = append(rows, notebookRow{id: archivePrefix + nb.ID, count: count, kind: nb.Kind})
		for _, s := range subs {
			rows = append(rows, notebookRow{id: archivePrefix + s.ID, count: archivedIn(s), kind: s.Kind})
		}
	}
	count := m.archiveCount()
	if count == 0 && len(rows) == 0 {
		return nil
	}
	return append([]notebookRow{{id: archivePrefix, count: count}}, rows...)
}

// archiveParam returns the archived parameter of a list or a search of the
// notebook filter: only the archive in the archive, all the notes of a
// notebook there, only the notes put in the archive of another notebook under
// the archive, and the default, which leaves the archive out, otherwise.
func (m *Model) archiveParam(filter string) string {
	if filter == archivePrefix {
		return "only"
	}
	if id, ok := archivedID(filter); ok {
		if m.archivedNotebook(id) != nil {
			return "include"
		}
		return "only"
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
		if nb == nil {
			return nil // a notebook with notes in the archive, not there itself
		}
		if nb.ArchivedAt == nil {
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
