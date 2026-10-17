package agent

import (
	"context"
	"errors"
	"strings"

	"goa.design/goa-ai/runtime/agent/model"
	"goa.design/goa-ai/runtime/agent/runtime"
	"goa.design/goa-ai/runtime/agent/storage/inmem"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	"github.com/ikawaha/ebira/gen/assistant/agents/assistant"
	assistantnotes "github.com/ikawaha/ebira/gen/assistant/agents/assistant/notes"
	"github.com/ikawaha/ebira/gen/notes"
)

// Assistant answers questions about the notes. It implements the assistant
// service of the API.
type Assistant struct {
	rt *runtime.Runtime
}

// New returns an assistant that calls the model through client and reads the
// notes through svc, in process. Runs are kept in memory.
func New(ctx context.Context, client model.Client, svc notes.Service, cfg Config) (*Assistant, error) {
	rt := runtime.New(inmem.New())
	if err := rt.RegisterModel(ModelID, client); err != nil {
		return nil, err
	}
	if err := assistant.RegisterAssistantAgent(ctx, rt, assistant.AssistantAgentConfig{Planner: NewPlanner(cfg)}); err != nil {
		return nil, err
	}
	e := notes.NewEndpoints(svc)
	c := notes.NewClient(e.List, e.Get, e.Create, e.Update, e.Delete, e.Search, e.Restore, e.Move, e.Notebooks, e.Tags, e.CreateNotebook, e.SaveNotebook, e.DeleteNotebook, e.TrashedNotebooks, e.RestoreNotebook, e.DeleteTrashedNotebook, e.Import, e.CreateAiNote, e.ImportAiNote, e.UpdateAiNote, e.DeleteAiNote, e.CreateAiNotebook, e.MakeNotebookPrivate, e.AddFile, e.GetFile, e.DeleteFile, e.CheckFiles, e.Events, e.Sync)
	exec := assistantnotes.NewAssistantNotesExec(assistantnotes.WithClient(c))
	if err := assistant.RegisterUsedToolsets(ctx, rt, assistant.WithNotesExecutor(exec)); err != nil {
		return nil, err
	}
	return &Assistant{rt: rt}, nil
}

// Ask answers one question, without the history of earlier questions.
func (a *Assistant) Ask(ctx context.Context, p *genassistant.AskPayload) (*genassistant.AskResult, error) {
	out, err := assistant.NewClient(a.rt).OneShotRun(ctx, []*model.Message{{
		Role:  model.ConversationRoleUser,
		Parts: []model.Part{model.TextPart{Text: p.Question}},
	}})
	if err != nil {
		return nil, err
	}
	if out.Final == nil {
		return nil, errors.New("the assistant returned no answer")
	}
	var sb strings.Builder
	for _, p := range out.Final.Parts {
		if t, ok := p.(model.TextPart); ok {
			sb.WriteString(t.Text)
		}
	}
	return &genassistant.AskResult{Answer: sb.String()}, nil
}

// Unavailable is the assistant service when the assistant cannot run; every
// question fails with unavailable and Reason.
type Unavailable struct {
	Reason string
}

func (u Unavailable) Ask(context.Context, *genassistant.AskPayload) (*genassistant.AskResult, error) {
	return nil, genassistant.MakeUnavailable(errors.New(u.Reason))
}
