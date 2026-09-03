package pricing_test

import (
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/pricing"
)

func TestRealTableCoversObservedModels(t *testing.T) {
	tb, err := pricing.Load("../../pricing.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"claude-opus-5", "claude-sonnet-5", "claude-fable-5", "claude-fable-5-1", "gpt-5.6-terra", "gpt-5.6-sol", "gpt-5.6-luna", "codex-auto-review", "gemini-3.8-flash", "gemini-3.8-flash-control", "gemini-3.7-flash", "gemini-3.7-flash-control", "gemini-default", "gemini-3-flash-a", "gemini-3-flash-c", "gemini-3.6-flash", "gemini-3.6-flash-exp-a", "gemini-3.1-pro-low", "claude-sonnet-4-6", "claude-opus-4-6-thinking", "antigravity-internal"} {
		c := tb.Cost(event.UsageEvent{Model: m, Timestamp: time.Now(), Input: 1e6, Output: 1e6, CacheRead: 1e6})
		if !c.Known {
			t.Errorf("%s: no price", m)
		}
		t.Logf("%-28s in+out+cache(1M each) = $%.3f estimated=%v", m, c.USD, c.Estimated)
	}
	if th := tb.Thresholds(); len(th) != 2 || th[0] != 200000 || th[1] != 272000 {
		t.Errorf("thresholds = %v", th)
	}
	// Period rows: the dated price changes must resolve by event date.
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	for _, c := range []struct {
		model, at string
		input   float64
	}{
		{"gemini-3.6-flash", "2026-07-25", 1.50}, {"gemini-3.6-flash", "2026-08-13", 0.75}, {"gemini-3.6-flash", "2027-01-01", 1.50},
		{"gemini-3.6-flash-exp-a", "2026-07-25", 1.50},
		{"gemini-3.7-flash-control", "2026-09-01", 0.75}, {"gemini-default", "2027-02-01", 1.50},
		{"gpt-5.6-sol", "2026-08-01", 5.00}, {"gpt-5.6-sol", "2026-08-21", 4.00}, {"codex-auto-review", "2026-08-01", 5.00},
		{"gpt-5.6-terra", "2026-07-15", 2.50}, {"gpt-5.6-terra", "2026-07-30", 2.00},
		{"gpt-5.6-luna", "2026-07-15", 1.00}, {"gpt-5.6-luna", "2026-08-01", 0.20},
		{"claude-sonnet-5", "2026-07-01", 2.00}, {"claude-sonnet-5", "2026-10-01", 2.00},
	} {
		r, ok := tb.Lookup(c.model, day(c.at))
		if !ok || r.Input == nil || *r.Input != c.input {
			t.Errorf("%s @ %s: got %v ok=%v, want input %v", c.model, c.at, r.Input, ok, c.input)
		}
	}
}
