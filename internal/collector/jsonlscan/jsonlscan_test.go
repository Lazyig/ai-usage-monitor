package jsonlscan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// plainScanner is the shape a collector with no extra per-file state uses.
type plainScanner = Scanner[FileState, *FileState]

// collected records every line a scan handed to the collector.
type collected struct {
	offsets []int64
	lines   []string
}

func (c *collected) line(_ *FileState, off int64, raw []byte) error {
	c.offsets = append(c.offsets, off)
	c.lines = append(c.lines, strings.TrimSuffix(string(raw), "\n"))
	return nil
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
}

func scanOnce(t *testing.T, sc plainScanner, prev Cursor[FileState]) Cursor[FileState] {
	t.Helper()
	next, err := sc.Scan(context.Background(), prev)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return next
}

func TestScanStopsAtTheLastCompleteLine(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.jsonl"), "one\ntwo\nhalf")

	var got collected
	sc := plainScanner{Root: root, Name: "test", Line: got.line}
	cur := scanOnce(t, sc, DecodeCursor[FileState](nil))

	if want := []string{"one", "two"}; len(got.lines) != 2 || got.lines[0] != want[0] || got.lines[1] != want[1] {
		t.Fatalf("lines = %v, want %v (the partial trailing line must not be consumed)", got.lines, want)
	}
	if got.offsets[0] != 0 || got.offsets[1] != 4 {
		t.Errorf("offsets = %v, want [0 4]", got.offsets)
	}
	st := cur.Files["a.jsonl"]
	if st.Offset != 8 {
		t.Errorf("offset = %d, want 8 (just past \"two\\n\")", st.Offset)
	}
	if st.Size != 12 {
		t.Errorf("size = %d, want 12 (the whole file, partial line included)", st.Size)
	}

	// Completing the half written line makes the next scan read it whole, once.
	appendTo(t, filepath.Join(root, "a.jsonl"), "-done\n")
	got = collected{}
	scanOnce(t, sc, cur)
	if len(got.lines) != 1 || got.lines[0] != "half-done" {
		t.Fatalf("lines = %v, want [half-done]", got.lines)
	}
	if got.offsets[0] != 8 {
		t.Errorf("offset = %d, want 8", got.offsets[0])
	}
}

func TestScanResumesAndReportsNothingWhenIdle(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.jsonl"), "one\n")

	var got collected
	sc := plainScanner{Root: root, Name: "test", Line: got.line}
	cur := scanOnce(t, sc, DecodeCursor[FileState](nil))

	got = collected{}
	cur = scanOnce(t, sc, cur)
	if len(got.lines) != 0 {
		t.Fatalf("an unchanged file must yield no lines, got %v", got.lines)
	}

	appendTo(t, filepath.Join(root, "a.jsonl"), "two\n")
	got = collected{}
	scanOnce(t, sc, cur)
	if len(got.lines) != 1 || got.lines[0] != "two" {
		t.Fatalf("lines = %v, want [two]", got.lines)
	}
}

func TestShrunkFileIsReadFromTheBeginning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.jsonl")
	write(t, path, "one\ntwo\n")

	var got collected
	sc := plainScanner{Root: root, Name: "test", Line: got.line}
	cur := scanOnce(t, sc, DecodeCursor[FileState](nil))

	write(t, path, "new\n") // truncated and rewritten: the offset is meaningless now
	got = collected{}
	scanOnce(t, sc, cur)
	if len(got.lines) != 1 || got.lines[0] != "new" {
		t.Fatalf("lines = %v, want a full re-read [new]", got.lines)
	}
}

func TestReplacedFileIsReadFromTheBeginning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.jsonl")
	write(t, path, "one\ntwo\n")

	var got collected
	sc := plainScanner{Root: root, Name: "test", Line: got.line}
	cur := scanOnce(t, sc, DecodeCursor[FileState](nil))

	// Same path, same length, different file. The filesystem may hand a deleted
	// inode straight back, so the replacement is expressed in the cursor: a
	// changed inode where the platform has one, a moved mtime where it does not.
	write(t, path, "aaa\nbbb\n")
	st := cur.Files["a.jsonl"]
	if st.HasIno {
		st.Inode++
	} else {
		st.ModNS--
	}
	cur.Files["a.jsonl"] = st

	got = collected{}
	scanOnce(t, sc, cur)
	if len(got.lines) != 2 || got.lines[0] != "aaa" {
		t.Fatalf("lines = %v, want a full re-read [aaa bbb]", got.lines)
	}
}

