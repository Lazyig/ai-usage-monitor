package transport

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
)

func TestTemporaryHTTPResponsesKeepUndeliveredBatches(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var recover atomic.Bool
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !recover.Load() {
					w.WriteHeader(status)
					return
				}
				writeAccepted(w, 1, 0)
			}))
			defer srv.Close()
			c := newClient(t, srv)
			_, err := c.PushStats(context.Background(), makeEvents(1))
			if !Queued(err) || calls.Load() != int32(c.Attempts) {
				t.Fatalf("temporary response: err=%v calls=%d", err, calls.Load())
			}
			if n, _, _ := c.Queue.Stats(); n != 1 {
				t.Fatalf("queued batches = %d, want 1", n)
			}
			// Flushing during a rate limit must not permanently discard it.
			if _, err := c.Flush(context.Background()); err == nil {
				t.Fatal("flush unexpectedly succeeded")
			}
			if n, _, _ := c.Queue.Stats(); n != 1 {
				t.Fatalf("retryable flush lost the batch: %d", n)
			}
			recover.Store(true)
			if st, err := c.Flush(context.Background()); err != nil || st.Accepted != 1 {
				t.Fatalf("recovery: %+v %v", st, err)
			}
			if n, _, _ := c.Queue.Stats(); n != 0 {
				t.Fatalf("delivered batch remains queued: %d", n)
			}
		})
	}
}

func TestInvalidAcknowledgementsAreQueued(t *testing.T) {
	for _, kind := range []string{"html", "truncated", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "html":
					fmt.Fprint(w, "<html>temporary proxy error</html>")
				case "truncated":
					w.Header().Set("Content-Length", "1000")
					writeAccepted(w, 1, 0)
				case "oversized":
					writeAccepted(w, 1, 0)
					fmt.Fprint(w, strings.Repeat(" ", 1<<20))
				}
			}))
			defer srv.Close()
			c := newClient(t, srv)
			c.Attempts = 2
			if _, err := c.PushStats(context.Background(), makeEvents(1)); !Queued(err) {
				t.Fatalf("invalid acknowledgement was treated as delivered: %v", err)
			}
			if n, _, _ := c.Queue.Stats(); n != 1 {
				t.Fatalf("queued batches = %d, want 1", n)
			}
		})
	}
}

// recorder collects the decoded bodies of every ingest request.
type recorder struct {
	mu       sync.Mutex
	requests []ingestRequest
	headers  []http.Header
}

