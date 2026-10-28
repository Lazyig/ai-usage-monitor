// Package jsonlscan is the incremental reader shared by the collectors whose
// tools append JSONL transcripts to a directory tree (Claude Code, Codex).
//
// It owns the parts those collectors would otherwise each reimplement:
//
//   - walking a root for *.jsonl files (and, on request, the *.jsonl.zst files
//     Codex compresses finished rollouts into),
//   - a per-file resume point (offset/size/mtime/inode) that survives a restart,
//   - streaming the appended bytes without ever consuming a partial trailing
//     line, so a record still being written is read whole on the next scan,
//   - noticing that a path no longer holds the file the offset belongs to
//     (truncated, rotated, rewritten) and re-reading it from the beginning,
//   - the cursor JSON the agent persists in cursors.json.
//
// A collector plugs in by choosing the per-file state it wants to keep (either
// FileState itself or a struct embedding it) and a Line callback. Decoding a
// line, deduplicating and building events stay with the collector.
package jsonlscan

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// maxLineBytes caps a single JSONL record. These transcripts embed full prompt
// and response text, so lines get large; anything past this cannot be a record
// we could parse, so it is skipped (the offset still advances) rather than
// buffered.
const maxLineBytes = 32 << 20

// readBufBytes is the read-ahead buffer used per file.
const readBufBytes = 256 << 10

// ErrNoRoot reports that the scanned root does not exist. the field layout
// requires a device without the tool installed to produce an empty result, not
// a failure, so collectors translate this into "no events, cursor unchanged".
var ErrNoRoot = errors.New("jsonlscan: root does not exist")

// FileState is the per-file progress stored in a collector cursor.
//
// Offset is the byte offset just past the last *complete* line consumed.
// Size/ModNS/Inode identify the file that offset belongs to. Inode is only
// available on unix (see fileIdentity); everywhere else the size+mtime pair is
// the fallback identity check.
//
// A collector that needs more per-file state embeds this struct, which keeps
// the stored JSON one flat object and therefore backward compatible.
type FileState struct {
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	ModNS  int64  `json:"mtime_ns"`
	Inode  uint64 `json:"inode,omitempty"`
	HasIno bool   `json:"has_inode,omitempty"`
}

// Base lets the scanner reach the resume point of any per-file state, including
// one that only embeds FileState.
func (s *FileState) Base() *FileState { return s }

// StatePtr constrains the per-file cursor value T: the scanner needs to address
// T and to find the FileState inside it. Embedding FileState in T satisfies it.
type StatePtr[T any] interface {
	*T
	Base() *FileState
}

// Cursor is the JSON document a collector hands back to the agent. Keys are
// slash separated paths relative to the root, so a cursor stays valid across
// OSes and across a moved root directory.
type Cursor[T any] struct {
	Files map[string]T `json:"files"`
}

// DecodeCursor parses a stored cursor. A missing, empty or corrupt cursor is not
// fatal: it yields an empty state, everything is re-read and the server drops
// the duplicates by EventID.
func DecodeCursor[T any](raw []byte) Cursor[T] {
	empty := Cursor[T]{Files: map[string]T{}}
	if len(raw) == 0 {
		return empty
	}
	var got Cursor[T]
	if err := json.Unmarshal(raw, &got); err != nil || got.Files == nil {
		return empty
	}
	return got
}

// LineFunc receives one complete line, the per-file state it belongs to, and the
// byte offset the line started at. A returned error aborts that file.
type LineFunc[PT any] func(state PT, offset int64, line []byte) error

// Scanner reads every *.jsonl below Root that has grown since the cursor.
type Scanner[T any, PT StatePtr[T]] struct {
	// Root is the directory tree to walk.
	Root string
	// Name prefixes the errors, e.g. "claudecode".
	Name string
	// SkipDir, when set, is asked about every directory below Root by base name;
	// true prunes that subtree.
	SkipDir func(name string) bool
	// Line handles one appended line. It must not retain the line's bytes.
	Line LineFunc[PT]
	// Compressed also reads *.jsonl.zst. Offsets are counted in the
	// decompressed stream, so a line keeps the offset it had before the file
	// was compressed and the events built from it keep their ids.
	Compressed bool
}

// zstdExt marks a compressed transcript.
const zstdExt = ".jsonl.zst"

func isCompressed(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), zstdExt)
}

