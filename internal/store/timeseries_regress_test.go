package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/pricing"
)

// A price applies by the event's UTC day. A local day bucket (Asia/Seoul
// starts at 15:00 UTC) straddles two UTC days, so /timeseries must price per
// UTC day exactly as /summary does.
func TestTimeseriesPricesByUTCDay(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "kst")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := pricing.Parse([]byte(`
- match: "price-cut-model"
  effective_from: 2026-08-21
  input: 4
  output: 20
- match: "price-cut-model"
  effective_to: 2026-08-21
  input: 5
  output: 30
`))
	if err != nil {
		t.Fatal(err)
	}
	// 12:00 KST on Aug 21, 03:00 UTC: after the cut. The KST day began at
	// 15:00 UTC on Aug 20, before it.
	e := ev(t, "p1", d.ID, event.SourceCodex, "2026-08-21T03:00:00Z", "price-cut-model", 1_000_000, 0, 0, 0)
	if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{e}); err != nil {
		t.Fatal(err)
	}
	seoul, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	f := Filter{From: time.Date(2026, 8, 20, 0, 0, 0, 0, seoul), To: time.Date(2026, 8, 23, 0, 0, 0, 0, seoul)}
	sum, err := s.Summary(ctx, f, tbl)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, "summary", sum.Totals.CostUSD, 4)
	for _, bucket := range []string{"day", "hour"} {
		ts, err := s.Timeseries(ctx, f, bucket, "source", seoul, tbl)
		if err != nil {
			t.Fatal(err)
		}
		var cost float64
		for _, p := range ts.Series[0].Points {
			cost += p.CostUSD
		}
		approx(t, "timeseries "+bucket, cost, 4)
	}
}

// 2026-11-01 is 25 hours long in New York. Its last local hour must stay on
// that date instead of becoming a second "2026-11-01" point.
func TestTimeseriesFallBackDayIsOnePoint(t *testing.T) {
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
	evs := []event.UsageEvent{
		ev(t, "f1", d.ID, event.SourceCodex, "2026-11-01T14:00:00Z", "gpt-5.6-terra", 10, 0, 0, 0), // 09:00 EST
		ev(t, "f2", d.ID, event.SourceCodex, "2026-11-02T04:30:00Z", "gpt-5.6-terra", 20, 0, 0, 0), // 23:30 EST, Nov 1
		ev(t, "f3", d.ID, event.SourceCodex, "2026-11-02T06:00:00Z", "gpt-5.6-terra", 40, 0, 0, 0), // 01:00 EST, Nov 2
	}
	if _, _, _, err := s.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	f := Filter{From: time.Date(2026, 10, 31, 0, 0, 0, 0, ny), To: time.Date(2026, 11, 3, 0, 0, 0, 0, ny)}
	ts, err := s.Timeseries(ctx, f, "day", "source", ny, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	inputs := map[string]int64{}
	for _, p := range ts.Series[0].Points {
		got = append(got, p.T)
		inputs[p.T] = p.Input
	}
	if len(got) != 2 || inputs["2026-11-01"] != 30 || inputs["2026-11-02"] != 40 {
		t.Fatalf("points %v inputs %v, want 2026-11-01=30 and 2026-11-02=40", got, inputs)
	}
}

// Hourly buckets across the New York fall-back night: local 01:00 happens
// twice and both hours must come back, as must every hour after them.
func TestTimeseriesHourlyFallBack(t *testing.T) {
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
	evs := []event.UsageEvent{
		ev(t, "h1", d.ID, event.SourceCodex, "2026-11-01T05:30:00Z", "gpt-5.6-terra", 1, 0, 0, 0), // 01:30 EDT
		ev(t, "h2", d.ID, event.SourceCodex, "2026-11-01T06:30:00Z", "gpt-5.6-terra", 2, 0, 0, 0), // 01:30 EST
		ev(t, "h3", d.ID, event.SourceCodex, "2026-11-02T15:00:00Z", "gpt-5.6-terra", 4, 0, 0, 0), // next day
	}
	if _, _, _, err := s.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	f := Filter{From: time.Date(2026, 10, 31, 0, 0, 0, 0, ny), To: time.Date(2026, 11, 3, 0, 0, 0, 0, ny)}
	ts, err := s.Timeseries(ctx, f, "hour", "source", ny, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, p := range ts.Series[0].Points {
		got[p.T] = p.Input
	}
	want := map[string]int64{"2026-11-01T01:00:00-04:00": 1, "2026-11-01T01:00:00-05:00": 2, "2026-11-02T10:00:00-05:00": 4}
	if len(got) != len(want) {
		t.Fatalf("points %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("points %v, want %v", got, want)
		}
	}
}

// An event stored before the token cap would overflow every SUM over it; the
// migration removes it.
func TestMigrationDropsOversizedEvents(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{
		ev(t, "ok", d.ID, event.SourceCodex, "2026-10-01T01:00:00Z", "gpt-5.6-terra", 10, 0, 0, 0),
		ev(t, "huge", d.ID, event.SourceCodex, "2026-10-01T02:00:00Z", "gpt-5.6-terra", 9_000_000_000_000_000_000, 0, 0, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = '0004_drop_oversized_events.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d events left, want only the sane one", n)
	}
}

// Lord Howe springs forward by 30 minutes (02:00 +10:30 -> 02:30 +11:00). An
// event right after the shift belongs to the hour that starts at the shift,
// not to a whole hour before it that the event did not happen in.
func TestTimeseriesHalfHourShift(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	lh, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	d, _, err := s.CreateDevice(ctx, "lh")
	if err != nil {
		t.Fatal(err)
	}
	e := ev(t, "lh1", d.ID, event.SourceCodex, "2026-10-03T15:40:00Z", "gpt-5.6-terra", 5, 0, 0, 0) // 02:40 +11:00
	if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{e}); err != nil {
		t.Fatal(err)
	}
	f := Filter{From: time.Date(2026, 10, 4, 0, 0, 0, 0, lh), To: time.Date(2026, 10, 5, 0, 0, 0, 0, lh)}
	ts, err := s.Timeseries(ctx, f, "hour", "source", lh, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	pts := ts.Series[0].Points
	if len(pts) != 1 {
		t.Fatalf("points %+v", pts)
	}
	at, err := time.Parse(time.RFC3339, pts[0].T)
	if err != nil {
		t.Fatal(err)
	}
	if at.After(e.Timestamp) || e.Timestamp.Sub(at) >= time.Hour || at.Before(mustTime(t, "2026-10-03T15:30:00Z")) {
		t.Errorf("event at %s labelled %s", e.Timestamp.In(lh).Format(time.RFC3339), pts[0].T)
	}
}

// A range with more buckets than one series holds is refused, not cut short.
func TestTimeseriesRefusesTooManyBuckets(t *testing.T) {
	s := openTest(t)
	f := Filter{From: mustTime(t, "1900-01-01T00:00:00Z"), To: mustTime(t, "2026-01-01T00:00:00Z")}
	if _, err := s.Timeseries(context.Background(), f, "hour", "source", time.UTC, testTable(t)); !errors.Is(err, ErrTooManyBuckets) {
		t.Fatalf("got %v, want ErrTooManyBuckets", err)
	}
}
