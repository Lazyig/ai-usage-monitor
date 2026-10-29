package httpapi

import (
	"context"
	"net/http"
	"time"

	"ai-usage-monitor/internal/store"
)

// readiness evaluates everything a request needs: the database answers, a
// price table is loaded and the dashboard bundle is embedded. A process that is
// draining (SIGTERM received) reports not ready so traffic moves away first.
type readiness struct {
	ok     bool
	checks map[string]string // check -> "ok" or the reason it failed
	counts store.Counts
}

func (a *api) readiness(r *http.Request, withCounts bool) readiness {
	rd := readiness{ok: true, checks: map[string]string{}}
	note := func(name string, err string) {
		if err == "" {
			rd.checks[name] = "ok"
			return
		}
		rd.checks[name] = err
		rd.ok = false
	}
	pingCtx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	if err := a.store.Ping(pingCtx); err != nil {
		note("database", "unavailable: "+err.Error())
	} else {
		note("database", "")
		if withCounts {
			if c, err := a.cachedCounts(r.Context()); err == nil {
				rd.counts = c
			}
		}
	}
	if len(a.pricing.Rates()) == 0 {
		note("pricing", "no rates loaded")
	} else {
		note("pricing", "")
	}
	if !a.opt.SPABuilt {
		note("frontend", "SPA bundle not embedded")
	} else {
		note("frontend", "")
	}
	if a.opt.Draining != nil && a.opt.Draining.Load() {
		note("draining", "shutting down")
	}
	return rd
}

// handleLive answers as long as the process serves HTTP; k8s restarts the pod
// when this fails, which is only right for a hung process, not a bad dependency.
func (a *api) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": a.opt.Version})
}

// handleReady gates traffic: 503 until database, pricing and frontend are all
// good, and again once the process starts draining.
func (a *api) handleReady(w http.ResponseWriter, r *http.Request) {
	rd := a.readiness(r, false)
	status := http.StatusOK
	if !rd.ok {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ok": rd.ok, "version": a.opt.Version, "checks": rd.checks})
}

// countsTTL is how long /health reuses its row counts. /health needs no
// credentials, and COUNT(*) over millions of events would let anyone keep the
// small read pool busy.
const countsTTL = 30 * time.Second

func (a *api) cachedCounts(ctx context.Context) (store.Counts, error) {
	a.mu.Lock()
	if !a.countsAt.IsZero() && time.Since(a.countsAt) < countsTTL {
		c := a.counts
		a.mu.Unlock()
		return c, nil
	}
	a.mu.Unlock()
	c, err := a.store.Counts(ctx)
	if err != nil {
		return c, err
	}
	a.mu.Lock()
	a.counts, a.countsAt = c, time.Now()
	a.mu.Unlock()
	return c, nil
}

// handleHealth is the human-facing summary: readiness plus row counts.
func (a *api) handleHealth(w http.ResponseWriter, r *http.Request) {
	rd := a.readiness(r, true)
	status := http.StatusOK
	if !rd.ok {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":      rd.ok,
		"version": a.opt.Version,
		"events":  rd.counts.Events,
		"devices": rd.counts.Devices,
		"checks":  rd.checks,
	})
}
