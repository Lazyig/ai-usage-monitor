package codex

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxLineBytes caps one JSONL record. Rollout files embed full instruction and
// response text, so lines get large; anything beyond this is skipped (the offset
// still advances) rather than buffered.
const maxLineBytes = 32 << 20

// jsonlFiles lists every *.jsonl below root in lexical order. Codex lays sessions
// out as YYYY/MM/DD/rollout-*.jsonl, so the walk must recurse.
func jsonlFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable subtree: skip it, keep the rest
		}
		if d.IsDir() {
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

// lineFunc receives one complete line and the byte offset it started at.
type lineFunc func(offset int64, line []byte) error

// readAppended streams path from start to the end of the last complete line and
// returns the offset just past it. A trailing line with no newline is a record
// still being written: it is not parsed and not consumed, so the next scan sees
// it whole.
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
				return off, nil
			}
			return off, err
		}
	}
}
