package service

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	goahttp "goa.design/goa/v3/http"

	assistantspecs "github.com/ikawaha/ebira/gen/assistant/agents/assistant/specs"
	mcpnotessvr "github.com/ikawaha/ebira/gen/jsonrpc/mcp_notes/server"
	mcpnotes "github.com/ikawaha/ebira/gen/mcp_notes"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// TestMCPInstructions guards the instructions that the MCP server returns in
// both protocol revisions: server/discover of 2026-07-28 and initialize of
// 2025-11-25. Clients may add them to the system prompt of the model, where the
// model sees them before it loads the tools (github.com/ikawaha/goaaiplugins/mcpinstructions).
func TestMCPInstructions(t *testing.T) {
	mux := goahttp.NewMuxer()
	endpoints := mcpnotes.NewEndpoints(mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(&Notes{}), nil))
	mcpnotessvr.Mount(mux, mcpnotessvr.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, func(_ context.Context, _ http.ResponseWriter, err error) {
		t.Error(err)
	}))
	for _, tc := range []struct {
		name    string
		headers map[string]string
		body    string
		field   string // a field of the result that only this revision has
	}{
		{
			name:    "server/discover",
			headers: map[string]string{"MCP-Protocol-Version": "2026-07-28", "Mcp-Method": "server/discover"},
			body:    `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`,
			field:   "supportedVersions",
		},
		{
			name:  "initialize",
			body:  `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
			field: "protocolVersion",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/mcp", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			var res struct {
				Result map[string]json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("%v: %s", err, w.Body)
			}
			if _, ok := res.Result[tc.field]; !ok {
				t.Errorf("the result has no %s: %s", tc.field, w.Body)
			}
			var instructions string
			if err := json.Unmarshal(res.Result["instructions"], &instructions); err != nil || !strings.Contains(instructions, "search_notes") {
				t.Errorf("instructions = %q (%v), want ones that mention search_notes: %s", instructions, err, w.Body)
			}
		})
	}
}

// TestMCPTools guards the access rules for AI clients: they read notes except
// in private notebooks and the trash, write only notes in notebooks of the collab
// kind (*_ai_note) and their separators (*_ai_separator), may create a new collab notebook (create_ai_notebook) and may
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
		"add_ai_file", "archive_ai_notebook", "create_ai_note", "create_ai_notebook", "create_ai_separator", "delete_ai_file", "delete_ai_note", "delete_ai_separator",
		"get_ai_board", "get_note", "import_ai_note", "list_marks", "list_notebooks", "list_notes", "list_tags", "make_notebook_private",
		"move_ai_note", "move_ai_separator", "open_ai_board", "search_notes", "start_ai_board", "start_ai_upload", "update_ai_note", "update_ai_separator",
	}
	if !slices.Equal(got, want) {
		t.Errorf("MCP tools = %v, want %v", got, want)
	}
}

// TestMCPToolHints checks the hints of the MCP tools: MCP takes a tool without
// them as one that may destroy data and reach outside the notes, which no tool
// does. Those that remove or replace something are destructive even if people
// can bring it back.
func TestMCPToolHints(t *testing.T) {
	adapter := mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(&Notes{}), nil)
	res, err := adapter.ToolsList(context.Background(), &mcpnotes.ToolsListPayload{})
	if err != nil {
		t.Fatal(err)
	}
	destructive := []string{
		"add_ai_file", "delete_ai_file", "delete_ai_note", "delete_ai_separator",
		"start_ai_upload", "update_ai_note", "update_ai_separator",
	}
	for _, tool := range res.Tools {
		a := tool.Annotations
		if a == nil {
			t.Errorf("%s has no hints", tool.Name)
			continue
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s: openWorldHint = %v, want false", tool.Name, a.OpenWorldHint)
		}
		if a.ReadOnlyHint != nil && *a.ReadOnlyHint {
			continue
		}
		if want := slices.Contains(destructive, tool.Name); a.DestructiveHint == nil || *a.DestructiveHint != want {
			t.Errorf("%s: destructiveHint = %v, want %v", tool.Name, a.DestructiveHint, want)
		}
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

// TestMCPListen checks the subscription to tool list changes: the server
// advertises it, acknowledges it and holds the stream open until it closes,
// so that a client lists the tools again after a restart, which may have
// changed them.
func TestMCPListen(t *testing.T) {
	svc, _ := newTestService(t)
	mux := goahttp.NewMuxer()
	endpoints := mcpnotes.NewEndpoints(mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(svc), nil))
	mcpnotessvr.Mount(mux, mcpnotessvr.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, func(_ context.Context, _ http.ResponseWriter, err error) {
		t.Error(err)
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	post := func(method, params string) *http.Response {
		t.Helper()
		body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{` + params + `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
		r, err := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2026-07-28")
		r.Header.Set("Mcp-Method", method)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := post("server/discover", "")
	var discover struct {
		Result struct {
			Capabilities struct {
				Tools struct {
					ListChanged bool `json:"listChanged"`
				} `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	err := json.NewDecoder(res.Body).Decode(&discover)
	res.Body.Close()
	if err != nil || !discover.Result.Capabilities.Tools.ListChanged {
		t.Fatalf("server/discover: tools.listChanged = false (%v), want true", err)
	}

	res = post("subscriptions/listen", `"notifications":{"toolsListChanged":true},`)
	defer res.Body.Close()
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				lines <- data
			}
		}
	}()
	next := func() (string, bool) {
		select {
		case line, ok := <-lines:
			return line, ok
		case <-time.After(5 * time.Second):
			t.Fatal("no event in 5 seconds")
			return "", false
		}
	}
	if line, _ := next(); !strings.Contains(line, "notifications/subscriptions/acknowledged") || !strings.Contains(line, `"toolsListChanged":true`) {
		t.Fatalf("first event = %s, want the acknowledgment of toolsListChanged", line)
	}
	select {
	case line := <-lines:
		t.Fatalf("got %s before the server closed, want the stream held open", line)
	case <-time.After(100 * time.Millisecond):
	}

	svc.Close() // as the server does when it shuts down
	if line, _ := next(); !strings.Contains(line, `"result"`) {
		t.Errorf("event after closing = %s, want the final result", line)
	}
	if line, ok := next(); ok {
		t.Errorf("got %s after the final result, want the stream ended", line)
	}
}
