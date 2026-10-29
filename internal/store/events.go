package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-usage-monitor/internal/event"
)

// InsertEvents stores a batch idempotently. A new event_id is inserted; a known
// one is left alone unless the incoming copy reports more output tokens, in
// which case its token counts, timestamp, confidence and tool version replace
// the stored ones. That second case is a streamed response: Claude Code appends
// one line per content block, all under the same message id, and an agent that
// scanned mid-stream has already shipped the first, nearly empty line. Output
// only grows while a response streams, so "more output" identifies the later,
// more complete copy without any notion of ordering between agents or runs.
//
// It returns how many rows were new (accepted), how many were already present
// and unchanged (duplicates) and how many were refreshed (updated). The whole
// batch is one transaction.
//
// The result is the same as handling the events one by one in batch order,
// but the statements are batched: new events go in with multi-row INSERTs,
// and the stored copies of the others are read in bulk so only the copies that
// change a row cost an UPDATE. A full re-sync (every event already stored)
// therefore takes a few SELECTs rather than thousands of round trips.
func (s *Store) InsertEvents(ctx context.Context, evs []event.UsageEvent) (accepted, duplicates, updated int, err error) {
	if len(evs) == 0 {
		return 0, 0, 0, nil
	}
	batch := make([]event.UsageEvent, len(evs))
	for i, e := range evs {
		batch[i] = cleanEvent(e)
	}
	received := nowMillis()
	err = s.writeTx(ctx, func(tx *sql.Tx) error {
		var err error
		accepted, duplicates, updated, err = insertEventsTx(ctx, tx, s.batch, batch, received)
		return err
	})
	if err != nil {
		return 0, 0, 0, err
	}
	return accepted, duplicates, updated, nil
}

// batchSize is how many rows one multi-row INSERT carries and how many ids one
// IN list holds. Over the network every statement is a round trip, so
// PostgreSQL takes large batches (500 x 17 parameters stays below its limit of
// 65535). SQLite has no round trip, and its driver matches each numbered
// parameter against every argument, a cost that grows with the square of the
// parameter count, so there one row per prepared INSERT is fastest.
//
// prepare reuses each statement text within the transaction: on SQLite,
// parsing it again is most of the cost. PostgreSQL leaves it off, because a
// prepared statement there is a named server-side object that transaction
// pooling (PgBouncer) cannot follow from one connection to another.
type batchSize struct {
	rows, ids int
	prepare   bool
}

var sqliteBatch = batchSize{rows: 1, ids: 16, prepare: true}

// stmtTx runs statements in one transaction, preparing each distinct text
// once when prepare is set.
type stmtTx struct {
	tx      *sql.Tx
	prepare bool
	stmts   map[string]*sql.Stmt
}

func (t *stmtTx) stmt(ctx context.Context, q string) (*sql.Stmt, error) {
	if st, ok := t.stmts[q]; ok {
		return st, nil
	}
	st, err := t.tx.PrepareContext(ctx, q)
	if err != nil {
		return nil, err
	}
	if t.stmts == nil {
		t.stmts = map[string]*sql.Stmt{}
	}
	t.stmts[q] = st
	return st, nil
}

func (t *stmtTx) query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if !t.prepare {
		return t.tx.QueryContext(ctx, q, args...)
	}
	st, err := t.stmt(ctx, q)
	if err != nil {
		return nil, err
	}
	return st.QueryContext(ctx, args...)
}

func (t *stmtTx) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	if !t.prepare {
		return t.tx.ExecContext(ctx, q, args...)
	}
	st, err := t.stmt(ctx, q)
	if err != nil {
		return nil, err
	}
	return st.ExecContext(ctx, args...)
}

func (t *stmtTx) close() {
	for _, st := range t.stmts {
		st.Close()
	}
}

const eventCols = `event_id, device_id, source, ts, model, session_id, project,
	input, output, cache_read, cache_write_5m, cache_write_1h, cache_write, reasoning,
	confidence, tool_version, received_at`

// storedEvent is what the refresh decision needs to know about a stored row.
type storedEvent struct {
	device, model string
	output        int64
}

// refresh is the planned work for one incoming copy of an already stored event.
type refresh struct {
	idx          int // position in the batch
	update, fill bool
}

