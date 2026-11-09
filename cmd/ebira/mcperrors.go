package main

import (
	"errors"
	"log/slog"

	goa "goa.design/goa/v3/pkg"
)

// errMCPFailed is what MCP clients get for an error the notes service does
// not declare.
var errMCPFailed = errors.New("the request failed on the server; see its log")

// mcpError is the error MCP clients, which are AI clients, get for err, an
// error of the notes service: err itself when the service declares it (as
// not_found) or it tells a request is invalid, which tells them what to do;
// else, as an error of the file system with paths in the notes directory,
// errMCPFailed, logging err.
func mcpError(err error) error {
	var se *goa.ServiceError
	if errors.As(err, &se) && !se.Fault {
		return err
	}
	slog.Error("MCP request failed", "error", err)
	return errMCPFailed
}
