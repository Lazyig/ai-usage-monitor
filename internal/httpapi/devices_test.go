package httpapi

import (
	"net/http"
	"testing"
	"time"

	"ai-usage-monitor/internal/store"
)

func TestRotateDeviceToken(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	devID, old := h.newDevice("mac")
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	// Warm the auth cache so the rotate has something stale to invalidate.
	h.ingest(devID, old, sampleEvent("r1", devID, ts, 10, 20))

	w := h.do(http.MethodPost, "/api/v1/devices/"+devID+"/rotate", adminToken, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("rotate = %d %s", w.Code, w.Body)
	}
	var res struct {
		ID             string    `json:"id"`
		Name           string    `json:"name"`
		Token          string    `json:"token"`
		TokenCreatedAt time.Time `json:"token_created_at"`
	}
	h.decode(w, &res)
	if res.ID != devID || res.Name != "mac" {
		t.Fatalf("rotate response = %+v, want the same device", res)
	}
	if res.Token == "" || res.Token == old {
		t.Fatalf("rotate token = %q, want a fresh one", res.Token)
	}
	if res.TokenCreatedAt.IsZero() {
		t.Error("token_created_at is missing from the rotate response")
	}

	// The old token is dead immediately - not once its cache entry expires.
	if w := h.do(http.MethodPost, "/api/v1/ingest", old, map[string]any{"events": []any{}}); w.Code != http.StatusUnauthorized {
		t.Errorf("ingest with the rotated-out token = %d, want 401", w.Code)
	}
	// The new one works, and lands on the same device with its history intact.
	got := h.ingest(devID, res.Token, sampleEvent("r2", devID, ts.Add(time.Hour), 10, 20))
	if got.DeviceID != devID || got.Accepted != 1 {
		t.Fatalf("ingest with the new token = %+v", got)
	}
	var devs struct{ Devices []store.Device }
	h.decode(h.do(http.MethodGet, "/api/v1/devices", adminToken, nil), &devs)
	if len(devs.Devices) != 1 || devs.Devices[0].EventsTotal != 2 {
		t.Fatalf("devices after rotate = %+v, want one device with both events", devs.Devices)
	}

	if w := h.do(http.MethodPost, "/api/v1/devices/nope/rotate", adminToken, nil); w.Code != http.StatusNotFound {
		t.Errorf("rotate unknown = %d, want 404", w.Code)
	}
	if w := h.do(http.MethodPost, "/api/v1/devices/"+devID+"/rotate", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("rotate without the admin token = %d, want 401", w.Code)
	}
}