func insertEventsTx(ctx context.Context, sqlTx *sql.Tx, size batchSize, batch []event.UsageEvent, received int64) (accepted, duplicates, updated int, err error) {
	tx := &stmtTx{tx: sqlTx, prepare: size.prepare}
	defer tx.close()
	// The first copy of each id is offered for insertion; later copies in the
	// same batch, like copies of ids already stored, can only refresh it.
	firstOf := make(map[string]int, len(batch))
	var firsts []int
	for i, e := range batch {
		if _, seen := firstOf[e.EventID]; !seen {
			firstOf[e.EventID] = i
			firsts = append(firsts, i)
		}
	}
	// Inserting in id order makes two concurrent batches take their row locks
	// in the same order, so they wait for each other instead of deadlocking.
	sort.Slice(firsts, func(a, b int) bool { return batch[firsts[a]].EventID < batch[firsts[b]].EventID })

	inserted := make(map[string]bool, len(firsts))
	for start := 0; start < len(firsts); start += size.rows {
		part := firsts[start:min(start+size.rows, len(firsts))]
		ids, err := insertChunk(ctx, tx, batch, part, received)
		if err != nil {
			return 0, 0, 0, err
		}
		for _, id := range ids {
			inserted[id] = true
		}
	}
	accepted = len(inserted)

	var rest []int // batch order
	for i, e := range batch {
		if firstOf[e.EventID] == i && inserted[e.EventID] {
			continue
		}
		rest = append(rest, i)
	}
	if len(rest) == 0 {
		return accepted, 0, 0, nil
	}

	stored, err := storedEvents(ctx, tx, size.ids, batch, rest)
	if err != nil {
		return 0, 0, 0, err
	}
	// Replay the copies in batch order against the stored state to see which
	// ones change the row. device_id, source, model, session, project and
	// received_at stay as first recorded: the identity of the event does not
	// change, only its final size. The one exception is the model placeholder
	// (event.UnknownModel), filled in when a later copy names the model. Both
	// refreshes are limited to the event's own device: ids are global, so
	// without that one device token could rewrite (or inflate) another
	// device's stored calls.
	var plan []refresh
	for _, i := range rest {
		e := batch[i]
		st, ok := stored[e.EventID]
		if !ok || st.device != e.DeviceID {
			duplicates++
			continue
		}
		r := refresh{idx: i, update: st.output < e.Output, fill: e.Model != event.UnknownModel && st.model == event.UnknownModel}
		if !r.update && !r.fill {
			duplicates++
			continue
		}
		if r.update {
			st.output = e.Output
		}
		if r.fill {
			st.model = e.Model
		}
		stored[e.EventID] = st
		plan = append(plan, r)
	}
	// Run the refreshes in id order (copies of one id keep their batch order),
	// for the same lock-ordering reason as the inserts. The statements keep
	// their own conditions, so a row changed concurrently since it was read
	// only turns a planned refresh into a no-op.
	sort.SliceStable(plan, func(a, b int) bool { return batch[plan[a].idx].EventID < batch[plan[b].idx].EventID })
	for _, r := range plan {
		n, err := applyRefresh(ctx, tx, batch[r.idx], r)
		if err != nil {
			return 0, 0, 0, err
		}
		if n > 0 {
			updated++
		} else {
			duplicates++
		}
	}
	return accepted, duplicates, updated, nil
}