func (r *recorder) add(req ingestRequest, h http.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	r.headers = append(r.headers, h.Clone())
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// decodeIngest reads a request body, insisting that it really is gzipped JSON.
func decodeIngest(t *testing.T, w http.ResponseWriter, req *http.Request) (ingestRequest, bool) {
	t.Helper()
	var out ingestRequest
	if enc := req.Header.Get("Content-Encoding"); enc != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", enc)
		w.WriteHeader(http.StatusBadRequest)
		return out, false
	}
	zr, err := gzip.NewReader(req.Body)
	if err != nil {
		t.Errorf("body is not gzip: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return out, false
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Errorf("read body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return out, false
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Errorf("body is not the ingest schema: %v (%s)", err, raw)
		w.WriteHeader(http.StatusBadRequest)
		return out, false
	}
	return out, true
}

func writeAccepted(w http.ResponseWriter, accepted, duplicates int) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"accepted":%d,"duplicates":%d,"device_id":"dev"}`, accepted, duplicates)
}

// newClient wires a client to srv with a fast retry policy and a queue in a
// temp dir, so no test ever waits on the real backoff schedule.
func newClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	q, err := OpenQueue(filepath.Join(t.TempDir(), "queue"), 0)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{
		BaseURL: srv.URL,
		Token:   "aum_test",
		DeviceInfo: Device{
			ID: "device-1", Name: "laptop", OS: "linux", Arch: "amd64", AgentVersion: "1.2.3",
		},
		HTTPClient: srv.Client(),
		Queue:      q,
		Attempts:   4,
		BaseDelay:  time.Millisecond,
		MaxDelay:   2 * time.Millisecond,
		Logf:       func(string, ...any) {},
	}
}

func makeEvents(n int) []event.UsageEvent {
	out := make([]event.UsageEvent, n)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = event.UsageEvent{
			EventID:    fmt.Sprintf("e%05d", i),
			DeviceID:   "device-1",
			Source:     event.SourceClaudeCode,
			Timestamp:  base.Add(time.Duration(i) * time.Second),
			Model:      "claude-opus-5",
			Input:      10,
			Output:     20,
			Confidence: event.ConfidenceExact,
		}
	}
	return out
}

func TestPushSendsGzippedJSONWithHeaders(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ingest" {
			t.Errorf("path = %q, want /api/v1/ingest", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		rec.add(req, r.Header)
		writeAccepted(w, len(req.Events), 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	accepted, duplicates, rejected, err := c.Push(context.Background(), makeEvents(3))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if accepted != 3 || duplicates != 0 || rejected != 0 {
		t.Errorf("accepted=%d duplicates=%d rejected=%d, want 3/0/0", accepted, duplicates, rejected)
	}
	if rec.count() != 1 {
		t.Fatalf("%d requests, want 1", rec.count())
	}
	h := rec.headers[0]
	if got, want := h.Get("Authorization"), "Bearer aum_test"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got, want := h.Get("User-Agent"), "ai-usage-agent/1.2.3"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
	if got := h.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	dev := rec.requests[0].Device
	if dev.ID != "device-1" || dev.OS != "linux" || dev.Arch != "amd64" || dev.AgentVersion != "1.2.3" {
		t.Errorf("device block = %+v", dev)
	}
	if len(rec.requests[0].Events) != 3 {
		t.Errorf("%d events delivered, want 3", len(rec.requests[0].Events))
	}
}

func TestPushBatchesLargeRuns(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		if len(req.Events) > MaxBatchEvents {
			t.Errorf("batch of %d events exceeds the %d cap", len(req.Events), MaxBatchEvents)
		}
		rec.add(req, r.Header)
		writeAccepted(w, len(req.Events), 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	accepted, _, _, err := c.Push(context.Background(), makeEvents(5000))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	// 5,000 events at 2,000 per batch is 2,000 + 2,000 + 1,000.
	if rec.count() != 3 {
		t.Fatalf("%d requests, want 3", rec.count())
	}
	sizes := []int{len(rec.requests[0].Events), len(rec.requests[1].Events), len(rec.requests[2].Events)}
	if sizes[0] != 2000 || sizes[1] != 2000 || sizes[2] != 1000 {
		t.Errorf("batch sizes = %v, want [2000 2000 1000]", sizes)
	}
	if accepted != 5000 {
		t.Errorf("accepted = %d, want 5000", accepted)
	}
	// Every event must be delivered exactly once, in order.
	seen := map[string]int{}
	for _, req := range rec.requests {
		for _, e := range req.Events {
			seen[e.EventID]++
		}
	}
	if len(seen) != 5000 {
		t.Errorf("%d distinct events delivered, want 5000", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("event %s delivered %d times", id, n)
		}
	}
}

func TestPushHeartbeatIsOneEmptyBatch(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		rec.add(req, r.Header)
		writeAccepted(w, 0, 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	if _, _, _, err := c.Push(context.Background(), nil); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("%d requests, want exactly 1 heartbeat", rec.count())
	}
	if got := rec.requests[0].Events; len(got) != 0 || got == nil {
		t.Errorf("heartbeat events = %v, want an empty (non-null) array", got)
	}
}

func TestPushRetriesServerErrors(t *testing.T) {
	var attempts int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		writeAccepted(w, len(req.Events), 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	accepted, _, _, err := c.Push(context.Background(), makeEvents(2))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if attempts != 3 {
		t.Errorf("%d attempts, want 3 (two 500s then a success)", attempts)
	}
	if accepted != 2 {
		t.Errorf("accepted = %d, want 2", accepted)
	}
	if n, _, _ := c.Queue.Stats(); n != 0 {
		t.Errorf("%d batches queued after an eventual success, want 0", n)
	}
}

func TestPushAuthErrorIsTypedAndNotRetried(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	_, _, _, err := c.Push(context.Background(), makeEvents(2))
	if err == nil {
		t.Fatal("Push succeeded against a 401")
	}
	if attempts != 1 {
		t.Errorf("%d attempts, want 1: a rejected token must not be retried", attempts)
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error %v is not an *AuthError", err)
	}
	if authErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", authErr.StatusCode)
	}
	if !IsAuth(err) {
		t.Error("IsAuth did not recognise the error")
	}
	// The events are not lost: the run loop can advance its cursor because the
	// batch went to the queue.
	if !Queued(err) {
		t.Error("a 401 should still queue the batch for a later, fixed token")
	}
	if n, _, _ := c.Queue.Stats(); n != 1 {
		t.Errorf("%d batches queued, want 1", n)
	}
}

func TestPushForbiddenIsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"device mismatch"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	_, _, _, err := newClient(t, srv).Push(context.Background(), makeEvents(1))
	if !IsAuth(err) {
		t.Fatalf("403 gave %v, want an *AuthError", err)
	}
}

func TestPushPermanentErrorIsNotQueued(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	_, _, _, err := c.Push(context.Background(), makeEvents(1))
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Fatalf("error %v is not a *PermanentError", err)
	}
	if attempts != 1 {
		t.Errorf("%d attempts, want 1", attempts)
	}
	if n, _, _ := c.Queue.Stats(); n != 0 {
		t.Errorf("%d batches queued; a permanently rejected batch must not poison the queue", n)
	}
}

func TestPushQueuesWhenTheServerIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	c := newClient(t, srv)
	srv.Close() // nothing is listening any more: a network error, not an HTTP one

	_, _, _, err := c.Push(context.Background(), makeEvents(2500))
	if err == nil {
		t.Fatal("Push succeeded with no server")
	}
	if !Queued(err) {
		t.Fatalf("error %v does not report the batch as queued", err)
	}
	var q *QueuedError
	errors.As(err, &q)
	if q.Batches != 2 {
		t.Errorf("queued %d batches, want 2 (2,000 + 500)", q.Batches)
	}
	n, bytes, err := c.Queue.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || bytes == 0 {
		t.Errorf("queue holds %d batches / %d bytes, want 2 non-empty batches", n, bytes)
	}
}

func TestFlushReplaysQueuedBatchesOldestFirst(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		rec.add(req, r.Header)
		writeAccepted(w, len(req.Events), 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	// Queue two batches by hand, in a known order.
	for i := range 2 {
		payload, err := c.encode(makeEvents(i + 1))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Queue.Enqueue(payload); err != nil {
			t.Fatal(err)
		}
	}

	st, err := c.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if st.Requests != 2 || st.Accepted != 3 {
		t.Errorf("flush stats = %+v, want 2 requests and 3 accepted", st)
	}
	if rec.count() != 2 {
		t.Fatalf("%d requests, want 2", rec.count())
	}
	if len(rec.requests[0].Events) != 1 || len(rec.requests[1].Events) != 2 {
		t.Errorf("replay order was not oldest first: %d then %d events",
			len(rec.requests[0].Events), len(rec.requests[1].Events))
	}
	if n, _, _ := c.Queue.Stats(); n != 0 {
		t.Errorf("%d batches left in the queue after a clean flush", n)
	}
}

func TestFlushStopsAtTheFirstFailure(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "still down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	for range 3 {
		payload, err := c.encode(makeEvents(1))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Queue.Enqueue(payload); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Flush(context.Background()); err == nil {
		t.Fatal("Flush succeeded against a 503")
	}
	if n, _, _ := c.Queue.Stats(); n != 3 {
		t.Errorf("%d batches left, want all 3 kept for the next tick", n)
	}
	if attempts != c.Attempts {
		t.Errorf("%d requests, want %d (one batch's retries, then stop)", attempts, c.Attempts)
	}
}

func TestPushThenFlushDeliversEverythingOnce(t *testing.T) {
	var (
		mu     sync.Mutex
		up     bool
		byID   = map[string]int{}
		reqNum int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		online := up
		reqNum++
		mu.Unlock()
		if !online {
			http.Error(w, "offline", http.StatusBadGateway)
			return
		}
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		mu.Lock()
		for _, e := range req.Events {
			byID[e.EventID]++
		}
		mu.Unlock()
		writeAccepted(w, len(req.Events), 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	events := makeEvents(10)
	if _, _, _, err := c.Push(context.Background(), events); !Queued(err) {
		t.Fatalf("offline push gave %v, want a queued error", err)
	}

	mu.Lock()
	up = true
	mu.Unlock()

	if _, err := c.Flush(context.Background()); err != nil {
		t.Fatalf("Flush after recovery: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(byID) != 10 {
		t.Errorf("%d distinct events reached the server, want 10", len(byID))
	}
	for id, n := range byID {
		if n != 1 {
			t.Errorf("event %s delivered %d times", id, n)
		}
	}
}

func TestFlushRewritesTheDeviceBlock(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		rec.add(req, r.Header)
		writeAccepted(w, len(req.Events), 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	payload, err := c.encode(makeEvents(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Queue.Enqueue(payload); err != nil {
		t.Fatal(err)
	}
	// The agent was upgraded while the batch sat in the queue.
	c.DeviceInfo.AgentVersion = "9.9.9"
	if _, err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.requests[0].Device.AgentVersion; got != "9.9.9" {
		t.Errorf("replayed device version = %q, want the current 9.9.9", got)
	}
}

func TestRejectedCountsWhatTheServerDropped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := decodeIngest(t, w, r); !ok {
			return
		}
		writeAccepted(w, 6, 2) // 10 sent, 8 accounted for
	}))
	defer srv.Close()

	accepted, duplicates, rejected, err := newClient(t, srv).Push(context.Background(), makeEvents(10))
	if err != nil {
		t.Fatal(err)
	}
	if accepted != 6 || duplicates != 2 || rejected != 2 {
		t.Errorf("accepted=%d duplicates=%d rejected=%d, want 6/2/2", accepted, duplicates, rejected)
	}
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("health sent an Authorization header (%q); it must not leak the token", got)
		}
		fmt.Fprint(w, `{"ok":true,"version":"0.2.0","events":42}`)
	}))
	defer srv.Close()

	got, err := newClient(t, srv).Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !got.OK || got.Version != "0.2.0" || got.Events != 42 {
		t.Errorf("health = %+v", got)
	}
}

func TestHealthReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := newClient(t, srv).Health(context.Background()); err == nil {
		t.Fatal("Health succeeded against a 500")
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if _, ok := decodeIngest(t, w, r); !ok {
			return
		}
		writeAccepted(w, 1, 0)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	c.BaseURL = srv.URL + "/"
	if _, _, _, err := c.Push(context.Background(), makeEvents(1)); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/ingest" {
		t.Errorf("path = %q, want /api/v1/ingest (no double slash)", path)
	}
}

func TestPushRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	c.BaseDelay = time.Second
	c.MaxDelay = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, _, _, err := c.Push(ctx, makeEvents(1)); err == nil {
		t.Fatal("Push succeeded despite a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Errorf("Push took %s; it should abandon the backoff when the context ends", elapsed)
	}
}

func TestQueueCapDropsOldestBatches(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	var warnings []string
	q, err := OpenQueue(dir, 300) // tiny cap so a few batches overflow it
	if err != nil {
		t.Fatal(err)
	}
	q.Logf = func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	payloads := [][]byte{
		[]byte(strings.Repeat("a", 100)),
		[]byte(strings.Repeat("b", 100)),
		[]byte(strings.Repeat("c", 100)),
		[]byte(strings.Repeat("d", 100)),
	}
	for _, p := range payloads {
		if err := q.Enqueue(p); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	n, bytes, err := q.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if bytes > 300 {
		t.Errorf("queue holds %d bytes, over the 300 byte cap", bytes)
	}
	if n != 3 {
		t.Errorf("queue holds %d batches, want 3 after one was dropped", n)
	}
	items, err := q.List()
	if err != nil {
		t.Fatal(err)
	}
	first, err := q.Read(items[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(payloads[1]) {
		t.Error("the oldest batch was not the one dropped")
	}
	if len(warnings) == 0 {
		t.Error("dropping a batch must warn")
	}
}

func TestQueueRejectsABatchLargerThanTheCap(t *testing.T) {
	q, err := OpenQueue(filepath.Join(t.TempDir(), "queue"), 50)
	if err != nil {
		t.Fatal(err)
	}
	q.Logf = func(string, ...any) {}
	if err := q.Enqueue([]byte(strings.Repeat("x", 100))); err == nil {
		t.Fatal("Enqueue accepted a batch larger than the whole queue")
	}
	if n, _, _ := q.Stats(); n != 0 {
		t.Errorf("%d batches stored, want 0", n)
	}
}

func TestQueueOrderIsChronological(t *testing.T) {
	q, err := OpenQueue(filepath.Join(t.TempDir(), "queue"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		if err := q.Enqueue(fmt.Appendf(nil, `{"n":%d}`, i)); err != nil {
			t.Fatal(err)
		}
	}
	items, err := q.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 25 {
		t.Fatalf("%d items, want 25", len(items))
	}
	for i, it := range items {
		raw, err := q.Read(it)
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf(`{"n":%d}`, i); string(raw) != want {
			t.Fatalf("item %d = %s, want %s (names must sort chronologically)", i, raw, want)
		}
	}
}

func TestQueueIgnoresStrayFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	q, err := OpenQueue(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".batch-half.tmp"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	items, err := q.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("List returned %d items, want only the real batch", len(items))
	}
}

func TestOpenQueueDoesNotCreateTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	q, err := OpenQueue(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	// `agent status` opens the queue only to report on it, so it must not leave
	// a directory behind on a device that has never been offline.
	if _, err := os.Stat(dir); err == nil {
		t.Error("OpenQueue created the directory")
	}
	if n, bytes, err := q.Stats(); err != nil || n != 0 || bytes != 0 {
		t.Errorf("Stats on a missing queue = %d/%d/%v, want 0/0/nil", n, bytes, err)
	}
	if err := q.Enqueue([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("Enqueue must create the directory: %v", err)
	}
	if n, _, _ := q.Stats(); n != 1 {
		t.Errorf("%d batches after Enqueue, want 1", n)
	}
}

func TestQueueRemoveIsIdempotent(t *testing.T) {
	q, err := OpenQueue(filepath.Join(t.TempDir(), "queue"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	items, _ := q.List()
	if err := q.Remove(items[0]); err != nil {
		t.Fatal(err)
	}
	if err := q.Remove(items[0]); err != nil {
		t.Errorf("removing an already gone batch failed: %v", err)
	}
}

func TestBackoffStaysWithinBounds(t *testing.T) {
	c := &Client{BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
	for attempt := 1; attempt <= 8; attempt++ {
		for range 50 {
			d := c.backoff(attempt)
			if d <= 0 || d > c.MaxDelay {
				t.Fatalf("backoff(%d) = %s, outside (0, %s]", attempt, d, c.MaxDelay)
			}
		}
	}
	// Jitter must actually vary, or a fleet retries in lockstep.
	seen := map[time.Duration]bool{}
	for range 50 {
		seen[c.backoff(4)] = true
	}
	if len(seen) < 2 {
		t.Error("backoff produced no jitter")
	}
}

// Events the server refreshed in place (a grown streaming copy, a model filled
// in) were delivered fine and must not be reported as rejected.
func TestUpdatedEventsAreNotCountedAsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := decodeIngest(t, w, r); !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":6,"duplicates":2,"updated":2,"rejected":0,"device_id":"dev"}`))
	}))
	defer srv.Close()

	accepted, duplicates, rejected, err := newClient(t, srv).Push(context.Background(), makeEvents(10))
	if err != nil {
		t.Fatal(err)
	}
	if accepted != 6 || duplicates != 2 || rejected != 0 {
		t.Errorf("accepted=%d duplicates=%d rejected=%d, want 6/2/0", accepted, duplicates, rejected)
	}
}

