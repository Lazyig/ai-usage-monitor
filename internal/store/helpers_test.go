package store

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/pricing"
	"ai-usage-monitor/internal/store/storetest"
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
	s, err := Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// openSQLiteTest opens a SQLite store whatever backend the suite runs on.
func openSQLiteTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "sqlite.db"))
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

func approx(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}
