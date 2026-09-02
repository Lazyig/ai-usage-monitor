// This file implements GET /summary and GET /breakdown: both fold the same
// per-(device, source, model, UTC day) aggregate into grouped rows.

package store

import (
	"context"
	"fmt"
	"time"
)

// Summary is GET /summary.
type Summary struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Totals   Totals    `json:"totals"`
	BySource []Group   `json:"by_source"`
	ByDevice []Group   `json:"by_device"`
	ByModel  []Group   `json:"by_model"`
}

// Summary aggregates the range once (grouped by device, source, model and UTC
// day) and folds that single result set into totals and the three breakdowns.
// The UTC-day grain is what lets pricing honour `effective_from`.
func (s *Store) Summary(ctx context.Context, f Filter, p Pricer) (Summary, error) {
	rows, err := s.aggregate(ctx, f, "device_id", dayMs, 0, thresholdsOf(p))
	if err != nil {
		return Summary{}, err
	}
	names, err := s.deviceNames(ctx)
	if err != nil {
		return Summary{}, err
	}

	var (
		total    acc
		bySource = newAccSet()
		byDevice = newAccSet()
		byModel  = newAccSet()
	)
	for _, r := range rows {
		at := fromMillis(r.bucket * dayMs)
		usd, known, est := priceRow(p, r, at)

		total.add(r, usd, known, est)
		bySource.at(r.source, func(a *acc) { a.key = r.source }).add(r, usd, known, est)
		byDevice.at(r.key, func(a *acc) { a.key = r.key; a.name = names[r.key] }).add(r, usd, known, est)
		// A model name can appear under more than one tool, so the source is
		// part of the key even though the row reports only the model.
		byModel.at(r.model+"\x00"+r.source, func(a *acc) { a.key = r.model }).add(r, usd, known, est)
	}

	out := Summary{
		From:     f.From.UTC(),
		To:       f.To.UTC(),
		Totals:   total.totals(),
		BySource: bySource.groups(),
		ByDevice: byDevice.groups(),
		ByModel:  byModel.groups(),
	}
	// by_source rows carry no separate source field; by_device rows keep the
	// device name instead of a source when several sources contributed.
	for i := range out.BySource {
		out.BySource[i].Source = ""
	}
	return out, nil
}

// Breakdown is GET /breakdown.
type Breakdown struct {
	GroupBy string  `json:"group_by"`
	Rows    []Group `json:"rows"`
}

// Breakdown groups the range by one dimension. Rows are priced per (model, day)
// and folded; model groupings keep the source split (a model name can appear
// under more than one tool).
func (s *Store) Breakdown(ctx context.Context, f Filter, groupBy string, limit int, p Pricer) (Breakdown, error) {
	col, ok := GroupColumn(groupBy)
	if !ok {
		return Breakdown{}, fmt.Errorf("store: unsupported group_by %q", groupBy)
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := s.aggregate(ctx, f, col, dayMs, 0, thresholdsOf(p))
	if err != nil {
		return Breakdown{}, err
	}
	names, err := s.deviceNames(ctx)
	if err != nil {
		return Breakdown{}, err
	}

	groups := newAccSet()
	for _, r := range rows {
		at := fromMillis(r.bucket * dayMs)
		usd, known, est := priceRow(p, r, at)
		key := r.key
		if groupBy == "model" {
			key = r.model + "\x00" + r.source
		}
		groups.at(key, func(a *acc) {
			a.key, a.device = r.key, f.Device
			if groupBy == "device" {
				a.name = names[r.key]
				a.device = r.key
			}
		}).add(r, usd, known, est)
	}
	out := Breakdown{GroupBy: groupBy, Rows: groups.groups()}
	if len(out.Rows) > limit {
		out.Rows = out.Rows[:limit]
	}
	return out, nil
}
