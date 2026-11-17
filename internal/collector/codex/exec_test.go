package codex

import (
	"os"
	"path/filepath"
	"testing"
)

// Anonymized metadata and accounting from a codex-cli 0.167.0 exec run.
// Prompt/response text and account information are deliberately excluded.
const execLunaRollout = `{"timestamp":"2026-11-11T00:28:04.000Z","type":"session_meta","payload":{"id":"sess-exec-luna","cwd":"/work/translation","cli_version":"0.167.0","originator":"codex_exec","source":"exec","thread_source":"user"}}
{"timestamp":"2026-11-11T00:28:05.000Z","type":"turn_context","payload":{"model":"gpt-6-luna"}}
{"timestamp":"2026-11-11T00:28:09.577Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":9547,"cached_input_tokens":7680,"output_tokens":5,"total_tokens":9552},"last_token_usage":{"input_tokens":9547,"cached_input_tokens":7680,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0,"total_tokens":9552}}}}
`

// --json stdout uses a different schema from the persisted rollout. Even
// saving this stream as a .jsonl file does not make it a collectible rollout.
const ephemeralLunaStdout = `{"type":"thread.started","thread_id":"sess-ephemeral-luna"}
{"type":"turn.started"}
{"type":"turn.completed","usage":{"input_tokens":9547,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}
`

func TestExecLunaCollection(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want int
	}{
		{name: "persisted exec rollout", data: execLunaRollout, want: 1},
		{name: "ephemeral leaves no rollout", want: 0},
		{name: "saved exec stdout is not a rollout", data: ephemeralLunaStdout, want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.data != "" {
				if err := os.WriteFile(filepath.Join(root, "exec.jsonl"), []byte(tt.data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			c := New(root)
			evs, cur := scan(t, c, nil)
			if len(evs) != tt.want {
				t.Fatalf("got %d events, want %d: %+v", len(evs), tt.want, evs)
			}
			if tt.want > 0 {
				e := evs[0]
				if e.Model != "gpt-6-luna" || e.SessionID != "sess-exec-luna" || e.Project != "/work/translation" || e.ToolVersion != "0.167.0" {
					t.Fatalf("exec session context lost: %+v", e)
				}
				if e.Input != 1867 || e.CacheRead != 7680 || e.Output != 5 || e.TotalInput() != 9547 {
					t.Fatalf("usage differs from exec turn.completed: %+v", e)
				}
			}
			if again, _ := scan(t, c, cur); len(again) != 0 {
				t.Fatalf("unchanged exec scan re-emitted %d events", len(again))
			}
		})
	}
}
