package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/event"
)

// --no-restart everywhere: a developer machine may run the real service.

func TestRootsAddAndRemove(t *testing.T) {
	dir := setupState(t)
	home := os.Getenv("HOME")
	before, _, err := agentcfg.LoadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(agentcfg.EnvToken, "aum_from_env") // must never be written back

	out, code := runCLI(t, "roots", "add", "--no-restart", "~/.claude-work", filepath.Join(home, "b", "projects"))
	if code != 0 {
		t.Fatalf("roots add exit %d: %s", code, out)
	}
	cfg, _, err := agentcfg.LoadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceToken != "" {
		t.Error("roots add persisted a token that only came from the environment")
	}
	want := append(before.Source(event.SourceClaudeCode).Roots(),
		filepath.Join(home, ".claude-work", "projects"), filepath.Join(home, "b", "projects"))
	if got := cfg.Source(event.SourceClaudeCode).Roots(); !slices.Equal(got, want) {
		t.Fatalf("roots = %v, want %v", got, want)
	}

	if _, code := runCLI(t, "roots", "add", "--no-restart", "~/.claude-work"); code != 1 {
		t.Errorf("adding only duplicates must fail, exit %d", code)
	}

	if out, code := runCLI(t, "roots", "remove", "--no-restart", "~/.claude-work/projects"); code != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("roots remove exit %d: %s", code, out)
	}
	cfg, _, _ = agentcfg.LoadFile(dir)
	if cfg.Source(event.SourceClaudeCode).HasRoot(filepath.Join(home, ".claude-work", "projects")) {
		t.Error("removed root is still configured")
	}
}

func TestRootsRejectsSingleRootSources(t *testing.T) {
	setupState(t)
	if _, code := runCLI(t, "roots", "add", "--source", "codex", "/x"); code != 2 {
		t.Errorf("codex has one root; want a usage error, got exit %d", code)
	}
	if _, code := runCLI(t, "roots", "add", "/x", "--no-restart"); code != 2 {
		t.Errorf("a flag after the paths must be rejected, got exit %d", code)
	}
	if _, code := runCLI(t, "roots", "frobnicate"); code != 2 {
		t.Errorf("unknown roots command: exit %d", code)
	}
}

func TestRootsListExpandsPatterns(t *testing.T) {
	setupState(t)
	home := os.Getenv("HOME")
	if err := os.MkdirAll(filepath.Join(home, ".claude-work", "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, code := runCLI(t, "roots", "list", "--source", "claude_code")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, filepath.Join(home, ".claude-*", "projects")+"  pattern, 1 folder(s)") ||
		!strings.Contains(out, filepath.Join(home, ".claude-work", "projects")+"  ok, 0 log file(s)") {
		t.Errorf("pattern not expanded:\n%s", out)
	}
}

func TestInstallAddsClaudeDirs(t *testing.T) {
	setupState(t)
	home := os.Getenv("HOME")
	cfg := agentcfg.Defaults()
	if err := addClaudeDirs(&cfg, []string{"~/a", "~/a/projects"}); err != nil {
		t.Fatal(err)
	}
	roots := cfg.Source(event.SourceClaudeCode).Roots()
	if n := len(roots); roots[n-1] != filepath.Join(home, "a", "projects") || slices.Index(roots, roots[n-1]) != n-1 {
		t.Errorf("--claude-dir not added once: %v", roots)
	}

	// Without flags, $CLAUDE_CONFIG_DIR is used, comma separated.
	t.Setenv(agentcfg.EnvClaudeConfigDir, "~/x,~/y")
	cfg = agentcfg.Defaults()
	if err := addClaudeDirs(&cfg, nil); err != nil {
		t.Fatal(err)
	}
	roots = cfg.Source(event.SourceClaudeCode).Roots()
	if !slices.Contains(roots, filepath.Join(home, "x", "projects")) || !slices.Contains(roots, filepath.Join(home, "y", "projects")) {
		t.Errorf("CLAUDE_CONFIG_DIR not picked up: %v", roots)
	}

	// ... but never re-enables a source the user turned off.
	cfg = agentcfg.Defaults()
	cfg.Source(event.SourceClaudeCode).Enabled = false
	if err := addClaudeDirs(&cfg, nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Source(event.SourceClaudeCode).Enabled {
		t.Error("CLAUDE_CONFIG_DIR re-enabled a disabled source")
	}
}

func TestScanRootFlag(t *testing.T) {
	setupState(t)
	cfgDir := filepath.Dir(testdataRoots(t)[event.SourceClaudeCode]) // .../testdata, whose projects/ is read
	out, code := runCLI(t, "scan", "--since", "all", "--root", cfgDir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "claude_code") || strings.Contains(out, "codex") {
		t.Errorf("--root must scan only claude_code:\n%s", out)
	}
	if _, code := runCLI(t, "scan", "--root", cfgDir, "--source", "codex"); code != 2 {
		t.Errorf("--root with a --source that excludes claude_code: exit %d", code)
	}
}

func TestStatusPointsOutCLAUDE_CONFIG_DIR(t *testing.T) {
	setupState(t)
	t.Setenv(agentcfg.EnvClaudeConfigDir, filepath.Join(t.TempDir(), "elsewhere"))
	out, code := runCLI(t, "status", "--timeout", "1ms")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "roots add") {
		t.Errorf("status does not suggest adding $CLAUDE_CONFIG_DIR:\n%s", out)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/plain/path": "/plain/path",
		"/a b":        "'/a b'",
		"it's":        `'it'\''s'`,
		"~/.claude-*": "'~/.claude-*'",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
