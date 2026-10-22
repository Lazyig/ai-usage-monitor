// Package transport ships collected usage events to the server.
//
// It owns everything between "the collectors produced events" and "the server
// acknowledged them": batching, gzip, retries with backoff, and an on-disk
// queue that keeps the events of an offline device until it comes back.
package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"ai-usage-monitor/internal/event"
)

// MaxBatchEvents is the number of events per ingest request. The server accepts
// up to 5,000 (docs/API.md); we stay well under it to keep single requests
// small enough to retry cheaply on a slow uplink.
const MaxBatchEvents = 2000

// Defaults for the retry policy and the HTTP client.
const (
	DefaultAttempts    = 5
	DefaultBaseDelay   = 500 * time.Millisecond
	DefaultMaxDelay    = 30 * time.Second
	DefaultHTTPTimeout = 60 * time.Second
)

// Device is the identity block sent with every ingest request.
type Device struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
}

// ingestRequest is the POST /api/v1/ingest body.
type ingestRequest struct {
	Device Device             `json:"device"`
	Events []event.UsageEvent `json:"events"`
}

// ingestResponse is the server's reply. Anything neither accepted, a duplicate
// nor an update of a stored copy is counted as rejected by the caller.
type ingestResponse struct {
	Accepted   int    `json:"accepted"`
	Duplicates int    `json:"duplicates"`
	Updated    int    `json:"updated"` // stored copy refreshed (grown output, model filled in)
	DeviceID   string `json:"device_id"`
	Resync     bool   `json:"resync"` // an operator purged this device's data
}

// WhoAmIResponse is GET /api/v1/agent/whoami: the device the token is bound to.
type WhoAmIResponse struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

// HealthResponse is GET /health.
type HealthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Events  int64  `json:"events"`
}

// AuthError means the device token was refused (401) or does not match the
// device id (403). Retrying will not help; the run loop logs it once per tick
// and keeps collecting locally.
type AuthError struct {
	StatusCode int
	Body       string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("device token rejected (HTTP %d): %s", e.StatusCode, e.Body)
}

// PermanentError is a non-auth 4xx: the server understood the request and will
// not accept it, so the batch is neither retried nor queued.
type PermanentError struct {
	StatusCode int
	Body       string
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("server rejected the batch (HTTP %d): %s", e.StatusCode, e.Body)
}

// QueuedError means delivery failed but every undelivered event was written to
// the offline queue. The caller may advance its cursor: the events are safe on
// disk and will be retried on the next tick.
type QueuedError struct {
	Batches int
	Err     error
}

func (e *QueuedError) Error() string {
	return fmt.Sprintf("%v (%d batch(es) queued for retry)", e.Err, e.Batches)
}
func (e *QueuedError) Unwrap() error { return e.Err }

// Queued reports whether err means "not delivered, but safely on disk".
func Queued(err error) bool {
	var q *QueuedError
	return errors.As(err, &q)
}

// IsAuth reports whether err was caused by a rejected device token.
func IsAuth(err error) bool {
	var a *AuthError
	return errors.As(err, &a)
}

// Client posts batches to one server.
type Client struct {
	BaseURL    string
	Token      string
	DeviceInfo Device
	HTTPClient *http.Client

	// Queue, when set, receives batches that could not be delivered.
	Queue *Queue

	// UserAgent defaults to "ai-usage-agent/<DeviceInfo.AgentVersion>".
	UserAgent string

	// Retry policy. Zero values select the Default* constants above.
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration

	// Logf receives retry/queue notices. Never pass the token to it.
	Logf func(format string, args ...any)
}

// Stats is the outcome of a push or a queue flush.
type Stats struct {
	Accepted   int
	Duplicates int
	Updated    int
	Rejected   int
	Requests   int

	// Resync is set when any batch of this push came back with "resync": true,
	// meaning an operator threw away this device's stored events. The caller is
	// expected to drop its cursors and rescan from the beginning; the server
	// only ever says it once, so the flag must not be ignored.
	Resync bool
}

func (s *Stats) add(o Stats) {
	s.Accepted += o.Accepted
	s.Duplicates += o.Duplicates
	s.Updated += o.Updated
	s.Rejected += o.Rejected
	s.Requests += o.Requests
	s.Resync = s.Resync || o.Resync
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: DefaultHTTPTimeout}
}

func (c *Client) userAgent() string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	v := c.DeviceInfo.AgentVersion
	if v == "" {
		v = "dev"
	}
	return "ai-usage-agent/" + v
}

func (c *Client) attempts() int {
	if c.Attempts > 0 {
		return c.Attempts
	}
	return DefaultAttempts
}

