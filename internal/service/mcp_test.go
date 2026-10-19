package service

import (
	"context"
	"slices"
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
		"create_ai_note", "create_ai_notebook", "delete_ai_note", "get_note", "import_ai_note",
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
