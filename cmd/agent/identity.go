package main

import (
	"context"
	"time"

	"ai-usage-monitor/internal/agentcfg"
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
