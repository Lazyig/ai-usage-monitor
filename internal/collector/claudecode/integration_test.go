package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestIntegrationRealHome reads the real Claude Code transcripts on this machine
// read-only. It only checks sanity, never exact numbers, and skips when the tool
// is not installed. Run with -v to see the per-model totals table.
func TestIntegrationRealHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	root := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("%s not present: %v", root, err)
	}

	pairsBefore := countNativeKeys(t, root)

	evs, cur, err := New(root).Scan(context.Background(), "integration-device", nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("expected at least one event from the real transcripts")
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
		if e.Input < 0 || e.Output < 0 || e.CacheRead < 0 || e.CacheWrite5m < 0 || e.CacheWrite1h < 0 || e.Reasoning < 0 {
			t.Fatalf("negative token count: %+v", e)
		}
		ids[e.EventID] = struct{}{}
	}

	// One event per distinct message.id+requestId pair, counted independently of
	// the collector. The live transcripts may grow while the test runs (Claude
	// Code could be running right now), so the pair count is taken both before
	// and after the scan and the result has to land inside that window.
	if len(ids) != len(evs) {
		t.Errorf("Scan returned %d events for %d distinct EventIDs", len(evs), len(ids))
	}
	if pairsAfter := countNativeKeys(t, root); len(ids) < pairsBefore || len(ids) > pairsAfter {
		t.Errorf("distinct EventIDs = %d, want between %d and %d distinct message.id+requestId pairs",
			len(ids), pairsBefore, pairsAfter)
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
		calls, in, out, cr, w5m, w1h, think int64
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
		tt.w5m += e.CacheWrite5m
		tt.w1h += e.CacheWrite1h
		tt.think += e.Reasoning
	}
	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)

	var b strings.Builder
	fmt.Fprintf(&b, "\nclaude_code: %d events from %s\n", len(evs), root)
	const row = "%-24s %8s %12s %12s %14s %12s %12s %12s\n"
	fmt.Fprintf(&b, row, "model", "calls", "input", "output", "cache_read", "write_5m", "write_1h", "thinking")
	var all totals
	for _, m := range models {
		tt := byModel[m]
		fmt.Fprintf(&b, row, m, itoa(tt.calls), itoa(tt.in), itoa(tt.out), itoa(tt.cr), itoa(tt.w5m), itoa(tt.w1h), itoa(tt.think))
		all.calls += tt.calls
		all.in += tt.in
		all.out += tt.out
		all.cr += tt.cr
		all.w5m += tt.w5m
		all.w1h += tt.w1h
		all.think += tt.think
	}
	fmt.Fprintf(&b, row, "TOTAL", itoa(all.calls), itoa(all.in), itoa(all.out), itoa(all.cr), itoa(all.w5m), itoa(all.w1h), itoa(all.think))
	t.Log(b.String())
}

// countNativeKeys re-derives the expected number of events straight from the
// files, independently of the collector's own decoding.
func countNativeKeys(t *testing.T, root string) int {
	t.Helper()
	files, err := jsonlFiles(root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	keys := map[string]struct{}{}
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		r := bufio.NewReaderSize(f, 256<<10)
		for {
			raw, err := r.ReadBytes('\n')
			if len(raw) > 0 && raw[len(raw)-1] == '\n' {
				var l struct {
					Type              string `json:"type"`
					UUID              string `json:"uuid"`
					RequestID         string `json:"requestId"`
					Timestamp         string `json:"timestamp"`
					IsAPIErrorMessage bool   `json:"isApiErrorMessage"`
					Message           *struct {
						ID    string          `json:"id"`
						Model string          `json:"model"`
						Usage json.RawMessage `json:"usage"`
					} `json:"message"`
				}
				if json.Unmarshal(raw, &l) == nil &&
					l.Type == "assistant" && l.Message != nil && len(l.Message.Usage) > 0 &&
					!l.IsAPIErrorMessage && l.Message.Model != syntheticModel &&
					l.Timestamp != "" {
					id := l.Message.ID
					if id == "" {
						id = l.UUID
					}
					if id != "" {
						keys[id+"\x00"+l.RequestID] = struct{}{}
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					t.Fatalf("read %s: %v", path, err)
				}
				break
			}
		}
		f.Close()
	}
	return len(keys)
}

// itoa formats v with thousands separators so the eyeball table stays readable.
func itoa(v int64) string {
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
