package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ikawaha/ebira/internal/store"
)

// Limits on the size of request bodies. The generated decoders read a body
// whole before a request is authorized or validated, so a body past its
// limit is cut there instead of being buffered.
const (
	// maxBody fits the largest request but those below: an import by AI
	// clients, a Markdown file of 5 MB with 20 MB of images in base64, even
	// with its characters escaped in JSON, as < for <, six times as long.
	maxBody = 64 << 20
	// maxImportBody fits an import by people, whose images, each up to
	// store.MaxFileSize, are sent with the Markdown file in base64: about
	// 190 MB of images in all, a few of the largest. It does not grow with
	// the number of images, which is not limited, so that a request is
	// still bounded before its session is checked.
	maxImportBody = 256 << 20
	// maxFileBody fits one file added to a note, sent as is.
	maxFileBody = store.MaxFileSize + 1<<20
)

// tooLarge answers a request whose body is past its limit with 413, for the
// MCP server, which leaves the error of reading the body to its error
// handler, and only logs the other errors.
func tooLarge(ctx context.Context, w http.ResponseWriter, err error) {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		http.Error(w, "the request body is too large", http.StatusRequestEntityTooLarge)
		return
	}
	slog.ErrorContext(ctx, "request failed", "error", err)
}

// limitBodies limits the size of the request bodies next reads.
func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int64(maxBody)
		if r.Method == http.MethodPost {
			switch p := r.URL.Path; {
			case p == "/import":
				n = maxImportBody
			case strings.HasPrefix(p, "/notes/") && strings.HasSuffix(p, "/files"):
				n = maxFileBody
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
}
