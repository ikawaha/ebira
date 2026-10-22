// Package web serves the web UI: static files embedded in the binary that call
// the REST API from the browser.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

//go:embed static
var static embed.FS

// Prefix is the path of the page of the web UI, the top of the server.
const Prefix = "/"

// AssetsPrefix is the path the files of the page, such as its scripts, are
// served under, apart from the paths of the REST API.
const AssetsPrefix = "/assets/"

// LegacyPrefix is the path the web UI was served under before; it redirects
// to Prefix, keeping the fragment of links to notes (#note/<ID>).
const LegacyPrefix = "/ui/"

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

// Themes are the color themes of the UI: auto follows the system's light or
// dark mode.
var Themes = []string{"auto", "light", "dark"}

// ValidTheme reports whether theme is one of Themes.
func ValidTheme(theme string) bool { return slices.Contains(Themes, theme) }

// searchMode matches the names of search modes, which go into the page as
// they are.
var searchMode = regexp.MustCompile(`^[a-z0-9]+$`)

// Handler serves the web UI: the page at Prefix, and its files under
// AssetsPrefix. lang, one of Langs or empty, is the language set for this
// server, given to the page as the lang attribute of its html element; empty
// leaves the choice to the browser's languages. theme, one of Themes or
// empty, is the color theme set for this server, given to the page as the
// data-theme attribute of its html element when it is light or dark; auto and
// empty follow the system. A language or theme chosen in the page's settings
// menu wins over both. search is the search mode of the server, such as ja,
// given as the data-search-mode attribute, which the page tells in the
// placeholder of the search box; empty tells none.
func Handler(lang, theme, search string) http.Handler {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err) // the embedded directory always exists
	}
	page, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}
	attrs := ""
	if lang != "" {
		attrs += ` lang="` + lang + `"`
	}
	if theme == "light" || theme == "dark" {
		attrs += ` data-theme="` + theme + `"`
	}
	if searchMode.MatchString(search) {
		attrs += ` data-search-mode="` + search + `"`
	}
	page = bytes.Replace(page, []byte("<html>"), []byte("<html"+attrs+">"), 1)
	files := http.StripPrefix(AssetsPrefix, http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The files change with the binary and have no modification time.
		h.Set("Cache-Control", "no-cache")
		switch {
		case r.URL.Path == Prefix:
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
		case strings.HasPrefix(r.URL.Path, AssetsPrefix) && r.URL.Path != AssetsPrefix+"index.html":
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
