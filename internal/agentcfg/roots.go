package agentcfg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

// EnvClaudeConfigDir is Claude Code's own variable for a config directory
// other than ~/.claude. A service never sees the user's shell, so the agent
// only reads it at install time (and to point out a directory that is not
// configured). Like ccusage, a comma separated list is accepted.
const EnvClaudeConfigDir = "CLAUDE_CONFIG_DIR"

// ClaudeConfigDirsFromEnv splits $CLAUDE_CONFIG_DIR into its entries.
func ClaudeConfigDirsFromEnv() []string {
	var out []string
	for _, p := range strings.Split(os.Getenv(EnvClaudeConfigDir), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NormalizeRoot turns a path typed by the user into the root stored in the
// config for source s:
//
//   - ~, $VAR / ${VAR} and, on Windows, %VAR% are expanded, and the result is
//     made absolute. This happens now, as the installing user: the service may
//     run under another account (LocalSystem, a system unit) whose home is not
//     the one the user meant.
//   - For Claude Code the user names the config directory (what they put in
//     CLAUDE_CONFIG_DIR); the transcripts are in its projects folder, so
//     /projects is appended unless the path already ends there.
//
// Glob characters are kept as they are; the pattern is expanded on every pass.
func NormalizeRoot(s event.Source, input string) (string, error) {
	p := strings.TrimSpace(input)
	if p == "" {
		return "", errors.New("empty path")
	}
	p = expandHome(p)
	if runtime.GOOS == "windows" {
		p = expandPercentVars(p)
	}
	p = os.ExpandEnv(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("%s: %w", input, err)
	}
	if s == event.SourceClaudeCode && filepath.Base(abs) != "projects" {
		abs = filepath.Join(abs, "projects")
	}
	return abs, nil
}

// expandHome replaces a leading ~ (alone or followed by a separator).
func expandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(p, `~\`)) {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

// expandPercentVars expands cmd.exe style %VAR% references. Unknown variables
// and a lone % are left untouched, as cmd.exe does.
func expandPercentVars(p string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(p, '%')
		if i < 0 {
			break
		}
		j := strings.IndexByte(p[i+1:], '%')
		if j < 0 {
			break
		}
		name := p[i+1 : i+1+j]
		if v, ok := os.LookupEnv(name); ok && name != "" {
			b.WriteString(p[:i])
			b.WriteString(v)
		} else {
			b.WriteString(p[:i+1+j+1])
		}
		p = p[i+1+j+1:]
	}
	b.WriteString(p)
	return b.String()
}

// SameRoot reports whether two configured roots name the same directory: the
// same cleaned spelling, or (for existing directories) the same real path.
func SameRoot(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b || (runtime.GOOS == "windows" && strings.EqualFold(a, b)) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// HasRoot reports whether root is already one of the source's roots.
func (sc *SourceConfig) HasRoot(root string) bool {
	for _, r := range sc.Roots() {
		if SameRoot(r, root) {
			return true
		}
	}
	return false
}

// AddRoot appends root to the source's roots and enables the source. It
// reports false, changing nothing, when the root is already configured.
//
// A disabled source starts from an empty list: it was turned off, possibly by
// removing every root, and reviving its old roots would bring back folders the
// user took out.
func (sc *SourceConfig) AddRoot(root string) bool {
	if !sc.Enabled {
		sc.Enabled = true
		sc.Root, sc.ExtraRoots = root, []string{}
		return true
	}
	if sc.HasRoot(root) {
		return false
	}
	sc.ExtraRoots = append(sc.ExtraRoots, root)
	return true
}

// RemoveRoot takes root out of the source's roots and reports whether it was
// there. Root and ExtraRoots are one list here: removing Root promotes the
// first extra root, and removing the last root disables the source (Root then
// keeps its value, since an empty one would be refilled with the default).
func (sc *SourceConfig) RemoveRoot(root string) bool {
	var keep []string
	removed := false
	for _, r := range sc.Roots() {
		// Patterns are matched by spelling only: a pattern is not a directory.
		if filepath.Clean(r) == filepath.Clean(root) || (!collector.IsGlob(r) && SameRoot(r, root)) {
			removed = true
			continue
		}
		keep = append(keep, r)
	}
	if !removed {
		return false
	}
	if len(keep) == 0 {
		sc.Enabled = false
		sc.ExtraRoots = []string{}
		return true
	}
	sc.Root, sc.ExtraRoots = keep[0], append([]string{}, keep[1:]...)
	return true
}
