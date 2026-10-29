package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
)

// A small gzip body that expands to megabytes of tiny events is refused
// without being decoded in full: by its decompressed size, or by the event
// count once the 5,001st event is reached.
func TestIngestRefusesBombsEarly(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	_, tok := h.newDevice("mac")
	post := func(raw []byte) int {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write(raw)
		zw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", &buf)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Encoding", "gzip")
		w := httptest.NewRecorder()
		h.h.ServeHTTP(w, req)
		return w.Code
	}
	var many strings.Builder
	many.WriteString(`{"events":[`)
	for i := range MaxBatchEvents + 1 {
		if i > 0 {
			many.WriteByte(',')
		}
		many.WriteString(`{}`)
	}
	many.WriteString(`]}`)
	if code := post([]byte(many.String())); code != http.StatusBadRequest {
		t.Errorf("5,001 events: %d, want 400", code)
	}
	huge := append([]byte(`{"events":[],"pad":"`), bytes.Repeat([]byte("a"), MaxBodyBytes*2+1)...)
	huge = append(huge, `"}`...)
	if code := post(huge); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized decompressed body: %d, want 413", code)
	}
}

// One device token must not be able to rewrite another device's stored events.
func TestIngestCannotTouchAnotherDevicesEvents(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	aID, aTok := h.newDevice("a")
	bID, bTok := h.newDevice("b")
	ts := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	h.ingest(bID, bTok, sampleEvent("shared-id", bID, ts, 10, 5))
	h.ingest(aID, aTok, sampleEvent("shared-id", aID, ts, 10, 9_000_000_000))

	var out int64
	if err := h.store.DB().QueryRowContext(context.Background(), `SELECT output FROM usage_events WHERE event_id = 'shared-id'`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	if out != 5 {
		t.Errorf("device B's event was rewritten by device A: output %d", out)
	}
}

// Fields a device token holder controls are bounded.
func TestIngestBoundsStrings(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	id, tok := h.newDevice("mac")
	e := sampleEvent("long-project", id, time.Now().UTC(), 1, 1)
	e.Project = strings.Repeat("p", event.MaxProjectLen+1)
	if res := h.ingest(id, tok, e); res.Accepted != 0 || res.Rejected != 1 {
		t.Errorf("an over-long project was stored: %+v", res)
	}
	w := h.do(http.MethodPost, "/api/v1/ingest", tok, map[string]any{
		"device": map[string]string{"id": id, "agent_version": strings.Repeat("v", 10_000)},
		"events": []event.UsageEvent{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("ingest = %d", w.Code)
	}
	list := h.do(http.MethodGet, "/api/v1/devices", adminToken, nil)
	if strings.Contains(list.Body.String(), strings.Repeat("v", 300)) {
		t.Error("an over-long agent_version reached the device list")
	}
}

// The streaming decoder accepts what encoding/json did: null events, unknown
// keys, keys in any order.
func TestIngestDecoderCompat(t *testing.T) {
	for _, body := range []string{
		`{"device":{"id":"x"},"events":null}`,
		`{"events":[],"device":{"id":"x"},"extra":{"nested":[1,2]}}`,
		`{"device":{"id":"x"}}`,
	} {
		if _, err := decodeIngest(strings.NewReader(body)); err != nil {
			t.Errorf("%s: %v", body, err)
		}
	}
	for _, body := range []string{`[]`, `{"events":{}}`, `{"events":[1]}`, `{"events":[]`} {
		if _, err := decodeIngest(strings.NewReader(body)); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

// An id that holds NUL or invalid UTF-8 is rejected per event (cleaning it
// could merge two events); free text with such bytes is stored cleaned.
func TestIngestUncleanText(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	id, tok := h.newDevice("mac")
	ts := time.Now().UTC()
	badID := sampleEvent("id\x00nul", id, ts, 1, 1)
	badSession := sampleEvent("ok-session", id, ts, 1, 1)
	badSession.SessionID = "s\x00"
	text := sampleEvent("ok-text", id, ts, 1, 1)
	text.Project = "/p\x00q"
	res := h.ingest(id, tok, badID, badSession, text)
	if res.Accepted != 1 || res.Rejected != 2 {
		t.Fatalf("ingest = %+v, want 1 accepted, 2 rejected", res)
	}
	var project string
	if err := h.store.DB().QueryRowContext(context.Background(), `SELECT project FROM usage_events WHERE event_id = 'ok-text'`).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if project != "/p�q" {
		t.Errorf("project stored as %q", project)
	}
}
