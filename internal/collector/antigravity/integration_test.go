package antigravity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
)

type modelTotals struct {
	calls                                int
	uncached, cached, thoughts, response int64
}

// TestIntegrationRealHome reads the real ~/.gemini/antigravity-cli, read-only,
// checks every event and that the returned cursor makes a rescan a no-op, and
// logs the per-model sums. Opt in with AI_USAGE_INTEGRATION=1. It skips when
// the CLI is not installed on this machine.
func TestIntegrationRealHome(t *testing.T) {
	if os.Getenv("AI_USAGE_INTEGRATION") != "1" {
		t.Skip("real-home logs require AI_USAGE_INTEGRATION=1")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	root := filepath.Join(home, ".gemini", "antigravity-cli")
	if _, err := os.Stat(filepath.Join(root, "conversations")); err != nil {
		t.Skip("antigravity CLI data not present:", err)
	}

	c := New(root)
	start := time.Now()
	evs, cur, err := c.Scan(context.Background(), "integration-device", nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	t.Logf("scanned %d events in %s, cursor %d bytes", len(evs), time.Since(start).Round(time.Millisecond), len(cur))

	totals := map[string]*modelTotals{}
	ids := map[string]bool{}
	var minTS, maxTS time.Time
	for _, e := range evs {
		if !e.Valid() {
			t.Fatalf("invalid event: %+v", e)
		}
		if ids[e.EventID] {
			t.Fatalf("duplicate event id for model %s at %s", e.Model, e.Timestamp)
		}
		ids[e.EventID] = true
		if e.Reasoning > e.Output {
			t.Fatalf("reasoning above output: %+v", e)
		}
		if minTS.IsZero() || e.Timestamp.Before(minTS) {
			minTS = e.Timestamp
		}
		if e.Timestamp.After(maxTS) {
			maxTS = e.Timestamp
		}
		add(totals, e.Model, e)
	}

	names := make([]string, 0, len(totals))
	for m := range totals {
		names = append(names, m)
	}
	sort.Slice(names, func(i, j int) bool { return totals[names[i]].uncached > totals[names[j]].uncached })

	t.Logf("range %s .. %s", minTS.Format(time.RFC3339), maxTS.Format(time.RFC3339))
	logTable(t, names, totals)

	for _, d := range c.(*Collector).Diagnostics() {
		b, _ := json.Marshal(d)
		t.Logf("self-check failure: %s", b)
	}

	// Rescanning with the returned cursor must be a no-op.
	again, _, err := c.Scan(context.Background(), "integration-device", cur)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("rescan with cursor emitted %d events, want 0", len(again))
	}
}

func add(dst map[string]*modelTotals, model string, e event.UsageEvent) {
	mt := dst[model]
	if mt == nil {
		mt = &modelTotals{}
		dst[model] = mt
	}
	mt.calls++
	mt.uncached += e.Input
	mt.cached += e.CacheRead
	mt.thoughts += e.Reasoning
	mt.response += e.Output - e.Reasoning // response tokens are 1.4.10, our Output is 1.4.3
}

// logTable prints the per-model sums.
func logTable(t *testing.T, names []string, totals map[string]*modelTotals) {
	t.Helper()
	t.Logf("  %-28s %7s %14s %14s %10s %10s", "model", "calls", "prompt_unc", "prompt_cached", "thoughts", "response")
	for _, m := range names {
		mt := totals[m]
		t.Logf("  %-28s %7d %14d %14d %10d %10d", m, mt.calls, mt.uncached, mt.cached, mt.thoughts, mt.response)
	}
}
