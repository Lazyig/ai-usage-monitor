package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/cursor"
	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/transport"
)

// heartbeatEvery is how often an idle agent posts an empty batch so the server
// can refresh last_seen. A device with no activity still has to look alive.
const heartbeatEvery = 5 * time.Minute

// agent is the long running collect/push machine.
type agent struct {
	cfg        agentcfg.Config
	cursors    *cursor.Store
	client     *transport.Client
	collectors []active
	log        *log.Logger

	lastContact time.Time // last successful request of any kind
	authWarned  bool      // so an invalid token is not logged every single tick
}

func newAgent(cfg agentcfg.Config, dir string, sel selected) (*agent, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	cur, err := cursor.Open(agentcfg.CursorPath(dir))
	if err != nil {
		return nil, err
	}
	// One logger for everything the run loop says: it writes wherever the
	// standard logger points, which is the terminal for an interactive run and
	// <state dir>/agent.log once the service has called setupLogFile.
	lg := log.New(log.Writer(), "", log.LstdFlags|log.LUTC)

	q, err := transport.OpenQueue(agentcfg.QueueDir(dir), transport.DefaultQueueMaxBytes)
	if err != nil {
		return nil, err
	}
	q.Logf = func(format string, args ...any) { lg.Printf("warn: "+format, args...) }

	ag := &agent{
		cfg:     cfg,
		cursors: cur,
		log:     lg,
		client: &transport.Client{
			BaseURL: cfg.ServerURL,
			Token:   cfg.DeviceToken,
			DeviceInfo: transport.Device{
				ID:           cfg.DeviceID,
				Name:         cfg.DeviceName,
				OS:           runtime.GOOS,
				Arch:         runtime.GOARCH,
				AgentVersion: version,
			},
			HTTPClient: &http.Client{Timeout: transport.DefaultHTTPTimeout},
			Queue:      q,
			Logf:       func(format string, args ...any) { lg.Printf(format, args...) },
		},
		collectors: build(cfg, sel),
	}
	if len(ag.collectors) == 0 {
		return nil, fmt.Errorf("no collectors enabled (available: %s)", strings.Join(sourceNames(), ", "))
	}
	return ag, nil
}

// tick is one full pass: replay anything queued, scan every collector, push,
// then persist the cursors that were actually covered by that push.
func (a *agent) tick(ctx context.Context) error {
	return a.pass(ctx, true)
}

// pass is one tick. resyncOK guards the single retry a server-requested resync
// triggers, so the second (from-zero) pass can never ask for another one.
func (a *agent) pass(ctx context.Context, resyncOK bool) error {
	resync := false

	if st, err := a.client.Flush(ctx); err != nil {
		a.noteErr("queue flush", err)
	} else if st.Requests > 0 {
		a.lastContact = time.Now()
		resync = resync || st.Resync
		a.log.Printf("queue: replayed %d batch(es), accepted=%d duplicates=%d", st.Requests, st.Accepted, st.Duplicates)
	}

	var (
		events  []event.UsageEvent
		next    = map[event.Source][]byte{}
		counts  []string
		scanErr []error
	)
	for _, c := range a.collectors {
		if err := ctx.Err(); err != nil {
			return err
		}
		got, cur, err := c.Collector.Scan(ctx, a.cfg.DeviceID, a.cursors.Get(c.Source))
		if err != nil {
			scanErr = append(scanErr, fmt.Errorf("%s: %w", c.Source, err))
			// Keep whatever the collector managed to produce; its cursor came
			// back unchanged, so nothing is skipped on the next pass.
		}
		events = append(events, got...)
		next[c.Source] = cur
		counts = append(counts, fmt.Sprintf("%s=%d", c.Source, len(got)))
	}
	sort.Strings(counts)

	if len(events) == 0 {
		// Nothing new: keep the device visible with an occasional empty batch.
		// An idle device makes no other request, so this is also how it hears
		// about a resync.
		if time.Since(a.lastContact) >= heartbeatEvery {
			st, err := a.client.PushStats(ctx, nil)
			if err != nil {
				a.noteErr("heartbeat", err)
			} else {
				a.lastContact = time.Now()
				resync = resync || st.Resync
				a.log.Printf("tick: no new events, heartbeat sent")
			}
		} else {
			a.log.Printf("tick: no new events")
		}
		a.saveCursors(next) // file rotations can move a cursor without any events
		if resync && resyncOK {
			return errors.Join(errors.Join(scanErr...), a.resync(ctx))
		}
		return errors.Join(scanErr...)
	}

	st, err := a.client.PushStats(ctx, events)
	switch {
	case err == nil:
		a.lastContact = time.Now()
		a.authWarned = false
		a.saveCursors(next)
		resync = resync || st.Resync
	case transport.Queued(err):
		// Not delivered, but safely on disk: the cursor may advance.
		a.saveCursors(next)
		if transport.IsAuth(err) {
			if !a.authWarned {
				a.log.Printf("error: token invalid (%v); still collecting locally, events are queued", err)
				a.authWarned = true
			}
		} else {
			a.log.Printf("warn: push failed, queued for retry: %v", err)
		}
	default:
		// Cursors stay where they are, so nothing is lost; the same events are
		// re-scanned next tick and the server dedupes them by EventID.
		a.noteErr("push", err)
	}

	a.log.Printf("tick: scanned %s (%d events) | accepted=%d duplicates=%d updated=%d rejected=%d | %s",
		strings.Join(counts, " "), len(events), st.Accepted, st.Duplicates, st.Updated, st.Rejected, a.queueSummary())
	out := errors.Join(append(scanErr, ignoreHandled(err))...)
	if resync && resyncOK {
		return errors.Join(out, a.resync(ctx))
	}
	return out
}

