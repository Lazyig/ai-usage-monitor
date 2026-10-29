package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	for _, path := range []string{"/", "/api/v1/health", "/downloads/"} {
		w := h.do(http.MethodGet, path, "", nil)
		for _, name := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy"} {
			if w.Header().Get(name) == "" {
				t.Errorf("%s: no %s header", path, name)
			}
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s: CSP does not forbid framing", path)
		}
	}
}
