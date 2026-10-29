package httpapi

import (
	"sync/atomic"

	"ai-usage-monitor/internal/pricing"
)

// Pricing holds the active rate table behind an atomic pointer so
// POST /api/v1/pricing/reload can swap it under live traffic without a restart
// and without a lock on the read path.
type Pricing struct {
	path string
	cur  atomic.Pointer[loaded]
}

type loaded struct {
	table *pricing.Table
	rates []pricing.Rate
}

// NewPricing loads path once; the file must parse or the server does not start.
func NewPricing(path string) (*Pricing, error) {
	p := &Pricing{path: path}
	if err := p.Reload(); err != nil {
		return nil, err
	}
	return p, nil
}

// Path is the file the table was loaded from.
func (p *Pricing) Path() string { return p.path }

// Table returns the current table; it is never nil after NewPricing.
func (p *Pricing) Table() *pricing.Table {
	if l := p.cur.Load(); l != nil {
		return l.table
	}
	return nil
}

// Rates returns the current entries in file order.
func (p *Pricing) Rates() []pricing.Rate {
	if l := p.cur.Load(); l != nil {
		return l.rates
	}
	return nil
}

// Reload re-reads the file and swaps the table in on success. A parse failure
// leaves the previous table in place.
func (p *Pricing) Reload() error {
	table, err := pricing.Load(p.path)
	if err != nil {
		return err
	}
	p.cur.Store(&loaded{table: table, rates: table.Rates()})
	return nil
}

// rateView is one entry of GET /api/v1/pricing.
type rateView struct {
	Match         string   `json:"match"`
	Provider      string   `json:"provider,omitempty"`
	Input         *float64 `json:"input"`
	Output        *float64 `json:"output"`
	CacheRead     *float64 `json:"cache_read"`
	CacheWrite    *float64 `json:"cache_write"`
	CacheWrite5m  *float64 `json:"cache_write_5m"`
	CacheWrite1h  *float64 `json:"cache_write_1h"`
	AliasOf       string   `json:"alias_of,omitempty"`
	Estimated     bool     `json:"estimated"`
	EffectiveFrom string   `json:"effective_from,omitempty"`
	EffectiveTo   string   `json:"effective_to,omitempty"`
	Note          string   `json:"note,omitempty"`
}

func rateViews(rates []pricing.Rate) []rateView {
	out := make([]rateView, 0, len(rates))
	for _, r := range rates {
		out = append(out, rateView{
			Match:         r.Match,
			Provider:      r.Provider,
			Input:         r.Input,
			Output:        r.Output,
			CacheRead:     r.CacheRead,
			CacheWrite:    r.CacheWrite,
			CacheWrite5m:  r.CacheWrite5m,
			CacheWrite1h:  r.CacheWrite1h,
			AliasOf:       r.AliasOf,
			Estimated:     r.Estimated,
			EffectiveFrom: r.EffectiveFrom,
			EffectiveTo:   r.EffectiveTo,
			Note:          r.Note,
		})
	}
	return out
}
