package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-usage-monitor/internal/store"
)

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

// boolParam reads a flag query parameter: "true", "1", "yes" and "on" (any
// case) enable it, anything else - including a missing parameter - does not.
func boolParam(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

func intParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
