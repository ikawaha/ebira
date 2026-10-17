// Package web serves the web UI: static files embedded in the binary that call
// the REST API from the browser.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var static embed.FS

// Prefix is the path the web UI is served under.
const Prefix = "/ui/"

// contentSecurityPolicy lets the UI run only its own scripts. Notes are
// rendered as HTML after sanitizing, and this keeps scripts that slip through
// from running. Inline styles are allowed because the vendored KaTeX and
// Mermaid set them; the sanitizer removes style elements and attributes from
// notes, so that notes cannot add CSS. Images may come from anywhere over
// HTTPS, as notes link them.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Handler serves the web UI under Prefix.
func Handler() http.Handler {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err) // the embedded directory always exists
	}
	files := http.StripPrefix(Prefix, http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The files change with the binary and have no modification time.
		h.Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
