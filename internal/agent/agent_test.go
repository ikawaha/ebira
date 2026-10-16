package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goa.design/goa-ai/runtime/agent/model"
	"goa.design/goa-ai/runtime/agent/tools"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	"github.com/ikawaha/ebira/internal/index"
	"github.com/ikawaha/ebira/internal/service"
	"github.com/ikawaha/ebira/internal/store"
)

// step is one scripted model reply: a tool call, or the final text.
type step struct {
	tool string // suffix of the advertised tool name, such as "search"
	args string
	text string
}

// scriptedModel replays steps in order and records the requests.
type scriptedModel struct {
	mu    sync.Mutex
	steps []step
	reqs  []*model.Request
}

func (m *scriptedModel) Complete(context.Context, *model.Request) (*model.Response, error) {
	return nil, fmt.Errorf("unexpected Complete call")
}

func (m *scriptedModel) Stream(_ context.Context, req *model.Request) (model.Streamer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, req)
	if len(m.steps) == 0 {
		return nil, fmt.Errorf("no more scripted steps")
	}
	st := m.steps[0]
	m.steps = m.steps[1:]
	if st.tool == "" {
		msg := model.Message{Role: model.ConversationRoleAssistant, Parts: []model.Part{model.TextPart{Text: st.text}}}
		return &replay{
			chunks:   []model.Chunk{model.TextChunk{Message: msg}, model.StopChunk{Reason: "end_turn"}},
			response: &model.Response{Content: []model.Message{msg}, StopReason: "end_turn"},
		}, nil
	}
	name := ""
	for _, t := range req.Tools {
		if strings.HasSuffix(t.Name, st.tool) {
			name = t.Name
		}
	}
	if name == "" {
		return nil, fmt.Errorf("tool %q is not advertised", st.tool)
	}
	id := fmt.Sprintf("call-%d", len(m.reqs))
	return &replay{
		chunks: []model.Chunk{
			model.ToolCallChunk{ToolCall: model.ToolCall{Name: tools.Ident(name), ID: id, Payload: []byte(st.args)}},
			model.StopChunk{Reason: "tool_use"},
		},
		response: &model.Response{
			Content: []model.Message{{
				Role:  model.ConversationRoleAssistant,
				Parts: []model.Part{model.ToolUsePart{ID: id, Name: name, Input: []byte(st.args)}},
			}},
			StopReason: "tool_use",
		},
	}, nil
}

type replay struct {
	chunks   []model.Chunk
	response *model.Response
	done     bool
}

func (r *replay) Recv() (model.Chunk, error) {
	if len(r.chunks) == 0 {
		r.done = true
		return nil, io.EOF
	}
	c := r.chunks[0]
	r.chunks = r.chunks[1:]
	return c, nil
}

func (r *replay) Response() *model.Response {
	if !r.done {
		return nil
	}
	return r.response
}

func (r *replay) Close() error { return nil }

func newTestService(t *testing.T) *service.Notes {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if err := st.Create(&store.Note{ID: "tokyo", Notebook: "weather", Title: "東京の天気", Body: "昨日の東京は雨だった。", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(ctx, filepath.Join(dir, "index.sqlite"), index.Config{Mode: index.ModeBigram})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	svc := service.New(st, ix)
	t.Cleanup(svc.Close)
	return svc
}

func TestAsk(t *testing.T) {
	ctx := context.Background()
	m := &scriptedModel{steps: []step{
		{tool: "search", args: `{"q":"東京 天気"}`},
		{tool: "get", args: `{"id":"tokyo"}`},
		{text: "昨日の東京は雨でした（東京の天気, tokyo）。"},
	}}
	client, err := model.NewClient(m)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, client, newTestService(t), Config{Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Ask(ctx, &genassistant.AskPayload{Question: "昨日の東京の天気は？"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res.Answer, "昨日の東京は雨でした（東京の天気, tokyo）。"; got != want {
		t.Errorf("Ask = %q, want %q", got, want)
	}
	if len(m.reqs) != 3 {
		t.Fatalf("model was called %d times, want 3", len(m.reqs))
	}

	first := m.reqs[0]
	if first.Model != "test-model" || first.MaxTokens != defaultMaxTokens || first.ToolChoice != nil {
		t.Errorf("request = model %q, max tokens %d, tool choice %v", first.Model, first.MaxTokens, first.ToolChoice)
	}
	if len(first.Messages) == 0 || first.Messages[0].Role != model.ConversationRoleSystem {
		t.Errorf("the first message is not the system prompt: %+v", first.Messages)
	}
	// The get tool takes only the ID.
	var names []string
	for _, td := range first.Tools {
		names = append(names, td.Name)
		if !strings.HasSuffix(td.Name, "get") {
			continue
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(td.Input.Contract().Schema, &schema); err != nil {
			t.Fatal(err)
		}
		if _, ok := schema.Properties["q"]; ok || len(schema.Properties) != 1 {
			t.Errorf("get tool arguments = %v, want only id", schema.Properties)
		}
	}
	if len(names) != 2 {
		t.Errorf("advertised tools = %v, want search and get", names)
	}

	// The last request carries the results of both tools, read from the service.
	var results strings.Builder
	for _, msg := range m.reqs[2].Messages {
		for _, p := range msg.Parts {
			if tr, ok := p.(model.ToolResultPart); ok {
				b, _ := json.Marshal(tr.Content)
				results.Write(b)
			}
		}
	}
	for _, want := range []string{"tokyo", "昨日の東京は雨だった。"} {
		if !strings.Contains(results.String(), want) {
			t.Errorf("tool results %s do not contain %q", results.String(), want)
		}
	}
}

// TestAskToolCap checks that when the model goes over the tool call cap, the
// planner asks for a final answer without tools. The runtime denies the call
// past the cap and then resumes the planner with Finalize.
func TestAskToolCap(t *testing.T) {
	ctx := context.Background()
	var steps []step
	for range 6 + 1 { // MaxToolCalls in the design, and one more
		steps = append(steps, step{tool: "search", args: `{"q":"東京"}`})
	}
	steps = append(steps, step{text: "わかりませんでした。"})
	m := &scriptedModel{steps: steps}
	client, err := model.NewClient(m)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, client, newTestService(t), Config{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.Ask(ctx, &genassistant.AskPayload{Question: "東京について"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Answer != "わかりませんでした。" {
		t.Errorf("Ask = %q", res.Answer)
	}
	if r := m.reqs[0]; r.Model != "" || r.ModelClass != model.ModelClassDefault {
		t.Errorf("request without a model = model %q, class %q, want the default class", r.Model, r.ModelClass)
	}
	last := m.reqs[len(m.reqs)-1]
	if last.ToolChoice == nil || last.ToolChoice.Mode != model.ToolChoiceModeNone {
		t.Errorf("the final request allows tools: %+v (after %d requests)", last.ToolChoice, len(m.reqs))
	}
}
