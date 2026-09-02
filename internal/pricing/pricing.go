// Package pricing maps a model name to USD-per-million-token rates and computes
// the API-equivalent cost of a UsageEvent. Rates live in pricing.yaml so they can
// be corrected centrally without a rebuild.
package pricing

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"ai-usage-monitor/internal/event"
)

// Rate is one pricing.yaml entry. All prices are USD per 1M tokens. A nil price
// means "unknown" for that token class and makes the whole cost unknown.
type Rate struct {
	Match         string   `yaml:"match"` // glob on the model name (path.Match semantics)
	Provider      string   `yaml:"provider,omitempty"`
	AliasOf       string   `yaml:"alias_of,omitempty"` // use another entry's prices
	Estimated     bool     `yaml:"estimated,omitempty"`
	Input         *float64 `yaml:"input,omitempty"`
	Output        *float64 `yaml:"output,omitempty"`
	CacheRead     *float64 `yaml:"cache_read,omitempty"`
	CacheWrite    *float64 `yaml:"cache_write,omitempty"`    // generic (Codex, Gemini)
	CacheWrite5m  *float64 `yaml:"cache_write_5m,omitempty"` // Claude
	CacheWrite1h  *float64 `yaml:"cache_write_1h,omitempty"`
	EffectiveFrom string   `yaml:"effective_from,omitempty"` // YYYY-MM-DD, optional
	effectiveFrom time.Time
}

// Table is an ordered list of rates; first match wins, so put specific entries first.
type Table struct {
	rates []Rate
}

// Load reads pricing.yaml.
func Load(file string) (*Table, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse builds a Table from YAML bytes.
func Parse(b []byte) (*Table, error) {
	var rates []Rate
	if err := yaml.Unmarshal(b, &rates); err != nil {
		return nil, fmt.Errorf("pricing: %w", err)
	}
	for i := range rates {
		r := &rates[i]
		if r.Match == "" {
			return nil, fmt.Errorf("pricing: entry %d has no match", i)
		}
		if _, err := path.Match(r.Match, ""); err != nil {
			return nil, fmt.Errorf("pricing: bad glob %q: %w", r.Match, err)
		}
		if r.EffectiveFrom != "" {
			t, err := time.Parse("2006-01-02", r.EffectiveFrom)
			if err != nil {
				return nil, fmt.Errorf("pricing: %q effective_from: %w", r.Match, err)
			}
			r.effectiveFrom = t
		}
	}
	return &Table{rates: rates}, nil
}

// Lookup returns the rate for model at time at, following alias_of once.
// ok is false when no entry matches.
func (t *Table) Lookup(model string, at time.Time) (r Rate, ok bool) {
	r, ok = t.match(model, at)
	if ok && r.AliasOf != "" {
		base, baseOK := t.match(r.AliasOf, at)
		if !baseOK {
			return r, false
		}
		base.Estimated = true
		base.Match = r.Match
		return base, true
	}
	return r, ok
}

func (t *Table) match(model string, at time.Time) (Rate, bool) {
	model = strings.ToLower(model)
	for _, r := range t.rates {
		if !r.effectiveFrom.IsZero() && at.Before(r.effectiveFrom) {
			continue
		}
		if okM, _ := path.Match(strings.ToLower(r.Match), model); okM {
			return r, true
		}
	}
	return Rate{}, false
}

// Cost is the priced breakdown of one event.
type Cost struct {
	USD       float64 // total; only meaningful when Known
	Known     bool    // false when the model or a needed token class has no price
	Estimated bool    // priced via alias / marked estimated in the table
}

// Cost prices an event. Token classes with zero count never require a price.
func (t *Table) Cost(e event.UsageEvent) Cost {
	r, ok := t.Lookup(e.Model, e.Timestamp)
	if !ok {
		return Cost{}
	}
	var total float64
	known := true
	add := func(n int64, p *float64, fallback *float64) {
		if n == 0 {
			return
		}
		if p == nil {
			p = fallback
		}
		if p == nil {
			known = false
			return
		}
		total += float64(n) * *p / 1e6
	}
	add(e.Input, r.Input, nil)
	add(e.Output, r.Output, nil)
	add(e.CacheRead, r.CacheRead, nil)
	add(e.CacheWrite, r.CacheWrite, r.Input)
	add(e.CacheWrite5m, r.CacheWrite5m, r.CacheWrite)
	add(e.CacheWrite1h, r.CacheWrite1h, r.CacheWrite)
	return Cost{USD: total, Known: known, Estimated: r.Estimated}
}
