package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

const testDevice = "dev-test"

// copyTree materializes testdata under a writable temp dir so tests can append
// to and truncate the transcripts.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy testdata: %v", err)
	}
	return dst
}

func scan(t *testing.T, c collector.Collector, cur collector.Cursor) ([]event.UsageEvent, collector.Cursor) {
	t.Helper()
	evs, next, err := c.Scan(context.Background(), testDevice, cur)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return evs, next
}

func byID(evs []event.UsageEvent) map[string]event.UsageEvent {
	m := make(map[string]event.UsageEvent, len(evs))
	for _, e := range evs {
		m[e.EventID] = e
	}
	return m
}

func eventID(msgID, reqID string) string {
	return event.NewEventID(testDevice, event.SourceClaudeCode, msgID, reqID)
}

func TestScanMapping(t *testing.T) {
	root := filepath.Join("testdata", "projects")
	evs, cur := scan(t, New(root), nil)
	if len(cur) == 0 {
		t.Fatal("expected a non-empty cursor")
	}

	got := byID(evs)
	if len(evs) != 5 {
		t.Fatalf("want 5 events, got %d: %+v", len(evs), evs)
	}

	want := []struct {
		name  string
		id    string
		event event.UsageEvent
	}{
		{
			name: "dedupe keeps the last streamed usage",
			id:   eventID("msg_a001", "req_a1"),
			event: event.UsageEvent{
				Timestamp: time.Date(2026, 8, 1, 10, 0, 3, 0, time.UTC),
				Model:     "claude-opus-5", SessionID: "sess-alpha", Project: "/home/user/proj-alpha",
				Input: 12, Output: 355, CacheRead: 1000, CacheWrite5m: 200, CacheWrite1h: 300,
				Reasoning: 120, ToolVersion: "2.1.0",
			},
		},
		{
			name: "missing cache_creation falls back to the flat total as 5m",
			id:   eventID("msg_a002", "req_a2"),
			event: event.UsageEvent{
				Timestamp: time.Date(2026, 8, 1, 10, 1, 0, 0, time.UTC),
				Model:     "claude-sonnet-5", SessionID: "sess-alpha", Project: "/home/user/proj-alpha",
				Input: 5, Output: 77, CacheRead: 2000, CacheWrite5m: 1500, ToolVersion: "2.1.0",
			},
		},
		{
			name: "missing message.id falls back to the line uuid",
			id:   eventID("uuid-a009", "req_a9"),
			event: event.UsageEvent{
				Timestamp: time.Date(2026, 8, 1, 10, 4, 0, 0, time.UTC),
				Model:     "claude-fable-5-1", SessionID: "sess-alpha", Project: "/home/user/proj-alpha",
				Input: 3, Output: 8, ToolVersion: "2.1.0",
			},
		},
		{
			name: "sub-agent transcript in a session subdirectory",
			id:   eventID("msg_b001", "req_b1"),
			event: event.UsageEvent{
				Timestamp: time.Date(2026, 8, 1, 10, 5, 0, 0, time.UTC),
				Model:     "claude-sonnet-5", SessionID: "sess-alpha-sub", Project: "/home/user/proj-alpha",
				Input: 7, Output: 21, CacheRead: 4000, ToolVersion: "2.1.0",
			},
		},
		{
			name: "complete line before a partial trailing line",
			id:   eventID("msg_c001", "req_c1"),
			event: event.UsageEvent{
				Timestamp: time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC),
				Model:     "claude-opus-5", SessionID: "sess-beta", Project: "/home/user/proj-beta",
				Input: 30, Output: 60, ToolVersion: "2.1.1",
			},
		},
	}

	for _, tc := range want {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := got[tc.id]
			if !ok {
				t.Fatalf("event %s missing", tc.id)
			}
			exp := tc.event
			exp.EventID, exp.DeviceID = tc.id, testDevice
			exp.Source, exp.Confidence = event.SourceClaudeCode, event.ConfidenceExact
			if ev != exp {
				t.Errorf("got  %+v\nwant %+v", ev, exp)
			}
		})
	}
}

func TestScanSkipsUnwantedLines(t *testing.T) {
	evs, _ := scan(t, New(filepath.Join("testdata", "projects")), nil)
	for _, e := range evs {
		switch {
		case e.SessionID == "sess-memory":
			t.Error("memory/ directory must be skipped")
		case e.Model == syntheticModel:
			t.Error("<synthetic> model must be skipped")
		case e.EventID == eventID("msg_a003", "req_a3"):
			t.Error("isApiErrorMessage line must be skipped")
		case e.EventID == eventID("msg_c002", "req_c2"):
			t.Error("partial trailing line must not be emitted")
		}
	}
}

