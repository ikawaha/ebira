package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"goa.design/goa-ai/features/model/anthropic"
	"goa.design/goa-ai/features/model/openai"
	"goa.design/goa-ai/features/model/vertex"
	"goa.design/goa-ai/runtime/agent/model"
	"google.golang.org/genai"

	genassistant "github.com/ikawaha/ebira/gen/assistant"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/agent"
	"github.com/ikawaha/ebira/internal/apikey"
	"github.com/ikawaha/ebira/internal/config"
)

// Model providers of the assistant, in the order the assistant picks the
// one whose key it has when none is chosen.
const (
	providerAnthropic = "anthropic"
	providerGemini    = "gemini"
	providerOpenAI    = "openai"
)

var providers = []string{providerAnthropic, providerGemini, providerOpenAI}

var defaultModels = map[string]string{
	providerAnthropic: "claude-sonnet-5-5",
	providerGemini:    "gemini-3.8-flash",
	providerOpenAI:    "gpt-6.2-sol",
}

// keyEnvNames are the environment variables of the API keys by provider, in
// the order they are looked at.
var keyEnvNames = map[string][]string{
	providerAnthropic: {"ANTHROPIC_API_KEY"},
	providerGemini:    {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	providerOpenAI:    {"OPENAI_API_KEY"},
}

// envKeys returns the API keys in the environment variables, by provider.
func envKeys() map[string]string {
	keys := map[string]string{}
	for p, names := range keyEnvNames {
		for _, n := range names {
			if v := os.Getenv(n); v != "" {
				keys[p] = v
				break
			}
		}
	}
	return keys
}

// keyEnvProviders returns the providers of keys, in their order.
func keyEnvProviders(keys map[string]string) []string {
	var ps []string
	for _, p := range providers {
		if keys[p] != "" {
			ps = append(ps, p)
		}
	}
	return ps
}

// readKeyCommands reads the commands that print the API keys from the
// settings file, each time, so that those saved apply without a restart.
func readKeyCommands() (map[string]string, error) {
	c, err := config.Load()
	if err != nil {
		return nil, err
	}
	return c.KeyCommands(), nil
}

// lazyAssistant is the assistant service, which makes the agent when it is
// first used: a key that a command prints is read then, rather than when
// the server starts, so that a password manager asks to confirm it, as with
// Touch ID, when the assistant is opened. Until it is made, each use tries
// again, telling why it could not, so that a key confirmed later or a
// command fixed in the meantime is taken.
type lazyAssistant struct {
	ctx      context.Context // of the server, which the agent lives with
	svc      gennotes.Service
	provider string // chosen, or "" for the first whose key there is
	model    string
	env      map[string]string                             // keys of the environment variables
	commands func() (map[string]string, error)             // reads the commands of the keys
	runKey   func(context.Context, string) (string, error) // runs a command
	build    func(ctx context.Context, svc gennotes.Service, provider, modelID, key string) (genassistant.Service, error)

	mu      sync.Mutex
	made    genassistant.Service
	fromEnv bool // made with a key of the environment variables
}

// newAssistant returns the assistant service on the model of provider, or
// of the first provider whose key there is when it is "". A key in an
// environment variable makes the agent at once, and one failing stops the
// server, as a flag would; a key of a command makes it when it is first
// used. The function returned forgets an agent made with a command, so that
// saved settings apply when it is used next.
func newAssistant(ctx context.Context, svc gennotes.Service, provider, modelID string) (genassistant.Service, func(), error) {
	if _, ok := defaultModels[provider]; provider != "" && !ok {
		return nil, nil, fmt.Errorf("unknown model provider %q (anthropic, gemini, openai)", provider)
	}
	l := &lazyAssistant{
		ctx: ctx, svc: svc, provider: provider, model: modelID,
		env: envKeys(), commands: readKeyCommands, runKey: apikey.Run, build: buildAssistant,
	}
	if p := l.pick(nil); p != "" && l.env[p] != "" {
		a, err := l.build(ctx, svc, p, modelID, l.env[p])
		if err != nil {
			return nil, nil, err
		}
		l.made, l.fromEnv = a, true
	}
	return l, l.forget, nil
}

// pick returns the provider of the assistant: the one chosen, else the first
// with a key in the environment variables or a command; "" for none.
func (l *lazyAssistant) pick(commands map[string]string) string {
	if l.provider != "" {
		return l.provider
	}
	for _, p := range providers {
		if l.env[p] != "" || commands[p] != "" {
			return p
		}
	}
	return ""
}

// get returns the agent, made now if it is not yet, or a service that tells
// why it cannot be.
func (l *lazyAssistant) get(ctx context.Context) genassistant.Service {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.made != nil {
		return l.made
	}
	commands, err := l.commands()
	if err != nil {
		return agent.Unavailable{Reason: "the settings file cannot be read: " + err.Error()}
	}
	p := l.pick(commands)
	if p == "" {
		return agent.Unavailable{Reason: "the assistant is not configured: set a command that prints an API key in the settings file (anthropic_key_command, gemini_key_command or openai_key_command), or start the server with ANTHROPIC_API_KEY, GEMINI_API_KEY or OPENAI_API_KEY set"}
	}
	key, fromEnv := l.env[p], true
	if key == "" {
		cmd := commands[p]
		if cmd == "" {
			return agent.Unavailable{Reason: fmt.Sprintf("the %s provider needs %s_key_command in the settings file or %s", p, p, keyEnvNames[p][0])}
		}
		if key, err = l.runKey(ctx, cmd); err != nil {
			slog.Warn("reading the API key failed", "provider", p, "error", err)
			return agent.Unavailable{Reason: fmt.Sprintf("the API key of %s could not be read with %s_key_command: %v", p, p, err)}
		}
		fromEnv = false
	}
	a, err := l.build(l.ctx, l.svc, p, l.model, key)
	if err != nil {
		slog.Warn("the assistant failed to start", "provider", p, "error", err)
		return agent.Unavailable{Reason: err.Error()}
	}
	l.made, l.fromEnv = a, fromEnv
	return a
}

// forget drops an agent made with a key of a command.
func (l *lazyAssistant) forget() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.fromEnv {
		l.made = nil
	}
}

