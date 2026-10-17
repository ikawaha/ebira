package web

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	srv := httptest.NewServer(Handler(""))
	defer srv.Close()
	for _, path := range []string{"/ui/", "/ui/app.js", "/ui/style.css", "/ui/vendor/marked.umd.js", "/ui/vendor/purify.min.js", "/ui/vendor/katex/katex.min.js", "/ui/vendor/katex/katex.min.css", "/ui/vendor/katex/fonts/KaTeX_Main-Regular.woff2", "/ui/vendor/mermaid.min.js"} {
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
	for _, tt := range []struct{ lang, want string }{{"", "<html>"}, {"ja", `<html lang="ja">`}, {"en", `<html lang="en">`}} {
		srv := httptest.NewServer(Handler(tt.lang))
		for _, path := range []string{"/ui/", "/ui/index.html"} {
			res, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusOK || !strings.Contains(string(body), tt.want) {
				t.Errorf("lang %q, %s: status %d, no %s", tt.lang, path, res.StatusCode, tt.want)
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
