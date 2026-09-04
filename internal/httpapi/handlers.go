package httpapi

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/store"
)

// --- ingest ----------------------------------------------------------------

type ingestRequest struct {
	Device store.DeviceMeta   `json:"device"`
	Events []event.UsageEvent `json:"events"`
}

type ingestResponse struct {
	Accepted   int    `json:"accepted"`
	Duplicates int    `json:"duplicates"`
	Rejected   int    `json:"rejected"`
	DeviceID   string `json:"device_id"`        // the token's device; agents adopt it
	Resync     bool   `json:"resync,omitempty"` // an operator purged this device: rescan from zero
}

func (a *api) handleIngest(w http.ResponseWriter, r *http.Request) {
	dev, _ := deviceFrom(r.Context())

	// Agents gzip the batch. The decompressed stream is capped as well so a
	// small compressed body cannot expand without bound.
	var body io.Reader = r.Body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid gzip body")
			return
		}
		defer zr.Close()
		body = io.LimitReader(zr, MaxBodyBytes*8)
	}

	var req ingestRequest
	dec := json.NewDecoder(body)
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	// The token is the identity. Whatever id the agent put in device.id is
	// informational: everything is stored under the token's device, and the
	// response tells the agent which id to use from now on.
	req.Device.ID = dev.ID
	if len(req.Events) > MaxBatchEvents {
		writeError(w, http.StatusBadRequest, "batch too large: at most 5000 events")
		return
	}

	// Drop anything a collector should never have produced; the count comes
	// back so the agent can notice a broken decoder.
	good := make([]event.UsageEvent, 0, len(req.Events))
	rejected := 0
	for _, e := range req.Events {
		e.DeviceID = dev.ID
		if !e.Valid() {
			rejected++
			continue
		}
		e.Timestamp = e.Timestamp.UTC()
		good = append(good, e)
	}

	accepted, duplicates, err := a.store.InsertEvents(r.Context(), good)
	if err != nil {
		a.log.Error("insert events", "device", dev.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not store events")
		return
	}
	if err := a.store.TouchDevice(r.Context(), req.Device, true, time.Now()); err != nil {
		a.log.Error("touch device", "device", dev.ID, "err", err)
	}
	a.forgetDevice(dev.ID) // metadata changed; next lookup re-reads it

	// Consumed here, after the batch is stored, so exactly one answer carries
	// it. Heartbeats (empty batches) count: an idle device must hear about a
	// purge too, and it is the only request it makes.
	resync, err := a.store.ConsumeResync(r.Context(), dev.ID)
	if err != nil {
		// The batch is already stored; failing the request would only make the
		// agent resend it. The flag stays set and rides the next response.
		a.log.Error("consume resync", "device", dev.ID, "err", err)
	}

	writeJSON(w, http.StatusOK, ingestResponse{
		Accepted: accepted, Duplicates: duplicates, Rejected: rejected, DeviceID: dev.ID, Resync: resync,
	})
}

// handleWhoAmI lets an agent learn the device id its token is bound to, so the
// ids it bakes into EventIDs match the server from the very first batch.
func (a *api) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	dev, _ := deviceFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"device_id": dev.ID, "name": dev.Name})
}

// --- query parameters ------------------------------------------------------

type params struct {
	filter store.Filter
	loc    *time.Location
}

// parseParams reads from/to/tz plus the dimension filters. The range is
// [from, to); `to` is exclusive. Dates without a time are read in tz.
func parseParams(r *http.Request) (params, error) {
	q := r.URL.Query()
	tz := q.Get("tz")
	if tz == "" {
		tz = DefaultTZ
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return params{}, errors.New("unknown tz " + tz)
	}
	p := params{loc: loc}

	to := time.Now().UTC()
	if v := q.Get("to"); v != "" {
		to, err = parseTime(v, loc)
		if err != nil {
			return params{}, errors.New("bad to: " + v)
		}
	}
	from := startOfDay(to.In(loc)).AddDate(0, 0, -29).UTC()
	if v := q.Get("from"); v != "" {
		from, err = parseTime(v, loc)
		if err != nil {
			return params{}, errors.New("bad from: " + v)
		}
	}
	if !from.Before(to) {
		return params{}, errors.New("from must be before to")
	}

	p.filter = store.Filter{
		From:    from,
		To:      to,
		Source:  q.Get("source"),
		Device:  q.Get("device"),
		Model:   q.Get("model"),
		Project: q.Get("project"),
		Session: q.Get("session"),
	}
	return p, nil
}

func parseTime(v string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, errors.New("not RFC3339 or YYYY-MM-DD")
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// query parses the range and dimension filters every query endpoint shares. It
// answers 400 itself, so a false second result means the request is done.
func query(w http.ResponseWriter, r *http.Request) (params, bool) {
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return params{}, false
	}
	return p, true
}

// groupBy reads group_by, defaulting to def, and checks it against the columns
// the store will group on - user input never reaches the SQL text. want is the
// endpoint's own wording of the allowed values for the 400 it may answer.
func groupBy(w http.ResponseWriter, r *http.Request, def, want string) (string, bool) {
	g := r.URL.Query().Get("group_by")
	if g == "" {
		g = def
	}
	if _, ok := store.GroupColumn(g); !ok {
		writeError(w, http.StatusBadRequest, "group_by must be "+want)
		return "", false
	}
	return g, true
}

// --- queries ---------------------------------------------------------------

