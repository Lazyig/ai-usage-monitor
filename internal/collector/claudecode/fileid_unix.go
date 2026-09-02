//go:build unix

package claudecode

import (
	"io/fs"
	"syscall"
)

// fileIdentity returns the inode number of fi when the platform exposes one.
// It is best effort: when ok is false the cursor falls back to size+mtime to
// notice that a path now points at a different file.
func fileIdentity(fi fs.FileInfo) (id uint64, ok bool) {
	st, k := fi.Sys().(*syscall.Stat_t)
	if !k {
		return 0, false
	}
	return uint64(st.Ino), true
}
