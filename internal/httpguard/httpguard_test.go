package httpguard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	tests := []struct {
		name       string
		listenHost string
		method     string
		host       string
		headers    map[string]string
		want       int
	}{
		{name: "localhost", method: "GET", host: "localhost:8080", want: http.StatusNoContent},
		{name: "loopback v4", method: "GET", host: "127.0.0.1:8080", want: http.StatusNoContent},
		{name: "loopback v6", method: "GET", host: "[::1]:8080", want: http.StatusNoContent},
		{name: "upper case", method: "GET", host: "LocalHost:8080", want: http.StatusNoContent},
		{name: "DNS rebinding", method: "GET", host: "evil.example:8080", want: http.StatusMisdirectedRequest},
		{name: "listen host", listenHost: "ebira.local", method: "GET", host: "ebira.local:8080", want: http.StatusNoContent},
		{name: "wildcard listen host adds nothing", listenHost: "0.0.0.0", method: "GET", host: "0.0.0.0:8080", want: http.StatusMisdirectedRequest},
		{name: "no browser headers (TUI, curl, MCP)", method: "POST", host: "localhost:8080", want: http.StatusNoContent},
		{name: "same origin", method: "POST", host: "localhost:8080", headers: map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:8080"}, want: http.StatusNoContent},
		{name: "cross site", method: "POST", host: "localhost:8080", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, want: http.StatusForbidden},
		{name: "other port on localhost", method: "DELETE", host: "localhost:8080", headers: map[string]string{"Origin": "http://localhost:3000"}, want: http.StatusForbidden},
		{name: "cross site read", method: "GET", host: "localhost:8080", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/notes", nil)
			req.Host = tt.host
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			Guard(ok, tt.listenHost).ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
