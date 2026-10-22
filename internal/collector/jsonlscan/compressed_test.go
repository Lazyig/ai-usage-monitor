package jsonlscan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// writeZst stores body zstd-compressed at path, the way Codex archives a
// finished rollout.
func writeZst(t *testing.T, path, body string) {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	write(t, path, string(enc.EncodeAll([]byte(body), nil)))
}

// A compressed rollout must hand out the offsets its lines had before
// compression (collectors build event ids from them), and an unchanged archive
// must not be decompressed again.
func TestCompressedFileKeepsOffsetsAndIsReadOnce(t *testing.T) {
	body := "{\"a\":1}\n{\"b\":22}\n{\"c\":333}\n"

	plainRoot := t.TempDir()
	write(t, filepath.Join(plainRoot, "s.jsonl"), body)
	var plain collected
	scanOnce(t, plainScanner{Root: plainRoot, Name: "t", Line: plain.line}, Cursor[FileState]{})

	packedRoot := t.TempDir()
	writeZst(t, filepath.Join(packedRoot, "s.jsonl.zst"), body)
	var packed collected
	sc := plainScanner{Root: packedRoot, Name: "t", Line: packed.line, Compressed: true}
	cur := scanOnce(t, sc, Cursor[FileState]{})

	if !slices.Equal(packed.offsets, plain.offsets) || !slices.Equal(packed.lines, plain.lines) {
		t.Fatalf("compressed lines = %v @ %v, want %v @ %v", packed.lines, packed.offsets, plain.lines, plain.offsets)
	}
	if got := cur.Files["s.jsonl.zst"].Offset; got != int64(len(body)) {
		t.Fatalf("offset = %d, want the decompressed length %d", got, len(body))
	}

	var again collected
	sc.Line = again.line
	cur = scanOnce(t, sc, cur)
	if len(again.lines) != 0 {
		t.Fatalf("unchanged archive was read again: %v", again.lines)
	}
	if _, ok := cur.Files["s.jsonl.zst"]; !ok {
		t.Fatal("the skipped archive dropped out of the cursor")
	}
}

// A rewritten archive (new size) is read again from its first line.
func TestChangedCompressedFileIsReadFromTheStart(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "s.jsonl.zst")
	writeZst(t, path, "{\"a\":1}\n")
	sc := plainScanner{Root: root, Name: "t", Compressed: true}
	var first collected
	sc.Line = first.line
	cur := scanOnce(t, sc, Cursor[FileState]{})

	writeZst(t, path, "{\"a\":1}\n{\"b\":2}\n")
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	var second collected
	sc.Line = second.line
	scanOnce(t, sc, cur)
	if !slices.Equal(second.lines, []string{`{"a":1}`, `{"b":2}`}) || second.offsets[0] != 0 {
		t.Fatalf("re-read = %v @ %v, want both lines from offset 0", second.lines, second.offsets)
	}
}

// Without Compressed the archives are not even listed (Claude Code has none).
func TestCompressedFilesAreIgnoredUnlessAskedFor(t *testing.T) {
	root := t.TempDir()
	writeZst(t, filepath.Join(root, "s.jsonl.zst"), "{\"a\":1}\n")
	var got collected
	scanOnce(t, plainScanner{Root: root, Name: "t", Line: got.line}, Cursor[FileState]{})
	if len(got.lines) != 0 {
		t.Fatalf("archive read without Compressed: %v", got.lines)
	}
}

// A corrupt archive is reported and left out of the cursor, so the next scan
// tries it again instead of remembering it as done.
func TestCorruptCompressedFileIsRetried(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "ok.jsonl"), "{\"a\":1}\n")
	write(t, filepath.Join(root, "bad.jsonl.zst"), "not zstd at all\n")
	var got collected
	cur := scanOnce(t, plainScanner{Root: root, Name: "t", Line: got.line, Compressed: true}, Cursor[FileState]{})
	if _, ok := cur.Files["bad.jsonl.zst"]; ok {
		t.Fatal("corrupt archive was recorded as read")
	}
	if _, ok := cur.Files["ok.jsonl"]; !ok {
		t.Fatal("the readable file next to it was not recorded")
	}
}
