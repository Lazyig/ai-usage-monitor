package httpapi

import (
	"errors"
	"net/http"

	"ai-usage-monitor/internal/store"
)

// groupByChoices is the 400 wording for the endpoints that accept every
// group_by column.
const groupByChoices = "source, device, model, project or session"

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
	group, ok := groupBy(w, r, "source", groupByChoices)
	if !ok {
		return
	}
	ts, err := a.store.Timeseries(r.Context(), p.filter, bucket, group, p.loc, a.pricing.Table())
	if errors.Is(err, store.ErrTooManyBuckets) {
		writeError(w, http.StatusBadRequest, "range too long for this bucket")
		return
	}
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
	group, ok := groupBy(w, r, "source", groupByChoices)
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
		if errors.Is(err, store.ErrBadCursor) {
			writeError(w, http.StatusBadRequest, "bad cursor")
			return
		}
		a.fail(w, "events", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

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

func (a *api) fail(w http.ResponseWriter, what string, err error) {
	a.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
