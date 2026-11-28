package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	gennotes "github.com/ikawaha/ebira/gen/notes"
)

// madeAssistant is an agent made with a key, which tells it.
type madeAssistant struct{ provider, key string }

func (m madeAssistant) Status(context.Context, *genassistant.StatusPayload) (*genassistant.StatusResult, error) {
	return &genassistant.StatusResult{Available: true}, nil
}

func (m madeAssistant) Ask(context.Context, *genassistant.AskPayload) (*genassistant.AskResult, error) {
	return &genassistant.AskResult{Answer: m.provider + " " + m.key}, nil
}

func TestLazyAssistant(t *testing.T) {
	ctx := context.Background()
	commands := map[string]string{}
	runs := 0
	fail := errors.New("not signed in")
	var runErr error
	l := &lazyAssistant{
		ctx: ctx, env: map[string]string{},
		commands: func() (map[string]string, error) { return commands, nil },
		runKey: func(_ context.Context, cmd string) (string, error) {
			runs++
			if runErr != nil {
				return "", runErr
			}
			return "key of " + cmd, nil
		},
		build: func(_ context.Context, _ gennotes.Service, p, _, key string) (genassistant.Service, error) {
			return madeAssistant{p, key}, nil
		},
	}
	status := func() *genassistant.StatusResult {
		t.Helper()
		s, err := l.Status(ctx, &genassistant.StatusPayload{})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	answer := func() string {
		t.Helper()
		a, err := l.Ask(ctx, &genassistant.AskPayload{Question: "q"})
		if err != nil {
			t.Fatal(err)
		}
		return a.Answer
	}
	// Without a key nor a command, it is not configured.
	if s := status(); s.Available || !strings.Contains(*s.Reason, "not configured") {
		t.Errorf("without keys: %+v", s)
	}
	// A command saved applies at once; one failing tells why, and is tried
	// again.
	commands["gemini"] = "bw get password gemini"
	runErr = fail
	if s := status(); s.Available || !strings.Contains(*s.Reason, "gemini_key_command: not signed in") {
		t.Errorf("failing command: %+v", s)
	}
	runErr = nil
	if s := status(); !s.Available {
		t.Errorf("command: %+v", s)
	}
	if a := answer(); a != "gemini key of bw get password gemini" || runs != 2 {
		t.Errorf("answer = %q, %d runs; the key is read once", a, runs)
	}
	// Saved settings make it read the key again.
	commands["anthropic"] = "op read op://x/y/z"
	l.forget()
	if a := answer(); a != "anthropic key of op read op://x/y/z" {
		t.Errorf("after forget: %q", a)
	}
	// A key of an environment variable wins over the command, and stays.
	l.env["anthropic"] = "sk-env"
	l.forget()
	if a := answer(); a != "anthropic sk-env" {
		t.Errorf("env: %q", a)
	}
	l.fromEnv = true
	l.forget()
	if l.made == nil {
		t.Error("forget dropped the agent of an environment variable")
	}
	// A provider chosen without its key tells what it needs.
	l2 := &lazyAssistant{provider: "openai", env: map[string]string{}, commands: l.commands, runKey: l.runKey, build: l.build}
	if s, _ := l2.Status(ctx, nil); s.Available || !strings.Contains(*s.Reason, "openai_key_command") || !strings.Contains(*s.Reason, "OPENAI_API_KEY") {
		t.Errorf("openai without a key: %+v", s)
	}
}

// TestNewAssistantMistake checks that a mistake in the settings of the
// assistant, as a provider unknown or chosen without its key, as the
// settings of the desktop app may (issue #162), turns it off telling why,
// instead of stopping the server.
func TestNewAssistantMistake(t *testing.T) {
	for _, names := range keyEnvNames {
		for _, n := range names {
			t.Setenv(n, "")
		}
	}
	t.Setenv("EBIRA_CONFIG", t.TempDir()+"/config.yaml")
	for provider, want := range map[string]string{
		"nope":      `unknown model provider "nope"`,
		"anthropic": "ANTHROPIC_API_KEY",
		"gemini":    "GEMINI_API_KEY",
		"openai":    "openai_key_command",
		"":          "not configured",
	} {
		a, _ := newAssistant(context.Background(), nil, provider, "")
		s, err := a.Status(context.Background(), &genassistant.StatusPayload{})
		if err != nil || s.Available || s.Reason == nil || !strings.Contains(*s.Reason, want) {
			t.Errorf("provider %q: status = %+v, %v, want the reason to contain %q", provider, s, err, want)
		}
	}
}
