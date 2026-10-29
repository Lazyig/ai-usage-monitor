package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// With PUBLIC_URL set, client headers cannot change where the generated
// installer downloads from; without it, localhost keeps working as before.
func TestGeneratedInstallerUsesPublicURL(t *testing.T) {
	for _, tc := range []struct{ public, want string }{
		{"https://usage.example.com", "https://usage.example.com"},
		{"", "http://localhost:8080"},
	} {
		h := newHarness(t, Options{AdminToken: adminToken, PublicURL: tc.public})
		req := httptest.NewRequest(http.MethodGet, "/downloads/install.sh", nil)
		req.Host = "localhost:8080"
		if tc.public != "" {
			req.Header.Set("X-Forwarded-Host", "evil.example")
		}
		w := httptest.NewRecorder()
		h.h.ServeHTTP(w, req)
		body := w.Body.String()
		if !strings.Contains(body, fmt.Sprintf("SERVER='%s'", tc.want)) && !strings.Contains(body, tc.want) {
			t.Errorf("public %q: installer does not default to %s", tc.public, tc.want)
		}
		if strings.Contains(body, "evil.example") {
			t.Errorf("public %q: a forwarded host reached the installer", tc.public)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("generated installer is cacheable: %q", w.Header().Get("Cache-Control"))
		}
	}
}

func TestParsePublicURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                           "",
		"https://usage.example.com/": "https://usage.example.com",
		"http://localhost:8080":      "http://localhost:8080",
	} {
		if got, err := ParsePublicURL(in); err != nil || got != want {
			t.Errorf("ParsePublicURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"usage.example.com", "ftp://x", "https://x/?a=1", "https://u:p@x"} {
		if _, err := ParsePublicURL(bad); err == nil {
			t.Errorf("ParsePublicURL(%q) accepted", bad)
		}
	}
}
