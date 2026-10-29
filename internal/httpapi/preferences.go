package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// dashboardPrefs is GET/PUT /api/v1/preferences: what the dashboard selected
// last, kept on the server so it follows the operator to another browser.
// Every field is optional; an absent one means "use the default".
type dashboardPrefs struct {
	Range       *prefRange `json:"range,omitempty"`
	AutoRefresh *bool      `json:"auto_refresh,omitempty"`
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

var rangePresets = map[string]bool{
	"today": true, "yesterday": true, "7d": true, "30d": true,
	"thisMonth": true, "lastMonth": true, "all": true, "custom": true,
}

func (p *dashboardPrefs) validate() error {
	if p.Range == nil {
		return nil
	}
	r := p.Range
	if !rangePresets[r.Preset] {
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
	return nil
}

func (a *api) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	raw, ok, err := a.store.Preference(r.Context(), dashboardPrefsName)
	if err != nil {
		a.fail(w, "read preferences", err)
		return
	}
	var p dashboardPrefs
	// A document an older version stored but this one cannot read is ignored
	// rather than breaking the dashboard: it falls back to the defaults.
	if ok && (json.Unmarshal([]byte(raw), &p) != nil || p.validate() != nil) {
		p = dashboardPrefs{}
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *api) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPrefsBytes))
	dec.DisallowUnknownFields()
	var p dashboardPrefs
	if err := dec.Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid preferences: "+err.Error())
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
