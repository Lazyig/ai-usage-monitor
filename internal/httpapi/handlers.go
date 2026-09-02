package httpapi

import (
	"encoding/json"
	"errors"
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
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
	Rejected   int `json:"rejected"`
}

func (a *api) handleIngest(w http.ResponseWriter, r *http.Request) {
	dev, _ := deviceFrom(r.Context())

	var req ingestRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Device.ID == "" {
		writeError(w, http.StatusBadRequest, "device.id is required")
		return
	}
	if req.Device.ID != dev.ID {
		writeError(w, http.StatusForbidden, "device does not match token")
		return
	}
	if len(req.Events) > MaxBatchEvents {
		writeError(w, http.StatusBadRequest, "batch too large: at most 5000 events")
		return
	}

	// Drop anything a collector should never have produced; the count comes
	// back so the agent can notice a broken decoder.
	good := make([]event.UsageEvent, 0, len(req.Events))
	rejected := 0
	for _, e := range req.Events {
		if e.DeviceID == "" {
			e.DeviceID = dev.ID
		}
		if e.DeviceID != dev.ID || !e.Valid() {
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

	writeJSON(w, http.StatusOK, ingestResponse{Accepted: accepted, Duplicates: duplicates, Rejected: rejected})
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

// --- queries ---------------------------------------------------------------

func (a *api) handleSummary(w http.ResponseWriter, r *http.Request) {
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "source"
	}
	if _, ok := store.GroupColumn(groupBy); !ok {
		writeError(w, http.StatusBadRequest, "group_by must be source, device, model, project or session")
		return
	}
	ts, err := a.store.Timeseries(r.Context(), p.filter, bucket, groupBy, p.loc, a.pricing.Table())
	if err != nil {
		a.fail(w, "timeseries", err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (a *api) handleBreakdown(w http.ResponseWriter, r *http.Request) {
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "model"
	}
	if _, ok := store.GroupColumn(groupBy); !ok {
		writeError(w, http.StatusBadRequest, "group_by must be model, project, device, session or source")
		return
	}
	b, err := a.store.Breakdown(r.Context(), p.filter, groupBy, intParam(r, "limit", 50), a.pricing.Table())
	if err != nil {
		a.fail(w, "breakdown", err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (a *api) handleEvents(w http.ResponseWriter, r *http.Request) {
	p, err := parseParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	// The only time the plaintext token exists outside the agent.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":               dev.ID,
		"name":             dev.Name,
		"token":            token,
		"token_created_at": dev.TokenCreatedAt,
	})
}

func (a *api) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.RevokeDevice(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		a.fail(w, "revoke device", err)
		return
	}
	a.forgetDevice(id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "revoked": true})
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

func (a *api) handleHealth(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.Counts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "version": a.opt.Version, "error": "database unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": a.opt.Version,
		"events":  c.Events,
		"devices": c.Devices,
	})
}

func (a *api) fail(w http.ResponseWriter, what string, err error) {
	a.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
