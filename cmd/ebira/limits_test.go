package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goahttp "goa.design/goa/v3/http"

	notessvr "github.com/ikawaha/ebira/gen/http/notes/server"
	mcpnotessvr "github.com/ikawaha/ebira/gen/jsonrpc/mcp_notes/server"
	mcpnotes "github.com/ikawaha/ebira/gen/mcp_notes"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/importer"
	"github.com/ikawaha/ebira/internal/service"
)

func TestLimitBodies(t *testing.T) {
	read := limitBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		}
	}))
	const form = "multipart/form-data; boundary=x"
	for _, tc := range []struct {
		method, path, contentType string
		size                      int
		ok                        bool
	}{
		{"PATCH", "/notes/a", "", 1 << 20, true},
		{"PATCH", "/notes/a", "", maxBody + 1, false},
		{"POST", "/mcp", "", maxBody, true},
		{"POST", "/mcp", "", maxBody + 1, false},
		{"POST", "/import", "application/json", maxBody + 1, true},
		{"POST", "/import", "application/json", maxImportBody + 1, false},
		{"POST", "/import", form, maxImportBody + 1, true},
		{"POST", "/notes/a/files", "", maxFileBody + 1, false},
		{"POST", "/notes/a/files", "", maxFileBody, true},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(strings.Repeat("a", tc.size)))
		if tc.contentType != "" {
			r.Header.Set("Content-Type", tc.contentType)
		}
		w := httptest.NewRecorder()
		read.ServeHTTP(w, r)
		if ok := w.Code == http.StatusOK; ok != tc.ok {
			t.Errorf("%s %s (%s) of %d bytes: %d, want ok %v", tc.method, tc.path, tc.contentType, tc.size, w.Code, tc.ok)
		}
	}
}

// The largest import by AI clients fits, even with its Markdown escaped in
// JSON as Go escapes it.
func TestLimitFitsAIImport(t *testing.T) {
	images := make([]map[string]string, 4)
	for i := range images {
		images[i] = map[string]string{"name": "image.png", "data": base64.StdEncoding.EncodeToString(make([]byte, 5<<20))}
	}
	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "import_ai_note", "arguments": map[string]any{
			"name": "note.md", "content": strings.Repeat("<", importer.MaxSize), "images": images,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxBody {
		t.Errorf("an import of %d bytes is past the limit of %d", len(b), maxBody)
	}
}

// The MCP server answers a body past the limit with 413, not an empty 200.
func TestMCPTooLarge(t *testing.T) {
	mux := goahttp.NewMuxer()
	endpoints := mcpnotes.NewEndpoints(mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(&service.Notes{}), nil))
	mcpnotessvr.Mount(mux, mcpnotessvr.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, tooLarge))
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(strings.Repeat(" ", maxBody+1)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	limitBodies(mux).ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}

// A search query past its length is refused before it reaches the index.
func TestSearchQueryLimit(t *testing.T) {
	mux := goahttp.NewMuxer()
	notessvr.Mount(mux, notessvr.New(gennotes.NewEndpoints(service.REST{Notes: &service.Notes{}}), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	for _, path := range []string{"/search?q=", "/notes/a?q="} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path+strings.Repeat("a", 1001), nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s with a long query: status = %d, want 400", path, w.Code)
		}
	}
}
