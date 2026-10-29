// Package pricing maps a model name to USD-per-million-token rates and computes
// the API-equivalent cost of a UsageEvent. Rates live in pricing.yaml so they can
// be corrected centrally without a rebuild.
package pricing

import (
	"fmt"
	"os"
	"path"
	"slices"
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
	EffectiveFrom string   `yaml:"effective_from,omitempty"` // YYYY-MM-DD inclusive, optional
	EffectiveTo   string   `yaml:"effective_to,omitempty"`   // YYYY-MM-DD exclusive, optional
	Note          string   `yaml:"note,omitempty"`           // e.g. "introductory pricing", shown in the UI
	effectiveFrom time.Time
	effectiveTo   time.Time

	// LongContext, when set, replaces the base rates for the WHOLE request once
	// its prompt (input + every cache class) exceeds Threshold tokens. This is
	// how OpenAI (>272K), Gemini Pro (>200K), and Haiku 5.5 (>100K) bill.
	LongContext *LongContext `yaml:"long_context,omitempty"`
}

// LongContext is the over-threshold rate set of a Rate. Nil fields fall back to
// the base rate of the same class.
type LongContext struct {
	Threshold    int64    `yaml:"threshold"`
	Input        *float64 `yaml:"input,omitempty"`
	Output       *float64 `yaml:"output,omitempty"`
	CacheRead    *float64 `yaml:"cache_read,omitempty"`
	CacheWrite   *float64 `yaml:"cache_write,omitempty"`
	CacheWrite5m *float64 `yaml:"cache_write_5m,omitempty"`
	CacheWrite1h *float64 `yaml:"cache_write_1h,omitempty"`
}

// Table is an ordered list of rates; first match wins, so put specific entries first.
type Table struct {
	rates []Rate
}

// Load reads pricing.yaml.
func Load(file string) (*Table, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("pricing: read %s: %w", file, err)
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
		if r.LongContext != nil && r.LongContext.Threshold <= 0 {
			return nil, fmt.Errorf("pricing: %q long_context.threshold must be > 0", r.Match)
		}
		if r.EffectiveFrom != "" {
			t, err := time.Parse("2006-01-02", r.EffectiveFrom)
			if err != nil {
				return nil, fmt.Errorf("pricing: %q effective_from: %w", r.Match, err)
			}
			r.effectiveFrom = t
		}
		if r.EffectiveTo != "" {
			t, err := time.Parse("2006-01-02", r.EffectiveTo)
			if err != nil {
				return nil, fmt.Errorf("pricing: %q effective_to: %w", r.Match, err)
			}
			r.effectiveTo = t
			if !r.effectiveFrom.IsZero() && !r.effectiveFrom.Before(r.effectiveTo) {
				return nil, fmt.Errorf("pricing: %q effective_to must be after effective_from", r.Match)
			}
		}
	}
	return &Table{rates: rates}, nil
}

// Rates returns a copy of the entries in file order.
func (t *Table) Rates() []Rate {
	if t == nil {
		return nil
	}
	return slices.Clone(t.rates)
}

// Thresholds lists every distinct long_context threshold in the table, ascending.
// The store groups events by these boundaries so aggregated rows can still be
// priced per tier (a tier rule is not linear, so summing first would be wrong).
func (t *Table) Thresholds() []int64 {
	if t == nil {
		return nil
	}
	seen := map[int64]bool{}
	var out []int64
	for _, r := range t.rates {
		if r.LongContext != nil && !seen[r.LongContext.Threshold] {
			seen[r.LongContext.Threshold] = true
			out = append(out, r.LongContext.Threshold)
		}
	}
	slices.Sort(out)
	return out
}

// Lookup returns the rate for model at time at, following alias_of once.
// ok is false when no entry matches.
func (t *Table) Lookup(model string, at time.Time) (r Rate, ok bool) {
	if t == nil {
		return Rate{}, false
	}
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

// match returns the first entry whose glob matches and whose period contains
// `at` ([effective_from, effective_to), both optional). A model with price
// changes therefore appears several times, each row bounded to its period.
func (t *Table) match(model string, at time.Time) (Rate, bool) {
	model = strings.ToLower(model)
	for _, r := range t.rates {
		if !r.effectiveFrom.IsZero() && at.Before(r.effectiveFrom) {
			continue
		}
		if !r.effectiveTo.IsZero() && !at.Before(r.effectiveTo) {
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

// Cost prices a single event. Token classes with zero count never require a
// price. The long-context tier is decided by the event's own prompt size.
func (t *Table) Cost(e event.UsageEvent) Cost {
	return t.CostFor(e, e.TotalInput())
}

// CostFor prices e as if its request prompt had promptTokens tokens. Aggregated
// rows use it with a representative prompt size for their tier, because the
// summed token counts of a group say nothing about individual request sizes.
func (t *Table) CostFor(e event.UsageEvent, promptTokens int64) Cost {
	r, ok := t.Lookup(e.Model, e.Timestamp)
	if !ok {
		return Cost{}
	}
	if lc := r.LongContext; lc != nil && promptTokens > lc.Threshold {
		pick := func(tier, base *float64) *float64 {
			if tier != nil {
				return tier
			}
			return base
		}
		r.Input = pick(lc.Input, r.Input)
		r.Output = pick(lc.Output, r.Output)
		r.CacheRead = pick(lc.CacheRead, r.CacheRead)
		r.CacheWrite = pick(lc.CacheWrite, r.CacheWrite)
		r.CacheWrite5m = pick(lc.CacheWrite5m, r.CacheWrite5m)
		r.CacheWrite1h = pick(lc.CacheWrite1h, r.CacheWrite1h)
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
