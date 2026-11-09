package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

// dashboardPrefs is GET/PUT /api/v1/preferences: what the dashboard selected
// last, kept on the server so it follows the operator to another browser.
// Every field is optional; an absent one means "use the default".
type dashboardPrefs struct {
	Range       *prefRange `json:"range,omitempty"`
	AutoRefresh *bool      `json:"auto_refresh,omitempty"`
	NumberUnits *string    `json:"number_units,omitempty"`
}

type prefRange struct {
	Preset string `json:"preset"`
	// From and To (YYYY-MM-DD, To exclusive) are only kept for "custom": the
	// other presets are relative to today and recomputed when loaded.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

const dashboardPrefsName = "dashboard"

// maxPrefsBytes bounds the stored document; the real one is under 100 bytes.
const maxPrefsBytes = 4 << 10

// minPrefYear and maxPrefYear bound a saved custom range.
const minPrefYear, maxPrefYear = 1970, 2200

// rangePreset matches the presets the dashboard offers: a calendar period
// counted back from today (day-0 today … day-6, week-0 … week-4, month-0 …
// month-12), a rolling window (last-7, last-30, last-90), all or custom.
var rangePreset = regexp.MustCompile(`^(day-[0-6]|week-[0-4]|month-([0-9]|1[0-2])|last-(7|30|90)|all|custom)$`)

func (p *dashboardPrefs) validate() error {
	if err := p.validateRange(); err != nil {
		return err
	}
	return p.validateNumberUnits()
}

func (p *dashboardPrefs) validateRange() error {
	if p.Range == nil {
		return nil
	}
	r := p.Range
	if !rangePreset.MatchString(r.Preset) {
		return errors.New("unknown range preset")
	}
	if r.Preset != "custom" {
		r.From, r.To = "", ""
		return nil
	}
	from, err1 := time.Parse(time.DateOnly, r.From)
	to, err2 := time.Parse(time.DateOnly, r.To)
	if err1 != nil || err2 != nil || !from.Before(to) {
		return errors.New("a custom range needs from < to as YYYY-MM-DD")
	}
	// A saved range is loaded on every device; one spanning millennia would
	// fail every chart there until someone picked another period.
	if from.Year() < minPrefYear || to.Year() > maxPrefYear {
		return fmt.Errorf("a custom range must lie between %d and %d", minPrefYear, maxPrefYear)
	}
	return nil
}

func (p *dashboardPrefs) validateNumberUnits() error {
	if p.NumberUnits != nil && *p.NumberUnits != "en" && *p.NumberUnits != "ko" {
		return errors.New("unknown number units")
	}
	return nil
}

func (a *api) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	raw, ok, err := a.store.Preference(r.Context(), dashboardPrefsName)
	if err != nil {
		a.fail(w, "read preferences", err)
		return
	}
	var p dashboardPrefs
	// A document another version stored that this one cannot read falls back
	// to the defaults rather than breaking the dashboard: all of it if it does
	// not parse, only the invalid fields otherwise.
	if ok && json.Unmarshal([]byte(raw), &p) != nil {
		p = dashboardPrefs{}
	}
	if p.validateRange() != nil {
		p.Range = nil
	}
	if p.validateNumberUnits() != nil {
		p.NumberUnits = nil
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPrefsBytes))
	dec.DisallowUnknownFields()
	var pp *dashboardPrefs
	if err := dec.Decode(&pp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid preferences: "+err.Error())
		return
	}
	if pp == nil {
		// null would otherwise save the defaults over the stored choice.
		writeError(w, http.StatusBadRequest, "invalid preferences: expected a JSON object")
		return
	}
	p := *pp
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid preferences: data after the JSON object")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid preferences: "+err.Error())
		return
	}
	raw, err := json.Marshal(p)
	if err != nil {
		a.fail(w, "encode preferences", err)
		return
	}
	if err := a.store.SetPreference(r.Context(), dashboardPrefsName, string(raw)); err != nil {
		a.fail(w, "save preferences", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
