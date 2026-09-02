package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/kardianos/service"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/cursor"
	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/transport"
)

// heartbeatEvery is how often an idle agent posts an empty batch so the server
// can refresh last_seen. A device with no activity still has to look alive.
const heartbeatEvery = 5 * time.Minute

// cmdRun collects and pushes on a timer until it is asked to stop.
func cmdRun(args []string) error {
	fs := newFlagSet("run")
	var (
		once     = fs.Bool("once", false, "do a single collect+push pass and exit")
		interval = fs.Int("interval", 0, "seconds between passes (default: the configured interval)")
		server   = fs.String("server", "", "server base URL (overrides the config and $AI_USAGE_SERVER)")
		token    = fs.String("token", "", "device token (overrides the config and $AI_USAGE_TOKEN)")
		sources  = fs.String("source", "", "comma separated sources to collect (default: all enabled)")
		stateDir = fs.String("state-dir", "", "state directory (default: $AI_USAGE_STATE_DIR or the per-OS config dir)")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: ai-usage-agent run [flags]\n\nCollect local usage and push it to the server.\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usagef("run takes no positional arguments (got %q)", fs.Arg(0))
	}

	sel, err := parseSources(*sources)
	if err != nil {
		return &usageError{err: err}
	}
	dir := resolveStateDir(*stateDir)
	ov := runOverrides{server: *server, token: *token, interval: *interval}

	// Under a service manager the process must reach svc.Run() quickly (Windows
	// gives it 30s), so every slow step happens after that, inside the service.
	if !*once && !service.Interactive() {
		return runAsService(dir, sel, ov)
	}

	cfg, err := loadRunConfig(dir, ov)
	if err != nil {
		return err
	}
	syncDeviceID(dir, &cfg, log.Printf)
	ag, err := newAgent(cfg, dir, sel)
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()
	if *once {
		return ag.tick(ctx)
	}
	return ag.loop(ctx)
}

// runOverrides are the run flags that take precedence over config.json.
type runOverrides struct {
	server, token string
	interval      int
}

// loadRunConfig reads config.json (creating defaults) and applies flag overrides.
func loadRunConfig(dir string, ov runOverrides) (agentcfg.Config, error) {
	cfg, err := agentcfg.LoadOrCreate(dir)
	if err != nil {
		return cfg, err
	}
	if ov.server != "" {
		cfg.ServerURL = ov.server
	}
	if ov.token != "" {
		cfg.DeviceToken = ov.token
	}
	if ov.interval > 0 {
		cfg.IntervalSeconds = ov.interval
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return cfg, usagef("no server configured: run `ai-usage-agent install --server URL --token TOKEN` or set %s", agentcfg.EnvServer)
	}
	return cfg, nil
}

// agent is the long running collect/push machine.
type agent struct {
	cfg        agentcfg.Config
	dir        string
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
	lg := log.New(log.Writer(), "", log.LstdFlags|log.LUTC) // follows the service log file when set

	q, err := transport.OpenQueue(agentcfg.QueueDir(dir), transport.DefaultQueueMaxBytes)
	if err != nil {
		return nil, err
	}
	q.Logf = func(format string, args ...any) { lg.Printf("warn: "+format, args...) }

	ag := &agent{
		cfg:     cfg,
		dir:     dir,
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
			HTTPClient: &http.Client{Timeout: transport.DefaultHTTPTimout},
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
	if st, err := a.client.Flush(ctx); err != nil {
		a.noteErr("queue flush", err)
	} else if st.Requests > 0 {
		a.lastContact = time.Now()
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
		if time.Since(a.lastContact) >= heartbeatEvery {
			if _, err := a.client.PushStats(ctx, nil); err != nil {
				a.noteErr("heartbeat", err)
			} else {
				a.lastContact = time.Now()
				a.log.Printf("tick: no new events, heartbeat sent")
			}
		} else {
			a.log.Printf("tick: no new events")
		}
		a.saveCursors(next) // file rotations can move a cursor without any events
		return errors.Join(scanErr...)
	}

	st, err := a.client.PushStats(ctx, events)
	switch {
	case err == nil:
		a.lastContact = time.Now()
		a.authWarned = false
		a.saveCursors(next)
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

	a.log.Printf("tick: scanned %s (%d events) | accepted=%d duplicates=%d rejected=%d | %s",
		strings.Join(counts, " "), len(events), st.Accepted, st.Duplicates, st.Rejected, a.queueSummary())
	return errors.Join(append(scanErr, ignoreHandled(err))...)
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

// signalContext cancels on SIGINT/SIGTERM so a run in a terminal, a systemd
// stop and a launchd shutdown all unwind the same way.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
