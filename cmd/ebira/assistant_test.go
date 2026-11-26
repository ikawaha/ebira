package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ikawaha/ebira/internal/agent"
)

// TestNewAssistantMistake checks that a mistake in the settings of the
// assistant turns it off, telling why, instead of stopping the server.
func TestNewAssistantMistake(t *testing.T) {
	for _, env := range []string{"ANTHROPIC_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(env, "")
	}
	for provider, want := range map[string]string{
		"nope":      `unknown model provider "nope"`,
		"anthropic": "needs ANTHROPIC_API_KEY",
		"gemini":    "needs GEMINI_API_KEY",
		"openai":    "needs OPENAI_API_KEY",
		"":          "not configured",
	} {
		a := newAssistant(context.Background(), nil, provider, "")
		u, ok := a.(agent.Unavailable)
		if !ok {
			t.Errorf("provider %q: got %T, want agent.Unavailable", provider, a)
			continue
		}
		if !strings.Contains(u.Reason, want) {
			t.Errorf("provider %q: reason %q, want it to contain %q", provider, u.Reason, want)
		}
	}
}
