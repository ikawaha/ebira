package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// tab in the search box switches between searching and filtering, keeps the
// mode, and a filtered list can still be reordered.
func TestFilterMode(t *testing.T) {
	var kept []bool
	m := New(context.Background(), &Clients{}, false, "en").FilterMode(false, func(filter bool) error {
		kept = append(kept, filter)
		return nil
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	tm, _ = m.Update(notebooksMsg{notebooks: []*gennotes.Notebook{
		{ID: inboxNotebook, Name: "Inbox", Kind: "default", Sort: "order", Reserved: true},
		{ID: "work", Name: "Work", Kind: "default", Sort: "order"},
	}})
	m = tm.(Model)
	m.notebook = "work"
	m.query = "-tag:renovate"
	if m.canReorder() {
		t.Error("a search can be reordered")
	}

	m.focus = focusSearch
	m.input.SetValue("-tag:renovate")
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if !m.filter || !m.filtered() || len(kept) != 1 || !kept[0] {
		t.Fatalf("tab did not turn the filter on: filter %v, kept %v", m.filter, kept)
	}
	if !m.canReorder() {
		t.Error("a filtered notebook in the manual order cannot be reordered")
	}
	if v := ansi.Strip(m.renderHeader()); !strings.Contains(v, "filtered") {
		t.Errorf("the header does not tell the filter:\n%s", v)
	}
	if v := ansi.Strip(m.renderList(60, 10)); !strings.Contains(v, "Filter") {
		t.Errorf("the list does not tell the filter:\n%s", v)
	}
	if !strings.Contains(m.input.Placeholder, "filter") {
		t.Errorf("placeholder = %q", m.input.Placeholder)
	}

	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.filter || len(kept) != 2 || kept[1] {
		t.Errorf("tab did not turn the filter off: filter %v, kept %v", m.filter, kept)
	}
}

// A filter lists the notebook with the query as q, and a search searches.
func TestFilterList(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+"?q="+r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":0,"notes":[]}`))
	}))
	defer srv.Close()
	api, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), api, false, "en").FilterMode(true, nil)
	m.notebook = "work"
	m.query = "-tag:renovate"
	m.loadList()()
	m.filter = false
	m.loadList()()
	if len(got) != 2 || got[0] != "/notes?q=-tag:renovate" || got[1] != "/search?q=-tag:renovate" {
		t.Errorf("requests = %q", got)
	}
}
