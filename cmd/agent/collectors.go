package main

import (
	"fmt"
	"sort"
	"strings"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/collector/antigravity"
	"ai-usage-monitor/internal/collector/claudecode"
	"ai-usage-monitor/internal/collector/codex"
	"ai-usage-monitor/internal/event"
)

// factory builds a collector for one source, given its log roots from the
// config (Root first, then ExtraRoots).
type factory func(roots []string) collector.Collector

// registry is the single place that knows which collectors are compiled in.
// Only Antigravity reads more than one root (CLI and desktop app).
var registry = map[event.Source]factory{
	event.SourceClaudeCode:  func(roots []string) collector.Collector { return claudecode.New(roots[0]) },
	event.SourceCodex:       func(roots []string) collector.Collector { return codex.New(roots[0]) },
	event.SourceAntigravity: func(roots []string) collector.Collector { return antigravity.New(roots...) },
}

// usedRoots is what a source's collector actually reads, for display.
func usedRoots(s event.Source, sc agentcfg.SourceConfig) []string {
	if s == event.SourceAntigravity {
		return sc.Roots()
	}
	return []string{sc.Root}
}

// anyDirExists reports whether at least one of the roots is present.
func anyDirExists(roots []string) bool {
	for _, r := range roots {
		if dirExists(r) {
			return true
		}
	}
	return false
}

// known lists the compiled-in sources in agentcfg.Sources order.
func known() []event.Source {
	var out []event.Source
	for _, s := range agentcfg.Sources {
		if _, ok := registry[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

// selected is a source filter parsed from --source.
type selected map[event.Source]bool

// parseSources turns "claude_code,codex" into a filter. An empty string means
// "every source". Unknown names are an error so a typo cannot silently produce
// an empty report.
func parseSources(list string) (selected, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return nil, nil
	}
	sel := selected{}
	for _, part := range strings.Split(list, ",") {
		name := event.Source(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if _, ok := registry[name]; !ok {
			return nil, fmt.Errorf("unknown source %q (available: %s)", name, strings.Join(sourceNames(), ", "))
		}
		sel[name] = true
	}
	if len(sel) == 0 {
		return nil, nil
	}
	return sel, nil
}

func sourceNames() []string {
	out := make([]string, 0, len(registry))
	for s := range registry {
		out = append(out, string(s))
	}
	sort.Strings(out)
	return out
}

// active is one collector plus the configuration it was built from.
type active struct {
	Source    event.Source
	Roots     []string
	Collector collector.Collector
}

// build returns the collectors to run for cfg, honouring the per-source
// "enabled" flag and the optional --source filter. Sources listed in the config
// but not compiled into this binary are skipped, so a config written by a newer
// agent still works.
func build(cfg agentcfg.Config, sel selected) []active {
	var out []active
	for _, s := range known() {
		if sel != nil && !sel[s] {
			continue
		}
		sc := cfg.Source(s)
		if !sc.Enabled {
			continue
		}
		roots := usedRoots(s, *sc)
		out = append(out, active{Source: s, Roots: roots, Collector: registry[s](roots)})
	}
	return out
}
