package agentcfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"ai-usage-monitor/internal/event"
)

func TestClaudeDefaultExtraRoots(t *testing.T) {
	home := setHome(t)
	want := []string{
		filepath.Join(home, ".config", "claude", "projects"),
		filepath.Join(home, ".claude-*", "projects"),
	}
	cfg := Defaults()
	if got := cfg.Source(event.SourceClaudeCode).ExtraRoots; !slices.Equal(got, want) {
		t.Errorf("claude extra roots = %v, want %v", got, want)
	}
	xdg := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got := DefaultExtraRoots(event.SourceClaudeCode)[0]; got != filepath.Join(xdg, "claude", "projects") {
		t.Errorf("with XDG_CONFIG_HOME: %q", got)
	}
}

// Agents before config_version always wrote an empty claude_code extra_roots;
// loading such a file gives it the defaults, relative to the configured root's
// home, and only once.
func TestVersion0ConfigGetsClaudeExtraRoots(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	user := filepath.Join(t.TempDir(), "someone")
	raw := `{"sources":{"claude_code":{"enabled":true,"root":"` + filepath.ToSlash(filepath.Join(user, ".claude", "projects")) + `","extra_roots":[]}}}`
	if err := os.WriteFile(ConfigPath(dir), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(user, ".config", "claude", "projects"),
		filepath.Join(user, ".claude-*", "projects"),
	}
	if got := cfg.Source(event.SourceClaudeCode).ExtraRoots; !slices.Equal(got, want) {
		t.Errorf("migrated extra roots = %v, want %v", got, want)
	}
	if cfg.Version != ConfigVersion {
		t.Errorf("version = %d, want %d", cfg.Version, ConfigVersion)
	}

	// Once migrated, an empty list is the user's choice and stays empty.
	cfg.Source(event.SourceClaudeCode).ExtraRoots = []string{}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Source(event.SourceClaudeCode).ExtraRoots; len(got) != 0 {
		t.Errorf("extra roots came back after the migration: %v", got)
	}
}

func TestNormalizeRoot(t *testing.T) {
	home := setHome(t)
	t.Setenv("AUM_TEST_DIR", filepath.Join(home, "env"))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		source event.Source
		in     string
		want   string
	}{
		{event.SourceClaudeCode, "~/.claude-work", filepath.Join(home, ".claude-work", "projects")},
		{event.SourceClaudeCode, "~/.claude-work/projects/", filepath.Join(home, ".claude-work", "projects")},
		{event.SourceClaudeCode, "~/.claude-*", filepath.Join(home, ".claude-*", "projects")},
		{event.SourceClaudeCode, "$AUM_TEST_DIR/cfg", filepath.Join(home, "env", "cfg", "projects")},
		// An unset name is part of the path, not an empty variable: "$" is an
		// ordinary character in Windows folder and share names.
		{event.SourceClaudeCode, "~/svc$/aum_unset_$AUM_UNSET_X", filepath.Join(home, "svc$", "aum_unset_$AUM_UNSET_X", "projects")},
		{event.SourceClaudeCode, "rel", filepath.Join(cwd, "rel", "projects")},
		{event.SourceAntigravity, "~/ag", filepath.Join(home, "ag")},
	}
	for _, tc := range cases {
		got, err := NormalizeRoot(tc.source, tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeRoot(%s, %q) = %q, %v; want %q", tc.source, tc.in, got, err, tc.want)
		}
	}
	if _, err := NormalizeRoot(event.SourceClaudeCode, "  "); err == nil {
		t.Error("an empty path must be rejected")
	}
}

func TestExpandPercentVars(t *testing.T) {
	t.Setenv("AUM_X", `C:\Users\me`)
	for in, want := range map[string]string{
		`%AUM_X%\.claude`: `C:\Users\me\.claude`,
		`%NOPE_AUM%\a`:    `%NOPE_AUM%\a`,
		`50%`:             `50%`,
		`%%AUM_X%`:        `%%AUM_X%`,
	} {
		if got := expandPercentVars(in); got != want {
			t.Errorf("expandPercentVars(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddAndRemoveRoots(t *testing.T) {
	base := t.TempDir()
	a, b, c := filepath.Join(base, "a"), filepath.Join(base, "b"), filepath.Join(base, "c")
	sc := &SourceConfig{Enabled: true, Root: a, ExtraRoots: []string{}}

	if !sc.AddRoot(b) || sc.AddRoot(b+string(filepath.Separator)) {
		t.Fatal("AddRoot must add a new root once")
	}
	if !sc.AddRoot(c) {
		t.Fatal("AddRoot(c)")
	}
	if !sc.RemoveRoot(a) || sc.Root != b || !slices.Equal(sc.ExtraRoots, []string{c}) {
		t.Fatalf("removing root must promote the first extra root: %+v", sc)
	}
	if sc.RemoveRoot(a) {
		t.Error("removing an absent root reported success")
	}
	sc.RemoveRoot(b)
	sc.RemoveRoot(c)
	if sc.Enabled || len(sc.ExtraRoots) != 0 {
		t.Fatalf("removing every root must disable the source: %+v", sc)
	}
	// A disabled source starts over with just the added root.
	if !sc.AddRoot(a) || !sc.Enabled || !slices.Equal(sc.Roots(), []string{a}) {
		t.Fatalf("AddRoot on a disabled source: %+v", sc)
	}
}

func TestClaudeConfigDirsFromEnv(t *testing.T) {
	t.Setenv(EnvClaudeConfigDir, " /a , ,/b")
	if got := ClaudeConfigDirsFromEnv(); !slices.Equal(got, []string{"/a", "/b"}) {
		t.Errorf("got %v", got)
	}
}

// A config written before extra roots existed picks up the Antigravity app
// folder on load; the other sources get none.
func TestOlderConfigGetsDefaultExtraRoots(t *testing.T) {
	home := setHome(t)
	dir := t.TempDir()
	raw := `{"sources":{"antigravity":{"enabled":true,"root":"x"},"codex":{"enabled":true,"root":"y"}}}`
	if err := os.WriteFile(ConfigPath(dir), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(home, ".gemini", "antigravity")}
	if got := cfg.Source(event.SourceAntigravity).ExtraRoots; !slices.Equal(got, want) {
		t.Errorf("antigravity extra roots = %v, want %v", got, want)
	}
	if got := cfg.Source(event.SourceAntigravity).Roots(); !slices.Equal(got, append([]string{"x"}, want...)) {
		t.Errorf("roots = %v", got)
	}
	if got := cfg.Source(event.SourceCodex).ExtraRoots; len(got) != 0 {
		t.Errorf("codex extra roots = %v, want none", got)
	}
}

// An explicit empty list turns the extra roots off and stays off.
func TestEmptyExtraRootsStayEmpty(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	cfg := Defaults()
	cfg.Source(event.SourceAntigravity).ExtraRoots = []string{}
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Source(event.SourceAntigravity).ExtraRoots; got == nil || len(got) != 0 {
		t.Errorf("extra roots after round trip = %#v, want an empty list", got)
	}
}
