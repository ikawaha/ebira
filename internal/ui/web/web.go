// Package web serves the web UI: static files embedded in the binary that call
// the REST API from the browser.
package web

import (
	"bytes"
	"embed"
	"errors"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ikawaha/ebira/internal/previewstyle"
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

// Options are the settings of the server the web UI follows.
type Options struct {
	// Lang, one of Langs or empty, is the language set for this server,
	// given to the page as the lang attribute of its html element; empty
	// leaves the choice to the browser's languages.
	Lang string
	// Theme, one of Themes or empty, is the color theme set for this server,
	// given to the page as the data-theme attribute of its html element when
	// it is light or dark; auto and empty follow the system.
	Theme string
	// Look, when not nil, gives the language and the theme at each request,
	// in place of Lang and Theme, as the server changes them when its
	// settings are saved.
	Look func() (lang, theme string)
	// Search is the search mode of the server, such as ja, given as the
	// data-search-mode attribute, which the page tells in the placeholder of
	// the search box; empty tells none.
	Search string
	// PreviewCSS is the path of the CSS the user wrote to style the preview,
	// read at each request (see PreviewCSS); empty or a missing file is none.
	PreviewCSS string
	// Version is the version of ebira in short, given as the data-version
	// attribute, which the settings menu tells; VersionText is all that
	// ebira version prints, given as data-version-text, which the menu
	// copies. Empty tells none.
	Version, VersionText string
	// GuideNames are the names of the guide (guide.Names), given as the
	// data-guide-names attribute, one a line, by which the page knows the
	// notebooks of the guide.
	GuideNames []string
	// SettingsError is the error of the settings files, of the machine or
	// shared with the notes, that the server runs without in the safe mode,
	// one a line, given as the data-settings-error attribute, which the page
	// tells above the panes; empty for none.
	SettingsError string
}

// stylesDir is where the built-in styles of the preview are served, under
// AssetsPrefix.
const stylesDir = "styles/"

// Handler serves the web UI: the page at Prefix, and its files under
// AssetsPrefix, with the CSS for the preview at AssetsPrefix+PreviewCSSFile
// and the built-in styles at AssetsPrefix+"styles/<name>.css".
func Handler(o Options) http.Handler {
	look := o.Look
	if look == nil {
		look = func() (string, string) { return o.Lang, o.Theme }
	}
	search := o.Search
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err) // the embedded directory always exists
	}
	page, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}
	attrs := ""
	if searchMode.MatchString(search) {
		attrs += ` data-search-mode="` + search + `"`
	}
	if o.Version != "" {
		attrs += ` data-version="` + html.EscapeString(o.Version) + `" data-version-text="` + html.EscapeString(o.VersionText) + `"`
	}
	if o.SettingsError != "" {
		attrs += ` data-settings-error="` + html.EscapeString(o.SettingsError) + `"`
	}
	if len(o.GuideNames) > 0 {
		attrs += ` data-guide-names="` + html.EscapeString(strings.Join(o.GuideNames, "\n")) + `"`
	}
	// The page of the language and the theme, which may change.
	pageOf := func() []byte {
		lang, theme := look()
		a := ""
		if ValidLang(lang) {
			a += ` lang="` + lang + `"`
		}
		if theme == "light" || theme == "dark" {
			a += ` data-theme="` + theme + `"`
		}
		return bytes.Replace(page, []byte("<html>"), []byte("<html"+a+attrs+">"), 1)
	}
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
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(pageOf()))
		case r.URL.Path == AssetsPrefix+PreviewCSSFile:
			servePreviewCSS(w, o.PreviewCSS)
		case strings.HasPrefix(r.URL.Path, AssetsPrefix+stylesDir):
			// A built-in style, to try it in the preview before using it.
			name, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, AssetsPrefix+stylesDir), ".css")
			css, found := previewstyle.CSS(name)
			if !ok || !found {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			io.WriteString(w, PreviewCSS(string(css)))
		case strings.HasPrefix(r.URL.Path, AssetsPrefix) && r.URL.Path != AssetsPrefix+"index.html":
			files.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// servePreviewCSS serves the CSS for the preview from path, as PreviewCSS
// makes it; the default style when there is no such file.
func servePreviewCSS(w http.ResponseWriter, path string) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	f, err := os.Open(path)
	if err != nil {
		if path != "" && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("reading the CSS for the preview", "error", err)
			return
		}
		css, _ := previewstyle.CSS(previewstyle.Default)
		io.WriteString(w, PreviewCSS(string(css)))
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPreviewCSS))
	if err != nil {
		slog.Warn("reading the CSS for the preview", "error", err)
		return
	}
	io.WriteString(w, PreviewCSS(string(previewstyle.Current(b))))
}
