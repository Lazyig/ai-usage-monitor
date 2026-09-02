package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPushReportsResync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := decodeIngest(t, w, r); !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"accepted":3,"duplicates":0,"device_id":"d1","resync":true}`)
	}))
	defer srv.Close()

	st, err := newClient(t, srv).PushStats(context.Background(), makeEvents(3))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Resync {
		t.Errorf("Stats = %+v, want Resync", st)
	}
	if st.Accepted != 3 {
		t.Errorf("accepted = %d, want 3", st.Accepted)
	}
}

func TestPushWithoutResyncLeavesTheFlagUnset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := decodeIngest(t, w, r); !ok {
			return
		}
		writeAccepted(w, 1, 0) // no "resync" key at all
	}))
	defer srv.Close()

	st, err := newClient(t, srv).PushStats(context.Background(), makeEvents(1))
	if err != nil {
		t.Fatal(err)
	}
	if st.Resync {
		t.Error("Resync is set although the server never asked for one")
	}
}

// A push splits into several batches; the server flags exactly one of them, and
// that must not be lost in the aggregate.
func TestResyncFromAnySingleBatchWins(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeIngest(t, w, r)
		if !ok {
			return
		}
		n++
		w.Header().Set("Content-Type", "application/json")
		// Only the second of the three batches carries the flag.
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":0,"resync":%t}`, len(req.Events), n == 2)
	}))
	defer srv.Close()

	st, err := newClient(t, srv).PushStats(context.Background(), makeEvents(2*MaxBatchEvents+1))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("%d requests, want 3", n)
	}
	if !st.Resync {
		t.Errorf("Stats = %+v, want Resync from the middle batch", st)
	}
}

func TestFlushReportsResync(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	c := newClient(t, down)
	if _, err := c.PushStats(context.Background(), makeEvents(2)); !Queued(err) {
		t.Fatalf("push against a broken server = %v, want it queued", err)
	}
	down.Close()

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := decodeIngest(t, w, r); !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"accepted":0,"duplicates":2,"resync":true}`)
	}))
	defer up.Close()
	c.BaseURL = up.URL
	c.HTTPClient = up.Client()

	st, err := c.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Resync {
		t.Errorf("Flush stats = %+v, want Resync", st)
	}
}

func TestQueueReset(t *testing.T) {
	q, err := OpenQueue(t.TempDir()+"/queue", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Reset on a queue whose directory was never created is a no-op, not an error.
	if err := q.Reset(); err != nil {
		t.Fatalf("Reset on an empty queue: %v", err)
	}
	for i := range 3 {
		if err := q.Enqueue([]byte(fmt.Sprintf(`{"events":[%d]}`, i))); err != nil {
			t.Fatal(err)
		}
	}
	if n, _, _ := q.Stats(); n != 3 {
		t.Fatalf("queued %d batches, want 3", n)
	}
	if err := q.Reset(); err != nil {
		t.Fatal(err)
	}
	n, bytes, err := q.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || bytes != 0 {
		t.Fatalf("after Reset: %d batches, %d bytes", n, bytes)
	}
	// The queue still works afterwards.
	if err := q.Enqueue([]byte(`{"events":[]}`)); err != nil {
		t.Fatalf("enqueue after Reset: %v", err)
	}
}
