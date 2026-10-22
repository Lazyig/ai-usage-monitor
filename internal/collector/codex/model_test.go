package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"

	"ai-usage-monitor/internal/event"
)

// desktopRollout is what Codex Desktop writes today: no session_meta.model,
// the starting model under base_instructions.provenance, a switch announced by
// thread_settings_applied and a later turn_context.
const desktopRollout = `{"timestamp":"2026-10-21T13:43:00.000Z","type":"session_meta","payload":{"id":"sess-desk","session_id":"sess-desk","cwd":"C:\\work","cli_version":"0.170.0","base_instructions":{"text":"...","provenance":{"type":"model","model":"gpt-6.1-sol"}}}}
{"timestamp":"2026-10-21T13:43:01.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"total_tokens":110}}}}
{"timestamp":"2026-10-21T13:43:02.000Z","type":"event_msg","payload":{"type":"thread_settings_applied","thread_settings":{"model":"gpt-6-astra"}}}
{"timestamp":"2026-10-21T13:43:03.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":200,"cached_input_tokens":0,"output_tokens":20,"total_tokens":220}}}}
{"timestamp":"2026-10-21T13:43:04.000Z","type":"turn_context","payload":{"model":"gpt-6-luna"}}
{"timestamp":"2026-10-21T13:43:05.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":300,"cached_input_tokens":0,"output_tokens":30,"total_tokens":330}}}}
`

func models(evs []event.UsageEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Model
	}
	return out
}

func TestModelFromProvenanceThreadSettingsAndTurnContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "2026", "10", "21", "rollout-desk.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(desktopRollout), 0o644); err != nil {
		t.Fatal(err)
	}

	evs, _ := scan(t, New(root), nil)
	want := []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna"}
	if got := models(evs); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("models = %v, want %v", got, want)
	}
}

// Codex compresses finished rollouts in place; the archived copy must yield
// the very same events (same ids), so nothing is counted twice.
func TestCompressedRolloutYieldsTheSameEvents(t *testing.T) {
	plainRoot := t.TempDir()
	plainPath := filepath.Join(plainRoot, "2026", "10", "21", "rollout-desk.jsonl")
	if err := os.MkdirAll(filepath.Dir(plainPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plainPath, []byte(desktopRollout), 0o644); err != nil {
		t.Fatal(err)
	}
	plain, _ := scan(t, New(plainRoot), nil)

	packedRoot := t.TempDir()
	packedPath := filepath.Join(packedRoot, "2026", "10", "21", "rollout-desk.jsonl.zst")
	if err := os.MkdirAll(filepath.Dir(packedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packedPath, enc.EncodeAll([]byte(desktopRollout), nil), 0o644); err != nil {
		t.Fatal(err)
	}
	enc.Close()
	packed, cur := scan(t, New(packedRoot), nil)

	if len(packed) != len(plain) {
		t.Fatalf("archive gave %d events, plain file %d", len(packed), len(plain))
	}
	for i := range plain {
		if packed[i].EventID != plain[i].EventID || packed[i].Model != plain[i].Model || packed[i].Input != plain[i].Input {
			t.Errorf("event %d: archive %+v, plain %+v", i, packed[i], plain[i])
		}
	}

	again, _ := scan(t, New(packedRoot), cur)
	if len(again) != 0 {
		t.Fatalf("unchanged archive re-emitted %d events", len(again))
	}
}
