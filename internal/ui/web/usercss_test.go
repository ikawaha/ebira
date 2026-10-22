package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewCSS(t *testing.T) {
	inner := func(s string) string { // the CSS inside the scope
		s = strings.TrimPrefix(s, previewScope+" {\n")
		return strings.TrimSuffix(s, "\n}\n")
	}
	for _, tt := range []struct {
		name, css, want string
	}{
		{"kept", "h1 { color: var(--accent); border-bottom: 1px dotted #888; }\nfont-size: 15px;",
			"h1 { color: var(--accent); border-bottom: 1px dotted #888; }\nfont-size: 15px;"},
		{"media and nesting", `@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) & h1 { color: gold } }`,
			`@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) & h1 { color: gold } }`},
		{"content strings", `li::before { content: "\201C url(x)" }`, `li::before { content: "\201C url(x)" }`},
		{"comments go", "h1 { /* url(https://x) */ color: red }", "h1 {   color: red }"},
		{"import", "@import url(https://evil.example/x.css);\n@import 'x.css' screen;\nh1{}", "\n\nh1{}"},
		{"escaped at-rule", `@\69mport "https://evil.example/x.css"; h1{}`, " h1{}"},
		{"charset", `@charset "utf-8"; h1{}`, " h1{}"},
		{"url", `a[href^="a"] { background: url(https://evil.example/?a) }`, `a[href^="a"] { background:  }`},
		{"quoted url", `a { background: URL( "https://evil.example/" ) no-repeat }`, `a { background:  no-repeat }`},
		{"escaped url", `a { background: u\72l(https://evil.example/) }`, `a { background:  }`},
		{"escaped scheme", `a { background: url(d\61ta:x) }`, `a { background:  }`},
		{"image-set", `a { background: image-set("https://evil.example/" 1x) }`, `a { background:  }`},
		{"nested url in a function", `a { background: linear-gradient(red, blue), url(//evil.example/) }`, `a { background: linear-gradient(red, blue),  }`},
		{"data url", `a { background: url(data:image/png;base64,AAAA) }`, `a { background: url(data:image/png;base64,AAAA) }`},
		{"quoted data url", `@font-face { font-family: X; src: url("data:font/woff2;base64,AA"), local("X") }`,
			`@font-face { font-family: X; src: url("data:font/woff2;base64,AA"), local("X") }`},
		{"src with a URL", `@font-face { src: src("https://evil.example/f.woff2") }`, `@font-face { src:  }`},
		{"escape out of the scope", "h1 { color: red } } body { display: none }", "h1 { color: red }  body { display: none }"},
		{"blocks left open", "h1 { color: red", "h1 { color: red}"},
		{"import in a block keeps its end", "@media print { @import 'x' }", "@media print { }"},
	} {
		got := PreviewCSS(tt.css)
		if !strings.HasPrefix(got, previewScope+" {\n") || !strings.HasSuffix(got, "\n}\n") {
			t.Errorf("%s: not nested in the scope: %q", tt.name, got)
			continue
		}
		if inner(got) != tt.want {
			t.Errorf("%s: PreviewCSS(%q) = %q, want %q", tt.name, tt.css, inner(got), tt.want)
		}
	}
}

func TestServePreviewCSS(t *testing.T) {
	path := filepath.Join(t.TempDir(), PreviewCSSFile)
	srv := httptest.NewServer(Handler(Options{PreviewCSS: path}))
	defer srv.Close()
	get := func() (string, string) {
		res, err := http.Get(srv.URL + AssetsPrefix + PreviewCSSFile)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", res.StatusCode)
		}
		return res.Header.Get("Content-Type"), string(b)
	}
	// Without the file, an empty stylesheet.
	if ct, body := get(); body != "" || !strings.HasPrefix(ct, "text/css") {
		t.Errorf("no file: %q, %q", ct, body)
	}
	// The file is read at each request.
	if err := os.WriteFile(path, []byte("h1 { color: red; background: url(https://evil.example/) }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, body := get(); body != previewScope+" {\nh1 { color: red; background:  }\n}\n" {
		t.Errorf("body = %q", body)
	}
}
