package httpapi

import (
	"context"
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
	if w := h.do(http.MethodPut, "/api/v1/preferences", adminToken, `{"range":{"preset":"day-1","from":"2026-01-01","to":"2026-01-02"},"auto_refresh":true,"number_units":"ko"}`); w.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", w.Code, w.Body)
	}
	if got := get(); got != `{"range":{"preset":"day-1"},"auto_refresh":true,"number_units":"ko"}` {
		t.Errorf("after save: %s", got)
	}
	if w := h.do(http.MethodPut, "/api/v1/preferences", adminToken, `{"range":{"preset":"custom","from":"2026-10-01","to":"2026-10-11"},"auto_refresh":false,"number_units":"en"}`); w.Code != http.StatusOK {
		t.Fatalf("PUT custom = %d %s", w.Code, w.Body)
	}
	if got := get(); got != `{"range":{"preset":"custom","from":"2026-10-01","to":"2026-10-11"},"auto_refresh":false,"number_units":"en"}` {
		t.Errorf("after custom save: %s", got)
	}

	for _, bad := range []string{
		`{"range":{"preset":"tomorrow"}}`,
		`{"range":{"preset":"day-7"}}`,
		`{"range":{"preset":"month-13"}}`,
		`{"range":{"preset":"last-14"}}`,
		`{"range":{"preset":"30d"}}`,
		`{"range":{"preset":"custom","from":"2026-10-11","to":"2026-10-01"}}`,
		`{"range":{"preset":"custom","from":"yesterday","to":"2026-10-01"}}`,
		`{"range":{"preset":"custom","from":"0001-01-01","to":"9999-12-31"}}`,
		`{"range":{"preset":"custom","from":"1969-12-31","to":"2026-10-01"}}`,
		`{"range":{"preset":"custom","from":"2026-10-01","to":"2201-01-01"}}`,
		`null`,
		`{"auto_refresh":"yes"}`,
		`{"number_units":"jp"}`,
		`{"number_units":""}`,
		`{"number_units":1}`,
		`{"theme":"dark"}`,
		`not json`,
		`{"auto_refresh":true} {"auto_refresh":false}`,
		`{"auto_refresh":true}}`,
		`{"range":{"preset":"last-7"},"pad":"` + strings.Repeat("x", maxPrefsBytes) + `"}`,
	} {
		if w := h.do(http.MethodPut, "/api/v1/preferences", adminToken, bad); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid preferences: ") {
			t.Errorf("PUT %.60s = %d %s, want 400 invalid preferences", bad, w.Code, w.Body)
		}
	}
	// A refused save leaves the stored document alone.
	if got := get(); !strings.Contains(got, `"custom"`) {
		t.Errorf("after refused saves: %s", got)
	}
}

// A stored document this version cannot fully read keeps what it can: an
// unknown range or number units fall back to the default, the rest survives.
func TestPreferencesStoredByAnotherVersion(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	for stored, want := range map[string]string{
		`{"range":{"preset":"week-1"},"auto_refresh":true,"number_units":"jp"}`:    `{"range":{"preset":"week-1"},"auto_refresh":true}`,
		`{"range":{"preset":"week-1"},"auto_refresh":false,"number_units":""}`:     `{"range":{"preset":"week-1"},"auto_refresh":false}`,
		`{"range":{"preset":"quarter-0"},"auto_refresh":true,"number_units":"ko"}`: `{"auto_refresh":true,"number_units":"ko"}`,
		`{"range":{"preset":"quarter-0"},"auto_refresh":true}`:                     `{"auto_refresh":true}`,
		`{"range":{"preset":"week-1"},"theme":"dark"}`:                             `{"range":{"preset":"week-1"}}`,
		`{"auto_refresh":"yes"}`: `{}`,
		`not json`:               `{}`,
	} {
		if err := h.store.SetPreference(context.Background(), dashboardPrefsName, stored); err != nil {
			t.Fatal(err)
		}
		w := h.do(http.MethodGet, "/api/v1/preferences", adminToken, nil)
		if got := strings.TrimSpace(w.Body.String()); w.Code != http.StatusOK || got != want {
			t.Errorf("stored %s: GET = %d %s, want %s", stored, w.Code, got, want)
		}
	}
}
