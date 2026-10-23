package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	assistantspecs "github.com/ikawaha/ebira/gen/assistant/agents/assistant/specs"
	mcpnotes "github.com/ikawaha/ebira/gen/mcp_notes"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// TestMCPTools guards the access rules for AI clients: they read notes except
// in private notebooks and the trash, write only notes in notebooks of the collab
// kind (*_ai_note), may create a new collab notebook (create_ai_notebook) and may
// make a notebook private (make_notebook_private). The methods that set any
// kind (create_notebook, save_notebook) and the create, update, delete and
// import methods for people must never become MCP tools. A new tool fails this test on
// purpose: add it to the list only after checking that it keeps these rules.
func TestMCPTools(t *testing.T) {
	svc, _ := newTestService(t)
	adapter := mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(svc), nil)
	res, err := adapter.ToolsList(context.Background(), &mcpnotes.ToolsListPayload{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
	}
	slices.Sort(got)
	want := []string{
		"add_ai_file", "create_ai_note", "create_ai_notebook", "delete_ai_note", "get_note", "import_ai_note", "list_marks",
		"list_notebooks", "list_notes", "list_tags", "make_notebook_private", "move_ai_note", "search_notes", "update_ai_note",
	}
	if !slices.Equal(got, want) {
		t.Errorf("MCP tools = %v, want %v", got, want)
	}
}

// TestAssistantTools guards the tools of the assistant agent (POST /ask): it
// only searches and reads notes.
func TestAssistantTools(t *testing.T) {
	var got []string
	for _, spec := range assistantspecs.Specs() {
		got = append(got, string(spec.Name))
	}
	slices.Sort(got)
	if want := []string{"notes.get", "notes.search"}; !slices.Equal(got, want) {
		t.Errorf("assistant tools = %v, want %v", got, want)
	}
}

// TestMCPIgnoresSession checks that MCP calls are from AI even with the
// session cookie of a browser, which the MCP adapter passes on to the
// endpoints: they see neither the private notebooks nor their notes.
func TestMCPIgnoresSession(t *testing.T) {
	svc, _ := newTestService(t)
	person := AsPerson(context.Background())
	diary, err := svc.CreateNotebook(person, &gennotes.CreateNotebookPayload{Name: "diary", Kind: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(person, &gennotes.CreatePayload{ID: ptr("secret"), Notebook: &diary.ID, Title: "secret"}); err != nil {
		t.Fatal(err)
	}
	adapter := mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(svc), nil)
	for _, c := range []struct{ tool, args string }{
		{"list_notebooks", `{}`},
		{"get_note", `{"id":"secret"}`},
	} {
		res, err := adapter.ToolsCall(context.Background(), &mcpnotes.ToolsCallPayload{Name: c.tool, Arguments: json.RawMessage(c.args), HTTPCredential0: ptr("a session")})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), diary.ID) || strings.Contains(string(b), `"secret"`) {
			t.Errorf("%s with a session reveals the private notebook: %s", c.tool, b)
		}
	}
}
