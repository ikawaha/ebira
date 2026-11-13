package tui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// s in the notebook pane makes a sub notebook in the top notebook selected,
// and refuses the others.
func TestAddSubNotebook(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = io.Copy(b, r.Body)
		body = b.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"work/s","name":"S","kind":"default","sort":"order","count":0,"order":10,"parent":"work"}`))
	}))
	defer srv.Close()
	api, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), api, false, "en")
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = tm.(Model)
	tm, _ = m.Update(notebooksMsg{notebooks: []*gennotes.Notebook{
		{ID: inboxNotebook, Name: "Inbox", Kind: "drop", Sort: "order", Reserved: true},
		{ID: "work", Name: "Work", Kind: "collab", Sort: "order"},
		{ID: "work/old", Name: "Old", Kind: "collab", Sort: "order", Parent: ptr("work")},
		{ID: trashNotebook, Name: "Trash", Sort: "order", Reserved: true},
	}})
	m = tm.(Model)
	m.focus = focusNotebooks
	at := func(id string) int {
		return slices.IndexFunc(m.notebookRows(), func(r notebookRow) bool { return r.id == id })
	}
	for _, id := range []string{inboxNotebook, "work/old", trashNotebook} {
		m.nbSel = at(id)
		m, _ = press(t, m, text("s"))
		if m.prompt != promptNone || m.status != m.tr.t("status.subNotebookWhere") {
			t.Errorf("s on %s: prompt %v, status %q", id, m.prompt, m.status)
		}
	}
	m.nbSel = at("work")
	m, _ = press(t, m, text("s"))
	if m.prompt != promptAddNotebook || m.addParent != "work" {
		t.Fatalf("s on work: prompt %v, parent %q", m.prompt, m.addParent)
	}
	if v := m.renderFooter(); !strings.Contains(v, "New sub notebook in Work") {
		t.Errorf("the prompt does not name the parent:\n%s", v)
	}
	m.promptIn.SetValue("S")
	m, cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter does not create the sub notebook")
	}
	cmd()
	if !strings.Contains(body, `"parent":"work"`) {
		t.Errorf("request = %s, want the parent", body)
	}
}
