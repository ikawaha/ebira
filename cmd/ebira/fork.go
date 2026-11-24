package main

import (
	"context"
	"log/slog"

	mcpnotes "github.com/ikawaha/ebira/gen/mcp_notes"
	gennotes "github.com/ikawaha/ebira/gen/notes"
	"github.com/ikawaha/ebira/internal/claudecode"
	"github.com/ikawaha/ebira/internal/service"
)

// forker answers the sticky notes of the fork kind with claude.
type forker struct{ claudecode.Fork }

func (f forker) Ask(ctx context.Context, a service.ForkAsk) (string, error) {
	return f.Fork.Ask(ctx, claudecode.ForkAsk(a))
}

// newForker returns the forks of Claude Code sessions for the server at
// addr, or nil when claude is not found.
func newForker(ctx context.Context, addr string) service.Forker {
	c, err := claudecode.Find(ctx)
	if err != nil {
		slog.Info("forks of Claude Code disabled for whiteboards", "reason", err)
		return nil
	}
	dir, err := claudecode.ConfigDir(ctx)
	if err != nil {
		slog.Warn("forks of Claude Code disabled for whiteboards", "error", err)
		return nil
	}
	allowed, disallowed, err := forkTools(ctx)
	if err != nil {
		slog.Warn("forks of Claude Code disabled for whiteboards", "error", err)
		return nil
	}
	return forker{claudecode.Fork{Client: c, ConfigDir: dir, MCPURL: claudecode.URL(addr), Allowed: allowed, Disallowed: disallowed}}
}

// forkTools splits the MCP tools of ebira into those that only read, which
// forks may call, and the others, which they may not: from the hints of the
// design, so that a tool added later that writes is not missed.
func forkTools(ctx context.Context) (readOnly, others []string, err error) {
	a := mcpnotes.NewMCPAdapter(gennotes.NewEndpoints(&service.Notes{}), nil)
	var cursor *string
	for {
		res, err := a.ToolsList(ctx, &mcpnotes.ToolsListPayload{Cursor: cursor})
		if err != nil {
			return nil, nil, err
		}
		for _, t := range res.Tools {
			if h := t.Annotations; h != nil && h.ReadOnlyHint != nil && *h.ReadOnlyHint {
				readOnly = append(readOnly, t.Name)
			} else {
				others = append(others, t.Name)
			}
		}
		if res.NextCursor == nil || *res.NextCursor == "" {
			return readOnly, others, nil
		}
		cursor = res.NextCursor
	}
}
