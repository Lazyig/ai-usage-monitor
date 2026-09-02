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
	for _, m := range []string{"claude-opus-5", "claude-sonnet-5", "claude-fable-5", "claude-fable-5-1", "gpt-5.6-terra", "gpt-5.6-sol", "gpt-5.6-luna", "codex-auto-review", "gemini-3.7-flash", "gemini-3.7-flash-control", "gemini-default", "gemini-3-flash-a", "gemini-3-flash-c", "gemini-3.6-flash", "gemini-3.6-flash-exp-a", "gemini-3.1-pro-low", "claude-sonnet-4-6", "claude-opus-4-6-thinking", "antigravity-internal"} {
		c := tb.Cost(event.UsageEvent{Model: m, Timestamp: time.Now(), Input: 1e6, Output: 1e6, CacheRead: 1e6})
		if !c.Known {
			t.Errorf("%s: no price", m)
		}
		t.Logf("%-28s in+out+cache(1M each) = $%.3f estimated=%v", m, c.USD, c.Estimated)
	}
	if th := tb.Thresholds(); len(th) != 2 || th[0] != 200000 || th[1] != 272000 {
		t.Errorf("thresholds = %v", th)
	}
}