func (l *lazyAssistant) Status(ctx context.Context, p *genassistant.StatusPayload) (*genassistant.StatusResult, error) {
	return l.get(ctx).Status(ctx, p)
}

func (l *lazyAssistant) Ask(ctx context.Context, p *genassistant.AskPayload) (*genassistant.AskResult, error) {
	return l.get(ctx).Ask(ctx, p)
}

// buildAssistant returns the agent on the model modelID, or the default one,
// of provider with key, reading the notes through svc.
func buildAssistant(ctx context.Context, svc gennotes.Service, provider, modelID, key string) (genassistant.Service, error) {
	modelID = cmp.Or(modelID, defaultModels[provider])
	var (
		client model.Client
		err    error
	)
	switch provider {
	case providerAnthropic:
		client, err = anthropic.NewFromAPIKey(key, modelID)
	case providerGemini:
		// The vertex adapter takes the genai models service, which also serves
		// the Gemini API (Google AI Studio keys).
		var gc *genai.Client
		gc, err = genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
		if err == nil {
			client, err = vertex.New(gc.Models, vertex.Options{DefaultModel: modelID})
		}
	case providerOpenAI:
		client, err = openai.NewFromAPIKey(key, modelID)
	default:
		return nil, errors.New("unknown model provider " + provider)
	}
	if err != nil {
		return nil, err
	}
	a, err := agent.New(ctx, client, svc, agent.Config{Model: modelID})
	if err != nil {
		return nil, err
	}
	slog.Info("assistant enabled", "provider", provider, "model", modelID)
	return a, nil
}
