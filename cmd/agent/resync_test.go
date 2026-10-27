package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/event"
)

func TestResyncFromPartialQueueFlushIsNotLost(t *testing.T) {
	dir := setupState(t)
	var calls, received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer zr.Close()
		var body struct{ Events []event.UsageEvent }
		if err := json.NewDecoder(zr).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received.Add(int32(len(body.Events)))
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":0,"resync":%t}`, len(body.Events), n == 2)
	}))
	defer srv.Close()
	cfg, _, err := agentcfg.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ServerURL = srv.URL
	ag, err := newAgent(cfg, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ag.client.Attempts = 1
	if err := ag.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := received.Load()
	if first == 0 {
		t.Fatal("fixture history was not collected")
	}
	for range 2 {
		if err := ag.client.Queue.Enqueue([]byte(`{"events":[]}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ag.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if received.Load() != 2*first {
		t.Fatalf("resync lost after partial flush: %d events, want %d", received.Load(), 2*first)
	}
}

// ingestServer is a minimal /api/v1/ingest that counts what reached it and can
// ask for a resync exactly once, the way the real server does.
type ingestServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests int
	events   int
	resync   bool // armed; served (and cleared) by the next response
	asked    int  // how many responses carried resync
}

func newIngestServer(t *testing.T) *ingestServer {
	t.Helper()
	s := &ingestServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ingest" {
			http.NotFound(w, r)
			return
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Errorf("request body is not gzip: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer zr.Close()
		var body struct {
			Events []event.UsageEvent `json:"events"`
		}
		if err := json.NewDecoder(zr).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		s.requests++
		s.events += len(body.Events)
		resync := s.resync
		if resync {
			s.resync = false
			s.asked++
		}
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":0,"resync":%t}`, len(body.Events), resync)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *ingestServer) armResync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resync = true
}

func (s *ingestServer) counts() (requests, events, asked int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, s.events, s.asked
}

func TestTickRescansWhenTheServerAsksForAResync(t *testing.T) {
	dir := setupState(t)
	srv := newIngestServer(t)

	// First pass: the whole history goes up and the cursors are saved.
	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("first pass exit %d\n%s", code, out)
	}
	_, first, _ := srv.counts()
	if first == 0 {
		t.Fatal("the first pass sent nothing")
	}
	if _, err := os.Stat(agentcfg.CursorPath(dir)); err != nil {
		t.Fatalf("cursors were not saved: %v", err)
	}

	// The operator purges this device on the server. The next contact - here a
	// heartbeat, since nothing new was written locally - carries the request.
	srv.armResync()
	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("resync pass exit %d\n%s", code, out)
	}
	_, after, asked := srv.counts()
	if asked != 1 {
		t.Fatalf("the server was asked to resync %d times, want 1", asked)
	}
	if got := after - first; got != first {
		t.Fatalf("the resync re-sent %d events, want the full history again (%d)", got, first)
	}

	// And it is over: the pass after that finds nothing new again.
	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("third pass exit %d\n%s", code, out)
	}
	_, third, _ := srv.counts()
	if third != after {
		t.Errorf("the pass after the resync re-sent %d events, want 0", third-after)
	}
}

// A resync throws the offline queue away: its batches only hold events the
// rescan produces again, and the server has just dropped them anyway.
func TestResyncClearsTheOfflineQueue(t *testing.T) {
	dir := setupState(t)

	// Collect with no server reachable, so everything lands in the queue.
	if _, code := runCLI(t, "run", "--once", "--server", "http://127.0.0.1:1", "--token", "tok"); code != 0 {
		t.Fatal("queueing pass did not exit 0")
	}
	entries, err := os.ReadDir(agentcfg.QueueDir(dir))
	if err != nil || len(entries) == 0 {
		t.Fatalf("nothing was queued: %v", err)
	}

	srv := newIngestServer(t)
	srv.armResync()
	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	entries, err = os.ReadDir(agentcfg.QueueDir(dir))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("queued batch %s survived the resync", e.Name())
		}
	}
	if _, events, asked := srv.counts(); asked != 1 || events == 0 {
		t.Errorf("server saw %d events and %d resync requests", events, asked)
	}
}

