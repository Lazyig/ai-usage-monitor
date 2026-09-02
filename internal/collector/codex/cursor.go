package codex

import (
	"encoding/json"
	"io/fs"
)

// fileState is the per-file progress stored in the collector cursor.
//
// Offset is the byte offset just past the last *complete* line we consumed.
// Size/ModNS/Inode identify the file that offset belongs to; Inode exists only
// on unix (see fileIdentity) and size+mtime is the portable fallback.
//
// The session fields are carried in the cursor on purpose: session_meta and
// turn_context appear once near the top of a rollout file, so a resumed scan
// that only reads appended bytes would otherwise have no model, cwd or session
// id for the token_count lines it finds.
type fileState struct {
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	ModNS  int64  `json:"mtime_ns"`
	Inode  uint64 `json:"inode,omitempty"`
	HasIno bool   `json:"has_inode,omitempty"`

	SessionID  string `json:"session_id,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	CLIVersion string `json:"cli_version,omitempty"`
	Model      string `json:"model,omitempty"` // most recent turn_context model
}

// cursorState is the JSON document handed back to the agent. Keys are slash
// separated paths relative to root so the cursor survives an OS or root change.
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
		// A corrupt cursor is not fatal: re-read everything, the server dedupes.
		return st
	}
	return got
}

// startOffset decides where to resume reading fi. It returns 0 whenever the file
// cannot be proven to be the same append-only file as last time:
//   - it shrank below the recorded offset or size (truncated or rotated),
//   - the inode changed (unix only),
//   - or, without an inode, the size is unchanged while the mtime moved, which
//     means same-length content was rewritten in place.
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
