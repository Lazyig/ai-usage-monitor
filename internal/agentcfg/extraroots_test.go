package agentcfg

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"ai-usage-monitor/internal/event"
)

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