// Scan reads what is new since prev and returns the cursor to store.
//
// Files are visited in lexical order. A file that cannot be read is recorded and
// skipped; only when no file at all could be read does Scan fail, so a broken
// install is visible instead of silently reporting zero usage.
func (s Scanner[T, PT]) Scan(ctx context.Context, prev Cursor[T]) (Cursor[T], error) {
	root := s.walkRoot()
	files, err := s.Files()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Cursor[T]{}, ErrNoRoot
		}
		return Cursor[T]{}, fmt.Errorf("%s: walk %s: %w", s.Name, s.Root, err)
	}

	next := Cursor[T]{Files: make(map[string]T, len(files))}
	var (
		fileErr []error
		okFiles int
	)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return Cursor[T]{}, err
		}
		key := cursorKey(root, path)
		fi, err := os.Stat(path)
		if err != nil {
			fileErr = append(fileErr, fmt.Errorf("%s: stat %s: %w", s.Name, path, err))
			continue
		}
		ino, hasIno := fileIdentity(fi)
		stored, seen := prev.Files[key]
		start := startOffset(PT(&stored).Base(), seen, fi, ino, hasIno)

		compressed := isCompressed(path)
		if compressed {
			// A compressed transcript is written once and never appended to:
			// the same size and mtime mean it was already read to the end, so
			// it is not even opened. Any change means a full re-read.
			if b := PT(&stored).Base(); seen && fi.Size() == b.Size && fi.ModTime().UnixNano() == b.ModNS {
				next.Files[key] = stored
				okFiles++
				continue
			}
			start = 0
		}

		// Whatever the collector kept about this file describes the bytes it
		// already consumed, so a re-read from zero starts clean.
		var st T
		if start > 0 {
			st = stored
		}
		p := PT(&st)

		end, err := readAppended(path, start, func(offset int64, line []byte) error {
			return s.Line(p, offset, line)
		})
		if err != nil {
			fileErr = append(fileErr, fmt.Errorf("%s: read %s: %w", s.Name, path, err))
			if compressed {
				// A compressed file cannot be resumed mid-stream: forget it so
				// the next scan reads it again from the start (the server drops
				// the events it already has by EventID).
				continue
			}
			// Keep whatever was consumed so the next scan resumes there.
		} else {
			okFiles++
		}
		*p.Base() = FileState{
			Offset: end,
			Size:   fi.Size(),
			ModNS:  fi.ModTime().UnixNano(),
			Inode:  ino,
			HasIno: hasIno,
		}
		next.Files[key] = st
	}

	// Nothing at all could be read but files existed: report it so the agent can
	// surface a broken install instead of silently reporting zero usage.
	if okFiles == 0 && len(fileErr) > 0 {
		return Cursor[T]{}, errors.Join(fileErr...)
	}
	return next, nil
}

// cursorKey is the stable, OS independent cursor map key for path: relative to
// the root and slash separated, so a cursor survives a moved root and an agent
// that moved between OSes.
func cursorKey(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return filepath.ToSlash(rel)
}

// walkRoot is Root with symlinks resolved. filepath.WalkDir does not follow a
// root that is itself a symlink (a projects folder moved to another disk and
// linked back), so the walk starts at the target. Paths below it, and with them
// the cursor keys, stay relative to the same directory either way.
func (s Scanner[T, PT]) walkRoot() string {
	if real, err := filepath.EvalSymlinks(s.Root); err == nil {
		return real
	}
	return s.Root
}

// Files lists every *.jsonl below Root (plus *.jsonl.zst when Compressed), in
// lexical order. Both tools nest their transcripts (per project and session,
// or per date), so the walk recurses.
func (s Scanner[T, PT]) Files() ([]string, error) {
	var out []string
	root := s.walkRoot()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable subtree: skip it, keep collecting the rest
		}
		if d.IsDir() {
			if path != root && s.SkipDir != nil && s.SkipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".jsonl") || (s.Compressed && isCompressed(d.Name())) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// startOffset decides where to resume reading fi from. It returns 0 whenever the
// file cannot be proven to be the same append-only file we saw last time:
//   - the file shrank below the recorded offset or size (truncated or rotated),
//   - the inode changed (unix only),
//   - or, without an inode, the size is unchanged but the mtime moved, which
//     means same-length content was rewritten in place.
func startOffset(prev *FileState, seen bool, fi fs.FileInfo, ino uint64, hasIno bool) int64 {
	if !seen {
		return 0
	}
	switch {
	case fi.Size() < prev.Offset, fi.Size() < prev.Size:
		return 0
	case hasIno && prev.HasIno && ino != prev.Inode:
		return 0
	case !hasIno && prev.Offset > 0 && fi.Size() == prev.Size && fi.ModTime().UnixNano() != prev.ModNS:
		return 0
	}
	return prev.Offset
}

// readAppended streams path from start to the end of the last complete line and
// returns the offset just past it. A trailing line without a newline is a record
// still being written: it is neither parsed nor counted, so the next scan picks
// it up whole. A compressed file is always read from the start of its
// decompressed stream (Scan never resumes one part way).
func readAppended(path string, start int64, fn func(offset int64, line []byte) error) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return start, err
	}
	defer f.Close()

	var src io.Reader = f
	switch {
	case isCompressed(path):
		// One goroutine and low-memory mode: these are read once each, in the
		// background, next to the user's own work.
		dec, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
		if err != nil {
			return 0, err
		}
		defer dec.Close()
		src = dec
		start = 0
	case start > 0:
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return start, err
		}
	}

	r := bufio.NewReaderSize(src, readBufBytes)
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