func (c *Client) url(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

// Push delivers events to /api/v1/ingest in batches of at most MaxBatchEvents.
//
// Calling it with no events sends a single empty batch, which is the heartbeat
// the server uses to refresh the device's last_seen.
//
// The returned counts cover the batches the server actually answered. Batches
// that could not be delivered are written to the queue (when one is configured)
// and reported as a *QueuedError, which the caller should treat as "handled":
// the events are on disk and will be retried before the next push.
func (c *Client) Push(ctx context.Context, events []event.UsageEvent) (accepted, duplicates, rejected int, err error) {
	st, err := c.PushStats(ctx, events)
	return st.Accepted, st.Duplicates, st.Rejected, err
}

// PushStats is Push with the full Stats value.
func (c *Client) PushStats(ctx context.Context, events []event.UsageEvent) (Stats, error) {
	var total Stats
	heartbeat := len(events) == 0

	for i := 0; i < len(events) || (heartbeat && i == 0); i += MaxBatchEvents {
		end := min(i+MaxBatchEvents, len(events))
		batch := events[i:end]

		payload, err := c.encode(batch)
		if err != nil {
			return total, err
		}
		st, err := c.send(ctx, payload, len(batch))
		total.add(st)
		if err == nil {
			continue
		}

		var perm *PermanentError
		if errors.As(err, &perm) {
			// The server will never take this batch; queuing it would poison
			// the queue forever. Surface it and let the operator look.
			return total, err
		}
		if heartbeat {
			return total, err // nothing worth persisting
		}
		// Everything from this batch on is undelivered: queue it in order.
		queued, qerr := c.queueRemaining(events[i:])
		if qerr != nil {
			return total, errors.Join(err, qerr)
		}
		return total, &QueuedError{Batches: queued, Err: err}
	}
	return total, nil
}

// queueRemaining writes every not-yet-delivered event to the offline queue,
// split into the same batch sizes so a later flush replays them unchanged.
func (c *Client) queueRemaining(events []event.UsageEvent) (int, error) {
	if c.Queue == nil {
		return 0, errors.New("transport: no offline queue configured")
	}
	n := 0
	for i := 0; i < len(events); i += MaxBatchEvents {
		end := min(i+MaxBatchEvents, len(events))
		payload, err := c.encode(events[i:end])
		if err != nil {
			return n, err
		}
		if err := c.Queue.Enqueue(payload); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Flush replays queued batches, oldest first, deleting each one the server
// accepts. It stops at the first failure so ordering is preserved and a server
// that is still down is not hammered with the whole backlog.
func (c *Client) Flush(ctx context.Context) (Stats, error) {
	var total Stats
	if c.Queue == nil {
		return total, nil
	}
	items, err := c.Queue.List()
	if err != nil {
		return total, err
	}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		payload, err := c.Queue.Read(it)
		if err != nil {
			return total, err
		}
		// The device block is rewritten so a batch queued by an older agent
		// version still reports the version that finally delivered it.
		payload = c.rewriteDevice(payload)

		st, err := c.send(ctx, payload, -1)
		total.add(st)
		if err != nil {
			var perm *PermanentError
			if errors.As(err, &perm) {
				// Undeliverable forever: drop it rather than block the queue.
				c.logf("queue: dropping %s, server rejected it permanently: %v", it.Name, err)
				if rerr := c.Queue.Remove(it); rerr != nil {
					return total, rerr
				}
				continue
			}
			return total, err
		}
		if err := c.Queue.Remove(it); err != nil {
			return total, err
		}
	}
	return total, nil
}

// rewriteDevice replaces the device block of a stored payload with the current
// one. On any decode problem the payload is sent untouched.
func (c *Client) rewriteDevice(payload []byte) []byte {
	var req ingestRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return payload
	}
	req.Device = c.DeviceInfo
	out, err := json.Marshal(req)
	if err != nil {
		return payload
	}
	return out
}

// WhoAmI asks the server which device this token belongs to. Agents call it at
// startup and adopt the returned id so EventIDs match the server from the first
// batch (a locally generated id would otherwise be remapped server-side, and a
// reinstall would change it).
func (c *Client) WhoAmI(ctx context.Context) (WhoAmIResponse, error) {
	var out WhoAmIResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/api/v1/agent/whoami"), nil)
	if err != nil {
		return out, fmt.Errorf("transport: whoami request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("transport: whoami: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return out, &AuthError{StatusCode: resp.StatusCode, Body: snippet(body)}
	case resp.StatusCode/100 != 2:
		return out, fmt.Errorf("transport: whoami: HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("transport: whoami: %w", err)
	}
	return out, nil
}

// Health queries GET /health. It never sends the device token.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	var out HealthResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/health"), nil)
	if err != nil {
		return out, fmt.Errorf("transport: health request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("transport: health: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return out, fmt.Errorf("transport: health: HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("transport: health: decode: %w", err)
	}
	return out, nil
}

// encode builds a gzipped ingest payload. The stored (queued) form is the plain
// JSON body; gzip happens per request in send.
func (c *Client) encode(events []event.UsageEvent) ([]byte, error) {
	if events == nil {
		events = []event.UsageEvent{} // an explicit empty array, never null
	}
	raw, err := json.Marshal(ingestRequest{Device: c.DeviceInfo, Events: events})
	if err != nil {
		return nil, fmt.Errorf("transport: encode batch: %w", err)
	}
	return raw, nil
}

// send posts one payload, retrying network errors and 5xx with exponential
// backoff plus jitter. count is the number of events in the payload, or -1 when
// it is not known (replayed from the queue), in which case "rejected" cannot be
// derived and stays zero.
func (c *Client) send(ctx context.Context, payload []byte, count int) (Stats, error) {
	var last error
	attempts := c.attempts()
	for attempt := range attempts {
		if attempt > 0 {
			d := c.backoff(attempt)
			c.logf("ingest: retry %d/%d in %s: %v", attempt, attempts-1, d.Round(time.Millisecond), last)
			if err := sleepCtx(ctx, d); err != nil {
				return Stats{}, errors.Join(last, err)
			}
		}
		st, err, retryable := c.sendOnce(ctx, payload, count)
		if err == nil {
			return st, nil
		}
		last = err
		if !retryable || ctx.Err() != nil {
			return st, err
		}
	}
	return Stats{}, fmt.Errorf("transport: giving up after %d attempts: %w", attempts, last)
}

func (c *Client) sendOnce(ctx context.Context, payload []byte, count int) (Stats, error, bool) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		return Stats{}, fmt.Errorf("transport: gzip: %w", err), false
	}
	if err := zw.Close(); err != nil {
		return Stats{}, fmt.Errorf("transport: gzip close: %w", err), false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/api/v1/ingest"), bytes.NewReader(buf.Bytes()))
	if err != nil {
		return Stats{}, fmt.Errorf("transport: ingest request: %w", err), false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.ContentLength = int64(buf.Len())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Stats{}, fmt.Errorf("transport: ingest: %w", err), true // network: retry
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return Stats{}, &AuthError{StatusCode: resp.StatusCode, Body: snippet(body)}, false
	case resp.StatusCode == http.StatusRequestTimeout, resp.StatusCode == http.StatusTooManyRequests:
		return Stats{}, fmt.Errorf("transport: ingest: temporary HTTP %d: %s", resp.StatusCode, snippet(body)), true
	case resp.StatusCode/100 == 4:
		return Stats{}, &PermanentError{StatusCode: resp.StatusCode, Body: snippet(body)}, false
	case resp.StatusCode/100 != 2:
		return Stats{}, fmt.Errorf("transport: ingest: HTTP %d: %s", resp.StatusCode, snippet(body)), true
	}
	if readErr != nil {
		return Stats{}, fmt.Errorf("transport: ingest response: %w", readErr), true
	}
	if len(body) > 1<<20 {
		return Stats{}, errors.New("transport: ingest response exceeds 1 MiB"), true
	}

	var out ingestResponse
	if err := json.Unmarshal(body, &out); err != nil {
		// A proxy can return a 2xx error page before the batch reaches the
		// server. Retry within the normal bound, then retain the batch in the
		// offline queue. Replaying an already-stored event is idempotent.
		return Stats{}, fmt.Errorf("transport: ingest acknowledgement: %w", err), true
	}
	st := Stats{Accepted: out.Accepted, Duplicates: out.Duplicates, Updated: out.Updated, Requests: 1, Resync: out.Resync}
	if count >= 0 {
		if r := count - out.Accepted - out.Duplicates - out.Updated; r > 0 {
			st.Rejected = r
		}
	}
	return st, nil, false
}

// backoff is base * 2^(attempt-1) with full jitter, capped at MaxDelay.
func (c *Client) backoff(attempt int) time.Duration {
	base := c.BaseDelay
	if base <= 0 {
		base = DefaultBaseDelay
	}
	maxDelay := c.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	d := base << (attempt - 1)
	if d <= 0 || d > maxDelay {
		d = maxDelay
	}
	// Full jitter keeps a fleet of agents from retrying in lockstep after an
	// outage, and never waits less than half the nominal delay.
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// snippet trims a server error body to something loggable.
func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
