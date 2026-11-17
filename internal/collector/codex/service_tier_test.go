package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/klauspost/compress/zstd"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

const tierUsage = `{"timestamp":"2026-11-16T08:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10,"total_tokens":110}}}}` + "\n"

// Scan after every append with a fresh collector, as an agent does after a
// restart. Settings belong to their following usage, never the whole session.
func TestServiceTierTransitionsResumeAndLegacyCursor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-tiers.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session_meta","payload":{"id":"tier-session","model":"gpt-6.1-sol"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	steps := []struct{ settings, want string }{
		{"", ""},
		{`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6.1-sol","service_tier":"default"}}}`, "default"},
		{`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6.1-sol","service_tier":"priority"}}}`, "fast"},
		{`{"type":"turn_context","payload":{"model":"gpt-6.1-sol"}}`, "fast"},
		{`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_id":"other-thread","thread_settings":{"model":"other-model","service_tier":"default"}}}`, "fast"},
		{`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6.1-sol","service_tier":"default"}}}`, "default"},
		{`{"type":"turn_context","payload":{"model":"gpt-6.1-sol","service_tier":"fast"}}`, "fast"},
		{`{"type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6.1-sol"}}}`, ""},
		{`{"type":"turn_context","payload":{"model":"gpt-6.1-sol","service_tier":"priority"}}`, "fast"},
		{`{"type":"turn_context","payload":{"model":"gpt-6.1-sol","service_tier":null}}`, ""},
		{`{"type":"turn_context","payload":{"model":"gpt-6.1-sol","service_tier":"auto"}}`, "auto"},
		{`{"type":"turn_context","payload":{"model":"gpt-6.1-sol","service_tier":"flex"}}`, "flex"},
	}
	var cur collector.Cursor
	var all []event.UsageEvent
	for i, step := range steps {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := f.WriteString(step.settings + "\n" + tierUsage)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("append: %v / %v", writeErr, closeErr)
		}
		evs, next := scan(t, New(root), cur)
		if len(evs) != 1 {
			t.Fatalf("step %d: got %d events, want 1", i, len(evs))
		}
		e := evs[0]
		if e.ServiceTier != step.want || e.Model != "gpt-6.1-sol" || e.Input != 80 || e.Output != 10 || e.CacheRead != 20 {
			t.Fatalf("step %d: %+v, want tier %q with original model/tokens", i, e, step.want)
		}
		all = append(all, e)
		cur = next
	}
	if again, _ := scan(t, New(root), cur); len(again) != 0 {
		t.Fatal("unchanged files re-emitted events")
	}

	// Simulate the previously shipped cursor format. Metadata backfill must
	// emit the same IDs and tokens, then return to incremental scanning.
	var legacy struct {
		Files map[string]map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(cur, &legacy); err != nil {
		t.Fatal(err)
	}
	for _, state := range legacy.Files {
		delete(state, "context_version")
		delete(state, "service_tier")
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	replayed, upgraded := scan(t, New(root), raw)
	if !reflect.DeepEqual(replayed, all) {
		t.Fatalf("legacy cursor replay changed event identities or metadata: %+v", replayed)
	}
	if again, _ := scan(t, New(root), upgraded); len(again) != 0 {
		t.Fatal("upgraded cursor repeatedly rescans")
	}

	// Archives keep decompressed offsets, including for tier switches.
	plain, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	packedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(packedRoot, "rollout-tiers.jsonl.zst"), enc.EncodeAll(plain, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	packed, packedCur := scan(t, New(packedRoot), nil)
	if !reflect.DeepEqual(packed, all) {
		t.Fatal("compressed rollout changed tier, tokens or event ID")
	}
	if again, _ := scan(t, New(packedRoot), packedCur); len(again) != 0 {
		t.Fatal("unchanged archive re-emitted events")
	}
}
