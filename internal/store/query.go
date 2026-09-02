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

// Summary is GET /summary.
type Summary struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Totals   Totals    `json:"totals"`
	BySource []Group   `json:"by_source"`
	ByDevice []Group   `json:"by_device"`
	ByModel  []Group   `json:"by_model"`
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

// --- summary ---------------------------------------------------------------

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
		total     acc
		bySource  = map[string]*acc{}
		byDevice  = map[string]*acc{}
		byModel   = map[string]*acc{}
		sourceOrd []string
		deviceOrd []string
		modelOrd  []string
	)
	get := func(m map[string]*acc, ord *[]string, key string, init func(*acc)) *acc {
		a, ok := m[key]
		if !ok {
			a = &acc{}
			if init != nil {
				init(a)
			}
			m[key] = a
			*ord = append(*ord, key)
		}
		return a
	}
	for _, r := range rows {
		at := fromMillis(r.bucket * dayMs)
		usd, known, est := priceRow(p, r, at)

		total.add(r, usd, known, est)
		get(bySource, &sourceOrd, r.source, func(a *acc) { a.key = r.source }).add(r, usd, known, est)
		get(byDevice, &deviceOrd, r.key, func(a *acc) { a.key = r.key; a.name = names[r.key] }).add(r, usd, known, est)
		mk := r.model + "\x00" + r.source
		get(byModel, &modelOrd, mk, func(a *acc) { a.key = r.model }).add(r, usd, known, est)
	}

	out := Summary{
		From: f.From.UTC(), To: f.To.UTC(),
		Totals: Totals{
			Input:          total.tok.input,
			Output:         total.tok.output,
			CacheRead:      total.tok.cacheRead,
			CacheWrite:     total.tok.cacheWrite(),
			Reasoning:      total.tok.reasoning,
			CostUSD:        round6(total.costUSD),
			CostKnownRatio: ratio(total.knownTokens, total.tok.total()),
			Events:         total.tok.events,
		},
		BySource: collect(bySource, sourceOrd),
		ByDevice: collect(byDevice, deviceOrd),
		ByModel:  collect(byModel, modelOrd),
	}
	// by_source rows carry no separate source field; by_device rows keep the
	// device name instead of a source when several sources contributed.
	for i := range out.BySource {
		out.BySource[i].Source = ""
	}
	return out, nil
}

func collect(m map[string]*acc, ord []string) []Group {
	out := make([]Group, 0, len(ord))
	for _, k := range ord {
		out = append(out, m[k].group())
	}
	sortGroups(out)
	return out
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

// --- breakdown -------------------------------------------------------------

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

	groups := map[string]*acc{}
	var ord []string
	for _, r := range rows {
		at := fromMillis(r.bucket * dayMs)
		usd, known, est := priceRow(p, r, at)
		key := r.key
		if groupBy == "model" {
			key = r.model + "\x00" + r.source
		}
		a, exists := groups[key]
		if !exists {
			a = &acc{key: r.key, device: f.Device}
			if groupBy == "device" {
				a.name = names[r.key]
				a.device = r.key
			}
			groups[key] = a
			ord = append(ord, key)
		}
		a.add(r, usd, known, est)
	}
	out := Breakdown{GroupBy: groupBy, Rows: collect(groups, ord)}
	if len(out.Rows) > limit {
		out.Rows = out.Rows[:limit]
	}
	return out, nil
}

// --- timeseries ------------------------------------------------------------

