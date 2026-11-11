package web

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	srv := httptest.NewServer(Handler(Options{}))
	defer srv.Close()
	for _, path := range []string{"/", "/assets/app.js", "/assets/style.css", "/assets/i18n/ja.json", "/assets/vendor/marked.umd.js", "/assets/vendor/purify.min.js", "/assets/vendor/katex/katex.min.js", "/assets/vendor/katex/katex.min.css", "/assets/vendor/katex/fonts/KaTeX_Main-Regular.woff2", "/assets/vendor/mermaid.min.js", "/assets/vendor/codemirror.js"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d", path, res.StatusCode)
			continue
		}
		if len(body) == 0 {
			t.Errorf("%s: empty body", path)
		}
		if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s: Content-Security-Policy = %q", path, csp)
		}
	}
}

func TestHandlerLang(t *testing.T) {
	for _, tt := range []struct{ lang, theme, search, want string }{
		{"", "", "", "<html>"},
		{"ja", "", "", `<html lang="ja">`},
		{"en", "auto", "", `<html lang="en">`},
		{"", "dark", "", `<html data-theme="dark">`},
		{"ja", "light", "", `<html lang="ja" data-theme="light">`},
		{"ja", "", "bigram", `<html lang="ja" data-search-mode="bigram">`},
		{"", "", `x"><script>`, "<html>"}, // not a mode
	} {
		srv := httptest.NewServer(Handler(Options{Lang: tt.lang, Theme: tt.theme, Search: tt.search}))
		for _, path := range []string{"/"} {
			res, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusOK || !strings.Contains(string(body), tt.want) {
				t.Errorf("lang %q, theme %q, %s: status %d, no %s", tt.lang, tt.theme, path, res.StatusCode, tt.want)
			}
			if csp := res.Header.Get("Content-Security-Policy"); csp != contentSecurityPolicy {
				t.Errorf("lang %q, %s: Content-Security-Policy = %q", tt.lang, path, csp)
			}
		}
		srv.Close()
	}
}

// TestMessages checks that every language has the messages of English, with
// the same {name}s, and that the page and app.js know the languages.
func TestMessages(t *testing.T) {
	load := func(lang string) map[string]string {
		t.Helper()
		b, err := fs.ReadFile(static, "static/i18n/"+lang+".json")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s.json: %v", lang, err)
		}
		return m
	}
	vars := regexp.MustCompile(`\{\w+\}`)
	names := func(s string) []string {
		v := vars.FindAllString(s, -1)
		slices.Sort(v)
		return slices.Compact(v)
	}
	en := load("en")
	page, _ := fs.ReadFile(static, "static/index.html")
	app, _ := fs.ReadFile(static, "static/app.js")
	for _, lang := range Langs {
		m := load(lang)
		for k, v := range en {
			w, ok := m[k]
			if !ok {
				t.Errorf("%s.json: no %s", lang, k)
				continue
			}
			if !slices.Equal(names(v), names(w)) {
				t.Errorf("%s.json: %s has %v, en.json %v", lang, k, names(w), names(v))
			}
		}
		for k := range m {
			if _, ok := en[k]; !ok {
				t.Errorf("%s.json: %s is not in en.json", lang, k)
			}
		}
		if m["langName"] == "" {
			t.Errorf("%s.json: no langName", lang)
		}
		if !strings.Contains(string(page), `data-lang="`+lang+`"`) {
			t.Errorf("index.html: no menu item for %s", lang)
		}
		if !strings.Contains(string(app), `"`+lang+`"`) {
			t.Errorf("app.js: %s is not in LANGS", lang)
		}
	}
}

func TestHandlerVersion(t *testing.T) {
	srv := httptest.NewServer(Handler(Options{Version: `v0.1.0"<x>`, VersionText: "version:   v0.1.0\ngo:        go1.26.0\n"}))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	want := `<html data-version="v0.1.0&#34;&lt;x&gt;" data-version-text="version:   v0.1.0` + "\n" + `go:        go1.26.0` + "\n" + `">`
	if !strings.Contains(string(body), want) {
		t.Errorf("no %q in the page", want)
	}
}

func TestHandlerGuideNames(t *testing.T) {
	srv := httptest.NewServer(Handler(Options{GuideNames: []string{"ebira Guide", `a"b`}}))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	want := `<html data-guide-names="ebira Guide` + "\n" + `a&#34;b">`
	if !strings.Contains(string(body), want) {
		t.Errorf("no %q in the page", want)
	}
}

// TestStyleScale checks that style.css keeps to its scales: the sizes of text
// and the radii of corners are the variables of :root (or relative to the
// text, or round), and spaces are multiples of 4px, but for the hairlines of
// 1px and 2px.
func TestStyleScale(t *testing.T) {
	b, err := fs.ReadFile(static, "static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?:^|[\s;{])(font-size|font|border-radius|padding(?:-\w+)?|margin(?:-\w+)?|gap|row-gap|column-gap)\s*:\s*([^;}\n]*)`)
	px := regexp.MustCompile(`(-?\d+(?:\.\d+)?)px`)
	for i, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue // the scales themselves
		}
		for _, m := range decl.FindAllStringSubmatch(line, -1) {
			prop, value := m[1], strings.TrimSpace(m[2])
			switch prop {
			case "font-size", "border-radius":
				if strings.Contains(value, "px") {
					t.Errorf("style.css:%d: %s: %s, not a variable of the scale", i+1, prop, value)
				}
			case "font":
				if px.MatchString(value) {
					t.Errorf("style.css:%d: font: %s, not a variable of the scale", i+1, value)
				}
			default:
				for _, n := range px.FindAllStringSubmatch(value, -1) {
					switch v := strings.TrimPrefix(n[1], "-"); v {
					case "0", "1", "2":
					default:
						if f, _ := strconv.ParseFloat(v, 64); f != float64(int(f)) || int(f)%4 != 0 {
							t.Errorf("style.css:%d: %s: %s, off the grid of 4px", i+1, prop, value)
						}
					}
				}
			}
		}
	}
}
