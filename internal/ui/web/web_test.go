package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()
	for _, path := range []string{"/ui/", "/ui/app.js", "/ui/style.css", "/ui/vendor/marked.umd.js", "/ui/vendor/purify.min.js"} {
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
