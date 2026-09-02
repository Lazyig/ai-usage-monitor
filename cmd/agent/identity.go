package main

import (
	"context"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/cursor"
	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/transport"
)

// syncDeviceID replaces the locally generated device id with the one the server
// bound to our token, and persists it. Best effort: when the server cannot be
// reached the local id is kept (the server remaps it on ingest anyway).
func syncDeviceID(dir string, cfg *agentcfg.Config, logf func(format string, args ...any)) {
	if cfg.ServerURL == "" || cfg.DeviceToken == "" {
		return
	}
	c := &transport.Client{
		BaseURL:    cfg.ServerURL,
		Token:      cfg.DeviceToken,
		DeviceInfo: transport.Device{ID: cfg.DeviceID, Name: cfg.DeviceName, AgentVersion: version},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	who, err := c.WhoAmI(ctx)
	if err != nil {
		logf("whoami: %v (keeping local device id %s)", err, cfg.DeviceID)
		return
	}
	if who.DeviceID == "" || who.DeviceID == cfg.DeviceID {
		return
	}
	logf("device id: adopting server-issued %s (was local %s)", who.DeviceID, cfg.DeviceID)
	cfg.DeviceID = who.DeviceID
	if err := agentcfg.Save(dir, *cfg); err != nil {
		logf("save config: %v", err)
	}
}

// migrateIDScheme makes an upgraded agent rescan its logs once after the EventID
// derivation changed. The events it already delivered are stored under the old
// ids, so the server cannot recognise the new ones as duplicates; re-sending
// everything is what gets this device's history onto the new scheme. The cursors
// are cleared before the new scheme is recorded, so a failure here means the
// migration is simply retried on the next start rather than silently skipped.
func migrateIDScheme(dir string, cfg *agentcfg.Config, logf func(format string, args ...any)) {
	if cfg.IDScheme >= event.IDScheme {
		return
	}
	logf("event id scheme %d -> %d: clearing cursors so this device's history is re-sent with the new ids",
		cfg.IDScheme, event.IDScheme)
	if err := cursor.Clear(agentcfg.CursorPath(dir)); err != nil {
		logf("warn: %v (will retry on the next start)", err)
		return
	}
	cfg.IDScheme = event.IDScheme
	if err := agentcfg.Save(dir, *cfg); err != nil {
		logf("save config: %v", err)
	}
}
