package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/store"
)

const adminToken = "admin-secret"

const testPricingYAML = `
- match: "claude-opus-5"
  provider: anthropic
  input: 5
  output: 25
  cache_read: 0.5
  cache_write_5m: 6.25
`

type harness struct {
	t        *testing.T
	h        http.Handler
	store    *store.Store
	dir      string
	priceCfg string
}

func newHarness(t *testing.T, opt Options) *harness {
	t.Helper()
	dir := t.TempDir()
	priceCfg := filepath.Join(dir, "pricing.yaml")
	if err := os.WriteFile(priceCfg, []byte(testPricingYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	pr, err := NewPricing(priceCfg)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if opt.Version == "" {
		opt.Version = "test"
	}
	if opt.DownloadsDir == "" {
		opt.DownloadsDir = filepath.Join(dir, "downloads")
		if err := os.MkdirAll(opt.DownloadsDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &harness{t: t, h: New(st, pr, opt), store: st, dir: dir, priceCfg: priceCfg}
}

func (h *harness) do(method, path, token string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rdr = strings.NewReader(b)
	case io.Reader:
		rdr = b
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			h.t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.h.ServeHTTP(w, req)
	return w
}

func (h *harness) decode(w *httptest.ResponseRecorder, v any) {
	h.t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		h.t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
}

// newDevice registers a device through the admin API and returns id + token.
func (h *harness) newDevice(name string) (string, string) {
	h.t.Helper()
	w := h.do(http.MethodPost, "/api/v1/devices", adminToken, map[string]string{"name": name})
	if w.Code != http.StatusCreated {
		h.t.Fatalf("create device = %d %s", w.Code, w.Body)
	}
	var res struct{ ID, Name, Token string }
	h.decode(w, &res)
	if res.ID == "" || !strings.HasPrefix(res.Token, "aum_") {
		h.t.Fatalf("create device response = %s", w.Body)
	}
	return res.ID, res.Token
}

func TestAdminAuth(t *testing.T) {
	// No admin token configured: every admin route is 503, not 401.
	unset := newHarness(t, Options{})
	for _, path := range []string{"/api/v1/summary", "/api/v1/devices", "/api/v1/pricing"} {
		w := unset.do(http.MethodGet, path, "anything", nil)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s without ADMIN_TOKEN = %d, want 503", path, w.Code)
		}
		var e struct{ Error string }
		unset.decode(w, &e)
		if e.Error != "admin token not configured" {
			t.Errorf("%s error = %q", path, e.Error)
		}
	}

	h := newHarness(t, Options{AdminToken: adminToken})
	if w := h.do(http.MethodGet, "/api/v1/summary", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", w.Code)
	}
	if w := h.do(http.MethodGet, "/api/v1/summary", "wrong", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", w.Code)
	}
	w := h.do(http.MethodGet, "/api/v1/summary", adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("admin token = %d %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	// A device token must not open the admin API, and the admin token must not
	// open ingest.
	_, devToken := h.newDevice("mac")
	if w := h.do(http.MethodGet, "/api/v1/summary", devToken, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("device token on admin route = %d, want 401", w.Code)
	}
	if w := h.do(http.MethodPost, "/api/v1/ingest", adminToken, map[string]any{}); w.Code != http.StatusUnauthorized {
		t.Errorf("admin token on ingest = %d, want 401", w.Code)
	}
	if w := h.do(http.MethodPost, "/api/v1/ingest", "", map[string]any{}); w.Code != http.StatusUnauthorized {
		t.Errorf("no token on ingest = %d, want 401", w.Code)
	}
}

func sampleEvent(id, device string, ts time.Time, in, out int64) event.UsageEvent {
	return event.UsageEvent{
		EventID:    id,
		DeviceID:   device,
		Source:     event.SourceClaudeCode,
		Timestamp:  ts,
		Model:      "claude-opus-5",
		SessionID:  "s1",
		Project:    "/work/app",
		Input:      in,
		Output:     out,
		Confidence: event.ConfidenceExact,
	}
}

func TestIngest(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	devID, devToken := h.newDevice("mac")
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	body := map[string]any{
		"device": map[string]string{"id": devID, "name": "mac", "os": "darwin", "arch": "arm64", "agent_version": "0.2.0"},
		"events": []event.UsageEvent{
			sampleEvent("a1", devID, ts, 1_000_000, 100_000),
			sampleEvent("a2", devID, ts.Add(time.Hour), 500_000, 0),
			{EventID: "bad", DeviceID: devID, Source: event.SourceClaudeCode}, // no ts / model -> rejected
			sampleEvent("a3", "someone-else", ts, 10, 10),                     // foreign device id -> remapped to the token's device
		},
	}
	w := h.do(http.MethodPost, "/api/v1/ingest", devToken, body)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest = %d %s", w.Code, w.Body)
	}
	var res ingestResponse
	h.decode(w, &res)
	if res.Accepted != 3 || res.Duplicates != 0 || res.Rejected != 1 || res.DeviceID != devID {
		t.Fatalf("ingest = %+v, want accepted 3 duplicates 0 rejected 1 device %s", res, devID)
	}

	// Replaying the batch is idempotent.
	w = h.do(http.MethodPost, "/api/v1/ingest", devToken, body)
	h.decode(w, &res)
	if res.Accepted != 0 || res.Duplicates != 3 {
		t.Fatalf("replay = %+v, want 0 accepted / 3 duplicates", res)
	}

	// The heartbeat and the self-reported metadata landed.
	w = h.do(http.MethodGet, "/api/v1/devices", adminToken, nil)
	var devs struct{ Devices []store.Device }
	h.decode(w, &devs)
	if len(devs.Devices) != 1 {
		t.Fatalf("devices = %+v", devs.Devices)
	}
	d := devs.Devices[0]
	if d.OS != "darwin" || d.Arch != "arm64" || d.AgentVersion != "0.2.0" || d.EventsTotal != 3 {
		t.Fatalf("device after ingest = %+v", d)
	}
	if d.LastSeen == nil || d.LastIngest == nil {
		t.Fatalf("heartbeat missing: %+v", d)
	}

	// The token is the identity: a batch claiming another device id is still
	// stored under the token's device, and the response says which id that is.
	otherID, _ := h.newDevice("linux")
	mismatch := map[string]any{
		"device": map[string]string{"id": otherID},
		"events": []event.UsageEvent{},
	}
	if w := h.do(http.MethodPost, "/api/v1/ingest", devToken, mismatch); w.Code != http.StatusOK {
		t.Errorf("device mismatch = %d, want 200", w.Code)
	} else {
		var out struct {
			DeviceID string `json:"device_id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if out.DeviceID == "" || out.DeviceID == otherID {
			t.Errorf("ingest device_id = %q, want the token's device", out.DeviceID)
		}
	}
	if w := h.do(http.MethodPost, "/api/v1/ingest", devToken, map[string]any{"events": []any{}}); w.Code != http.StatusOK {
		t.Errorf("missing device.id = %d, want 200", w.Code)
	}
	if w := h.do(http.MethodGet, "/api/v1/agent/whoami", devToken, nil); w.Code != http.StatusOK {
		t.Errorf("whoami = %d, want 200", w.Code)
	}
	if w := h.do(http.MethodPost, "/api/v1/ingest", devToken, "{not json"); w.Code != http.StatusBadRequest {
		t.Errorf("bad JSON = %d, want 400", w.Code)
	}

	// Oversized batch.
	big := make([]event.UsageEvent, MaxBatchEvents+1)
	for i := range big {
		big[i] = sampleEvent(fmt.Sprintf("b%d", i), devID, ts, 1, 1)
	}
	w = h.do(http.MethodPost, "/api/v1/ingest", devToken, map[string]any{
		"device": map[string]string{"id": devID}, "events": big,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("oversized batch = %d, want 400", w.Code)
	}

	// Oversized body (> 8 MiB) is cut off by MaxBytesReader.
	var huge bytes.Buffer
	huge.WriteString(`{"device":{"id":"` + devID + `","name":"`)
	huge.WriteString(strings.Repeat("x", MaxBodyBytes+1024))
	huge.WriteString(`"},"events":[]}`)
	if w := h.do(http.MethodPost, "/api/v1/ingest", devToken, &huge); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body = %d, want 413", w.Code)
	}

	// Revoking the token locks the agent out immediately (cache is invalidated).
	if w := h.do(http.MethodDelete, "/api/v1/devices/"+devID, adminToken, nil); w.Code != http.StatusOK {
		t.Fatalf("revoke = %d %s", w.Code, w.Body)
	}
	if w := h.do(http.MethodPost, "/api/v1/ingest", devToken, body); w.Code != http.StatusUnauthorized {
		t.Errorf("ingest after revoke = %d, want 401", w.Code)
	}
	if w := h.do(http.MethodDelete, "/api/v1/devices/nope", adminToken, nil); w.Code != http.StatusNotFound {
		t.Errorf("revoke unknown = %d, want 404", w.Code)
	}
}

func TestQueryEndpoints(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken})
	devID, devToken := h.newDevice("mac")
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	w := h.do(http.MethodPost, "/api/v1/ingest", devToken, map[string]any{
		"device": map[string]string{"id": devID, "name": "mac"},
		"events": []event.UsageEvent{
			sampleEvent("a1", devID, ts, 1_000_000, 100_000),    // $5.00 + $2.50
			sampleEvent("a2", devID, ts.Add(6*time.Hour), 0, 0), // KST next day
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("ingest = %d %s", w.Code, w.Body)
	}

	rng := "from=2026-09-01&to=2026-09-03"
	w = h.do(http.MethodGet, "/api/v1/summary?"+rng, adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("summary = %d %s", w.Code, w.Body)
	}
	var sum store.Summary
	h.decode(w, &sum)
	if sum.Totals.Events != 2 || sum.Totals.Input != 1_000_000 || sum.Totals.Output != 100_000 {
		t.Fatalf("summary totals = %+v", sum.Totals)
	}
	if sum.Totals.CostUSD != 7.5 || sum.Totals.CostKnownRatio != 1 {
		t.Fatalf("summary cost = %+v", sum.Totals)
	}
	if len(sum.BySource) != 1 || sum.BySource[0].Key != "claude_code" {
		t.Fatalf("by_source = %+v", sum.BySource)
	}
	if len(sum.ByDevice) != 1 || sum.ByDevice[0].Key != devID || sum.ByDevice[0].Name != "mac" {
		t.Fatalf("by_device = %+v", sum.ByDevice)
	}
	if len(sum.ByModel) != 1 || sum.ByModel[0].Key != "claude-opus-5" || sum.ByModel[0].Source != "claude_code" {
		t.Fatalf("by_model = %+v", sum.ByModel)
	}
	// Raw JSON must carry the documented snake_case keys.
	var raw map[string]json.RawMessage
	h.decode(w, &raw)
	for _, k := range []string{"from", "to", "totals", "by_source", "by_device", "by_model"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("summary JSON missing %q: %s", k, w.Body)
		}
	}

	// Default tz is Asia/Seoul, so 16:00Z belongs to the next day.
	w = h.do(http.MethodGet, "/api/v1/timeseries?"+rng+"&bucket=day&group_by=source", adminToken, nil)
	var ts1 store.Timeseries
	h.decode(w, &ts1)
	if ts1.TZ != DefaultTZ || len(ts1.Series) != 1 || len(ts1.Series[0].Points) != 2 {
		t.Fatalf("timeseries = %+v", ts1)
	}
	if ts1.Series[0].Points[0].T != "2026-09-01" || ts1.Series[0].Points[1].T != "2026-09-02" {
		t.Fatalf("timeseries points = %+v", ts1.Series[0].Points)
	}
	if w := h.do(http.MethodGet, "/api/v1/timeseries?bucket=week", adminToken, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad bucket = %d, want 400", w.Code)
	}
	if w := h.do(http.MethodGet, "/api/v1/timeseries?group_by=nope", adminToken, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad group_by = %d, want 400", w.Code)
	}
	if w := h.do(http.MethodGet, "/api/v1/summary?tz=Mars/Olympus", adminToken, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad tz = %d, want 400", w.Code)
	}
	if w := h.do(http.MethodGet, "/api/v1/summary?from=yesterday", adminToken, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad from = %d, want 400", w.Code)
	}

	w = h.do(http.MethodGet, "/api/v1/breakdown?"+rng+"&group_by=model", adminToken, nil)
	var bd store.Breakdown
	h.decode(w, &bd)
	if bd.GroupBy != "model" || len(bd.Rows) != 1 || bd.Rows[0].CostUSD != 7.5 {
		t.Fatalf("breakdown = %+v", bd)
	}

	w = h.do(http.MethodGet, "/api/v1/events?"+rng+"&limit=1", adminToken, nil)
	var page store.EventPage
	h.decode(w, &page)
	if len(page.Events) != 1 || page.NextCursor == "" {
		t.Fatalf("events page = %+v", page)
	}
	w = h.do(http.MethodGet, "/api/v1/events?"+rng+"&limit=1&cursor="+page.NextCursor, adminToken, nil)
	var page2 store.EventPage
	h.decode(w, &page2)
	if len(page2.Events) != 1 || page2.Events[0].EventID == page.Events[0].EventID {
		t.Fatalf("second page = %+v", page2)
	}
	if page2.NextCursor != "" {
		t.Errorf("expected the last page, got cursor %q", page2.NextCursor)
	}
	if w := h.do(http.MethodGet, "/api/v1/events?cursor=%21%21", adminToken, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad cursor = %d, want 400", w.Code)
	}
}

func TestHealthAndPricing(t *testing.T) {
	h := newHarness(t, Options{AdminToken: adminToken, Version: "1.2.3"})

	w := h.do(http.MethodGet, "/health", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("health = %d", w.Code)
	}
	var health struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Events  int64  `json:"events"`
	}
	h.decode(w, &health)
	if !health.OK || health.Version != "1.2.3" || health.Events != 0 {
		t.Fatalf("health = %+v", health)
	}

	w = h.do(http.MethodGet, "/api/v1/pricing", adminToken, nil)
	var rates struct{ Rates []rateView }
	h.decode(w, &rates)
	if len(rates.Rates) != 1 || rates.Rates[0].Match != "claude-opus-5" || *rates.Rates[0].Input != 5 {
		t.Fatalf("pricing = %+v", rates.Rates)
	}

	// Hot reload picks up an edited file.
	if err := os.WriteFile(h.priceCfg, []byte(testPricingYAML+"\n- match: \"gpt-5.6-*\"\n  input: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := h.do(http.MethodPost, "/api/v1/pricing/reload", adminToken, nil); w.Code != http.StatusOK {
		t.Fatalf("reload = %d %s", w.Code, w.Body)
	}
	w = h.do(http.MethodGet, "/api/v1/pricing", adminToken, nil)
	h.decode(w, &rates)
	if len(rates.Rates) != 2 {
		t.Fatalf("after reload = %+v", rates.Rates)
	}
	// A broken file leaves the previous table in place.
	if err := os.WriteFile(h.priceCfg, []byte("- nope: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := h.do(http.MethodPost, "/api/v1/pricing/reload", adminToken, nil); w.Code != http.StatusBadRequest {
		t.Errorf("broken reload = %d, want 400", w.Code)
	}
	w = h.do(http.MethodGet, "/api/v1/pricing", adminToken, nil)
	h.decode(w, &rates)
	if len(rates.Rates) != 2 {
		t.Fatalf("rates changed after a failed reload: %+v", rates.Rates)
	}
}

func TestDownloads(t *testing.T) {
	dir := t.TempDir()
	man := `{"version":"v0.3.0","built_at":"2026-09-02T00:00:00Z","files":[
	  {"name":"ai-usage-agent_linux_amd64","os":"linux","arch":"amd64","size":9437184,"sha256":"deadbeef"},
	  {"name":"ai-usage-agent_windows_amd64.exe","os":"windows","arch":"amd64","size":100,"sha256":"cafe"}]}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ai-usage-agent_linux_amd64"), []byte("ELF..."), 0o755); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Options{AdminToken: adminToken, DownloadsDir: dir})

	w := h.do(http.MethodGet, "/downloads/", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("downloads index = %d", w.Code)
	}
	var m manifest
	h.decode(w, &m)
	if m.Version != "v0.3.0" || len(m.Files) != 2 || m.Files[0].SHA256 != "deadbeef" {
		t.Fatalf("manifest = %+v", m)
	}

	// A browser gets the HTML table.
	req := httptest.NewRequest(http.MethodGet, "/downloads/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Host = "usage.example.com"
	hw := httptest.NewRecorder()
	h.h.ServeHTTP(hw, req)
	if ct := hw.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("html content type = %q", ct)
	}
	for _, want := range []string{"ai-usage-agent_linux_amd64", "deadbeef", "9.0 MiB", "http://usage.example.com/downloads/install.sh"} {
		if !strings.Contains(hw.Body.String(), want) {
			t.Errorf("HTML listing missing %q", want)
		}
	}

	if w := h.do(http.MethodGet, "/downloads/manifest.json", "", nil); w.Code != http.StatusOK ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("manifest passthrough = %d %q", w.Code, w.Header().Get("Content-Type"))
	}

	w = h.do(http.MethodGet, "/downloads/ai-usage-agent_linux_amd64", "", nil)
	if w.Code != http.StatusOK || w.Body.String() != "ELF..." {
		t.Fatalf("binary = %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "ai-usage-agent_linux_amd64") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	// install.sh is generated when the directory ships none.
	w = h.do(http.MethodGet, "/downloads/install.sh", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("install.sh = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"#!/bin/sh", "ai-usage-agent_${os}_${arch}", "/usr/local/bin", "$HOME/.local/bin", "install --server"} {
		if !strings.Contains(body, want) {
			t.Errorf("generated install.sh missing %q", want)
		}
	}
	// A static install.sh wins.
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\necho static\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	w = h.do(http.MethodGet, "/downloads/install.sh", "", nil)
	if !strings.Contains(w.Body.String(), "echo static") {
		t.Errorf("static install.sh not served: %q", w.Body.String())
	}

	if w := h.do(http.MethodGet, "/downloads/nope", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing file = %d, want 404", w.Code)
	}
	if w := h.do(http.MethodGet, "/downloads/..%2fpricing.yaml", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("traversal = %d, want 404", w.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	// Without a build, "/" explains how to build it and the API still answers.
	nob := newHarness(t, Options{AdminToken: adminToken})
	w := nob.do(http.MethodGet, "/", "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "not built") {
		t.Fatalf("unbuilt SPA = %d %s", w.Code, w.Body)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("content type = %q", w.Header().Get("Content-Type"))
	}

	dist := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>dashboard</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		"favicon.svg":   &fstest.MapFile{Data: []byte("<svg/>")},
	}
	h := newHarness(t, Options{AdminToken: adminToken, SPA: dist, SPABuilt: true})

	for _, path := range []string{"/", "/devices", "/models/claude-opus-5"} {
		w := h.do(http.MethodGet, path, "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dashboard") {
			t.Errorf("%s = %d %q, want index.html", path, w.Code, w.Body.String())
		}
	}
	w = h.do(http.MethodGet, "/assets/app.js", "", nil)
	if w.Code != http.StatusOK || w.Body.String() != "console.log(1)" {
		t.Fatalf("asset = %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("asset content type = %q", ct)
	}
	// Unknown API/download paths must 404 as JSON, never as index.html.
	for _, path := range []string{"/api/v1/nope", "/api/v2/summary", "/downloads/x/y"} {
		w := h.do(http.MethodGet, path, adminToken, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "dashboard") {
			t.Errorf("%s fell back to the SPA", path)
		}
	}
}
