package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The search box searches, listing the notes found by relevance, or, in the
// filter mode, filters the list shown: its notes matching the query, in its
// order, with its separators, which can still be reordered. tab in the box
// switches the mode, which the settings file keeps (tui_search).

// FilterMode returns m with the search box filtering the list when on is
// true. save, if not nil, keeps the mode when the user switches it (tab).
func (m Model) FilterMode(on bool, save func(filter bool) error) Model {
	m.filter, m.saveFilter = on, save
	m.input.Placeholder = m.searchPlaceholder()
	return m
}

// searching reports whether the list shows the notes found by a search, and
// filtered whether it shows the list filtered by the query.
func (m *Model) searching() bool { return m.query != "" && !m.filter }
func (m *Model) filtered() bool  { return m.query != "" && m.filter }

// searchPlaceholder returns the placeholder of the search box, which tells
// the mode.
func (m *Model) searchPlaceholder() string {
	if m.filter {
		return m.tr.t("filter.placeholder")
	}
	return m.tr.t("search.placeholder")
}

// toggleFilter switches the search box between searching and filtering, and
// lists again with the query typed.
func (m *Model) toggleFilter() tea.Cmd {
	m.filter = !m.filter
	m.input.Placeholder = m.searchPlaceholder()
	key := "status.searchMode"
	if m.filter {
		key = "status.filterMode"
	}
	m.setStatus(statusInfo, key)
	if m.saveFilter != nil {
		if err := m.saveFilter(m.filter); err != nil {
			m.setStatus(statusWarn, "status.searchModeNotKept", "err", err)
		}
	}
	m.typeSeq++
	m.query = strings.TrimSpace(m.input.Value())
	if m.query == "" {
		return nil
	}
	return m.loadList()
}
