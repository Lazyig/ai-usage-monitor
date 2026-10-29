package codex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

const testDevice = "dev-test"

var (
	sessOne = filepath.Join("2026", "08", "01", "rollout-2026-08-01T09-00-00-sess-one.jsonl")
	sessTwo = filepath.Join("2026", "08", "02", "rollout-2026-08-02T11-30-00-sess-two.jsonl")
)

// copyTree materializes testdata under a writable temp dir so tests can append
// to and truncate the rollout files.
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

// lineOffsets returns the byte offset of the start of each complete line, which
// is the second half of the codex native dedupe key.
func lineOffsets(t *testing.T, path string) []int64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var (
		offs []int64
		off  int64
	)
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			break // trailing partial line
		}
		offs = append(offs, off)
		off += int64(i + 1)
		b = b[i+1:]
	}
	return offs
}

func scan(t *testing.T, c collector.Collector, cur collector.Cursor) ([]event.UsageEvent, collector.Cursor) {
	t.Helper()
	evs, next, err := c.Scan(context.Background(), testDevice, cur)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return evs, next
}

func eventID(sessionID string, offset int64) string {
	return event.NewEventID(event.SourceCodex, sessionID, strconv.FormatInt(offset, 10))
}

func TestScanMapping(t *testing.T) {
	root := filepath.Join("testdata", "sessions")
	oneOff := lineOffsets(t, filepath.Join(root, sessOne))
	twoOff := lineOffsets(t, filepath.Join(root, sessTwo))

	evs, cur := scan(t, New(root), nil)
	if len(cur) == 0 {
		t.Fatal("expected a non-empty cursor")
	}
	if len(evs) != 4 {
		t.Fatalf("want 4 events, got %d: %+v", len(evs), evs)
	}

	want := []struct {
		name  string
		got   event.UsageEvent
		event event.UsageEvent
	}{
		{
			name: "before any turn_context and without a session_meta model",
			got:  evs[0],
			event: event.UsageEvent{
				EventID:   eventID("sess-one", oneOff[2]),
				Timestamp: time.Date(2026, 8, 1, 9, 0, 2, 0, time.UTC),
				Model:     event.UnknownModel, SessionID: "sess-one", Project: "/home/user/proj-one",
				Input: 200, Output: 50, CacheRead: 800, Reasoning: 30, ToolVersion: "0.150.0",
			},
		},
		{
			name: "input excludes cached, cache_write mapped, model from turn_context",
			got:  evs[1],
			event: event.UsageEvent{
				EventID:   eventID("sess-one", oneOff[5]),
				Timestamp: time.Date(2026, 8, 1, 9, 0, 5, 0, time.UTC),
				Model:     "gpt-5.6-terra", SessionID: "sess-one", Project: "/home/user/proj-one",
				Input: 600, Output: 370, CacheRead: 1600, CacheWrite: 128, Reasoning: 270, ToolVersion: "0.150.0",
			},
		},
		{
			name: "a later turn_context switches the model",
			got:  evs[2],
			event: event.UsageEvent{
				EventID:   eventID("sess-one", oneOff[8]),
				Timestamp: time.Date(2026, 8, 1, 9, 0, 8, 0, time.UTC),
				Model:     "codex-auto-review", SessionID: "sess-one", Project: "/home/user/proj-one",
				Input: 200, Output: 80, CacheRead: 600, Reasoning: 30, ToolVersion: "0.150.0",
			},
		},
		{
			name: "sub-agent session, model taken from session_meta",
			got:  evs[3],
			event: event.UsageEvent{
				EventID:   eventID("sess-two", twoOff[1]),
				Timestamp: time.Date(2026, 8, 2, 11, 30, 1, 0, time.UTC),
				Model:     "gpt-5.6-sol", SessionID: "sess-two", Project: "/home/user/proj-two",
				Input: 600, Output: 90, CacheWrite: 512, Reasoning: 40, ToolVersion: "0.151.0",
			},
		},
	}

	for _, tc := range want {
		t.Run(tc.name, func(t *testing.T) {
			exp := tc.event
			exp.DeviceID, exp.Source, exp.Confidence = testDevice, event.SourceCodex, event.ConfidenceExact
			if tc.got != exp {
				t.Errorf("got  %+v\nwant %+v", tc.got, exp)
			}
		})
	}
}

func TestScanSkipsUnusableTokenCounts(t *testing.T) {
	root := filepath.Join("testdata", "sessions")
	oneOff := lineOffsets(t, filepath.Join(root, sessOne))
	evs, _ := scan(t, New(root), nil)

	skipped := map[string]string{
		eventID("sess-one", oneOff[1]): "token_count with info:null",
		eventID("sess-one", oneOff[6]): "token_count with an all-zero last_token_usage",
	}
	for _, e := range evs {
		if why, bad := skipped[e.EventID]; bad {
			t.Errorf("%s must be skipped, got %+v", why, e)
		}
	}
}

