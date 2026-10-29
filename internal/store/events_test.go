package store

import (
	"context"
	"testing"

	"ai-usage-monitor/internal/event"
)

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

// A streamed response reaches the server twice: first the nearly empty line the
// agent caught mid-stream, later the finished one under the same event id. The
// finished copy must replace the stored tokens; anything smaller must not.
func TestInsertEventsRefreshesGrownCopies(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	d, _, err := s.CreateDevice(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	partial := ev(t, "m1", d.ID, event.SourceClaudeCode, "2026-09-01T10:00:00Z", "claude-opus-5", 2, 3, 24_000, 9_000)
	acc, dup, upd, err := s.InsertEvents(ctx, []event.UsageEvent{partial})
	if err != nil || acc != 1 || dup != 0 || upd != 0 {
		t.Fatalf("first insert = %d/%d/%d, %v", acc, dup, upd, err)
	}

	final := ev(t, "m1", d.ID, event.SourceClaudeCode, "2026-09-01T10:00:41Z", "claude-opus-5", 2, 1_447, 24_289, 9_450)
	final.Reasoning = 1_153
	acc, dup, upd, err = s.InsertEvents(ctx, []event.UsageEvent{final})
	if err != nil || acc != 0 || dup != 0 || upd != 1 {
		t.Fatalf("grown copy = %d/%d/%d, %v; want 0/0/1", acc, dup, upd, err)
	}
	page, err := s.Events(ctx, Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-02T00:00:00Z")}, 10, "", nil)
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("events = %+v, %v", page, err)
	}
	got := page.Events[0]
	if got.Output != 1_447 || got.CacheRead != 24_289 || got.CacheWrite5m != 9_450 || got.Reasoning != 1_153 || got.Timestamp != final.Timestamp {
		t.Fatalf("stored after refresh = %+v", got.UsageEvent)
	}
	if got.DeviceID != d.ID {
		t.Fatalf("device changed: %s", got.DeviceID)
	}

	// Replaying the finished copy, or an older smaller one, changes nothing.
	acc, dup, upd, err = s.InsertEvents(ctx, []event.UsageEvent{final, partial})
	if err != nil || acc != 0 || dup != 2 || upd != 0 {
		t.Fatalf("replay = %d/%d/%d, %v; want 0/2/0", acc, dup, upd, err)
	}
	page, _ = s.Events(ctx, Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-02T00:00:00Z")}, 10, "", nil)
	if page.Events[0].Output != 1_447 {
		t.Fatalf("smaller copy downgraded the row: %+v", page.Events[0].UsageEvent)
	}

	// The resync flag alone deletes nothing and is consumed once.
	if err := s.RequestResync(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestResync(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("resync unknown device = %v, want ErrNotFound", err)
	}
	if flag, err := s.ConsumeResync(ctx, d.ID); err != nil || !flag {
		t.Fatalf("consume = %v, %v", flag, err)
	}
	if flag, _ := s.ConsumeResync(ctx, d.ID); flag {
		t.Fatal("resync flag must be consumed once")
	}
	if c, _ := s.Counts(ctx); c.Events != 1 {
		t.Fatalf("events after resync request = %+v", c)
	}
}

// A collector that could not tell the model at first stores event.UnknownModel;
// a later copy of the same event that names it fills it in. A known model is
// never replaced, and a placeholder never overwrites a known one.
func TestInsertEventsFillsInAnUnknownModel(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	d, _, err := s.CreateDevice(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	modelOf := func() string {
		t.Helper()
		page, err := s.Events(ctx, Filter{From: mustTime(t, "2026-10-21T00:00:00Z"), To: mustTime(t, "2026-10-22T00:00:00Z")}, 10, "", nil)
		if err != nil || len(page.Events) != 1 {
			t.Fatalf("events = %+v, %v", page, err)
		}
		return page.Events[0].Model
	}

	first := ev(t, "c1", d.ID, event.SourceCodex, "2026-10-21T13:43:01Z", event.UnknownModel, 60, 10, 40, 0)
	if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{first}); err != nil {
		t.Fatal(err)
	}

	named := first
	named.Model = "gpt-6.1-sol"
	acc, dup, upd, err := s.InsertEvents(ctx, []event.UsageEvent{named})
	if err != nil || acc != 0 || dup != 0 || upd != 1 {
		t.Fatalf("named copy = %d/%d/%d, %v; want 0/0/1", acc, dup, upd, err)
	}
	if got := modelOf(); got != "gpt-6.1-sol" {
		t.Fatalf("model = %q, want it filled in", got)
	}

	for _, m := range []string{event.UnknownModel, "gpt-6-astra"} {
		again := first
		again.Model = m
		acc, dup, upd, err := s.InsertEvents(ctx, []event.UsageEvent{again})
		if err != nil || acc != 0 || dup != 1 || upd != 0 {
			t.Fatalf("copy with %q = %d/%d/%d, %v; want a plain duplicate", m, acc, dup, upd, err)
		}
		if got := modelOf(); got != "gpt-6.1-sol" {
			t.Fatalf("model = %q after a copy with %q", got, m)
		}
	}
}