// An agent upgraded across an EventID scheme change must rescan once, so its
// history is re-sent under the new ids.
func TestIDSchemeMigrationClearsCursors(t *testing.T) {
	dir := setupState(t)
	srv := newIngestServer(t)

	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("first pass exit %d\n%s", code, out)
	}
	_, first, _ := srv.counts()
	if first == 0 {
		t.Fatal("the first pass sent nothing")
	}

	// Pretend the cursors were written by an agent on the old scheme.
	cfg, _, err := agentcfg.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.IDScheme = event.IDScheme - 1
	if err := agentcfg.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("upgraded pass exit %d\n%s", code, out)
	}
	_, after, asked := srv.counts()
	if got := after - first; got != first {
		t.Errorf("the upgraded agent re-sent %d events, want the full history (%d)", got, first)
	}
	if asked != 0 {
		t.Errorf("the server was asked for %d resyncs; this migration is purely local", asked)
	}

	// The new scheme is recorded, so the next start does not rescan again.
	cfg, _, err = agentcfg.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IDScheme != event.IDScheme {
		t.Fatalf("id_scheme = %d after the migration, want %d", cfg.IDScheme, event.IDScheme)
	}
	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("third pass exit %d\n%s", code, out)
	}
	if _, third, _ := srv.counts(); third != after {
		t.Errorf("the pass after the migration re-sent %d events, want 0", third-after)
	}
}

func TestRescanClearsTheState(t *testing.T) {
	dir := setupState(t)
	srv := newIngestServer(t)

	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("run exit %d\n%s", code, out)
	}
	_, first, _ := srv.counts()
	if _, err := os.Stat(agentcfg.CursorPath(dir)); err != nil {
		t.Fatalf("no cursors to clear: %v", err)
	}

	out, code := runCLI(t, "rescan", "--state-dir", dir)
	if code != 0 {
		t.Fatalf("rescan exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "cleared") {
		t.Errorf("rescan said nothing about what it cleared:\n%s", out)
	}
	if _, err := os.Stat(agentcfg.CursorPath(dir)); !os.IsNotExist(err) {
		t.Errorf("cursors.json survived rescan: %v", err)
	}
	if _, err := os.Stat(agentcfg.QueueDir(dir)); !os.IsNotExist(err) {
		t.Errorf("the queue directory survived rescan: %v", err)
	}

	// The next pass really does start from zero.
	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("run after rescan exit %d\n%s", code, out)
	}
	if _, after, _ := srv.counts(); after-first != first {
		t.Errorf("the pass after rescan sent %d events, want the full history (%d)", after-first, first)
	}
}

func TestRescanRejectsPositionalArguments(t *testing.T) {
	setupState(t)
	if _, code := runCLI(t, "rescan", "everything"); code != 2 {
		t.Errorf("exit code %d for a stray argument, want 2", code)
	}
}

// A stopping loop sends what was written since its last tick: on a short lived
// host the logs vanish with it, so waiting for the next interval loses them.
func TestLoopSendsAFinalPassWhenStopped(t *testing.T) {
	dir := setupState(t)
	srv := newIngestServer(t)

	// Start with empty roots so the first tick finds nothing, then fill them in
	// while the loop sleeps through an interval that would never come round.
	cfg, _, err := agentcfg.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	roots := testdataRoots(t)
	empty := map[event.Source]string{}
	for src := range roots {
		empty[src] = filepath.Join(t.TempDir(), "root")
		cfg.Source(src).Root = empty[src]
	}
	if err := agentcfg.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	ag, err := bootstrap(dir, nil, runOverrides{server: srv.URL, token: "tok", interval: 3600})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ag.loop(ctx) }()

	// The first tick is a heartbeat: no events yet, but the server hears from it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if req, _, _ := srv.counts(); req > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first tick never reached the server")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, events, _ := srv.counts(); events != 0 {
		t.Fatalf("the first tick sent %d events from empty roots", events)
	}

	for src, root := range roots {
		if err := os.CopyFS(empty[src], os.DirFS(root)); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop returned %v", err)
		}
	case <-time.After(finalPassTimeout + 5*time.Second):
		t.Fatal("the loop did not stop")
	}
	if _, events, _ := srv.counts(); events == 0 {
		t.Fatal("nothing written after the last tick reached the server on stop")
	}
}

func TestFinalPassDefersResyncUntilNextRun(t *testing.T) {
	dir := setupState(t)
	srv := newIngestServer(t)
	ag, err := bootstrap(dir, nil, runOverrides{server: srv.URL, token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ag.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, first, _ := srv.counts()
	if first == 0 {
		t.Fatal("the first pass sent nothing")
	}
	srv.armResync()
	ag.lastContact = time.Time{} // Make the final pass send a heartbeat.
	ag.finalPass()
	_, after, asked := srv.counts()
	if asked != 1 || after != first {
		t.Fatalf("final pass: events=%d resync requests=%d, want %d and 1", after, asked, first)
	}
	// Bootstrap again to verify the reset survived on disk.
	ag, err = bootstrap(dir, nil, runOverrides{server: srv.URL, token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ag.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, total, _ := srv.counts()
	if total != 2*first {
		t.Fatalf("next run sent %d events, want %d", total-first, first)
	}
}
