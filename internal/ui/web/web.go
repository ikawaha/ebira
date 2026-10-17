// Package web serves the web UI: static files embedded in the binary that call
// the REST API from the browser.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"slices"
	"time"
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

// Langs are the languages of the UI, each with static/i18n/<lang>.json.
// English is the base: a message missing in another language is shown in
// English.
var Langs = []string{"en", "ja"}

// ValidLang reports whether lang is one of Langs.
func ValidLang(lang string) bool { return slices.Contains(Langs, lang) }

// Handler serves the web UI under Prefix. lang, one of Langs or empty, is the
// language set for this server, given to the page as the lang attribute of
// its html element; empty leaves the choice to the browser's languages.
func Handler(lang string) http.Handler {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err) // the embedded directory always exists
	}
	page, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}
	if lang != "" {
		page = bytes.Replace(page, []byte("<html>"), []byte(`<html lang="`+lang+`">`), 1)
	}
	files := http.StripPrefix(Prefix, http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The files change with the binary and have no modification time.
		h.Set("Cache-Control", "no-cache")
		if r.URL.Path == Prefix || r.URL.Path == Prefix+"index.html" {
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
			return
		}
		files.ServeHTTP(w, r)
	})
}
