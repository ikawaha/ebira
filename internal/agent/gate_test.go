package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"goa.design/goa-ai/runtime/agent/model"
	goa "goa.design/goa/v3/pkg"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	"github.com/ikawaha/ebira/internal/store"
)

func checkSession(session string) (valid, verified bool) {
	return session == "good", true
}

func TestGate(t *testing.T) {
	ctx := context.Background()
	open := Gate{Service: Unavailable{Reason: "off"}}
	if _, err := open.APIKeyAuth(ctx, "", nil); err != nil {
		t.Errorf("without passkeys: %v", err)
	}
	g := Gate{Service: Unavailable{Reason: "off"}, Session: checkSession}
	for _, s := range []string{"", "bad"} {
		_, err := g.APIKeyAuth(ctx, s, nil)
		if se, ok := err.(*goa.ServiceError); !ok || se.Name != "unauthorized" {
			t.Errorf("session %q: err = %v, want unauthorized", s, err)
		}
	}
	if _, err := g.APIKeyAuth(ctx, "good", nil); err != nil {
		t.Errorf("valid session: %v", err)
	}

	// Through the endpoints, as the server mounts them.
	e := genassistant.NewEndpoints(g)
	if _, err := e.Status(ctx, &genassistant.StatusPayload{}); err == nil {
		t.Error("status without a session passed")
	}
	good := "good"
	res, err := e.Status(ctx, &genassistant.StatusPayload{Session: &good})
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(*genassistant.StatusResult); r.Available {
		t.Errorf("status = %+v", r)
	}
}

// TestGateReadsAsAI checks that a question with a valid session still reads
// the notes as AI: the assistant must not send private notes to the model.
func TestGateReadsAsAI(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, func(st *store.Store) error {
		if err := st.SaveNotebookMeta("secret", store.NotebookMeta{Kind: store.KindPrivate}); err != nil {
			return err
		}
		now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
		return st.Create(&store.Note{ID: "hidden", Notebook: "secret", Title: "secret", Body: "the private body", CreatedAt: now, UpdatedAt: now})
	})
	m := &scriptedModel{steps: []step{
		{tool: "get", args: `{"id":"hidden"}`},
		{text: "done"},
	}}
	client, err := model.NewClient(m)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, client, svc, Config{Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	good := "good"
	e := genassistant.NewEndpoints(Gate{Service: a, Session: checkSession})
	if _, err := e.Ask(ctx, &genassistant.AskPayload{Session: &good, Question: "what is secret?"}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m.reqs[len(m.reqs)-1].Messages)
	if strings.Contains(string(b), "the private body") {
		t.Errorf("the model got a private note: %s", b)
	}
}