func TestPartialLineIsNotConsumed(t *testing.T) {
	root := copyTree(t, filepath.Join("testdata", "projects"))
	path := filepath.Join(root, "-home-user-proj-beta", "partial.jsonl")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if before[len(before)-1] == '\n' {
		t.Fatal("testdata partial.jsonl lost its incomplete trailing line")
	}

	c := New(root)
	_, cur := scan(t, c, nil)

	// Completing the line must yield exactly that one new event next scan.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`tant","usage":{"input_tokens":40,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	evs, _ := scan(t, c, cur)
	if len(evs) != 1 {
		t.Fatalf("want 1 event after completing the partial line, got %d: %+v", len(evs), evs)
	}
	if evs[0].EventID != eventID("msg_c002", "req_c2") {
		t.Errorf("unexpected event %+v", evs[0])
	}
	if evs[0].Input != 40 || evs[0].Output != 80 {
		t.Errorf("usage not read from the completed line: %+v", evs[0])
	}
}

func TestCursorResume(t *testing.T) {
	root := copyTree(t, filepath.Join("testdata", "projects"))
	c := New(root)

	first, cur := scan(t, c, nil)
	if len(first) != 5 {
		t.Fatalf("want 5 events on the first scan, got %d", len(first))
	}

	// Nothing changed: a repeat scan must be empty.
	again, cur := scan(t, c, cur)
	if len(again) != 0 {
		t.Fatalf("want 0 events on an unchanged rescan, got %d: %+v", len(again), again)
	}

	// Append one message: only that one comes back.
	path := filepath.Join(root, "-home-user-proj-alpha", "sess-alpha.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","uuid":"uuid-a010","requestId":"req_a10","timestamp":"2026-08-01T11:00:00.000Z","sessionId":"sess-alpha","cwd":"/home/user/proj-alpha","version":"2.1.0","message":{"id":"msg_a010","model":"claude-opus-5","role":"assistant","usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n"
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	f.Close()

	appended, cur := scan(t, c, cur)
	if len(appended) != 1 || appended[0].EventID != eventID("msg_a010", "req_a10") {
		t.Fatalf("want only the appended message, got %+v", appended)
	}

	// Truncate the same file: the shrink must force a full re-read of it.
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	reread, _ := scan(t, c, cur)
	if len(reread) != 1 || reread[0].EventID != eventID("msg_a010", "req_a10") {
		t.Fatalf("want a full re-read of the truncated file, got %+v", reread)
	}
}

func TestCursorForgetsDeletedFiles(t *testing.T) {
	root := copyTree(t, filepath.Join("testdata", "projects"))
	c := New(root)
	_, cur := scan(t, c, nil)

	if err := os.RemoveAll(filepath.Join(root, "-home-user-proj-beta")); err != nil {
		t.Fatal(err)
	}
	_, cur = scan(t, c, cur)

	st := decodeCursor(cur)
	if _, ok := st.Files["-home-user-proj-beta/partial.jsonl"]; ok {
		t.Error("cursor must drop entries for files that no longer exist")
	}
	if len(st.Files) != 2 {
		t.Errorf("want 2 tracked files, got %d: %v", len(st.Files), st.Files)
	}
}

func TestMissingRootIsNotAnError(t *testing.T) {
	evs, cur, err := New(filepath.Join(t.TempDir(), "nope")).Scan(context.Background(), testDevice, nil)
	if err != nil {
		t.Fatalf("missing root must not error: %v", err)
	}
	if len(evs) != 0 || cur != nil {
		t.Errorf("want no events and a nil cursor, got %d events / %q", len(evs), cur)
	}
}

func TestCorruptCursorRereadsEverything(t *testing.T) {
	evs, _ := scan(t, New(filepath.Join("testdata", "projects")), collector.Cursor("not json"))
	if len(evs) != 5 {
		t.Fatalf("a corrupt cursor must fall back to a full scan, got %d events", len(evs))
	}
}

func TestName(t *testing.T) {
	if got := New("x").Name(); got != event.SourceClaudeCode {
		t.Errorf("Name() = %q", got)
	}
}
