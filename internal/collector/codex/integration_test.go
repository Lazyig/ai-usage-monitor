package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestIntegrationRealHome reads the real Codex rollout files on this machine
// read-only. It only checks sanity, never exact numbers, and skips when the tool
// is not installed. Opt in with AI_USAGE_INTEGRATION=1; -v prints totals.
func TestIntegrationRealHome(t *testing.T) {
	if os.Getenv("AI_USAGE_INTEGRATION") != "1" {
		t.Skip("real-home logs require AI_USAGE_INTEGRATION=1")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	root := filepath.Join(home, ".codex", "sessions")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("%s not present: %v", root, err)
	}

	evs, cur, err := New(root).Scan(context.Background(), "integration-device", nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("expected at least one event from the real rollout files")
	}
	if len(cur) == 0 {
		t.Fatal("expected a non-empty cursor")
	}

	ids := map[string]struct{}{}
	for _, e := range evs {
		if !e.Valid() {
			t.Fatalf("invalid event: %+v", e)
		}
		if e.Timestamp.IsZero() || e.Timestamp.Location().String() != "UTC" {
			t.Fatalf("timestamp must be a parsed UTC time: %+v", e)
		}
		if e.Input < 0 || e.Output < 0 || e.CacheRead < 0 || e.CacheWrite < 0 || e.Reasoning < 0 {
			t.Fatalf("negative token count: %+v", e)
		}
		if e.Reasoning > e.Output {
			t.Fatalf("reasoning must be a subset of output: %+v", e)
		}
		ids[e.EventID] = struct{}{}
	}
	// session id + line offset is unique by construction, so every event must
	// have its own EventID.
	if len(ids) != len(evs) {
		t.Errorf("Scan returned %d events for only %d distinct EventIDs", len(evs), len(ids))
	}

	// A second scan with the returned cursor must not re-emit the whole history.
	again, _, err := New(root).Scan(context.Background(), "integration-device", cur)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(again) > len(evs)/10 {
		t.Errorf("cursor did not take effect: %d events re-emitted out of %d", len(again), len(evs))
	}

	type totals struct {
		calls, in, out, cr, cw, reason int64
	}
	byModel := map[string]*totals{}
	for _, e := range evs {
		tt := byModel[e.Model]
		if tt == nil {
			tt = &totals{}
			byModel[e.Model] = tt
		}
		tt.calls++
		tt.in += e.Input
		tt.out += e.Output
		tt.cr += e.CacheRead
		tt.cw += e.CacheWrite
		tt.reason += e.Reasoning
	}
	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)

	var b strings.Builder
	fmt.Fprintf(&b, "\ncodex: %d events from %s\n", len(evs), root)
	const row = "%-24s %8s %14s %12s %16s %12s %12s\n"
	fmt.Fprintf(&b, row, "model", "calls", "input", "output", "cache_read", "cache_write", "reasoning")
	var all totals
	for _, m := range models {
		tt := byModel[m]
		fmt.Fprintf(&b, row, m, commas(tt.calls), commas(tt.in), commas(tt.out), commas(tt.cr), commas(tt.cw), commas(tt.reason))
		all.calls += tt.calls
		all.in += tt.in
		all.out += tt.out
		all.cr += tt.cr
		all.cw += tt.cw
		all.reason += tt.reason
	}
	fmt.Fprintf(&b, row, "TOTAL", commas(all.calls), commas(all.in), commas(all.out), commas(all.cr), commas(all.cw), commas(all.reason))
	t.Log(b.String())
}

// commas formats v with thousands separators so the eyeball table stays readable.
func commas(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}
