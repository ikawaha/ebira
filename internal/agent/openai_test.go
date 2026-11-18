package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"goa.design/goa-ai/features/model/openai"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
)

// fakeOpenAI stands in for the Responses API of OpenAI and replays steps in
// order as streamed events, as OpenAI would reply.
type fakeOpenAI struct {
	mu     sync.Mutex
	steps  []step
	bodies []string
	models []string
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	body, err := io.ReadAll(r.Body)
	if err == nil {
		err = json.Unmarshal(body, &req)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.bodies = append(f.bodies, string(body))
	f.models = append(f.models, req.Model)
	if len(f.steps) == 0 {
		f.mu.Unlock()
		http.Error(w, "no more scripted steps", http.StatusInternalServerError)
		return
	}
	st := f.steps[0]
	f.steps = f.steps[1:]
	f.mu.Unlock()

	var events []map[string]any
	var item map[string]any
	if st.tool != "" {
		name := ""
		for _, t := range req.Tools {
			if strings.HasSuffix(t.Name, st.tool) {
				name = t.Name
			}
		}
		if name == "" {
			http.Error(w, fmt.Sprintf("function %q is not declared", st.tool), http.StatusBadRequest)
			return
		}
		key := "id"
		if st.tool == "search" {
			key = "q"
		}
		args, _ := json.Marshal(map[string]string{key: st.args})
		item = map[string]any{"id": "fc_1", "type": "function_call", "call_id": "call_1", "name": name, "arguments": string(args), "status": "completed"}
		events = []map[string]any{
			{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "fc_1", "type": "function_call", "call_id": "call_1", "name": name, "arguments": "", "status": "in_progress"}},
			{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "output_index": 0, "delta": string(args)},
			{"type": "response.output_item.done", "output_index": 0, "item": item},
		}
	} else {
		item = map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
			"content": []any{map[string]any{"type": "output_text", "text": st.text, "annotations": []any{}, "logprobs": []any{}}}}
		events = []map[string]any{
			{"type": "response.output_text.delta", "item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": st.text, "logprobs": []any{}},
		}
	}
	events = append(events, map[string]any{"type": "response.completed", "response": map[string]any{
		"model":  req.Model,
		"status": "completed",
		"usage":  map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15},
		"output": []any{item},
	}})
	w.Header().Set("Content-Type", "text/event-stream")
	for i, e := range events {
		e["sequence_number"] = i + 1
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e["type"], b)
	}
}

// TestAskOpenAI runs the assistant on the OpenAI adapter, which the server
// uses with OPENAI_API_KEY.
func TestAskOpenAI(t *testing.T) {
	ctx := context.Background()
	f := &fakeOpenAI{steps: []step{
		{tool: "search", args: "東京 天気"},
		{tool: "get", args: "tokyo"},
		{text: "昨日の東京は雨でした（東京の天気, tokyo）。"},
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	sdk := openaisdk.NewClient(option.WithAPIKey("test"), option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	client, err := openai.New(openai.Options{Client: &sdk.Responses, DefaultModel: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, client, newTestService(t), Config{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Ask(ctx, &genassistant.AskPayload{Question: "昨日の東京の天気は？"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "昨日の東京は雨でした（東京の天気, tokyo）。"; res.Answer != want {
		t.Errorf("Ask = %q, want %q", res.Answer, want)
	}
	if len(f.bodies) != 3 {
		t.Fatalf("OpenAI was called %d times, want 3", len(f.bodies))
	}
	if f.models[0] != "gpt-test" {
		t.Errorf("model = %q", f.models[0])
	}
	if !strings.Contains(f.bodies[0], "ebira") {
		t.Errorf("the system prompt was not sent: %s", f.bodies[0])
	}
}
