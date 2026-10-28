package collector

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IsGlob reports whether a configured root is a filepath.Match pattern rather
// than a plain directory.
func IsGlob(root string) bool { return strings.ContainsAny(root, "*?[") }

// ExpandRoots turns configured roots into the directories to read.
//
// Patterns are expanded on every call, so a folder created after the agent
// started is picked up by the next pass without a restart; a pattern only
// yields directories, in lexical order. A plain root is kept even when it does
// not exist (yet): collectors treat a missing root as "tool not used here".
// Roots that resolve to the same real directory (a symlink, the same folder
// listed twice, a pattern matching a configured root) are read once, under the
// first spelling seen. Empty entries are dropped.
func ExpandRoots(roots []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		dir = filepath.Clean(dir)
		id := dir
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			id = real
		}
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, dir)
	}
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		if !IsGlob(r) {
			add(r)
			continue
		}
		matches, err := filepath.Glob(r)
		if err != nil {
			continue // malformed pattern: nothing to read
		}
		sort.Strings(matches)
		for _, m := range matches {
			if isDir(m) {
				add(m)
			}
		}
	}
	return out
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
