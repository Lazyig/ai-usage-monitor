package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
)

func TestServiceTierMigrationLeavesExistingUsageUnknown(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	d, _, err := s.CreateDevice(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	e := ev(t, "old", d.ID, event.SourceCodex, "2026-11-16T08:00:00Z", "gpt-5.6-terra", 100, 10, 0, 0)
	if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{e}); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema on this isolated test database.
	if _, err := s.db.Exec(`ALTER TABLE usage_events DROP COLUMN service_tier`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migrations WHERE version = '0006_service_tier.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.Events(ctx, Filter{ServiceTier: "unknown"}, 10, "", nil)
	if err != nil || len(page.Events) != 1 || page.Events[0].ServiceTier != "" || page.Events[0].Input != 100 {
		t.Fatalf("migrated existing event: %+v, %v", page, err)
	}
}

func TestServiceTierBackfillOwnershipAndReplay(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	d, _, err := s.CreateDevice(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreateDevice(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	old := ev(t, "tier-backfill", d.ID, event.SourceCodex, "2026-11-16T08:00:00Z", "gpt-5.6-terra", 100, 10, 0, 0)
	insert := func(e event.UsageEvent, want [3]int) {
		t.Helper()
		a, dup, u, err := s.InsertEvents(ctx, []event.UsageEvent{e})
		if err != nil || [3]int{a, dup, u} != want {
			t.Fatalf("insert = %d/%d/%d, %v; want %v", a, dup, u, err, want)
		}
	}
	insert(old, [3]int{1, 0, 0})
	copy := old
	copy.ServiceTier = "priority"
	copy.DeviceID = other.ID
	insert(copy, [3]int{0, 1, 0}) // another device cannot enrich the event
	page, err := s.Events(ctx, Filter{ServiceTier: "unknown"}, 10, "", nil)
	if err != nil || len(page.Events) != 1 || page.Events[0].ServiceTier != "" {
		t.Fatalf("cross-device enrichment: %+v, %v", page, err)
	}
	copy.DeviceID = d.ID
	insert(copy, [3]int{0, 0, 1}) // same tokens, enriched metadata
	insert(copy, [3]int{0, 1, 0})
	insert(old, [3]int{0, 1, 0}) // older agent cannot erase the tier
	copy.ServiceTier = "default"
	insert(copy, [3]int{0, 1, 0}) // an established tier is immutable
	old.Output++
	insert(old, [3]int{0, 0, 1}) // growing copies still cannot erase it
	page, err = s.Events(ctx, Filter{ServiceTier: "priority"}, 10, "", nil)
	if err != nil || len(page.Events) != 1 || page.Events[0].ServiceTier != "fast" || page.Events[0].Output != 11 {
		t.Fatalf("after backfill/replays: %+v, %v", page, err)
	}
	counts, err := s.Counts(ctx)
	if err != nil || counts.Events != 1 {
		t.Fatalf("backfill duplicated usage: %+v, %v", counts, err)
	}
}

// Filtering every aggregate endpoint must narrow both usage and cost; tier
// grouping must preserve the model total and use the same Standard API prices.
func TestServiceTierQueriesPreserveModelAndStandardCost(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	d, _, err := s.CreateDevice(ctx, "mac")
	if err != nil {
		t.Fatal(err)
	}
	for i, tier := range []string{"", "default", "priority", "fast", "auto"} {
		e := ev(t, fmt.Sprint("tier", i), d.ID, event.SourceCodex, "2026-11-16T08:00:00Z", "gpt-5.6-terra", 1_000_000, 100_000, 0, 0)
		e.ServiceTier = tier
		if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{e}); err != nil {
			t.Fatal(err)
		}
	}
	f := Filter{From: mustTime(t, "2026-11-16T00:00:00Z"), To: mustTime(t, "2026-11-17T00:00:00Z"), Source: "codex"}
	prices := testTable(t)
	summary, err := s.Summary(ctx, f, prices)
	if err != nil || len(summary.ByModel) != 1 || summary.Totals.Events != 5 || summary.ByModel[0].Key != "gpt-5.6-terra" {
		t.Fatalf("model total fragmented: %+v, %v", summary, err)
	}
	approx(t, "Standard cost total", summary.Totals.CostUSD, 14)
	breakdown, err := s.Breakdown(ctx, f, "service_tier", 50, prices)
	if err != nil || len(breakdown.Rows) != 4 {
		t.Fatalf("tier groups: %+v, %v", breakdown, err)
	}
	groups := map[string]Group{}
	for _, row := range breakdown.Rows {
		groups[row.Key] = row
	}
	if groups["fast"].Events != 2 || groups["unknown"].Events != 1 || groups["default"].Events != 1 || groups["auto"].Events != 1 {
		t.Fatalf("groups: %+v", groups)
	}
	for _, tier := range []string{"fast", "priority", "default", "unknown", "auto"} {
		t.Run(tier, func(t *testing.T) {
			filtered := f
			filtered.ServiceTier = tier
			want := int64(1)
			if tier == "fast" || tier == "priority" {
				want = 2
			}
			sum, err := s.Summary(ctx, filtered, prices)
			if err != nil || sum.Totals.Events != want || sum.Totals.Input != want*1_000_000 {
				t.Fatalf("summary: %+v, %v", sum, err)
			}
			approx(t, "filtered cost", sum.Totals.CostUSD, float64(want)*2.8)
			page, err := s.Events(ctx, filtered, 100, "", prices)
			if err != nil || int64(len(page.Events)) != want {
				t.Fatalf("events: %+v, %v", page, err)
			}
			series, err := s.Timeseries(ctx, filtered, "day", "service_tier", time.UTC, prices)
			if err != nil || len(series.Series) != 1 || len(series.Series[0].Points) != 1 || series.Series[0].Points[0].Events != want {
				t.Fatalf("timeseries: %+v, %v", series, err)
			}
			tp, err := s.Throughput(ctx, filtered, "service_tier", time.UTC)
			if err != nil || len(tp.Groups) != 1 || tp.Total.Events != want || tp.Total.ActiveMinutes != 1 {
				t.Fatalf("throughput: %+v, %v", tp, err)
			}
		})
	}
}