func TestPartialLineIsNotConsumed(t *testing.T) {
	root := copyTree(t, filepath.Join("testdata", "sessions"))
	path := filepath.Join(root, sessTwo)

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if b[len(b)-1] == '\n' {
		t.Fatal("testdata sess-two lost its incomplete trailing line")
	}
	partialStart := int64(bytes.LastIndexByte(b, '\n') + 1)

	c := New(root)
	_, cur := scan(t, c, nil)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`0},"last_token_usage":{"input_tokens":600,"cached_input_tokens":400,"cache_write_input_tokens":0,"output_tokens":50,"reasoning_output_tokens":20,"total_tokens":650}}}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	evs, _ := scan(t, c, cur)
	if len(evs) != 1 {
		t.Fatalf("want 1 event after completing the partial line, got %d: %+v", len(evs), evs)
	}
	if evs[0].EventID != eventID("sess-two", partialStart) {
		t.Errorf("native key must use the offset of the line start, got %+v", evs[0])
	}
	if evs[0].Input != 200 || evs[0].CacheRead != 400 || evs[0].Output != 50 {
		t.Errorf("usage not read from the completed line: %+v", evs[0])
	}
	if evs[0].Model != "gpt-5.6-sol" {
		t.Errorf("session model must survive the resume, got %q", evs[0].Model)
	}
}

func TestCursorResumeKeepsSessionContext(t *testing.T) {
	root := copyTree(t, filepath.Join("testdata", "sessions"))
	path := filepath.Join(root, sessOne)
	c := New(root)

	first, cur := scan(t, c, nil)
	if len(first) != 4 {
		t.Fatalf("want 4 events on the first scan, got %d", len(first))
	}

	again, cur := scan(t, c, cur)
	if len(again) != 0 {
		t.Fatalf("want 0 events on an unchanged rescan, got %d: %+v", len(again), again)
	}

	// Append a token_count with no preceding turn_context in the appended range:
	// the model, session id, cwd and cli version must come from the cursor.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	appendedAt := fi.Size()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	line := `{"timestamp":"2026-08-01T09:10:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":900,"cached_input_tokens":100,"cache_write_input_tokens":0,"output_tokens":25,"reasoning_output_tokens":5,"total_tokens":925}}}}` + "\n"
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
	f.Close()

	evs, cur := scan(t, c, cur)
	if len(evs) != 1 {
		t.Fatalf("want only the appended event, got %d: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.EventID != eventID("sess-one", appendedAt) {
		t.Errorf("unexpected native key for the appended line: %+v", ev)
	}
	if ev.Model != "codex-auto-review" || ev.SessionID != "sess-one" ||
		ev.Project != "/home/user/proj-one" || ev.ToolVersion != "0.150.0" {
		t.Errorf("session context lost across the resume: %+v", ev)
	}
	if ev.Input != 800 || ev.CacheRead != 100 || ev.Output != 25 || ev.Reasoning != 5 {
		t.Errorf("bad mapping for the appended line: %+v", ev)
	}

	// Truncating to just the session_meta plus that line forces a full re-read.
	meta, err := os.ReadFile(filepath.Join("testdata", "sessions", sessOne))
	if err != nil {
		t.Fatal(err)
	}
	head := meta[:bytes.IndexByte(meta, '\n')+1]
	if err := os.WriteFile(path, append(append([]byte{}, head...), line...), 0o644); err != nil {
		t.Fatal(err)
	}
	reread, _ := scan(t, c, cur)
	if len(reread) != 1 {
		t.Fatalf("want a full re-read of the truncated file, got %d: %+v", len(reread), reread)
	}
	if reread[0].Model != event.UnknownModel {
		t.Errorf("a re-read file has no turn_context yet, want %q, got %q", event.UnknownModel, reread[0].Model)
	}
}

func TestCursorForgetsDeletedFiles(t *testing.T) {
	root := copyTree(t, filepath.Join("testdata", "sessions"))
	c := New(root)
	_, cur := scan(t, c, nil)

	if err := os.RemoveAll(filepath.Join(root, "2026", "08", "02")); err != nil {
		t.Fatal(err)
	}
	_, cur = scan(t, c, cur)

	st := decodeCursor(cur)
	if len(st.Files) != 1 {
		t.Errorf("want 1 tracked file, got %d: %v", len(st.Files), st.Files)
	}
	if _, ok := st.Files[filepath.ToSlash(sessTwo)]; ok {
		t.Error("cursor must drop entries for files that no longer exist")
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
	evs, _ := scan(t, New(filepath.Join("testdata", "sessions")), collector.Cursor("not json"))
	if len(evs) != 4 {
		t.Fatalf("a corrupt cursor must fall back to a full scan, got %d events", len(evs))
	}
}

func TestName(t *testing.T) {
	if got := New("x").Name(); got != event.SourceCodex {
		t.Errorf("Name() = %q", got)
	}
}