func TestScanWalksNestedDirsAndSkipsPrunedOnes(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "proj", "a.jsonl"), "a\n")
	write(t, filepath.Join(root, "proj", "sub", "b.jsonl"), "b\n")
	write(t, filepath.Join(root, "proj", "memory", "c.jsonl"), "c\n")
	write(t, filepath.Join(root, "proj", "notes.md"), "not jsonl\n")

	var got collected
	sc := plainScanner{
		Root:    root,
		Name:    "test",
		SkipDir: func(name string) bool { return name == "memory" },
		Line:    got.line,
	}
	cur := scanOnce(t, sc, DecodeCursor[FileState](nil))

	if len(got.lines) != 2 {
		t.Fatalf("lines = %v, want the two *.jsonl outside memory/", got.lines)
	}
	if _, ok := cur.Files["proj/sub/b.jsonl"]; !ok {
		t.Errorf("cursor keys must be slash separated and relative to root: %v", cur.Files)
	}
	if len(cur.Files) != 2 {
		t.Errorf("tracked files = %v, want 2", cur.Files)
	}
}

func TestCursorForgetsFilesThatDisappeared(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.jsonl"), "a\n")
	write(t, filepath.Join(root, "b.jsonl"), "b\n")

	var got collected
	sc := plainScanner{Root: root, Name: "test", Line: got.line}
	cur := scanOnce(t, sc, DecodeCursor[FileState](nil))

	if err := os.Remove(filepath.Join(root, "b.jsonl")); err != nil {
		t.Fatal(err)
	}
	cur = scanOnce(t, sc, cur)
	if _, ok := cur.Files["b.jsonl"]; ok {
		t.Errorf("cursor must drop entries for files that no longer exist: %v", cur.Files)
	}
}

func TestMissingRootIsErrNoRoot(t *testing.T) {
	sc := plainScanner{Root: filepath.Join(t.TempDir(), "nope"), Name: "test", Line: (&collected{}).line}
	if _, err := sc.Scan(context.Background(), DecodeCursor[FileState](nil)); !errors.Is(err, ErrNoRoot) {
		t.Fatalf("err = %v, want ErrNoRoot", err)
	}
}

func TestCancelledContextStopsTheScan(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.jsonl"), "a\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sc := plainScanner{Root: root, Name: "test", Line: (&collected{}).line}
	if _, err := sc.Scan(ctx, DecodeCursor[FileState](nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestUnreadableFileFailsOnlyWhenNothingCouldBeRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("no unreadable-file permissions here")
	}
	root := t.TempDir()
	bad := filepath.Join(root, "bad.jsonl")
	write(t, bad, "x\n")
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}

	var got collected
	sc := plainScanner{Root: root, Name: "test", Line: got.line}
	if _, err := sc.Scan(context.Background(), DecodeCursor[FileState](nil)); err == nil {
		t.Fatal("a scan that could read nothing at all must report the failure")
	}

	// One readable file is enough to keep the scan (and the cursor) alive.
	write(t, filepath.Join(root, "ok.jsonl"), "ok\n")
	cur, err := sc.Scan(context.Background(), DecodeCursor[FileState](nil))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got.lines) != 1 || got.lines[0] != "ok" {
		t.Errorf("lines = %v, want [ok]", got.lines)
	}
	if _, ok := cur.Files["ok.jsonl"]; !ok {
		t.Errorf("the readable file must be tracked: %v", cur.Files)
	}
}

func TestLineErrorAbortsThatFileOnly(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a.jsonl"), "one\nboom\nthree\n")
	write(t, filepath.Join(root, "b.jsonl"), "fine\n")

	var seen []string
	boom := errors.New("boom")
	sc := plainScanner{Root: root, Name: "test", Line: func(_ *FileState, _ int64, raw []byte) error {
		s := strings.TrimSuffix(string(raw), "\n")
		if s == "boom" {
			return boom
		}
		seen = append(seen, s)
		return nil
	}}
	cur, err := sc.Scan(context.Background(), DecodeCursor[FileState](nil))
	if err != nil {
		t.Fatalf("one failing file must not fail the scan: %v", err)
	}
	if len(seen) != 2 || seen[0] != "one" || seen[1] != "fine" {
		t.Fatalf("seen = %v, want [one fine]", seen)
	}
	if st := cur.Files["a.jsonl"]; st.Offset != 4 {
		t.Errorf("offset = %d, want 4: only the lines before the error were consumed", st.Offset)
	}
}

