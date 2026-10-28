package store

import (
	"context"
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
