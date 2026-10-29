// This file implements GET /throughput: tokens per *active* minute. The
// collectors only know when a response completed, so every event's tokens are
// credited to the minute of its completion timestamp; a minute with at least
// one event is "active". Dividing by active minutes rather than by the whole
// range keeps idle time from diluting the rate, and the peak minute per class
// is what compares directly against per-minute API rate limits (ITPM/OTPM).

package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

const minuteMs = int64(60 * 1000)

// Peak is the busiest minute of one token class.
type Peak struct {
	T      time.Time `json:"t"` // minute start, in the request tz
	Tokens int64     `json:"tokens"`
}

// PerMinute is the average over active minutes.
type PerMinute struct {
	Prompt float64 `json:"prompt"`
	Output float64 `json:"output"`
	Total  float64 `json:"total"`
}

// ThroughputGroup is one grouped row of GET /throughput (and its total).
type ThroughputGroup struct {
	Key           string    `json:"key"`
	Name          string    `json:"name,omitempty"`
	ActiveMinutes int64     `json:"active_minutes"`
	Prompt        int64     `json:"prompt"` // input + cache_read + cache_write: everything the model read
	Input         int64     `json:"input"`
	CacheRead     int64     `json:"cache_read"`
	CacheWrite    int64     `json:"cache_write"`
	Output        int64     `json:"output"`
	Events        int64     `json:"events"`
	PerMinute     PerMinute `json:"per_minute"`
	PeakPrompt    *Peak     `json:"peak_prompt,omitempty"`
	PeakOutput    *Peak     `json:"peak_output,omitempty"`
}

// Throughput is GET /throughput.
type Throughput struct {
	GroupBy string            `json:"group_by"`
	TZ      string            `json:"tz"`
	Total   ThroughputGroup   `json:"total"`
	Groups  []ThroughputGroup `json:"groups"`
}

// minuteRow is one (group, minute) cell of the aggregate.
type minuteRow struct {
	key                                       string
	minute                                    int64 // ts / minuteMs
	input, cacheRead, cacheWrite, output, evs int64
}

func (m *minuteRow) prompt() int64 { return m.input + m.cacheRead + m.cacheWrite }

// tpAcc folds minute rows into one output group.
type tpAcc struct {
	g                     ThroughputGroup
	peakPromptM, peakOutM int64
	// total needs the union of minutes across groups, so its cells are merged
	// per minute before the peaks are taken.
	minutes map[int64]*minuteRow
}

func (a *tpAcc) add(r minuteRow) {
	a.g.ActiveMinutes++
	a.g.Input += r.input
	a.g.CacheRead += r.cacheRead
	a.g.CacheWrite += r.cacheWrite
	a.g.Output += r.output
	a.g.Events += r.evs
	if p := r.prompt(); a.g.PeakPrompt == nil || p > a.g.PeakPrompt.Tokens || (p == a.g.PeakPrompt.Tokens && r.minute < a.peakPromptM) {
		a.g.PeakPrompt = &Peak{Tokens: p}
		a.peakPromptM = r.minute
	}
	if a.g.PeakOutput == nil || r.output > a.g.PeakOutput.Tokens || (r.output == a.g.PeakOutput.Tokens && r.minute < a.peakOutM) {
		a.g.PeakOutput = &Peak{Tokens: r.output}
		a.peakOutM = r.minute
	}
}

func (a *tpAcc) finish(loc *time.Location) ThroughputGroup {
	g := a.g
	g.Prompt = g.Input + g.CacheRead + g.CacheWrite
	if g.ActiveMinutes > 0 {
		m := float64(g.ActiveMinutes)
		g.PerMinute = PerMinute{
			Prompt: round1(float64(g.Prompt) / m),
			Output: round1(float64(g.Output) / m),
			Total:  round1(float64(g.Prompt+g.Output) / m),
		}
	}
	if g.PeakPrompt != nil {
		g.PeakPrompt.T = fromMillis(a.peakPromptM * minuteMs).In(loc)
	}
	if g.PeakOutput != nil {
		g.PeakOutput.T = fromMillis(a.peakOutM * minuteMs).In(loc)
	}
	return g
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// Throughput groups the range by one dimension and reports tokens per active
// minute plus the peak minute of prompt and output tokens, for each group and
// for everything together. Minute boundaries are the same in every whole-minute
// zone, so loc only formats the peak timestamps.
func (s *Store) Throughput(ctx context.Context, f Filter, groupBy string, loc *time.Location) (Throughput, error) {
	col, ok := GroupColumn(groupBy)
	if !ok {
		return Throughput{}, fmt.Errorf("store: unsupported group_by %q", groupBy)
	}
	names := map[string]string{}
	if groupBy == "device" {
		var err error
		if names, err = s.deviceNames(ctx); err != nil {
			return Throughput{}, err
		}
	}
	q := newQueryBuilder(fmt.Sprintf(`
		SELECT %s AS k, ts / %d AS m,
		       CAST(SUM(input) AS BIGINT), CAST(SUM(cache_read) AS BIGINT),
		       CAST(SUM(cache_write_5m + cache_write_1h + cache_write) AS BIGINT), CAST(SUM(output) AS BIGINT), COUNT(*)
		FROM usage_events`, col, minuteMs))
	f.apply(q)
	q.tail("GROUP BY k, m ORDER BY k, m")

	cells, err := s.minuteRows(ctx, f, q)
	if err != nil {
		return Throughput{}, err
	}

	groups := map[string]*tpAcc{}
	var ord []string
	total := &tpAcc{minutes: map[int64]*minuteRow{}}
	for _, r := range cells {
		a, ok := groups[r.key]
		if !ok {
			a = &tpAcc{g: ThroughputGroup{Key: r.key, Name: names[r.key]}}
			groups[r.key] = a
			ord = append(ord, r.key)
		}
		a.add(r)
		if t, ok := total.minutes[r.minute]; ok {
			t.input += r.input
			t.cacheRead += r.cacheRead
			t.cacheWrite += r.cacheWrite
			t.output += r.output
			t.evs += r.evs
		} else {
			c := r
			total.minutes[r.minute] = &c
		}
	}
	for _, m := range total.minutes {
		total.add(*m)
	}

	out := Throughput{GroupBy: groupBy, TZ: loc.String(), Total: total.finish(loc), Groups: make([]ThroughputGroup, 0, len(ord))}
	for _, k := range ord {
		out.Groups = append(out.Groups, groups[k].finish(loc))
	}
	// Busiest first: by tokens per active minute, then by volume, then by key.
	sort.Slice(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.PerMinute.Total != b.PerMinute.Total {
			return a.PerMinute.Total > b.PerMinute.Total
		}
		if at, bt := a.Prompt+a.Output, b.Prompt+b.Output; at != bt {
			return at > bt
		}
		return a.Key < b.Key
	})
	return out, nil
}

// minuteRows runs the per-minute aggregate q for f.
func (s *Store) minuteRows(ctx context.Context, f Filter, q *queryBuilder) ([]minuteRow, error) {
	if s.unmatchable(f.texts()...) {
		return nil, nil // folded like any other empty result
	}
	rows, err := s.rdb.QueryContext(ctx, q.sql(), q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []minuteRow
	for rows.Next() {
		var r minuteRow
		if err := rows.Scan(&r.key, &r.minute, &r.input, &r.cacheRead, &r.cacheWrite, &r.output, &r.evs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
