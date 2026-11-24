package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goahttp "goa.design/goa/v3/http"

	"github.com/ikawaha/ebira/internal/board"
	"github.com/ikawaha/ebira/internal/httpguard"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/service"
	"github.com/ikawaha/ebira/internal/store"
)

// The whiteboards end to end, over HTTP as the server serves them: the REST
// API of the web UI and the MCP server of AI clients, mounted as serve mounts
// them (mountNotes), behind the guard and the limits of the server.

// e2eServer is a test server of the notes service with whiteboards.
type e2eServer struct {
	*httptest.Server
	answers chan string // what the assistant answers, one at a time
}

// newE2EServer starts a server; session checks the sessions of the REST API
// as passkeys do, or nil for auth: none.
func newE2EServer(t *testing.T, session func(string) (bool, bool)) *e2eServer {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(context.Background(), filepath.Join(dir, "index.sqlite"), index.Config{Mode: index.ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	svc := service.New(st, ix)
	t.Cleanup(svc.Close)
	if _, err := svc.Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	boards, err := board.New(filepath.Join(dir, "boards"))
	if err != nil {
		t.Fatal(err)
	}
	s := &e2eServer{answers: make(chan string, 4)}
	ask := func(ctx context.Context, _ string) (string, error) {
		select {
		case a := <-s.answers:
			return a, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if err := svc.SetBoards(boards, ask); err != nil {
		t.Fatal(err)
	}
	mux := goahttp.NewMuxer()
	mountNotes(mux, svc, service.REST{Notes: svc, Session: session})
	s.Server = httptest.NewServer(httpguard.Guard(limitBodies(withBaseURL(mux)), "127.0.0.1"))
	t.Cleanup(s.Close)
	return s
}

// do sends a request and returns the status and the body.
func (s *e2eServer) do(t *testing.T, method, path, body string, header ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// tool calls the MCP tool name with args and returns its structured result.
func (s *e2eServer) tool(t *testing.T, name, args string) map[string]any {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}},"name":"` + name + `","arguments":` + args + `}}`
	code, out := s.do(t, "POST", "/mcp", body,
		"Accept", "application/json, text/event-stream",
		"MCP-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", name)
	if code != http.StatusOK {
		t.Fatalf("%s: %d %s", name, code, out)
	}
	var res struct {
		Result struct {
			IsError           bool           `json:"isError"`
			Content           []any          `json:"content"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%s: %v: %s", name, err, out)
	}
	if res.Error != nil || res.Result.IsError {
		t.Fatalf("%s failed: %s", name, out)
	}
	return res.Result.StructuredContent
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return v
}

type e2eBoard struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Text     string `json:"text"`
	Stickies []struct {
		ID       string `json:"id"`
		State    string `json:"state"`
		Messages []struct {
			Role string `json:"role"`
			Text string `json:"text"`
		} `json:"messages"`
	} `json:"stickies"`
	Result *struct {
		Text   string `json:"text"`
		NoteID string `json:"note_id"`
	} `json:"result"`
}

// events reads the event stream of the server and sends the types of its
// events on the channel returned.
func (s *e2eServer) events(t *testing.T) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 64)
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if typ, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				ch <- typ
			}
		}
	}()
	return ch
}

// waitEvent waits for an event of type typ.
func waitEvent(t *testing.T, ch <-chan string, typ string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-ch:
			if e == typ {
				return
			}
		case <-timeout:
			t.Fatalf("no %s event", typ)
		}
	}
}

