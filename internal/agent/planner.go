// Package agent runs the assistant agent declared in the design, which answers
// questions about the user's notes by searching and reading them.
package agent

import (
	"context"
	"errors"

	"goa.design/goa-ai/runtime/agent/model"
	"goa.design/goa-ai/runtime/agent/planner"
)

// ModelID is the ID the model client is registered under in the runtime.
const ModelID = "primary"

// systemPrompt tells the model how to use the note tools.
const systemPrompt = `You are the assistant of ebira, a note app. Answer the user's question using their notes.

- Find notes with the search tool. Space-separated terms are combined with AND, so when nothing is found, retry with fewer or different terms (synonyms, other spellings, English or Japanese).
- Read the notes that look relevant with the get tool before answering; search results only show an excerpt.
- Base the answer on the notes. Mention the notes you used by title, as Markdown links to /ui/#note/<ID>, such as [Meeting notes](/ui/#note/abc123); the web UI opens the note from the link.
- If the notes do not contain the answer, say so instead of guessing.
- Answer in the language of the question.`

// Config configures the planner.
type Config struct {
	// Model is the provider's model ID. Empty uses the provider's default model
	// (model.ModelClassDefault), such as the one given to anthropic.NewFromAPIKey.
	Model string
	// MaxTokens caps the output tokens of each model call.
	MaxTokens int
}

const defaultMaxTokens = 4096

// NewPlanner returns a planner that lets the model decide each step: call the
// note tools or answer.
func NewPlanner(cfg Config) planner.Planner {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	return &notesPlanner{cfg: cfg}
}

type notesPlanner struct {
	cfg Config
}

func (p *notesPlanner) PlanStart(ctx context.Context, in *planner.PlanInput) (*planner.PlanResult, error) {
	return p.plan(ctx, in.Agent, in.Messages, false)
}

// PlanResume continues after tool results. When the runtime asks for a final
// answer (the tool call cap or the time budget is used up), tools are no
// longer offered.
func (p *notesPlanner) PlanResume(ctx context.Context, in *planner.PlanResumeInput) (*planner.PlanResult, error) {
	return p.plan(ctx, in.Agent, in.Messages, in.SynthesisOnly || in.Finalize != nil)
}

func (p *notesPlanner) plan(ctx context.Context, ag planner.PlannerContext, messages []*model.Message, final bool) (*planner.PlanResult, error) {
	client, ok := ag.PlannerModelClient(ModelID)
	if !ok {
		return nil, errors.New("model " + ModelID + " is not registered")
	}
	// The saved conversation has no system message; add it to every request.
	msgs := make([]*model.Message, 0, len(messages)+1)
	msgs = append(msgs, &model.Message{
		Role:  model.ConversationRoleSystem,
		Parts: []model.Part{model.TextPart{Text: systemPrompt}},
	})
	msgs = append(msgs, messages...)
	req := &model.Request{
		Model:     p.cfg.Model,
		Messages:  msgs,
		Tools:     ag.AdvertisedToolDefinitions(),
		MaxTokens: p.cfg.MaxTokens,
		Stream:    true,
	}
	if req.Model == "" {
		req.ModelClass = model.ModelClassDefault
	}
	if final {
		req.ToolChoice = &model.ToolChoice{Mode: model.ToolChoiceModeNone}
	}
	sum, err := client.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(sum.ToolCalls) > 0 {
		if final {
			return nil, errors.New("the model called a tool when a final answer was required")
		}
		return &planner.PlanResult{ToolCalls: sum.ToolCalls}, nil
	}
	res := sum.FinalResponse()
	if res == nil {
		return nil, errors.New("the model returned neither an answer nor a tool call")
	}
	return &planner.PlanResult{FinalResponse: res}, nil
}
