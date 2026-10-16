package agent

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"
	"testing"

	"goa.design/goa-ai/features/model/vertex"
	"google.golang.org/genai"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
)

// fakeGemini stands in for the genai models service and replays steps in
// order, as Gemini would reply.
type fakeGemini struct {
	mu      sync.Mutex
	steps   []step
	configs []*genai.GenerateContentConfig
	models  []string
}

func (f *fakeGemini) GenerateContent(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	return nil, fmt.Errorf("unexpected GenerateContent call")
}

func (f *fakeGemini) CountTokens(context.Context, string, []*genai.Content, *genai.CountTokensConfig) (*genai.CountTokensResponse, error) {
	return nil, fmt.Errorf("unexpected CountTokens call")
}

func (f *fakeGemini) GenerateContentStream(_ context.Context, modelID string, _ []*genai.Content, config *genai.GenerateContentConfig) iter.Seq2[*genai.GenerateContentResponse, error] {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs = append(f.configs, config)
	f.models = append(f.models, modelID)
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		if len(f.steps) == 0 {
			yield(nil, fmt.Errorf("no more scripted steps"))
			return
		}
		st := f.steps[0]
		f.steps = f.steps[1:]
		part := &genai.Part{Text: st.text}
		if st.tool != "" {
			name := ""
			for _, t := range config.Tools {
				for _, fd := range t.FunctionDeclarations {
					if strings.HasSuffix(fd.Name, st.tool) {
						name = fd.Name
					}
				}
			}
			if name == "" {
				yield(nil, fmt.Errorf("function %q is not declared", st.tool))
				return
			}
			args := map[string]any{}
			if st.tool == "search" {
				args["q"] = st.args
			} else {
				args["id"] = st.args
			}
			part = &genai.Part{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}
		}
		yield(&genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{
				Content:      &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{part}},
				FinishReason: genai.FinishReasonStop,
			}},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5, TotalTokenCount: 15},
		}, nil)
	}
}

// TestAskGemini runs the assistant on the Gemini adapter, which the server uses
// with GEMINI_API_KEY.
func TestAskGemini(t *testing.T) {
	ctx := context.Background()
	f := &fakeGemini{steps: []step{
		{tool: "search", args: "東京 天気"},
		{tool: "get", args: "tokyo"},
		{text: "昨日の東京は雨でした（東京の天気, tokyo）。"},
	}}
	client, err := vertex.New(f, vertex.Options{DefaultModel: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, client, newTestService(t), Config{Model: "gemini-test"})
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
	if len(f.configs) != 3 {
		t.Fatalf("Gemini was called %d times, want 3", len(f.configs))
	}
	if f.models[0] != "gemini-test" {
		t.Errorf("model = %q", f.models[0])
	}
	sys := f.configs[0].SystemInstruction
	if sys == nil || len(sys.Parts) == 0 || !strings.Contains(sys.Parts[0].Text, "ebira") {
		t.Errorf("the system prompt was not sent as the system instruction: %+v", sys)
	}
}
