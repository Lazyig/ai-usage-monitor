package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"ai-usage-monitor/internal/collector"
)

// writeSession writes one transcript holding a single assistant message.
func writeSession(t *testing.T, root, project, msgID string) {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","uuid":"u-` + msgID + `","requestId":"req-` + msgID + `","timestamp":"2026-08-01T11:00:00.000Z","sessionId":"s","cwd":"/w","version":"2.1.0","message":{"id":"` + msgID + `","model":"claude-opus-5","usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScansEveryRoot(t *testing.T) {
	base := t.TempDir()
	home, work := filepath.Join(base, ".claude", "projects"), filepath.Join(base, ".claude-work", "projects")
	// Same relative path in both roots: the cursor must keep them apart.
	writeSession(t, home, "-p", "msg_home")
	writeSession(t, work, "-p", "msg_work")

	c := New(home, work)
	evs, cur := scan(t, c, nil)
	if got := byID(evs); len(got) != 2 || got[eventID("msg_home", "req-msg_home")].EventID == "" || got[eventID("msg_work", "req-msg_work")].EventID == "" {
		t.Fatalf("want one event per root, got %+v", evs)
	}
	if again, _ := scan(t, c, cur); len(again) != 0 {
		t.Fatalf("unchanged roots must scan empty, got %+v", again)
	}

	// Reordering the roots keeps the progress: it is keyed by path.
	if again, _ := scan(t, New(work, home), cur); len(again) != 0 {
		t.Fatalf("reordered roots re-read %d events", len(again))
	}
}

func TestGlobRootFindsNewFolders(t *testing.T) {
	base := t.TempDir()
	pattern := filepath.Join(base, ".claude-*", "projects")
	c := New(filepath.Join(base, ".claude", "projects"), pattern)

	evs, cur := scan(t, c, nil)
	if len(evs) != 0 {
		t.Fatalf("want nothing yet, got %+v", evs)
	}
	writeSession(t, filepath.Join(base, ".claude-new", "projects"), "-p", "msg_new")
	evs, _ = scan(t, c, cur)
	if len(evs) != 1 || evs[0].EventID != eventID("msg_new", "req-msg_new") {
		t.Fatalf("a folder created after the first scan must be read, got %+v", evs)
	}
}

func TestSameMessageInTwoRootsIsOneEvent(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	writeSession(t, a, "-p", "msg_same")
	writeSession(t, b, "-q", "msg_same")
	if evs, _ := scan(t, New(a, b), nil); len(evs) != 1 {
		t.Fatalf("a copied transcript must yield one event, got %d", len(evs))
	}
}

func TestSymlinkedRootIsReadOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeSession(t, real, "-p", "msg_1")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	_, cur := scan(t, New(real, link), nil)
	var mc multiCursor
	if err := json.Unmarshal(cur, &mc); err != nil {
		t.Fatal(err)
	}
	if len(mc.Roots) != 1 {
		t.Errorf("want the shared directory tracked once, got %v", mc.Roots)
	}
}

// A cursor written by an agent that read a single root is taken over by the
// configured root, so an upgrade does not re-read the history.
func TestLegacyCursorBelongsToFirstRoot(t *testing.T) {
	root := copyTree(t, filepath.Join("testdata", "projects"))
	noop := func(*fileState, int64, []byte) error { return nil }
	legacy, err := scanner{Root: root, Name: "test", SkipDir: skipDir, Line: noop}.Scan(t.Context(), cursorState{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(t.TempDir(), "extra")
	writeSession(t, extra, "-p", "msg_extra")

	evs, _ := scan(t, New(root, extra), collector.Cursor(raw))
	if len(evs) != 1 || evs[0].EventID != eventID("msg_extra", "req-msg_extra") {
		t.Fatalf("want only the new root's event after the upgrade, got %+v", evs)
	}
}

// A root that disappears (unmounted disk, renamed folder) keeps its progress,
// so it is not read from the start again when it comes back.
func TestMissingRootKeepsItsCursor(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	writeSession(t, a, "-p", "msg_a")
	writeSession(t, b, "-p", "msg_b")
	c := New(a, b)
	_, cur := scan(t, c, nil)

	moved := filepath.Join(base, "b.off")
	if err := os.Rename(b, moved); err != nil {
		t.Fatal(err)
	}
	_, cur = scan(t, c, cur)
	if err := os.Rename(moved, b); err != nil {
		t.Fatal(err)
	}
	if evs, _ := scan(t, c, cur); len(evs) != 0 {
		t.Fatalf("a root that came back was re-read: %+v", evs)
	}
}
