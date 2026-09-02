package claudecode

import (
	"encoding/json"
	"io/fs"
)

// fileState is the per-file progress stored in the collector cursor.
//
// Offset is the byte offset just past the last *complete* line we consumed.
// Size/ModNS/Inode identify the file the offset belongs to. Inode is only
// available on unix (see fileIdentity); everywhere else the size+mtime pair is
// the fallback identity check.
type fileState struct {
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	ModNS  int64  `json:"mtime_ns"`
	Inode  uint64 `json:"inode,omitempty"`
	HasIno bool   `json:"has_inode,omitempty"`
}

// cursorState is the JSON document handed back to the agent. Keys are slash
// separated paths relative to root so a cursor stays valid across OSes and
// across a moved root directory.
type cursorState struct {
	Files map[string]fileState `json:"files"`
}

func decodeCursor(raw []byte) cursorState {
	st := cursorState{Files: map[string]fileState{}}
	if len(raw) == 0 {
		return st
	}
	var got cursorState
	if err := json.Unmarshal(raw, &got); err != nil || got.Files == nil {
		// A corrupt cursor is not fatal: re-read everything. The server drops
		// the duplicates by EventID.
		return st
	}
	return got
}

// startOffset decides where to resume reading fi from. It returns 0 whenever
// the file cannot be proven to be the same append-only file we saw last time:
//   - the file shrank below the recorded offset (truncated or rotated),
//   - the inode changed (unix only),
//   - or, without an inode, the size is unchanged but the mtime moved, which
//     means the same-length content was rewritten in place.
func startOffset(prev fileState, ok bool, fi fs.FileInfo, ino uint64, hasIno bool) int64 {
	if !ok {
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

func newFileState(offset int64, fi fs.FileInfo, ino uint64, hasIno bool) fileState {
	return fileState{
		Offset: offset,
		Size:   fi.Size(),
		ModNS:  fi.ModTime().UnixNano(),
		Inode:  ino,
		HasIno: hasIno,
	}
}
