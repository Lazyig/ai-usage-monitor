package claudecode

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxLineBytes caps a single JSONL record. Claude Code lines carry full message
// content and can be large, but anything past this is certainly not a record we
// can parse, so we skip it (still advancing the offset) instead of buffering it.
const maxLineBytes = 32 << 20

// jsonlFiles lists every *.jsonl below root, deepest-last in lexical order.
// Sub-agent transcripts live in <project>/<session-id>/*.jsonl, so the walk has
// to recurse; memory/ directories only ever hold markdown notes and are skipped.
func jsonlFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable subtree: skip it, keep collecting the rest
		}
		if d.IsDir() {
			if path != root && d.Name() == "memory" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// lineFunc receives one complete line together with the byte offset at which it
// started. The returned error aborts the file.
type lineFunc func(offset int64, line []byte) error

// readAppended streams the file from start to the end of the last complete line
// and returns the offset just past it. A trailing line without a newline is a
// record still being written: it is neither parsed nor counted, so the next scan
// picks it up whole.
func readAppended(path string, start int64, fn lineFunc) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return start, err
	}
	defer f.Close()

	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return start, err
		}
	}

	r := bufio.NewReaderSize(f, 256<<10)
	off := start
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			next := off + int64(len(line))
			if len(line) <= maxLineBytes {
				if cbErr := fn(off, line); cbErr != nil {
					return off, cbErr
				}
			}
			off = next
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return off, nil // any leftover partial line stays unconsumed
			}
			return off, err
		}
	}
}
