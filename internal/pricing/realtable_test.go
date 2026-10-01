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
	for _, m := range []string{"claude-opus-5", "claude-sonnet-5", "claude-fable-5", "claude-fable-5-1", "claude-opus-5-5", "claude-opus-5-5-20260922", "claude-opus-5.5", "claude-5-5", "claude-5.5", "claude-sonnet-5-5", "claude-sonnet-5-5-20260928", "claude-sonnet-5.5", "claude-haiku-4-5", "claude-haiku-4-5-20251001", "gpt-6", "gpt-6-astra", "gpt-6-sol", "gpt-6-sol-2026-09-22", "gpt-6-luna", "gpt-6-luna-2026-09-22", "gpt-6.1-sol", "gpt-6.1-sol-2026-09-29", "gpt-5.6-terra", "gpt-5.6-sol", "gpt-5.6-luna", "codex-auto-review", "gemini-3.8-flash", "gemini-3.8-flash-control", "gemini-3.7-flash", "gemini-3.7-flash-control", "gemini-default", "gemini-3-flash-a", "gemini-3-flash-c", "gemini-3.6-flash", "gemini-3.6-flash-exp-a", "gemini-3.1-pro-low", "claude-sonnet-4-6", "claude-opus-4-6-thinking", "antigravity-internal"} {
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
		input     float64
	}{
		{"gemini-3.6-flash", "2026-07-25", 1.50}, {"gemini-3.6-flash", "2026-08-13", 0.75}, {"gemini-3.6-flash", "2027-01-01", 1.50},
		{"gemini-3.6-flash-exp-a", "2026-07-25", 1.50},
		{"gemini-3.7-flash-control", "2026-09-01", 0.75}, {"gemini-default", "2027-02-01", 1.50},
		{"gpt-5.6-sol", "2026-08-01", 5.00}, {"gpt-5.6-sol", "2026-08-21", 4.00}, {"codex-auto-review", "2026-08-01", 5.00},
		{"gpt-5.6-terra", "2026-07-15", 2.50}, {"gpt-5.6-terra", "2026-07-30", 2.00},
		{"gpt-5.6-luna", "2026-07-15", 1.00}, {"gpt-5.6-luna", "2026-08-01", 0.20},
		{"gpt-6-astra", "2026-09-09", 10.00}, {"gpt-6-astra-2026-09-03", "2026-09-09", 10.00},
		{"gpt-6-sol", "2026-09-24", 2.00}, {"gpt-6-sol-2026-09-22", "2026-09-24", 2.00},
		{"gpt-6-luna", "2026-09-24", 0.10}, {"gpt-6-luna-2026-09-22", "2026-09-24", 0.10},
		{"gpt-6.1-sol", "2026-10-01", 2.00}, {"gpt-6.1-sol-2026-09-29", "2026-10-01", 2.00},
		{"claude-sonnet-5-5", "2026-10-01", 2.00}, {"claude-sonnet-5-5-20260928", "2026-10-01", 2.00},
		{"claude-opus-5-5", "2026-09-24", 4.00}, {"claude-opus-5-5-20260922", "2026-09-24", 4.00},
		{"claude-sonnet-5", "2026-07-01", 2.00}, {"claude-sonnet-5", "2026-10-01", 2.00},
	} {
		r, ok := tb.Lookup(c.model, day(c.at))
		if !ok || r.Input == nil || *r.Input != c.input {
			t.Errorf("%s @ %s: got %v ok=%v, want input %v", c.model, c.at, r.Input, ok, c.input)
		}
	}
	// gpt-6-* glob should NOT match unknown gpt-6 models
	if _, ok := tb.Lookup("gpt-6-unknown", time.Now()); ok {
		t.Errorf("gpt-6-unknown should not match any gpt-6 glob")
	}

	// Standard (prompt <= 272K) vs Long-context (>272K) pricing for GPT-6 models
	for _, tc := range []struct {
		model    string
		stdInput float64
		stdOut   float64
		stdCache float64
		lcInput  float64
		lcOut    float64
		lcCache  float64
	}{
		{"gpt-6-astra", 10.00, 50.00, 1.00, 20.00, 75.00, 2.00},
		{"gpt-6-sol", 2.00, 10.00, 0.20, 4.00, 15.00, 0.40},
		{"gpt-6-luna", 0.10, 0.50, 0.01, 0.20, 0.75, 0.02},
		{"gpt-6.1-sol", 2.00, 10.00, 0.10, 4.00, 15.00, 0.20},
	} {
		// Prompt = 200k (under 272k)
		cStd := tb.Cost(event.UsageEvent{Model: tc.model, Timestamp: time.Now(), Input: 100_000, Output: 100_000, CacheRead: 100_000})
		wantStd := (tc.stdInput + tc.stdOut + tc.stdCache) * 0.1
		if diff := cStd.USD - wantStd; diff < -1e-6 || diff > 1e-6 {
			t.Errorf("%s standard cost = %v, want %v", tc.model, cStd.USD, wantStd)
		}

		// Prompt = 300k (over 272k)
		cLong := tb.Cost(event.UsageEvent{Model: tc.model, Timestamp: time.Now(), Input: 200_000, Output: 100_000, CacheRead: 100_000})
		wantLong := tc.lcInput*0.2 + tc.lcOut*0.1 + tc.lcCache*0.1
		if diff := cLong.USD - wantLong; diff < -1e-6 || diff > 1e-6 {
			t.Errorf("%s long context cost = %v, want %v", tc.model, cLong.USD, wantLong)
		}
	}

	// gpt-6.1-sol bills cache writes ($2.50/1M) separately from input
	if c := tb.Cost(event.UsageEvent{Model: "gpt-6.1-sol", Timestamp: time.Now(), CacheWrite: 100_000}); c.USD < 0.25-1e-6 || c.USD > 0.25+1e-6 {
		t.Errorf("gpt-6.1-sol cache write cost = %v, want 0.25", c.USD)
	}
}