// Point is one bucket of a series.
type Point struct {
	T          string  `json:"t"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
	Reasoning  int64   `json:"reasoning"`
	CostUSD    float64 `json:"cost_usd"`
	Events     int64   `json:"events"`
}

// Series is one group's points, oldest first.
type Series struct {
	Key    string  `json:"key"`
	Name   string  `json:"name,omitempty"`
	Points []Point `json:"points"`
}

// Timeseries is GET /timeseries.
type Timeseries struct {
	Bucket  string   `json:"bucket"`
	GroupBy string   `json:"group_by"`
	TZ      string   `json:"tz"`
	Series  []Series `json:"series"`
}

type tzSegment struct {
	startMs, endMs int64
	offsetSec      int64
}

// bucketSegments splits [from,to) into maximal runs of buckets that share one
// UTC offset, using the offset in effect at each bucket's *start*. Inside a
// segment, (ts+offset)/bucketMs assigns every event to the right local bucket,
// including across a DST change (the transition simply ends the segment).
func bucketSegments(loc *time.Location, from, to time.Time, bucket string) []tzSegment {
	if !from.Before(to) {
		return nil
	}
	var segs []tzSegment
	cur := bucketStart(from.In(loc), bucket)
	fromMs, toMs := toMillis(from), toMillis(to)
	for i := 0; cur.Before(to) && i < 1_000_000; i++ {
		_, off := cur.Zone()
		next := nextBucket(cur, bucket)
		if !next.After(cur) { // paranoia: never loop forever
			break
		}
		if n := len(segs); n > 0 && segs[n-1].offsetSec == int64(off) && segs[n-1].endMs == cur.UnixMilli() {
			segs[n-1].endMs = next.UnixMilli()
		} else {
			segs = append(segs, tzSegment{startMs: cur.UnixMilli(), endMs: next.UnixMilli(), offsetSec: int64(off)})
		}
		cur = next
	}
	for i := range segs {
		if segs[i].startMs < fromMs {
			segs[i].startMs = fromMs
		}
		if segs[i].endMs > toMs {
			segs[i].endMs = toMs
		}
	}
	return segs
}

func bucketStart(t time.Time, bucket string) time.Time {
	y, m, d := t.Date()
	if bucket == "hour" {
		return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
	}
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func nextBucket(t time.Time, bucket string) time.Time {
	if bucket == "hour" {
		return bucketStart(t.Add(time.Hour), "hour")
	}
	y, m, d := t.Date()
	// Noon on the next calendar day, then back to that day's start: immune to
	// zones where local midnight does not exist on a DST day.
	return bucketStart(time.Date(y, m, d+1, 12, 0, 0, 0, t.Location()), "day")
}

// Timeseries buckets the range by day or hour in loc and groups by one dimension.
func (s *Store) Timeseries(ctx context.Context, f Filter, bucket, groupBy string, loc *time.Location, p Pricer) (Timeseries, error) {
	col, ok := GroupColumn(groupBy)
	if !ok {
		return Timeseries{}, fmt.Errorf("store: unsupported group_by %q", groupBy)
	}
	bucketMs := dayMs
	layout := "2006-01-02"
	switch bucket {
	case "day", "":
		bucket = "day"
	case "hour":
		bucketMs = hourMs
		layout = time.RFC3339
	default:
		return Timeseries{}, fmt.Errorf("store: unsupported bucket %q", bucket)
	}
	names, err := s.deviceNames(ctx)
	if err != nil {
		return Timeseries{}, err
	}

	type cell struct {
		start int64
		acc   acc
	}
	series := map[string]map[int64]*cell{}
	var keyOrd []string

	for _, seg := range bucketSegments(loc, f.From, f.To, bucket) {
		if seg.startMs >= seg.endMs {
			continue
		}
		sf := f
		sf.From = fromMillis(seg.startMs)
		sf.To = fromMillis(seg.endMs)
		rows, err := s.aggregate(ctx, sf, col, bucketMs, seg.offsetSec, thresholdsOf(p))
		if err != nil {
			return Timeseries{}, err
		}
		for _, r := range rows {
			startMs := r.bucket*bucketMs - seg.offsetSec*1000
			at := fromMillis(startMs)
			usd, known, est := priceRow(p, r, at)
			m, ok := series[r.key]
			if !ok {
				m = map[int64]*cell{}
				series[r.key] = m
				keyOrd = append(keyOrd, r.key)
			}
			c, ok := m[startMs]
			if !ok {
				c = &cell{start: startMs}
				m[startMs] = c
			}
			c.acc.add(r, usd, known, est)
		}
	}

	out := Timeseries{Bucket: bucket, GroupBy: groupBy, TZ: loc.String(), Series: []Series{}}
	sort.Strings(keyOrd)
	for _, key := range keyOrd {
		cells := series[key]
		starts := make([]int64, 0, len(cells))
		for k := range cells {
			starts = append(starts, k)
		}
		sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
		ser := Series{Key: key, Points: make([]Point, 0, len(starts))}
		if groupBy == "device" {
			ser.Name = names[key]
		}
		for _, st := range starts {
			a := cells[st]
			ser.Points = append(ser.Points, Point{
				T:          time.UnixMilli(st).In(loc).Format(layout),
				Input:      a.acc.tok.input,
				Output:     a.acc.tok.output,
				CacheRead:  a.acc.tok.cacheRead,
				CacheWrite: a.acc.tok.cacheWrite(),
				Reasoning:  a.acc.tok.reasoning,
				CostUSD:    round6(a.acc.costUSD),
				Events:     a.acc.tok.events,
			})
		}
		out.Series = append(out.Series, ser)
	}
	return out, nil
}
