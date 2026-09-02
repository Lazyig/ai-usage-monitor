package pricing

import (
	"math"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
)

const sample = `
- match: "claude-opus-5"
  input: 5
  output: 25
  cache_read: 0.5
  cache_write_5m: 6.25
  cache_write_1h: 10
- match: "gemini-3.7-flash*"
  input: 1
  output: 4
  cache_read: 0.1
- match: "gemini-default"
  alias_of: "gemini-3.7-flash"
- match: "gpt-5.6-*"
  input: 2
`

func TestCost(t *testing.T) {
	tb, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	e := event.UsageEvent{Model: "claude-opus-5", Timestamp: now, Input: 1_000_000, Output: 100_000, CacheRead: 2_000_000, CacheWrite5m: 500_000}
	c := tb.Cost(e)
	want := 5 + 2.5 + 1 + 3.125
	if !c.Known || math.Abs(c.USD-want) > 1e-9 || c.Estimated {
		t.Fatalf("claude cost = %+v, want %v known", c, want)
	}

	c = tb.Cost(event.UsageEvent{Model: "gemini-default", Timestamp: now, Input: 1_000_000})
	if !c.Known || !c.Estimated || c.USD != 1 {
		t.Fatalf("alias cost = %+v", c)
	}
	c = tb.Cost(event.UsageEvent{Model: "gemini-3.7-flash-control", Timestamp: now, Output: 1_000_000})
	if !c.Known || c.USD != 4 {
		t.Fatalf("glob cost = %+v", c)
	}
	// missing output price with output tokens -> unknown
	c = tb.Cost(event.UsageEvent{Model: "gpt-5.6-terra", Timestamp: now, Input: 10, Output: 10})
	if c.Known {
		t.Fatalf("expected unknown, got %+v", c)
	}
	// zero output tokens -> price not needed
	c = tb.Cost(event.UsageEvent{Model: "gpt-5.6-terra", Timestamp: now, Input: 1_000_000})
	if !c.Known || c.USD != 2 {
		t.Fatalf("partial price = %+v", c)
	}
	if c := tb.Cost(event.UsageEvent{Model: "codex-auto-review", Timestamp: now, Input: 1}); c.Known {
		t.Fatal("unlisted model must be unknown")
	}
}