// insertChunk inserts the events at idx (distinct ids) and returns the ids
// that were new.
func insertChunk(ctx context.Context, tx *stmtTx, batch []event.UsageEvent, idx []int, received int64) ([]string, error) {
	var b strings.Builder
	args := make([]any, 0, len(idx)*17)
	b.WriteString("INSERT INTO usage_events(" + eventCols + ") VALUES ")
	for n, i := range idx {
		e := batch[i]
		if n > 0 {
			b.WriteString(",")
		}
		b.WriteString("(")
		for c := range 17 {
			if c > 0 {
				b.WriteString(",")
			}
			b.WriteString("$" + strconv.Itoa(len(args)+c+1))
		}
		b.WriteString(")")
		args = append(args,
			e.EventID, e.DeviceID, string(e.Source), toMillis(e.Timestamp), e.Model, e.SessionID, e.Project,
			e.Input, e.Output, e.CacheRead, e.CacheWrite5m, e.CacheWrite1h, e.CacheWrite, e.Reasoning,
			string(e.Confidence), e.ToolVersion, received)
	}
	b.WriteString(" ON CONFLICT(event_id) DO NOTHING RETURNING event_id")
	rows, err := tx.query(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("store: insert events: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// storedEvents reads the stored state of the events at idx.
func storedEvents(ctx context.Context, tx *stmtTx, chunk int, batch []event.UsageEvent, idx []int) (map[string]storedEvent, error) {
	var ids []string
	seen := map[string]bool{}
	for _, i := range idx {
		if id := batch[i].EventID; !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	out := make(map[string]storedEvent, len(ids))
	for start := 0; start < len(ids); start += chunk {
		part := ids[start:min(start+chunk, len(ids))]
		var b strings.Builder
		args := make([]any, len(part))
		b.WriteString("SELECT event_id, device_id, output, model FROM usage_events WHERE event_id IN (")
		for n, id := range part {
			if n > 0 {
				b.WriteString(",")
			}
			b.WriteString("$" + strconv.Itoa(n+1))
			args[n] = id
		}
		b.WriteString(")")
		rows, err := tx.query(ctx, b.String(), args...)
		if err != nil {
			return nil, fmt.Errorf("store: read stored events: %w", err)
		}
		for rows.Next() {
			var id string
			var st storedEvent
			if err := rows.Scan(&id, &st.device, &st.output, &st.model); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = st
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// applyRefresh runs the planned statements for one copy and returns how many
// rows they changed.
func applyRefresh(ctx context.Context, tx *stmtTx, e event.UsageEvent, r refresh) (int64, error) {
	var n int64
	if r.update {
		res, err := tx.exec(ctx, `
			UPDATE usage_events SET
				ts = $1, input = $2, output = $3, cache_read = $4, cache_write_5m = $5, cache_write_1h = $6,
				cache_write = $7, reasoning = $8, confidence = $9, tool_version = $10
			WHERE event_id = $11 AND device_id = $12 AND output < $3`,
			toMillis(e.Timestamp), e.Input, e.Output, e.CacheRead, e.CacheWrite5m, e.CacheWrite1h,
			e.CacheWrite, e.Reasoning, string(e.Confidence), e.ToolVersion,
			e.EventID, e.DeviceID)
		if err != nil {
			return 0, fmt.Errorf("store: refresh event %s: %w", e.EventID, err)
		}
		if n, err = res.RowsAffected(); err != nil {
			return 0, err
		}
	}
	if r.fill {
		res, err := tx.exec(ctx,
			`UPDATE usage_events SET model = $1 WHERE event_id = $2 AND device_id = $3 AND model = $4`,
			e.Model, e.EventID, e.DeviceID, event.UnknownModel)
		if err != nil {
			return 0, fmt.Errorf("store: fill model of %s: %w", e.EventID, err)
		}
		filled, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		n += filled
	}
	return n, nil
}

// cleanEvent makes the agent-supplied free text storable on every backend.
// The id fields are not touched: ingest rejects events whose ids would need it.
func cleanEvent(e event.UsageEvent) event.UsageEvent {
	e.Source = event.Source(CleanText(string(e.Source)))
	e.Model = CleanText(e.Model)
	e.Project = CleanText(e.Project)
	e.ToolVersion = CleanText(e.ToolVersion)
	e.Confidence = event.Confidence(CleanText(string(e.Confidence)))
	return e
}

// EventRow is a stored event plus its computed cost.
type EventRow struct {
	event.UsageEvent
	ReceivedAt time.Time `json:"received_at"`
	CostUSD    *float64  `json:"cost_usd"` // null when the model has no price
	Estimated  bool      `json:"estimated,omitempty"`
}

// EventPage is one keyset page of raw events, newest first.
type EventPage struct {
	Events     []EventRow `json:"events"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

// Events returns raw events matching f, newest first, using keyset pagination on
// (ts, event_id). cursor is the opaque NextCursor of the previous page.
func (s *Store) Events(ctx context.Context, f Filter, limit int, cursor string, p Pricer) (EventPage, error) {
	switch {
	case limit <= 0:
		limit = 200
	case limit > 1000:
		limit = 1000
	}
	q := newQueryBuilder(`
		SELECT event_id, device_id, source, ts, model, session_id, project,
		       input, output, cache_read, cache_write_5m, cache_write_1h, cache_write, reasoning,
		       confidence, tool_version, received_at
		FROM usage_events`)
	f.apply(q)
	if cursor != "" {
		c, err := decodeCursor(cursor)
		if err != nil {
			return EventPage{}, err
		}
		ts := q.arg(c.TS)
		q.where(`(ts < ` + ts + ` OR (ts = ` + ts + ` AND event_id < ` + q.arg(c.EventID) + `))`)
	}
	q.tail(fmt.Sprintf(`ORDER BY ts DESC, event_id DESC LIMIT %d`, limit+1))

	rows, err := s.rdb.QueryContext(ctx, q.sql(), q.args...)
	if err != nil {
		return EventPage{}, err
	}
	defer rows.Close()

	page := EventPage{Events: []EventRow{}}
	for rows.Next() {
		var (
			r          EventRow
			src, conf  string
			ts, recvAt int64
		)
		if err := rows.Scan(&r.EventID, &r.DeviceID, &src, &ts, &r.Model, &r.SessionID, &r.Project,
			&r.Input, &r.Output, &r.CacheRead, &r.CacheWrite5m, &r.CacheWrite1h, &r.CacheWrite, &r.Reasoning,
			&conf, &r.ToolVersion, &recvAt); err != nil {
			return EventPage{}, err
		}
		r.Source = event.Source(src)
		r.Confidence = event.Confidence(conf)
		r.Timestamp = fromMillis(ts)
		r.ReceivedAt = fromMillis(recvAt)
		if p != nil {
			c := p.Cost(r.UsageEvent)
			if c.Known {
				usd := roundUSD(c.USD)
				r.CostUSD = &usd
			}
			r.Estimated = c.Estimated
		}
		page.Events = append(page.Events, r)
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, err
	}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		last := page.Events[len(page.Events)-1]
		page.NextCursor = encodeCursor(toMillis(last.Timestamp), last.EventID)
	}
	return page, nil
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

// ErrBadCursor is returned by Events for a cursor it did not issue.
var ErrBadCursor = errors.New("store: bad cursor")

func decodeCursor(s string) (cursorV1, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursorV1{}, ErrBadCursor
	}
	var c cursorV1
	if err := json.Unmarshal(b, &c); err != nil || c.EventID == "" {
		return cursorV1{}, ErrBadCursor
	}
	return c, nil
}
