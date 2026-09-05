package store

import (
	"context"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
)

func TestThroughput(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, _, err := s.CreateDevice(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	evs := []event.UsageEvent{
		// Two Claude events in the same minute, one an hour later, one Codex
		// event sharing the first minute.
		ev(t, "e1", a.ID, event.SourceClaudeCode, "2026-09-01T10:00:05Z", "claude-opus-5", 1_000, 100, 2_000, 500),
		ev(t, "e2", a.ID, event.SourceClaudeCode, "2026-09-01T10:00:50Z", "claude-opus-5", 500, 300, 0, 0),
		ev(t, "e3", a.ID, event.SourceClaudeCode, "2026-09-01T11:00:00Z", "claude-opus-5", 100, 50, 0, 0),
		ev(t, "e4", a.ID, event.SourceCodex, "2026-09-01T10:00:30Z", "gpt-5.6-terra", 10_000, 1_000, 0, 0),
	}
	if _, _, _, err := s.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	seoul, _ := time.LoadLocation("Asia/Seoul")
	f := Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-02T00:00:00Z")}
	tp, err := s.Throughput(ctx, f, "source", seoul)
	if err != nil {
		t.Fatal(err)
	}
	if tp.GroupBy != "source" || tp.TZ != "Asia/Seoul" {
		t.Fatalf("meta = %+v", tp)
	}
	byKey := map[string]ThroughputGroup{}
	for _, g := range tp.Groups {
		byKey[g.Key] = g
	}
	cc := byKey["claude_code"]
	if cc.ActiveMinutes != 2 || cc.Events != 3 {
		t.Fatalf("claude active/events = %d/%d, want 2/3", cc.ActiveMinutes, cc.Events)
	}
	// prompt = input + cache_read + cache_write = (1000+2000+500) + 500 + 100 = 4100
	if cc.Prompt != 4100 || cc.Output != 450 {
		t.Fatalf("claude prompt/output = %d/%d", cc.Prompt, cc.Output)
	}
	if cc.PerMinute.Prompt != 2050 || cc.PerMinute.Output != 225 || cc.PerMinute.Total != 2275 {
		t.Fatalf("claude per_minute = %+v", cc.PerMinute)
	}
	if cc.PeakPrompt == nil || cc.PeakPrompt.Tokens != 4000 || cc.PeakOutput == nil || cc.PeakOutput.Tokens != 400 {
		t.Fatalf("claude peaks = %+v / %+v", cc.PeakPrompt, cc.PeakOutput)
	}
	if got := cc.PeakOutput.T.Format(time.RFC3339); got != "2026-09-01T19:00:00+09:00" {
		t.Fatalf("peak minute = %s, want 2026-09-01T19:00:00+09:00", got)
	}
	// Codex is busier per minute, so it sorts first.
	if tp.Groups[0].Key != "codex" || tp.Groups[0].PerMinute.Total != 11_000 {
		t.Fatalf("first group = %+v", tp.Groups[0])
	}
	// Total: the 10:00 minute is shared, so 2 active minutes overall.
	tot := tp.Total
	if tot.ActiveMinutes != 2 || tot.Events != 4 || tot.Prompt != 14_100 || tot.Output != 1_450 {
		t.Fatalf("total = %+v", tot)
	}
	if tot.PeakPrompt.Tokens != 14_000 || tot.PeakOutput.Tokens != 1_400 {
		t.Fatalf("total peaks = %+v / %+v", tot.PeakPrompt, tot.PeakOutput)
	}

	// Device grouping carries the operator name; an empty range is empty, not an error.
	td, err := s.Throughput(ctx, f, "device", seoul)
	if err != nil || len(td.Groups) != 1 || td.Groups[0].Name != "a" {
		t.Fatalf("device grouping = %+v, %v", td, err)
	}
	empty, err := s.Throughput(ctx, Filter{From: mustTime(t, "2027-01-01T00:00:00Z"), To: mustTime(t, "2027-01-02T00:00:00Z")}, "model", seoul)
	if err != nil || len(empty.Groups) != 0 || empty.Total.ActiveMinutes != 0 || empty.Total.PeakOutput != nil {
		t.Fatalf("empty = %+v, %v", empty, err)
	}
	if _, err := s.Throughput(ctx, f, "ts", seoul); err == nil {
		t.Fatal("group_by=ts must be rejected")
	}
}
