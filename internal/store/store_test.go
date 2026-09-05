package store

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"path/filepath"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/pricing"
)

const testPricing = `
- match: "claude-opus-5"
  input: 5
  output: 25
  cache_read: 0.5
  cache_write_5m: 6.25
- match: "gpt-5.6-*"
  input: 2
  output: 8
- match: "gemini-3.7-flash"
  input: 1
  output: 4
- match: "gemini-default"
  alias_of: "gemini-3.7-flash"
`

func testTable(t *testing.T) *pricing.Table {
	t.Helper()
	tb, err := pricing.Parse([]byte(testPricing))
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts.UTC()
}

func TestMigrationsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Re-running migrate on the same handle must be a no-op.
	if err := s.migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if _, _, err := s.CreateDevice(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopening an existing database must not re-apply anything either.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	devs, err := s2.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 {
		t.Fatalf("devices after reopen = %d, want 1", len(devs))
	}
	// Exactly one row per embedded migration, however many there are by now.
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(files) {
		t.Fatalf("schema_migrations rows = %d, want %d (one per migrations/*.sql)", n, len(files))
	}
}

func TestDeviceLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	d, token, err := s.CreateDevice(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 20 || token[:4] != TokenPrefix {
		t.Fatalf("token %q has no aum_ prefix", token)
	}
	got, err := s.DeviceByTokenHash(ctx, HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != d.ID || got.Name != "macbook" {
		t.Fatalf("lookup = %+v", got)
	}

	now := mustTime(t, "2026-09-02T00:00:00Z")
	if err := s.TouchDevice(ctx, DeviceMeta{ID: d.ID, OS: "darwin", Arch: "arm64", AgentVersion: "0.1.0"}, true, now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Device(ctx, d.ID)
	if got.Name != "macbook" || got.OS != "darwin" || got.Arch != "arm64" || got.AgentVersion != "0.1.0" {
		t.Fatalf("after touch = %+v", got)
	}
	if got.LastSeen == nil || !got.LastSeen.Equal(now) || got.LastIngest == nil {
		t.Fatalf("heartbeat not recorded: %+v", got)
	}

	if err := s.RevokeDevice(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeviceByTokenHash(ctx, HashToken(token)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token still resolves: %v", err)
	}
	if err := s.RevokeDevice(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown = %v, want ErrNotFound", err)
	}
	// Events survive revocation.
	if _, err := s.Device(ctx, d.ID); err != nil {
		t.Fatalf("device row removed: %v", err)
	}
}

func ev(t *testing.T, id, device string, src event.Source, ts, model string, in, out, cr, cw5 int64) event.UsageEvent {
	t.Helper()
	return event.UsageEvent{
		EventID:      id,
		DeviceID:     device,
		Source:       src,
		Timestamp:    mustTime(t, ts),
		Model:        model,
		Project:      "/work/" + model,
		SessionID:    "sess-" + id,
		Input:        in,
		Output:       out,
		CacheRead:    cr,
		CacheWrite5m: cw5,
		Confidence:   event.ConfidenceExact,
	}
}

// seed builds the fixture every aggregate test asserts against:
//
//	e1 d1 claude_code claude-opus-5   2026-09-01T10:00Z  in 1M  out 100k  cr 2M  cw5 500k -> $11.625
//	e2 d1 claude_code claude-opus-5   2026-09-01T15:30Z  in 500k                          -> $2.50
//	e3 d1 codex       gpt-5.6-terra   2026-09-02T01:00Z  in 1M  out 200k                  -> $3.60
//	e4 d2 antigravity gemini-default  2026-09-02T02:00Z  in 2M  out 100k                  -> $2.40 (estimated)
//	e5 d2 antigravity unpriced-model  2026-09-02T03:00Z  in 1M                            -> unknown
func seed(t *testing.T, s *Store) (d1, d2 string) {
	t.Helper()
	ctx := context.Background()
	a, _, err := s.CreateDevice(ctx, "mac")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreateDevice(ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	evs := []event.UsageEvent{
		ev(t, "e1", a.ID, event.SourceClaudeCode, "2026-09-01T10:00:00Z", "claude-opus-5", 1_000_000, 100_000, 2_000_000, 500_000),
		ev(t, "e2", a.ID, event.SourceClaudeCode, "2026-09-01T15:30:00Z", "claude-opus-5", 500_000, 0, 0, 0),
		ev(t, "e3", a.ID, event.SourceCodex, "2026-09-02T01:00:00Z", "gpt-5.6-terra", 1_000_000, 200_000, 0, 0),
		ev(t, "e4", b.ID, event.SourceAntigravity, "2026-09-02T02:00:00Z", "gemini-default", 2_000_000, 100_000, 0, 0),
		ev(t, "e5", b.ID, event.SourceAntigravity, "2026-09-02T03:00:00Z", "unpriced-model", 1_000_000, 0, 0, 0),
	}
	acc, dup, _, err := s.InsertEvents(ctx, evs)
	if err != nil {
		t.Fatal(err)
	}
	if acc != 5 || dup != 0 {
		t.Fatalf("seed insert = %d/%d, want 5/0", acc, dup)
	}
	return a.ID, b.ID
}

func TestInsertDedupe(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d1, _ := seed(t, s)

	again := []event.UsageEvent{
		ev(t, "e1", d1, event.SourceClaudeCode, "2026-09-01T10:00:00Z", "claude-opus-5", 999, 999, 0, 0),
		ev(t, "e9", d1, event.SourceClaudeCode, "2026-09-03T10:00:00Z", "claude-opus-5", 10, 10, 0, 0),
	}
	acc, dup, _, err := s.InsertEvents(ctx, again)
	if err != nil {
		t.Fatal(err)
	}
	if acc != 1 || dup != 1 {
		t.Fatalf("re-insert = %d accepted / %d duplicates, want 1/1", acc, dup)
	}
	// The duplicate must not have overwritten the original values.
	var in int64
	if err := s.DB().QueryRow(`SELECT input FROM usage_events WHERE event_id='e1'`).Scan(&in); err != nil {
		t.Fatal(err)
	}
	if in != 1_000_000 {
		t.Fatalf("duplicate overwrote the first value: input=%d", in)
	}
	c, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Events != 6 || c.Devices != 2 {
		t.Fatalf("counts = %+v", c)
	}
	if n, d, u, err := s.InsertEvents(ctx, nil); n != 0 || d != 0 || u != 0 || err != nil {
		t.Fatalf("empty batch = %d/%d/%v", n, d, err)
	}
}

func approx(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestSummary(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d1, d2 := seed(t, s)
	f := Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-03T00:00:00Z")}

	sum, err := s.Summary(ctx, f, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Totals.Events != 5 {
		t.Fatalf("events = %d, want 5", sum.Totals.Events)
	}
	if sum.Totals.Input != 5_500_000 || sum.Totals.Output != 400_000 ||
		sum.Totals.CacheRead != 2_000_000 || sum.Totals.CacheWrite != 500_000 {
		t.Fatalf("totals = %+v", sum.Totals)
	}
	approx(t, "totals.cost_usd", sum.Totals.CostUSD, 20.125)
	approx(t, "totals.cost_known_ratio", sum.Totals.CostKnownRatio, round6(7_400_000.0/8_400_000.0))

	bySource := map[string]Group{}
	for _, g := range sum.BySource {
		bySource[g.Key] = g
	}
	if len(bySource) != 3 {
		t.Fatalf("by_source keys = %v", bySource)
	}
	approx(t, "by_source[claude_code]", bySource["claude_code"].CostUSD, 14.125)
	approx(t, "by_source[codex]", bySource["codex"].CostUSD, 3.6)
	approx(t, "by_source[antigravity]", bySource["antigravity"].CostUSD, 2.4)
	if bySource["claude_code"].Events != 2 {
		t.Errorf("claude_code events = %d, want 2", bySource["claude_code"].Events)
	}
	if sum.BySource[0].Key != "claude_code" {
		t.Errorf("by_source not sorted by cost: %v", sum.BySource)
	}

	byDevice := map[string]Group{}
	for _, g := range sum.ByDevice {
		byDevice[g.Key] = g
	}
	approx(t, "by_device[d1]", byDevice[d1].CostUSD, 17.725)
	approx(t, "by_device[d2]", byDevice[d2].CostUSD, 2.4)
	if byDevice[d1].Name != "mac" || byDevice[d2].Name != "linux" {
		t.Errorf("device names missing: %+v", sum.ByDevice)
	}

	byModel := map[string]Group{}
	for _, g := range sum.ByModel {
		byModel[g.Key] = g
	}
	if len(byModel) != 4 {
		t.Fatalf("by_model = %+v", sum.ByModel)
	}
	approx(t, "by_model[claude-opus-5]", byModel["claude-opus-5"].CostUSD, 14.125)
	if byModel["claude-opus-5"].Source != "claude_code" {
		t.Errorf("model row lost its source: %+v", byModel["claude-opus-5"])
	}
	if !byModel["gemini-default"].Estimated {
		t.Errorf("alias-priced model should be estimated: %+v", byModel["gemini-default"])
	}
	if byModel["unpriced-model"].CostUSD != 0 || byModel["unpriced-model"].CostKnownRatio != 0 {
		t.Errorf("unpriced model = %+v, want zero cost and ratio", byModel["unpriced-model"])
	}

	// A device filter narrows every section.
	only, err := s.Summary(ctx, Filter{From: f.From, To: f.To, Device: d2}, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if only.Totals.Events != 2 || len(only.ByDevice) != 1 {
		t.Fatalf("device filter = %+v", only.Totals)
	}
}

func TestBreakdown(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, d2 := seed(t, s)
	f := Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-03T00:00:00Z")}

	b, err := s.Breakdown(ctx, f, "model", 2, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Rows) != 2 {
		t.Fatalf("limit ignored: %d rows", len(b.Rows))
	}
	if b.Rows[0].Key != "claude-opus-5" || b.Rows[1].Key != "gpt-5.6-terra" {
		t.Fatalf("rows not ordered by cost: %+v", b.Rows)
	}
	approx(t, "rows[0].cost", b.Rows[0].CostUSD, 14.125)
	if b.Rows[0].FirstSeen == nil || !b.Rows[0].FirstSeen.Equal(mustTime(t, "2026-09-01T10:00:00Z")) {
		t.Errorf("first_seen = %v", b.Rows[0].FirstSeen)
	}
	if b.Rows[0].LastSeen == nil || !b.Rows[0].LastSeen.Equal(mustTime(t, "2026-09-01T15:30:00Z")) {
		t.Errorf("last_seen = %v", b.Rows[0].LastSeen)
	}

	byProject, err := s.Breakdown(ctx, f, "project", 50, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(byProject.Rows) != 4 {
		t.Fatalf("project rows = %+v", byProject.Rows)
	}

	bySession, err := s.Breakdown(ctx, Filter{From: f.From, To: f.To, Device: d2}, "session", 50, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession.Rows) != 2 || bySession.Rows[0].Device != d2 {
		t.Fatalf("session rows = %+v", bySession.Rows)
	}

	byDevice, err := s.Breakdown(ctx, f, "device", 50, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if byDevice.Rows[0].Name == "" {
		t.Errorf("device breakdown has no name: %+v", byDevice.Rows[0])
	}
	if _, err := s.Breakdown(ctx, f, "bogus", 50, nil); err == nil {
		t.Error("bogus group_by accepted")
	}
}

func TestTimeseriesKSTDayBoundary(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	seed(t, s)
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// KST 2026-09-01 00:00 .. 2026-09-04 00:00
	f := Filter{From: mustTime(t, "2026-08-31T15:00:00Z"), To: mustTime(t, "2026-09-03T15:00:00Z")}

	ts, err := s.Timeseries(ctx, f, "day", "source", kst, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	series := map[string]Series{}
	for _, ser := range ts.Series {
		series[ser.Key] = ser
	}
	cc := series["claude_code"]
	if len(cc.Points) != 2 {
		t.Fatalf("claude_code points = %+v", cc.Points)
	}
	// 2026-09-01T15:30Z is 2026-09-02 00:30 KST: it must land on the *next* day.
	if cc.Points[0].T != "2026-09-01" || cc.Points[1].T != "2026-09-02" {
		t.Fatalf("day labels = %q, %q", cc.Points[0].T, cc.Points[1].T)
	}
	if cc.Points[0].Input != 1_000_000 || cc.Points[1].Input != 500_000 {
		t.Fatalf("tokens on the wrong side of KST midnight: %+v", cc.Points)
	}
	approx(t, "kst day 1 cost", cc.Points[0].CostUSD, 11.625)
	approx(t, "kst day 2 cost", cc.Points[1].CostUSD, 2.5)

	// The same range bucketed in UTC keeps both events on 2026-09-01.
	utc, err := s.Timeseries(ctx, f, "day", "source", time.UTC, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, ser := range utc.Series {
		if ser.Key == "claude_code" {
			if len(ser.Points) != 1 || ser.Points[0].T != "2026-09-01" || ser.Points[0].Events != 2 {
				t.Fatalf("utc bucketing = %+v", ser.Points)
			}
		}
	}

	hourly, err := s.Timeseries(ctx, f, "hour", "model", kst, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, ser := range hourly.Series {
		if ser.Key == "claude-opus-5" && len(ser.Points) != 2 {
			t.Fatalf("hourly points = %+v", ser.Points)
		}
	}
	if _, err := s.Timeseries(ctx, f, "week", "source", kst, nil); err == nil {
		t.Error("bogus bucket accepted")
	}
}

// A zone with DST must still bucket by local day: the spring-forward day is only
// 23 hours long in UTC and the segment logic has to follow the offset change.
func TestTimeseriesDSTDayBoundary(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	d, _, err := s.CreateDevice(ctx, "ny")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-08 is the US spring-forward date (02:00 EST -> 03:00 EDT).
	evs := []event.UsageEvent{
		ev(t, "n1", d.ID, event.SourceCodex, "2026-03-08T06:30:00Z", "gpt-5.6-terra", 10, 0, 0, 0), // 01:30 EST, Mar 8
		ev(t, "n2", d.ID, event.SourceCodex, "2026-03-08T20:00:00Z", "gpt-5.6-terra", 20, 0, 0, 0), // 16:00 EDT, Mar 8
		ev(t, "n3", d.ID, event.SourceCodex, "2026-03-09T03:30:00Z", "gpt-5.6-terra", 30, 0, 0, 0), // 23:30 EDT, Mar 8
		ev(t, "n4", d.ID, event.SourceCodex, "2026-03-09T06:00:00Z", "gpt-5.6-terra", 40, 0, 0, 0), // 02:00 EDT, Mar 9
	}
	if _, _, _, err := s.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	f := Filter{From: mustTime(t, "2026-03-07T00:00:00Z"), To: mustTime(t, "2026-03-10T12:00:00Z")}
	ts, err := s.Timeseries(ctx, f, "day", "source", ny, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ts.Series) != 1 {
		t.Fatalf("series = %+v", ts.Series)
	}
	got := map[string]int64{}
	for _, p := range ts.Series[0].Points {
		got[p.T] = p.Input
	}
	want := map[string]int64{"2026-03-08": 60, "2026-03-09": 40}
	if len(got) != len(want) {
		t.Fatalf("points = %+v, want %+v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("points = %+v, want %+v", got, want)
		}
	}
}

func TestEventsPagination(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	seed(t, s)
	f := Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-03T00:00:00Z")}

	var ids []string
	cursor := ""
	for range 10 {
		page, err := s.Events(ctx, f, 2, cursor, testTable(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Events {
			ids = append(ids, e.EventID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	want := []string{"e5", "e4", "e3", "e2", "e1"} // newest first
	if len(ids) != len(want) {
		t.Fatalf("paged ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("paged ids = %v, want %v", ids, want)
		}
	}

	page, err := s.Events(ctx, Filter{From: f.From, To: f.To, Source: "codex"}, 200, "", testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.NextCursor != "" {
		t.Fatalf("filtered page = %+v", page)
	}
	if page.Events[0].CostUSD == nil {
		t.Fatal("cost_usd missing for a priced event")
	}
	approx(t, "event cost", *page.Events[0].CostUSD, 3.6)

	unpriced, err := s.Events(ctx, Filter{From: f.From, To: f.To, Model: "unpriced-model"}, 200, "", testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(unpriced.Events) != 1 || unpriced.Events[0].CostUSD != nil {
		t.Fatalf("unpriced event = %+v", unpriced.Events)
	}
	if _, err := s.Events(ctx, f, 200, "!!not-base64!!", nil); err == nil {
		t.Error("bad cursor accepted")
	}
}
