package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestPreferences(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	get := func() string {
		w := h.do(http.MethodGet, "/api/v1/preferences", adminToken, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET = %d %s", w.Code, w.Body)
		}
		return strings.TrimSpace(w.Body.String())
	}
	if got := get(); got != `{}` {
		t.Errorf("before any save: %s", got)
	}
	if w := h.do(http.MethodGet, "/api/v1/preferences", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("without a token: %d", w.Code)
	}

	// Relative presets drop any dates; custom keeps them.
	if w := h.do(http.MethodPut, "/api/v1/preferences", adminToken, `{"range":{"preset":"yesterday","from":"2026-01-01","to":"2026-01-02"},"auto_refresh":true}`); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", w.Code, w.Body)
	}
	if got := get(); got != `{"range":{"preset":"yesterday"},"auto_refresh":true}` {
		t.Errorf("after save: %s", got)
	}
	if w := h.do(http.MethodPut, "/api/v1/preferences", adminToken, `{"range":{"preset":"custom","from":"2026-10-01","to":"2026-10-11"},"auto_refresh":false}`); w.Code != http.StatusOK {
		t.Fatalf("PUT custom = %d %s", w.Code, w.Body)
	}
	if got := get(); got != `{"range":{"preset":"custom","from":"2026-10-01","to":"2026-10-11"},"auto_refresh":false}` {
		t.Errorf("after custom save: %s", got)
	}

	for _, bad := range []string{
		`{"range":{"preset":"tomorrow"}}`,
		`{"range":{"preset":"custom","from":"2026-10-11","to":"2026-10-01"}}`,
		`{"range":{"preset":"custom","from":"yesterday","to":"2026-10-01"}}`,
		`{"auto_refresh":"yes"}`,
		`{"theme":"dark"}`,
		`not json`,
		`{"range":{"preset":"7d"},"pad":"` + strings.Repeat("x", maxPrefsBytes) + `"}`,
	} {
		if w := h.do(http.MethodPut, "/api/v1/preferences", adminToken, bad); w.Code != http.StatusBadRequest {
			t.Errorf("PUT %.60s = %d, want 400", bad, w.Code)
		}
	}
	// A refused save leaves the stored document alone.
	if got := get(); !strings.Contains(got, `"custom"`) {
		t.Errorf("after refused saves: %s", got)
	}
}