// A 4xx that is not the server's own JSON error (a proxy's 404 page, an nginx
// 413) never judged the batch: the queued copy must survive, since the cursors
// already moved past it. The server's own rejection still drops it.
func TestFlushKeepsBatchesOnAForeign4xx(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantKept  int
		wantError bool
	}{
		{"proxy 404 page", http.StatusNotFound, "<html>not here</html>", 1, true},
		{"nginx 413", http.StatusRequestEntityTooLarge, "<html>413 Request Entity Too Large</html>", 1, true},
		{"server rejection", http.StatusBadRequest, `{"error":"invalid JSON body"}`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := newClient(t, srv)
			payload, err := c.encode(makeEvents(1))
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Queue.Enqueue(payload); err != nil {
				t.Fatal(err)
			}
			_, err = c.Flush(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("Flush error = %v, want error %v", err, tc.wantError)
			}
			if n, _, _ := c.Queue.Stats(); n != tc.wantKept {
				t.Errorf("%d batches left, want %d", n, tc.wantKept)
			}
		})
	}
}

// A 2xx JSON body that does not name the device is not the server's receipt.
func TestAcknowledgementNeedsADeviceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv.Close()
	c := newClient(t, srv)
	_, _, _, err := c.Push(context.Background(), makeEvents(1))
	if !Queued(err) {
		t.Fatalf("a portal's 200 counted as delivery: %v", err)
	}
}

// One batch a proxy keeps refusing must not hold back the ones queued after it.
func TestFlushMovesPastAForeignRejection(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		if len(req.Events) == 1 {
			attempts++
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			fmt.Fprint(w, "<html>413</html>")
			return
		}
		writeAccepted(w, len(req.Events), 0)
	}))
	defer srv.Close()
	c := newClient(t, srv)
	for _, n := range []int{1, 2} {
		payload, err := c.encode(makeEvents(n))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Queue.Enqueue(payload); err != nil {
			t.Fatal(err)
		}
	}
	st, err := c.Flush(context.Background())
	if err == nil {
		t.Fatal("Flush hid the refused batch")
	}
	if st.Accepted != 2 {
		t.Errorf("the batch behind the refused one was not delivered: %+v", st)
	}
	if n, _, _ := c.Queue.Stats(); n != 1 {
		t.Errorf("%d batches left, want the refused one only", n)
	}
	if attempts != 1 {
		t.Errorf("refused batch sent %d times in one flush, want 1", attempts)
	}
}