// --- per-file state --------------------------------------------------------

// sessionState is the shape a collector that must remember file-level context
// uses (Codex keeps the session id and model this way).
type sessionState struct {
	FileState
	Model string `json:"model,omitempty"`
}

func TestExtraStateResumesButResetsOnFullReread(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.jsonl")
	write(t, path, "model=gpt\nuse\n")

	var models []string
	sc := Scanner[sessionState, *sessionState]{
		Root: root,
		Name: "test",
		Line: func(st *sessionState, _ int64, raw []byte) error {
			s := strings.TrimSuffix(string(raw), "\n")
			if m, ok := strings.CutPrefix(s, "model="); ok {
				st.Model = m
				return nil
			}
			models = append(models, st.Model)
			return nil
		},
	}
	cur, err := sc.Scan(context.Background(), DecodeCursor[sessionState](nil))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if cur.Files["a.jsonl"].Model != "gpt" {
		t.Fatalf("the cursor must carry the file state, got %+v", cur.Files["a.jsonl"])
	}

	// A resumed scan sees only the appended line, but keeps the stored context.
	appendTo(t, path, "use\n")
	models = nil
	cur, err = sc.Scan(context.Background(), cur)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(models) != 1 || models[0] != "gpt" {
		t.Fatalf("models = %v, want [gpt] carried over from the cursor", models)
	}

	// A file read from the beginning starts with no context at all.
	write(t, path, "use\n")
	models = nil
	if _, err := sc.Scan(context.Background(), cur); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(models) != 1 || models[0] != "" {
		t.Fatalf("models = %v, want [\"\"]: a re-read must not reuse stale state", models)
	}
}

// --- cursor encoding -------------------------------------------------------

// legacyCursor is exactly what agents shipped before this package existed wrote
// into cursors.json. Loading it must keep every offset, or every device would
// re-read its whole history after an upgrade.
const legacyCursor = `{"files":{"proj/a.jsonl":{"offset":128,"size":256,"mtime_ns":1700000000000000000,` +
	`"inode":4242,"has_inode":true,"session_id":"sess-1","cwd":"/home/me/p","cli_version":"1.2.3","model":"gpt-5"}}}`

func TestLegacyCursorStillLoads(t *testing.T) {
	type codexState struct {
		FileState
		SessionID  string `json:"session_id,omitempty"`
		CWD        string `json:"cwd,omitempty"`
		CLIVersion string `json:"cli_version,omitempty"`
		Model      string `json:"model,omitempty"`
	}
	got := DecodeCursor[codexState]([]byte(legacyCursor))
	st, ok := got.Files["proj/a.jsonl"]
	if !ok {
		t.Fatalf("files = %v", got.Files)
	}
	switch {
	case st.Offset != 128, st.Size != 256, st.ModNS != 1700000000000000000:
		t.Errorf("resume point lost: %+v", st)
	case st.Inode != 4242 || !st.HasIno:
		t.Errorf("file identity lost: %+v", st)
	case st.SessionID != "sess-1" || st.CWD != "/home/me/p" || st.CLIVersion != "1.2.3" || st.Model != "gpt-5":
		t.Errorf("session context lost: %+v", st)
	}

	// ... and re-encoding produces the same flat object again.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal([]byte(legacyCursor), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if b, a := mustJSON(t, before), mustJSON(t, after); b != a {
		t.Errorf("cursor round trip changed the JSON:\n old %s\n new %s", b, a)
	}
}

func TestCorruptCursorStartsOver(t *testing.T) {
	for _, raw := range []string{"", "not json", "null", "[]", `{"files":null}`} {
		got := DecodeCursor[FileState]([]byte(raw))
		if got.Files == nil || len(got.Files) != 0 {
			t.Errorf("DecodeCursor(%q) = %v, want an empty map", raw, got.Files)
		}
	}
}

func TestCursorKeyIsRelativeAndSlashSeparated(t *testing.T) {
	root := filepath.Join("var", "logs")
	if got := cursorKey(root, filepath.Join(root, "a", "b.jsonl")); got != "a/b.jsonl" {
		t.Errorf("cursorKey = %q, want a/b.jsonl", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
