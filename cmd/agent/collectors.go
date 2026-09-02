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

// factory builds a collector for one source, given the log root from the config.
type factory func(root string) collector.Collector

// registry is the single place that knows which collectors are compiled in.
var registry = map[event.Source]factory{
	event.SourceClaudeCode:  claudecode.New,
	event.SourceCodex:       codex.New,
	event.SourceAntigravity: antigravity.New,
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
	Root      string
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
		out = append(out, active{Source: s, Root: sc.Root, Collector: registry[s](sc.Root)})
	}
	return out
}
