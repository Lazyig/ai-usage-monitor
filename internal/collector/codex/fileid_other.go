//go:build !unix

package codex

import "io/fs"

// fileIdentity has no portable inode equivalent off unix, so the cursor relies
// on the size+mtime fallback to detect that a path was replaced.
func fileIdentity(fs.FileInfo) (uint64, bool) { return 0, false }
