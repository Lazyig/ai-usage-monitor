package store

import (
	"context"
	"testing"

	"ai-usage-monitor/internal/event"
)

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
