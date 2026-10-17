// Package httpguard guards the HTTP server, the REST API and MCP alike,
// against requests that other sites make through the browser.
package httpguard

import (
	"net"
	"net/http"
	"strings"
)

// Guard rejects requests that a browser sends on behalf of other sites. The
// REST API treats its callers as people, who see every notebook, so a web page
// on another site must not be able to call it:
//   - The Host header must name this machine (localhost, 127.0.0.1, ::1) or
//     listenHost, the host the server listens on. This stops DNS rebinding,
//     where another site's name resolves to 127.0.0.1 and its pages become
//     same-origin with the API.
//   - Requests that change state must come from the same origin
//     (http.CrossOriginProtection, which checks Sec-Fetch-Site and Origin).
//     Reads from other origins are not readable by their pages anyway, as the
//     server sends no CORS headers.
//   - Responses carry Cross-Origin-Resource-Policy: same-origin, so that
//     pages of other sites cannot load them either, such as the images of
//     notes with <img>, whose sizes they could read; and
//     X-Content-Type-Options: nosniff, so that the browser does not guess
//     another content type.
//
// Requests without these browser headers, such as from the TUI, curl and MCP
// clients, pass.
func Guard(next http.Handler, listenHost string) http.Handler {
	allowed := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	if h := strings.ToLower(strings.Trim(listenHost, "[]")); h != "" && !net.ParseIP(h).IsUnspecified() {
		allowed[h] = true
	}
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
	}))
	protected := cop.Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[hostname(r.Host)] {
			http.Error(w, "unexpected Host header", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-Content-Type-Options", "nosniff")
		protected.ServeHTTP(w, r)
	})
}

// hostname returns the lower-cased host of a Host header, without the port and
// the brackets of an IPv6 address.
func hostname(hostport string) string {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		h = hostport // no port
	}
	return strings.ToLower(strings.Trim(h, "[]"))
}
