package service

import (
	"context"
	"strings"

	gennotes "github.com/ikawaha/ebira/gen/notes"
	ebiratools "github.com/ikawaha/ebira/gen/notes/toolsets/ebira"
)

// ListTools lists every MCP tool on one page. The tools are fixed while the
// server runs; listing them as a changing catalog lets clients subscribe to
// changes (Listen), so that they list them again after a restart.
func (s *Notes) ListTools(_ context.Context, _ *gennotes.ListToolsPayload) (*gennotes.ListToolsResult, error) {
	specs := ebiratools.Specs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		// The name is qualified with the toolset (ebira.get_note).
		_, name, _ := strings.Cut(string(spec.Name), ".")
		names = append(names, name)
	}
	return &gennotes.ListToolsResult{Tools: names}, nil
}

// Listen accepts a subscription to tool list changes and holds it open until
// the client goes away or the server shuts down. It never reports a change:
// the tools change only with a new server, and the end of the stream is what
// tells the client to list them again.
func (s *Notes) Listen(ctx context.Context, p *gennotes.ListenPayload, stream gennotes.ListenServerStream) error {
	accepted := p.ToolsListChanged != nil && *p.ToolsListChanged
	ack := &gennotes.ListenEvent{Change: gennotes.NewChangeAcknowledged(&gennotes.ListenSelection{ToolsListChanged: &accepted})}
	if err := stream.SendWithContext(ctx, ack); err != nil {
		return err
	}
	if !accepted {
		return nil
	}
	select {
	case <-ctx.Done():
	case <-s.events.done:
	}
	return nil
}
