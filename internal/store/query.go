// This file holds the query machinery every aggregate endpoint shares: the
// WHERE clause, the SQL builder, the single GROUP BY that feeds all of them and
// the accumulators their rows are folded into. The endpoints themselves live in
// summary.go (/summary, /breakdown), timeseries.go and events.go.

package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"ai-usage-monitor/internal/event"
)

const dayMs = int64(24 * 60 * 60 * 1000)
const hourMs = int64(60 * 60 * 1000)

// Filter is the common WHERE clause of every query. The range is [From, To).
type Filter struct {
	From    time.Time
	To      time.Time
	Source  string
	Device  string
	Model   string
	Project string
	Session string
}

func (f Filter) apply(q *queryBuilder) {
	if !f.From.IsZero() {
		q.where("ts >= ?", toMillis(f.From))
	}
	if !f.To.IsZero() {
		q.where("ts < ?", toMillis(f.To))
	}
	if f.Source != "" {
		q.where("source = ?", f.Source)
	}
	if f.Device != "" {
		q.where("device_id = ?", f.Device)
	}
	if f.Model != "" {
		q.where("model = ?", f.Model)
	}
	if f.Project != "" {
		q.where("project = ?", f.Project)
	}
	if f.Session != "" {
		q.where("session_id = ?", f.Session)
	}
}

type queryBuilder struct {
	head    string
	wheres  []string
	tailSQL string
	args    []any
}

func newQueryBuilder(head string) *queryBuilder { return &queryBuilder{head: head} }

func (q *queryBuilder) where(cond string, args ...any) {
	q.wheres = append(q.wheres, cond)
	q.args = append(q.args, args...)
}

func (q *queryBuilder) tail(s string) { q.tailSQL = s }

func (q *queryBuilder) sql() string {
	var b strings.Builder
	b.WriteString(q.head)
	if len(q.wheres) > 0 {
		b.WriteString("\nWHERE ")
		b.WriteString(strings.Join(q.wheres, " AND "))
	}
	if q.tailSQL != "" {
		b.WriteString("\n")
		b.WriteString(q.tailSQL)
	}
	return b.String()
}

// --- cursors ---------------------------------------------------------------

type cursorV1 struct {
	TS      int64  `json:"ts"`
	EventID string `json:"e"`
}