func TestBoardsE2E(t *testing.T) {
	s := newE2EServer(t, nil)
	events := s.events(t)
	waitEvent(t, events, "ready")

	// An AI client opens a board by sending the text to an upload URL.
	up := s.tool(t, "start_ai_board", `{"title":"PR #1","format":"diff","session_id":"71f0d3f3-75c3-460e-b7ed-ce852dd93fb3","cwd":"/work"}`)
	url, _ := up["url"].(string)
	if !strings.HasPrefix(url, s.URL+"/boards/uploads/") {
		t.Fatalf("upload URL = %q", url)
	}
	diff := "-old line\n+new line\n"
	code, out := s.do(t, "PUT", strings.TrimPrefix(url, s.URL), diff)
	if code != http.StatusCreated {
		t.Fatalf("upload: %d %s", code, out)
	}
	opened := decode[struct{ ID, URL string }](t, out)
	if opened.URL != s.URL+"/#board/"+opened.ID {
		t.Errorf("board URL = %q", opened.URL)
	}
	waitEvent(t, events, "board")
	// Once only: the used URL is not found, rather than taken for a board.
	if code, _ := s.do(t, "PUT", strings.TrimPrefix(url, s.URL), diff); code != http.StatusNotFound {
		t.Errorf("second upload: %d, want 404", code)
	}

	// The person reads it on the web UI.
	code, out = s.do(t, "GET", "/boards", "")
	if code != http.StatusOK || !strings.Contains(out, opened.ID) {
		t.Fatalf("list: %d %s", code, out)
	}
	code, out = s.do(t, "GET", "/boards/"+opened.ID, "")
	if b := decode[e2eBoard](t, out); code != http.StatusOK || b.Text != diff || b.Status != "open" {
		t.Fatalf("board: %d %s", code, out)
	}
	if res := s.tool(t, "get_ai_board", `{"id":"`+opened.ID+`"}`); res["status"] != "open" || res["result"] != nil {
		t.Errorf("open board for AI = %v", res)
	}

	// A memo, and a question answered in the background, told by an event.
	code, out = s.do(t, "POST", "/boards/"+opened.ID+"/stickies", `{"kind":"memo","anchor":{"exact":"old line"},"text":"why?"}`)
	if code != http.StatusCreated {
		t.Fatalf("memo: %d %s", code, out)
	}
	waitEvent(t, events, "board")
	code, out = s.do(t, "POST", "/boards/"+opened.ID+"/stickies", `{"kind":"assistant","anchor":{"exact":"new line","prefix":"+"},"text":"What changed?"}`)
	if code != http.StatusCreated || !strings.Contains(out, `"state":"waiting"`) {
		t.Fatalf("question: %d %s", code, out)
	}
	waitEvent(t, events, "board") // the sticky note
	s.answers <- "The line was replaced."
	waitEvent(t, events, "board") // the answer
	_, out = s.do(t, "GET", "/boards/"+opened.ID, "")
	b := decode[e2eBoard](t, out)
	if len(b.Stickies) != 2 || b.Stickies[1].State != "idle" || len(b.Stickies[1].Messages) != 2 || b.Stickies[1].Messages[1].Text != "The line was replaced." {
		t.Fatalf("answered board: %s", out)
	}

	// The web UI is of the same origin; another site cannot change boards.
	if code, _ := s.do(t, "POST", "/boards/"+opened.ID+"/stickies", `{"kind":"memo","anchor":{"exact":"x"}}`,
		"Origin", "http://evil.example", "Sec-Fetch-Site", "cross-site"); code != http.StatusForbidden {
		t.Errorf("cross-origin sticky note: %d, want 403", code)
	}

	// The person finishes, saving the board as a note, and the AI client
	// reads what is given back.
	code, out = s.do(t, "POST", "/boards/"+opened.ID+"/finish", `{"text":"Keep the old line.","notebook":"inbox","lang":"ja"}`)
	b = decode[e2eBoard](t, out)
	if code != http.StatusOK || b.Status != "done" || b.Result == nil || b.Result.NoteID == "" {
		t.Fatalf("finish: %d %s", code, out)
	}
	code, out = s.do(t, "GET", "/notes/"+b.Result.NoteID, "")
	if code != http.StatusOK || !strings.Contains(out, "## 付箋") || !strings.Contains(out, "Keep the old line.") {
		t.Errorf("note: %d %s", code, out)
	}
	res := s.tool(t, "get_ai_board", `{"id":"`+opened.ID+`"}`)
	if res["status"] != "done" || res["result"] != "Keep the old line." || res["note_id"] != b.Result.NoteID {
		t.Errorf("finished board for AI = %v", res)
	}

	// From a note, as the button of the preview opens one.
	code, out = s.do(t, "POST", "/boards", `{"note_id":"`+b.Result.NoteID+`"}`)
	if code != http.StatusCreated || !strings.Contains(out, "Keep the old line.") {
		t.Errorf("board from a note: %d %s", code, out)
	}

	if code, _ := s.do(t, "DELETE", "/boards/"+opened.ID, ""); code != http.StatusNoContent {
		t.Errorf("delete: %d", code)
	}
	if code, _ := s.do(t, "GET", "/boards/"+opened.ID, ""); code != http.StatusNotFound {
		t.Errorf("deleted board: %d, want 404", code)
	}
}

// TestBoardsE2EPasskey checks that while passkeys are required, the REST API
// of boards needs a session, and the MCP server and the upload URL do not:
// an AI client opens a board and reads the result without one.
func TestBoardsE2EPasskey(t *testing.T) {
	s := newE2EServer(t, func(string) (bool, bool) { return false, false })
	for _, r := range []struct{ method, path, body string }{
		{"GET", "/boards", ""},
		{"GET", "/boards/abc", ""},
		{"POST", "/boards", `{"note_id":"abc"}`},
		{"POST", "/boards/abc/stickies", `{"kind":"memo","anchor":{"exact":"x"}}`},
		{"POST", "/boards/abc/finish", `{"text":""}`},
	} {
		if code, out := s.do(t, r.method, r.path, r.body); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d %s, want 401", r.method, r.path, code, out)
		}
	}
	up := s.tool(t, "start_ai_board", `{"title":"t","format":"text"}`)
	url, _ := up["url"].(string)
	code, out := s.do(t, "PUT", strings.TrimPrefix(url, s.URL), "text")
	if code != http.StatusCreated {
		t.Fatalf("upload: %d %s", code, out)
	}
	id := decode[struct{ ID string }](t, out).ID
	if res := s.tool(t, "get_ai_board", `{"id":"`+id+`"}`); res["status"] != "open" {
		t.Errorf("board for AI = %v", res)
	}
	opened := s.tool(t, "open_ai_board", `{"title":"t","text":"x"}`)
	if !strings.HasPrefix(opened["url"].(string), s.URL+"/#board/") {
		t.Errorf("open_ai_board = %v", opened)
	}
}