func (a *api) handleSummary(w http.ResponseWriter, r *http.Request) {
	p, ok := query(w, r)
	if !ok {
		return
	}
	sum, err := a.store.Summary(r.Context(), p.filter, a.pricing.Table())
	if err != nil {
		a.fail(w, "summary", err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (a *api) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	p, ok := query(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = "day"
	}
	if bucket != "day" && bucket != "hour" {
		writeError(w, http.StatusBadRequest, "bucket must be day or hour")
		return
	}
	group, ok := groupBy(w, r, "source", "source, device, model, project or session")
	if !ok {
		return
	}
	ts, err := a.store.Timeseries(r.Context(), p.filter, bucket, group, p.loc, a.pricing.Table())
	if err != nil {
		a.fail(w, "timeseries", err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (a *api) handleBreakdown(w http.ResponseWriter, r *http.Request) {
	p, ok := query(w, r)
	if !ok {
		return
	}
	group, ok := groupBy(w, r, "model", "model, project, device, session or source")
	if !ok {
		return
	}
	b, err := a.store.Breakdown(r.Context(), p.filter, group, intParam(r, "limit", 50), a.pricing.Table())
	if err != nil {
		a.fail(w, "breakdown", err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleThroughput answers tokens per active minute. It shares the range and
// filters of the other query endpoints; group_by defaults to source.
func (a *api) handleThroughput(w http.ResponseWriter, r *http.Request) {
	p, ok := query(w, r)
	if !ok {
		return
	}
	group, ok := groupBy(w, r, "source", "source, device, model, project or session")
	if !ok {
		return
	}
	tp, err := a.store.Throughput(r.Context(), p.filter, group, p.loc)
	if err != nil {
		a.fail(w, "throughput", err)
		return
	}
	writeJSON(w, http.StatusOK, tp)
}

func (a *api) handleEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := query(w, r)
	if !ok {
		return
	}
	page, err := a.store.Events(r.Context(), p.filter, intParam(r, "limit", 200), r.URL.Query().Get("cursor"), a.pricing.Table())
	if err != nil {
		if strings.Contains(err.Error(), "cursor") {
			writeError(w, http.StatusBadRequest, "bad cursor")
			return
		}
		a.fail(w, "events", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// --- devices ---------------------------------------------------------------

func (a *api) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devs, err := a.store.ListDevices(r.Context())
	if err != nil {
		a.fail(w, "devices", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devs})
}

func (a *api) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	dev, token, err := a.store.CreateDevice(r.Context(), req.Name)
	if err != nil {
		a.fail(w, "create device", err)
		return
	}
	writeDeviceToken(w, dev, token)
}

// writeDeviceToken answers device creation and rotation: the only two responses
// where the plaintext token exists outside the agent.
func writeDeviceToken(w http.ResponseWriter, dev store.Device, token string) {
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":               dev.ID,
		"name":             dev.Name,
		"token":            token,
		"token_created_at": dev.TokenCreatedAt,
	})
}

// handleRotateDevice issues a new token for an existing device. Prefer it over
// deleting and re-creating a device: the id and the history stay put, so the
// machine keeps one row in the dashboard.
func (a *api) handleRotateDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token, err := a.store.RotateToken(r.Context(), id)
	if err != nil {
		a.failDevice(w, "rotate token", err)
		return
	}
	// The old token must stop working now, not when its cache entry expires.
	a.forgetDevice(id)
	dev, err := a.store.Device(r.Context(), id)
	if err != nil {
		a.fail(w, "rotate token", err)
		return
	}
	writeDeviceToken(w, dev, token)
}

// handleRenameDevice changes the operator-owned display name.
func (a *api) handleRenameDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name must be 1-100 characters")
		return
	}
	if err := a.store.RenameDevice(r.Context(), id, req.Name); err != nil {
		a.failDevice(w, "rename device", err)
		return
	}
	a.forgetDevice(id) // the auth cache carries the name
	dev, err := a.store.Device(r.Context(), id)
	if err != nil {
		a.fail(w, "rename device", err)
		return
	}
	writeJSON(w, http.StatusOK, dev)
}

// handleDeleteDevice revokes the token and, with ?purge=true, erases the device
// and every event it ever sent.
func (a *api) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	purge := boolParam(r, "purge")
	deleted, err := a.store.DeleteDevice(r.Context(), id, purge)
	if err != nil {
		a.failDevice(w, "delete device", err)
		return
	}
	a.forgetDevice(id)
	if purge {
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted_events": deleted, "purged": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "revoked": true})
}

// handlePurgeDeviceEvents throws away a device's data but keeps the device and
// its token, and flags it so the next ingest tells the agent to rescan from
// zero. That is the way to re-sync a machine whose history looks wrong.
func (a *api) handlePurgeDeviceEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleted, err := a.store.PurgeEvents(r.Context(), id)
	if err != nil {
		a.failDevice(w, "purge device events", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "deleted_events": deleted, "resync_requested": true,
	})
}

// --- pricing ---------------------------------------------------------------

func (a *api) handlePricing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"rates": rateViews(a.pricing.Rates())})
}

func (a *api) handlePricingReload(w http.ResponseWriter, r *http.Request) {
	if err := a.pricing.Reload(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.log.Info("pricing reloaded", "path", a.pricing.Path(), "rates", len(a.pricing.Rates()))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rates": len(a.pricing.Rates())})
}

// --- health ----------------------------------------------------------------

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
			if c, err := a.store.Counts(r.Context()); err == nil {
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

func (a *api) fail(w http.ResponseWriter, what string, err error) {
	a.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

// failDevice is fail for the routes addressed by device id: an unknown id is the
// caller's mistake (404), anything else is ours.
func (a *api) failDevice(w http.ResponseWriter, what string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	a.fail(w, what, err)
}