func encodeCursor(ts int64, eventID string) string {
	b, _ := json.Marshal(cursorV1{TS: ts, EventID: eventID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursorV1, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursorV1{}, errors.New("store: bad cursor")
	}
	var c cursorV1
	if err := json.Unmarshal(b, &c); err != nil || c.EventID == "" {
		return cursorV1{}, errors.New("store: bad cursor")
	}
	return c, nil
}

// --- token accumulation ----------------------------------------------------

// tokens is the internal sum of one aggregation group. The cache-write classes
// are kept apart because pricing rates differ per class; the API reports their sum.
type tokens struct {
	input, output, cacheRead, w5m, w1h, wGen, reasoning, events int64
}

func (t *tokens) add(o tokens) {
	t.input += o.input
	t.output += o.output
	t.cacheRead += o.cacheRead
	t.w5m += o.w5m
	t.w1h += o.w1h
	t.wGen += o.wGen
	t.reasoning += o.reasoning
	t.events += o.events
}

func (t tokens) cacheWrite() int64 { return t.w5m + t.w1h + t.wGen }

// total counts every billable token class once (reasoning is a subset of output).
func (t tokens) total() int64 {
	return t.input + t.output + t.cacheRead + t.cacheWrite()
}

// asEvent shapes the group so pricing.Table.Cost can price it in one call.
// Cost is linear in each token class, so pricing the sum equals summing the
// per-event prices as long as the model and the rate window are the same -
// which is why every aggregation groups by model and by day.
func (t tokens) asEvent(model string, at time.Time) event.UsageEvent {
	return event.UsageEvent{
		Model:        model,
		Timestamp:    at,
		Input:        t.input,
		Output:       t.output,
		CacheRead:    t.cacheRead,
		CacheWrite5m: t.w5m,
		CacheWrite1h: t.w1h,
		CacheWrite:   t.wGen,
		Reasoning:    t.reasoning,
	}
}

// --- aggregation -----------------------------------------------------------

type aggRow struct {
	key    string
	source string
	model  string
	bucket int64 // (ts + offset) / bucketMs, or 0 when unbucketed
	tokens
	first, last int64
	// promptHint is a prompt size representative of this row's long-context
	// tier (0 = below every threshold). Rows are split per tier so non-linear
	// tier pricing stays exact after aggregation.
	promptHint int64
}

// aggregate groups usage_events by an optional key column, plus source, model and
// an optional time bucket. keyCol must be a fixed column name chosen by the
// caller (never user input). bucketMs == 0 disables bucketing; offsetSec shifts
// the timeline so buckets land on local boundaries.
func (s *Store) aggregate(ctx context.Context, f Filter, keyCol string, bucketMs, offsetSec int64, thresholds []int64) ([]aggRow, error) {
	keyExpr := "''"
	if keyCol != "" {
		keyExpr = keyCol
	}
	bucketExpr := "0"
	if bucketMs > 0 {
		bucketExpr = fmt.Sprintf("((ts + %d) / %d)", offsetSec*1000, bucketMs)
	}
	// Tier = how many long-context thresholds the request prompt exceeds. Each
	// model has at most one threshold and it is one of these boundaries, so no
	// tier straddles a model's cut-off.
	tierExpr := "0"
	if len(thresholds) > 0 {
		const prompt = "(input + cache_read + cache_write_5m + cache_write_1h + cache_write)"
		var b strings.Builder
		b.WriteString("CASE")
		for i := len(thresholds) - 1; i >= 0; i-- {
			fmt.Fprintf(&b, " WHEN %s > %d THEN %d", prompt, thresholds[i], i+1)
		}
		b.WriteString(" ELSE 0 END")
		tierExpr = b.String()
	}
	q := newQueryBuilder(fmt.Sprintf(`
		SELECT %s AS k, source, model, %s AS b, %s AS tier,
		       SUM(input), SUM(output), SUM(cache_read),
		       SUM(cache_write_5m), SUM(cache_write_1h), SUM(cache_write), SUM(reasoning),
		       COUNT(*), MIN(ts), MAX(ts)
		FROM usage_events`, keyExpr, bucketExpr, tierExpr))
	f.apply(q)
	q.tail("GROUP BY k, source, model, b, tier")

	rows, err := s.db.QueryContext(ctx, q.sql(), q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []aggRow
	for rows.Next() {
		var r aggRow
		var tier int
		if err := rows.Scan(&r.key, &r.source, &r.model, &r.bucket, &tier,
			&r.input, &r.output, &r.cacheRead, &r.w5m, &r.w1h, &r.wGen, &r.reasoning,
			&r.events, &r.first, &r.last); err != nil {
			return nil, err
		}
		if tier > 0 && tier <= len(thresholds) {
			r.promptHint = thresholds[tier-1] + 1
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- result shapes ---------------------------------------------------------

// Group is one aggregated row. Every grouped list in the API (by_source,
// by_device, by_model, breakdown rows) uses this shape; fields that do not apply
// to a grouping are omitted.
type Group struct {
	Key            string     `json:"key"`
	Name           string     `json:"name,omitempty"`
	Source         string     `json:"source,omitempty"`
	Device         string     `json:"device,omitempty"`
	Input          int64      `json:"input"`
	Output         int64      `json:"output"`
	CacheRead      int64      `json:"cache_read"`
	CacheWrite     int64      `json:"cache_write"`
	Reasoning      int64      `json:"reasoning"`
	CostUSD        float64    `json:"cost_usd"`
	CostKnownRatio float64    `json:"cost_known_ratio"`
	Estimated      bool       `json:"estimated"`
	Events         int64      `json:"events"`
	FirstSeen      *time.Time `json:"first_seen,omitempty"`
	LastSeen       *time.Time `json:"last_seen,omitempty"`
}

// Totals is the summary header.
type Totals struct {
	Input          int64   `json:"input"`
	Output         int64   `json:"output"`
	CacheRead      int64   `json:"cache_read"`
	CacheWrite     int64   `json:"cache_write"`
	Reasoning      int64   `json:"reasoning"`
	CostUSD        float64 `json:"cost_usd"`
	CostKnownRatio float64 `json:"cost_known_ratio"`
	Events         int64   `json:"events"`
}

// acc accumulates priced aggregate rows for one output group.
type acc struct {
	key, name, source, device string
	multiSource               bool
	tok                       tokens
	costUSD                   float64
	knownTokens               int64
	estimated                 bool
	first, last               int64
}

func (a *acc) add(r aggRow, usd float64, known, estimated bool) {
	if a.source == "" && !a.multiSource {
		a.source = r.source
	} else if a.source != r.source {
		a.source = ""
		a.multiSource = true
	}
	a.tok.add(r.tokens)
	if known {
		a.costUSD += usd
		a.knownTokens += r.tokens.total()
	}
	if estimated {
		a.estimated = true
	}
	if a.first == 0 || r.first < a.first {
		a.first = r.first
	}
	if r.last > a.last {
		a.last = r.last
	}
}

// totals is the summary header for everything this accumulator has seen.
func (a *acc) totals() Totals {
	return Totals{
		Input:          a.tok.input,
		Output:         a.tok.output,
		CacheRead:      a.tok.cacheRead,
		CacheWrite:     a.tok.cacheWrite(),
		Reasoning:      a.tok.reasoning,
		CostUSD:        round6(a.costUSD),
		CostKnownRatio: ratio(a.knownTokens, a.tok.total()),
		Events:         a.tok.events,
	}
}

// point renders one timeseries bucket. It carries no cost_known_ratio: a series
// is about shape over time, and the ratio lives on /summary.
func (a *acc) point(t string) Point {
	return Point{
		T:          t,
		Input:      a.tok.input,
		Output:     a.tok.output,
		CacheRead:  a.tok.cacheRead,
		CacheWrite: a.tok.cacheWrite(),
		Reasoning:  a.tok.reasoning,
		CostUSD:    round6(a.costUSD),
		Events:     a.tok.events,
	}
}

func (a *acc) group() Group {
	g := Group{
		Key:            a.key,
		Name:           a.name,
		Source:         a.source,
		Device:         a.device,
		Input:          a.tok.input,
		Output:         a.tok.output,
		CacheRead:      a.tok.cacheRead,
		CacheWrite:     a.tok.cacheWrite(),
		Reasoning:      a.tok.reasoning,
		CostUSD:        round6(a.costUSD),
		CostKnownRatio: ratio(a.knownTokens, a.tok.total()),
		Estimated:      a.estimated,
		Events:         a.tok.events,
	}
	if a.first > 0 {
		t := fromMillis(a.first)
		g.FirstSeen = &t
	}
	if a.last > 0 {
		t := fromMillis(a.last)
		g.LastSeen = &t
	}
	return g
}

func ratio(known, total int64) float64 {
	if total <= 0 {
		return 1
	}
	return round6(float64(known) / float64(total))
}

// round6 trims float noise off aggregate money and ratio values (micro-dollars).
func round6(f float64) float64 { return math.Round(f*1e6) / 1e6 }

// roundUSD keeps more digits for a single event, whose cost can be tiny.
func roundUSD(f float64) float64 { return math.Round(f*1e9) / 1e9 }

// priceRow prices one aggregate row at the row's bucket time.
func priceRow(p Pricer, r aggRow, at time.Time) (usd float64, known, estimated bool) {
	if p == nil {
		return 0, false, false
	}
	c := p.CostFor(r.tokens.asEvent(r.model, at), r.promptHint)
	return c.USD, c.Known, c.Estimated
}

// thresholdsOf tolerates a nil pricer.
func thresholdsOf(p Pricer) []int64 {
	if p == nil {
		return nil
	}
	return p.Thresholds()
}

// sortGroups orders by cost, then by token volume, then by key for stability.
func sortGroups(gs []Group) {
	sort.Slice(gs, func(i, j int) bool {
		a, b := gs[i], gs[j]
		if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		}
		at := a.Input + a.Output + a.CacheRead + a.CacheWrite
		bt := b.Input + b.Output + b.CacheRead + b.CacheWrite
		if at != bt {
			return at > bt
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Source < b.Source
	})
}

// accSet is an insertion ordered set of output groups. Every grouped list in the
// API is built the same way - fold the aggregate rows into accumulators, then
// sort - so a caller only says which key a row belongs to and how a new group
// starts.
type accSet struct {
	byKey map[string]*acc
	ord   []string
}

func newAccSet() *accSet { return &accSet{byKey: map[string]*acc{}} }

// at returns the accumulator for key, calling init the first time key appears.
func (s *accSet) at(key string, init func(*acc)) *acc {
	a, ok := s.byKey[key]
	if !ok {
		a = &acc{}
		if init != nil {
			init(a)
		}
		s.byKey[key] = a
		s.ord = append(s.ord, key)
	}
	return a
}

// groups renders the set as API rows, most expensive first.
func (s *accSet) groups() []Group {
	out := make([]Group, 0, len(s.ord))
	for _, k := range s.ord {
		out = append(out, s.byKey[k].group())
	}
	sortGroups(out)
	return out
}

// GroupColumn maps an API group_by value to its column. It returns ok=false for
// anything else, so no user input ever reaches the SQL text.
func GroupColumn(groupBy string) (col string, ok bool) {
	switch groupBy {
	case "source":
		return "source", true
	case "device":
		return "device_id", true
	case "model":
		return "model", true
	case "project":
		return "project", true
	case "session":
		return "session_id", true
	}
	return "", false
}

func (s *Store) deviceNames(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
