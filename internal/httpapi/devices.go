package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ai-usage-monitor/internal/store"
)

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

// handleResyncDevice asks a device to re-read its logs from zero without
// deleting anything. Re-sent events dedupe on event_id; copies that are more
// complete than the stored row (streamed responses that were shipped before
// they finished) refresh it.
func (a *api) handleResyncDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.store.RequestResync(r.Context(), id); err != nil {
		a.failDevice(w, "request resync", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "resync_requested": true})
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
