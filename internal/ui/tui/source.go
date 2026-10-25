package tui

import (
	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// The source mode shows the Markdown of a note in the document pane instead
// of rendering it, and edits it there without a key to start: while the pane
// has the focus, the keys go to the body. The changes are saved when the
// focus leaves the pane, another note is selected, or with ctrl+s. p in the
// list, or ctrl+r in the pane, renders the body as it is for a while (peek),
// unsaved changes included, without switching the mode.

// SourcePreview returns m showing notes in the source mode when on is true.
// save, if not nil, keeps the mode when the user switches it (v).
func (m Model) SourcePreview(on bool, save func(source bool) error) Model {
	m.source, m.saveSource = on, save
	return m
}

// toggleSource switches between the rendered and the source preview.
func (m *Model) toggleSource() tea.Cmd {
	var cmd tea.Cmd
	if m.src != nil && m.src.dirty() && m.src.saving == "" {
		cmd = m.saveSourceEditor()
	}
	m.source, m.peek = !m.source, false
	m.rendered = ""
	m.renderPreview(true)
	key := "status.previewRendered"
	if m.source {
		key = "status.previewSource"
	}
	m.setStatus(statusInfo, key)
	if m.saveSource != nil {
		if err := m.saveSource(m.source); err != nil {
			m.setStatus(statusWarn, "status.previewNotKept", "err", err)
		}
	}
	return cmd
}

// sourceEditable reports whether the note can be edited in the source mode:
// the notes in the trash are read only, as is the answer of the assistant.
func (m *Model) sourceEditable(n *gennotes.Note) bool {
	return n != nil && n.Notebook != trashNotebook && !m.answerOn
}

// hasSource reports whether the source editor holds the note of the pane.
func (m *Model) hasSource() bool {
	return m.source && m.src != nil && m.note != nil && m.src.id == m.note.ID && !m.answerOn
}

// showsSource reports whether the pane shows the source editor.
func (m *Model) showsSource() bool { return m.hasSource() && !m.peek }

// peeking reports whether the pane renders the body of the source editor.
func (m *Model) peeking() bool { return m.hasSource() && m.peek }

// togglePeek renders the body of the source editor, or goes back to it.
func (m *Model) togglePeek() {
	if !m.hasSource() {
		return
	}
	m.peek = !m.peek
	m.rendered = ""
	m.renderPreview(false)
}

// handlePeekKey handles a key while the pane renders the body of the source
// editor: it can be read, and the other keys do nothing, so that a letter
// typed by mistake changes nothing.
func (m *Model) handlePeekKey(key string) tea.Cmd {
	switch key {
	case "p", "ctrl+r", "esc":
		m.togglePeek()
	case "z", "ctrl+l":
		m.toggleZoom()
	case "f":
		return m.openFind()
	case "ctrl+s":
		if m.src.dirty() && m.src.saving == "" {
			return m.saveSourceEditor()
		}
	case "j", "down":
		m.vp.ScrollDown(1)
	case "k", "up":
		m.vp.ScrollUp(1)
	case "space", "pgdown":
		m.vp.PageDown()
	case "b", "pgup":
		m.vp.PageUp()
	case "g", "home", "<":
		m.vp.GotoTop()
	case "G", "end", ">":
		m.vp.GotoBottom()
	case "ctrl+n", "ctrl+p":
		m.nextMatch(key == "ctrl+n")
	}
	return nil
}

// syncSource keeps the source editor on the note of the preview, after any
// message: it saves the changes once the focus or the note leaves it, follows
// the changes made outside while it has none, and has the cursor shown only
// while the preview has the focus.
func (m *Model) syncSource() tea.Cmd {
	var cmds []tea.Cmd
	e := m.src
	if e != nil && m.prompt == promptNone && e.saving == "" && e.dirty() &&
		(m.focus != focusPreview || m.note == nil || m.note.ID != e.id || !m.source) {
		cmds = append(cmds, m.saveSourceEditor())
	}
	if m.peek && (m.src == nil || m.note == nil || m.src.id != m.note.ID) {
		m.peek = false // another note opens in the source
	}
	switch n := m.note; {
	case !m.source || !m.sourceEditable(n):
		if e != nil && !e.busy() {
			m.src = nil
		}
	case e == nil || e.id != n.ID && (e.saving != "" || !e.dirty()):
		m.src = newSourceEditor(m.th, m.tr, n, nil)
		m.layout()
	case e.id == n.ID && e.version != n.Version && !e.busy():
		m.src = newSourceEditor(m.th, m.tr, n, e)
		m.layout()
	}
	if e := m.src; e != nil {
		// The cursor shows the current match of the find field too.
		want := m.focus == focusPreview && m.showsSource() && (m.prompt == promptNone || m.prompt == promptFind)
		if want && !e.focused() {
			cmds = append(cmds, e.focus(e.field))
		} else if !want && e.focused() {
			e.blur()
		}
	}
	return tea.Batch(cmds...)
}

// saveSourceEditor saves the body of the source editor.
func (m *Model) saveSourceEditor() tea.Cmd {
	e := m.src
	d, err := e.draft()
	if err != nil {
		m.retry(d, m.errText(err))
		return nil
	}
	d.source = true
	e.saving = e.snapshot()
	return m.save(d, false)
}

// finishSourceSave takes the result of a save of the source editor.
func (m *Model) finishSourceSave(msg savedMsg) {
	e := m.src
	if e == nil || e.id != msg.d.id {
		return // the editor moved on to another note
	}
	saving := e.saving
	e.saving = ""
	if msg.err != nil {
		return
	}
	e.version, e.orig = msg.note.Version, saving
	if m.note != nil && m.note.ID == msg.note.ID {
		// The editor follows the note of the preview, which must not take it
		// back to the version before the save.
		m.note = msg.note
	}
}

// editSourceAgain resumes editing a draft of the source editor that could not
// be saved. When the editor has moved on to another note, the draft opens in
// the inline editor instead.
func (m *Model) editSourceAgain(d *draft) tea.Cmd {
	if e := m.src; e != nil && e.id == d.id && m.note != nil && m.note.ID == d.id {
		m.focus = focusPreview
		m.layout()
		return nil
	}
	m.edit = newEditor(m.th, m.tr, &gennotes.Note{ID: d.id, Version: d.version, Notebook: d.notebook, Title: d.title, Tags: d.tags, Body: d.body}, d.notebook)
	m.edit.orig = "" // unsaved
	m.focus = focusEdit
	m.layout()
	return m.edit.focus(fieldBody)
}

// discardSource gives up the changes of the source editor that could not be
// saved, reading the note again.
func (m *Model) discardSource(d *draft) tea.Cmd {
	if e := m.src; e != nil && e.id == d.id {
		m.src = nil
	}
	m.setStatus(statusInfo, "status.discarded")
	return m.loadNote(d.id)
}

// handleSourceKey handles a key while the preview edits the source: the keys
// other than those that leave the preview or save go to the body.
func (m *Model) handleSourceKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+s":
		if m.src.dirty() && m.src.saving == "" {
			return m.saveSourceEditor()
		}
		return nil
	case "ctrl+r":
		m.togglePeek()
		return nil
	case "ctrl+l":
		m.toggleZoom()
		return nil
	case "ctrl+f":
		return m.openFind()
	case "esc":
		if m.zoom {
			m.toggleZoom() // as in the rendered pane, esc ends the zoom first
			return nil
		}
		m.focus = focusList
		m.layout()
		return nil
	}
	return m.src.update(msg)
}

// sourceHint is the key hint of v, which switches to the other preview.
func sourceHint(source bool) string {
	if source {
		return "key.rendered"
	}
	return "key.source"
}
