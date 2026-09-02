package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursors.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := s.Get(event.SourceClaudeCode); got != nil {
		t.Errorf("empty store returned %q, want nil", got)
	}

	want := collector.Cursor(`{"files":{"a.jsonl":{"offset":42}}}`)
	if err := s.Set(event.SourceClaudeCode, want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set(event.SourceCodex, collector.Cursor(`{"files":{}}`)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := string(reopened.Get(event.SourceClaudeCode)); got != string(want) {
		t.Errorf("cursor = %s, want %s", got, want)
	}
	if got := reopened.Sources(); len(got) != 2 || got[0] != "claude_code" || got[1] != "codex" {
		t.Errorf("Sources() = %v", got)
	}
	if reopened.Size(event.SourceClaudeCode) != len(want) {
		t.Errorf("Size = %d, want %d", reopened.Size(event.SourceClaudeCode), len(want))
	}
}

func TestSavedFileIsReadableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cursors.json") // dir must be created
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(event.SourceCodex, collector.Cursor(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("cursors.json is not a JSON object: %v (%s)", err, raw)
	}
	if string(got["codex"]) != `{"n":1}` {
		t.Errorf("stored cursor = %s, want the collector JSON verbatim", got["codex"])
	}
	// The temp file used for the atomic rename must not survive.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only cursors.json", len(entries))
	}
}

func TestSetRejectsNonJSON(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cursors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(event.SourceCodex, collector.Cursor("not json")); err == nil {
		t.Fatal("Set accepted a non-JSON cursor")
	}
	if got := s.Get(event.SourceCodex); got != nil {
		t.Errorf("rejected cursor was stored anyway: %s", got)
	}
}

func TestSetEmptyClears(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cursors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(event.SourceCodex, collector.Cursor(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(event.SourceCodex, nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(event.SourceCodex); got != nil {
		t.Errorf("cursor survived a nil Set: %s", got)
	}
}

func TestCorruptFileStartsOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursors.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A truncated cursor file must not stop the agent: re-reading the logs is
	// cheap and the server drops the duplicates.
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a corrupt file: %v", err)
	}
	if got := s.Get(event.SourceCodex); got != nil {
		t.Errorf("corrupt store returned %s, want nil", got)
	}
}

func TestGetReturnsACopy(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cursors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(event.SourceCodex, collector.Cursor(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	got := s.Get(event.SourceCodex)
	got[0] = 'X'
	if string(s.Get(event.SourceCodex)) != `{"a":1}` {
		t.Error("the caller can mutate the stored cursor through Get")
	}
}