// resync answers a server that threw this device's stored events away. Its
// cursors now point past history the server no longer has, and the offline
// queue holds batches it no longer wants, so both go and one full pass runs
// from zero. Re-sending is safe: an EventID does not depend on the device or on
// which run produced it, so the server's dedupe settles anything sent twice.
func (a *agent) resync(ctx context.Context) error {
	a.log.Printf("server requested a full resync: clearing cursors and the offline queue, rescanning from zero")
	if err := a.cursors.Reset(); err != nil {
		return fmt.Errorf("resync: %w", err)
	}
	if err := a.client.Queue.Reset(); err != nil {
		// Not fatal: a stale batch replays as duplicates, it does not corrupt
		// anything. The rescan below is what actually restores the history.
		a.log.Printf("warn: resync: clear queue: %v", err)
	}
	return a.pass(ctx, false)
}

// ignoreHandled drops errors the tick already dealt with (queued batches) so a
// --once run still exits 0 when the events are safely on disk.
func ignoreHandled(err error) error {
	if err == nil || transport.Queued(err) {
		return nil
	}
	return err
}

func (a *agent) noteErr(what string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	a.log.Printf("error: %s: %v", what, err)
}

func (a *agent) saveCursors(next map[event.Source][]byte) {
	changed := false
	for src, cur := range next {
		if len(cur) == 0 {
			continue
		}
		if err := a.cursors.Set(src, cur); err != nil {
			a.log.Printf("warn: %v", err)
			continue
		}
		changed = true
	}
	if !changed {
		return
	}
	if err := a.cursors.Save(); err != nil {
		a.log.Printf("error: save cursors: %v", err)
	}
}

func (a *agent) queueSummary() string {
	n, bytes, err := a.client.Queue.Stats()
	if err != nil {
		return "queue unknown"
	}
	if n == 0 {
		return "queue empty"
	}
	return fmt.Sprintf("queue %d batch(es), %s bytes", n, num(bytes))
}

// loop runs ticks until ctx is cancelled. The first pass happens immediately so
// a freshly started service does not stay silent for a whole interval.
func (a *agent) loop(ctx context.Context) error {
	d := time.Duration(a.cfg.Interval()) * time.Second
	a.log.Printf("ai-usage-agent %s starting: device=%s server=%s interval=%s sources=%s",
		version, a.cfg.DeviceID, a.cfg.ServerURL, d, strings.Join(activeNames(a.collectors), ","))

	t := time.NewTicker(d)
	defer t.Stop()
	for {
		if err := a.tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Printf("error: %v", err)
		}
		select {
		case <-ctx.Done():
			a.log.Printf("stopping")
			return nil
		case <-t.C:
		}
	}
}

func activeNames(cs []active) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, string(c.Source))
	}
	return out
}