func TestDeleteDeviceWithPurge(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	devID, devToken := h.newDevice("mac")
	keepID, keepToken := h.newDevice("linux")
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	h.ingest(devID, devToken, sampleEvent("p1", devID, ts, 10, 20), sampleEvent("p2", devID, ts, 30, 40))
	h.ingest(keepID, keepToken, sampleEvent("k1", keepID, ts, 1, 2))

	w := h.do(http.MethodDelete, "/api/v1/devices/"+devID+"?purge=true", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("purge = %d %s", w.Code, w.Body)
	}
	var res struct {
		ID            string `json:"id"`
		DeletedEvents int64  `json:"deleted_events"`
		Purged        bool   `json:"purged"`
		Revoked       bool   `json:"revoked"`
	}
	h.decode(w, &res)
	if res.ID != devID || res.DeletedEvents != 2 || !res.Purged || res.Revoked {
		t.Fatalf("purge response = %+v, want 2 deleted events and purged=true", res)
	}

	// Row and events are gone; the other device is untouched.
	var devs struct{ Devices []store.Device }
	h.decode(h.do(http.MethodGet, "/api/v1/devices", adminToken, nil), &devs)
	if len(devs.Devices) != 1 || devs.Devices[0].ID != keepID || devs.Devices[0].EventsTotal != 1 {
		t.Fatalf("devices after purge = %+v, want only %s", devs.Devices, keepID)
	}
	var health struct {
		Events  int64 `json:"events"`
		Devices int64 `json:"devices"`
	}
	h.decode(h.do(http.MethodGet, "/health", "", nil), &health)
	if health.Events != 1 || health.Devices != 1 {
		t.Errorf("health after purge = %+v, want 1 event and 1 device", health)
	}
	// The token cannot come back to life against a device that no longer exists.
	if w := h.do(http.MethodPost, "/api/v1/ingest", devToken, map[string]any{"events": []any{}}); w.Code != http.StatusUnauthorized {
		t.Errorf("ingest after purge = %d, want 401", w.Code)
	}

	// Without the flag it is still a plain revoke: the row and events survive.
	w = h.do(http.MethodDelete, "/api/v1/devices/"+keepID, adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke = %d %s", w.Code, w.Body)
	}
	var rev struct {
		DeletedEvents int64 `json:"deleted_events"`
		Purged        bool  `json:"purged"`
		Revoked       bool  `json:"revoked"`
	}
	h.decode(w, &rev)
	if !rev.Revoked || rev.Purged || rev.DeletedEvents != 0 {
		t.Fatalf("revoke response = %+v, want revoked=true and nothing deleted", rev)
	}
	h.decode(h.do(http.MethodGet, "/api/v1/devices", adminToken, nil), &devs)
	if len(devs.Devices) != 1 || devs.Devices[0].EventsTotal != 1 || devs.Devices[0].RevokedAt == nil {
		t.Fatalf("devices after revoke = %+v, want the row kept and revoked", devs.Devices)
	}

	if w := h.do(http.MethodDelete, "/api/v1/devices/nope?purge=true", adminToken, nil); w.Code != http.StatusNotFound {
		t.Errorf("purge unknown = %d, want 404", w.Code)
	}
}

func TestPurgeDeviceEventsRequestsAResync(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	devID, devToken := h.newDevice("mac")
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	if got := h.ingest(devID, devToken, sampleEvent("s1", devID, ts, 10, 20)); got.Resync {
		t.Fatal("a device nobody purged was told to resync")
	}

	w := h.do(http.MethodDelete, "/api/v1/devices/"+devID+"/events", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("purge events = %d %s", w.Code, w.Body)
	}
	var res struct {
		ID              string `json:"id"`
		DeletedEvents   int64  `json:"deleted_events"`
		ResyncRequested bool   `json:"resync_requested"`
	}
	h.decode(w, &res)
	if res.ID != devID || res.DeletedEvents != 1 || !res.ResyncRequested {
		t.Fatalf("purge events response = %+v", res)
	}

	// The device and its token survive: it is still listed, just empty.
	var devs struct{ Devices []store.Device }
	h.decode(h.do(http.MethodGet, "/api/v1/devices", adminToken, nil), &devs)
	if len(devs.Devices) != 1 || devs.Devices[0].EventsTotal != 0 || devs.Devices[0].RevokedAt != nil {
		t.Fatalf("devices after purging events = %+v", devs.Devices)
	}

	// A heartbeat (empty batch) is enough to pick the request up, and it is
	// delivered exactly once.
	got := h.ingest(devID, devToken)
	if !got.Resync {
		t.Fatal("the heartbeat after a purge did not carry resync")
	}
	if got := h.ingest(devID, devToken, sampleEvent("s1", devID, ts, 10, 20)); got.Resync {
		t.Fatal("resync was served twice")
	}
	// And the re-sent history lands again, unchanged.
	h.decode(h.do(http.MethodGet, "/api/v1/devices", adminToken, nil), &devs)
	if devs.Devices[0].EventsTotal != 1 {
		t.Fatalf("events after the resync = %d, want 1", devs.Devices[0].EventsTotal)
	}

	if w := h.do(http.MethodDelete, "/api/v1/devices/nope/events", adminToken, nil); w.Code != http.StatusNotFound {
		t.Errorf("purge events of an unknown device = %d, want 404", w.Code)
	}
	if w := h.do(http.MethodDelete, "/api/v1/devices/"+devID+"/events", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("purge events without the admin token = %d, want 401", w.Code)
	}
}
