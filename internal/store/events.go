package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ai-usage-monitor/internal/event"
)

// InsertEvents stores a batch idempotently. A new event_id is inserted; a known
// one is left alone unless the incoming copy reports more output tokens, in
// which case its token counts, timestamp, confidence and tool version replace
// the stored ones. That
// second case is a streamed response: Claude Code appends one line per content
// block, all under the same message id, and an agent that scanned mid-stream
// has already shipped the first, nearly empty line. Output only grows while a
// response streams, so "more output" identifies the later, more complete copy
// without any notion of ordering between agents or runs.
//
// It returns how many rows were new (accepted), how many were already present
// and unchanged (duplicates) and how many were refreshed (updated). The whole
// batch is one transaction.
func (s *Store) InsertEvents(ctx context.Context, evs []event.UsageEvent) (accepted, duplicates, updated int, err error) {
	if len(evs) == 0 {
		return 0, 0, 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()

	ins, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO usage_events(
			event_id, device_id, source, ts, model, session_id, project,
			input, output, cache_read, cache_write_5m, cache_write_1h, cache_write, reasoning,
			confidence, tool_version, received_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer ins.Close()
	// device_id, source, model, session, project and received_at stay as first
	// recorded: the identity of the event does not change, only its final size.
	// The one exception is the model placeholder (event.UnknownModel), filled
	// in below when a later copy names the model.
	upd, err := tx.PrepareContext(ctx, `
		UPDATE usage_events SET
			ts = ?, input = ?, output = ?, cache_read = ?, cache_write_5m = ?, cache_write_1h = ?, cache_write = ?,
			reasoning = ?, confidence = ?, tool_version = ?
		WHERE event_id = ? AND device_id = ? AND output < ?`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer upd.Close()
	// Both refreshes are limited to the event's own device: ids are global, so
	// without it one device token could rewrite (or inflate) another
	// device's stored calls.
	fill, err := tx.PrepareContext(ctx, `UPDATE usage_events SET model = ? WHERE event_id = ? AND device_id = ? AND model = ?`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer fill.Close()

	received := nowMillis()
	for _, e := range evs {
		res, err := ins.ExecContext(ctx,
			e.EventID, e.DeviceID, string(e.Source), toMillis(e.Timestamp), e.Model, e.SessionID, e.Project,
			e.Input, e.Output, e.CacheRead, e.CacheWrite5m, e.CacheWrite1h, e.CacheWrite, e.Reasoning,
			string(e.Confidence), e.ToolVersion, received)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("store: insert event %s: %w", e.EventID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, 0, 0, err
		}
		if n > 0 {
			accepted++
			continue
		}
		res, err = upd.ExecContext(ctx,
			toMillis(e.Timestamp), e.Input, e.Output, e.CacheRead, e.CacheWrite5m, e.CacheWrite1h, e.CacheWrite,
			e.Reasoning, string(e.Confidence), e.ToolVersion,
			e.EventID, e.DeviceID, e.Output)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("store: refresh event %s: %w", e.EventID, err)
		}
		if n, err = res.RowsAffected(); err != nil {
			return 0, 0, 0, err
		}
		if e.Model != event.UnknownModel {
			res, err = fill.ExecContext(ctx, e.Model, e.EventID, e.DeviceID, event.UnknownModel)
			if err != nil {
				return 0, 0, 0, fmt.Errorf("store: fill model of %s: %w", e.EventID, err)
			}
			filled, err := res.RowsAffected()
			if err != nil {
				return 0, 0, 0, err
			}
			n += filled
		}
		if n > 0 {
			updated++
		} else {
			duplicates++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, err
	}
	return accepted, duplicates, updated, nil
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
		q.where(`(ts < ? OR (ts = ? AND event_id < ?))`, c.TS, c.TS, c.EventID)
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
