package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/cursor"
	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/transport"
)

// runOverrides are the run flags that take precedence over config.json.
type runOverrides struct {
	server, token string
	interval      int
	allowHTTP     bool
}

// loadRunConfig reads config.json (creating defaults) and applies flag overrides.
func loadRunConfig(dir string, ov runOverrides) (agentcfg.Config, error) {
	cfg, err := agentcfg.LoadOrCreate(dir)
	if err != nil {
		return cfg, err
	}
	if ov.server != "" {
		cfg.ServerURL = ov.server
	}
	if ov.token != "" {
		cfg.DeviceToken = ov.token
	}
	if ov.interval > 0 {
		cfg.IntervalSeconds = ov.interval
	}
	if ov.allowHTTP {
		cfg.AllowHTTP = true
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return cfg, usagef("no server configured: run `ai-usage-agent install --server URL --token TOKEN` or set %s", agentcfg.EnvServer)
	}
	if err := agentcfg.CheckServerURL(cfg.ServerURL, cfg.AllowHTTP); err != nil {
		return cfg, &usageError{err: err}
	}
	return cfg, nil
}

// bootstrap turns a state directory into an agent ready to run: the config with
// the flag overrides applied, the device id the server knows this token by, the
// one-off EventID scheme migration, then the collectors, cursors and queue.
//
// An interactive `run` and the service both go through here, so a machine
// behaves the same whether the operator started the agent by hand or systemd
// did. It is deliberately slow work: the service calls it inside Start, after
// the service manager has already been answered.
func bootstrap(dir string, sel selected, ov runOverrides) (*agent, error) {
	cfg, err := loadRunConfig(dir, ov)
	if err != nil {
		return nil, err
	}
	syncDeviceID(dir, &cfg, log.Printf)
	migrateIDScheme(dir, &cfg, log.Printf)
	return newAgent(cfg, dir, sel)
}

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
	id := who.DeviceID
	if err := updateStored(dir, func(c *agentcfg.Config) { c.DeviceID = id }); err != nil {
		logf("save config: %v", err)
	}
}

// updateStored changes one field of config.json as stored on disk. cfg in
// memory carries the environment and flag overrides (a test server, a token
// from a secret), which must never be written back.
func updateStored(dir string, change func(*agentcfg.Config)) error {
	stored, _, err := agentcfg.LoadFile(dir)
	if err != nil {
		return err
	}
	change(&stored)
	return agentcfg.Save(dir, stored)
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
	// Queued batches carry the old ids: replayed after the rescan they would
	// be stored a second time. The rescan re-sends what they held.
	if err := clearProgress(dir); err != nil {
		logf("warn: %v (will retry on the next start)", err)
		return
	}
	cfg.IDScheme = event.IDScheme
	if err := updateStored(dir, func(c *agentcfg.Config) { c.IDScheme = event.IDScheme }); err != nil {
		logf("save config: %v", err)
	}
}

// clearProgress deletes the collector cursors and the offline queue of the
// state directory, so the next pass re-reads every log from the beginning.
func clearProgress(dir string) error {
	if err := cursor.Clear(agentcfg.CursorPath(dir)); err != nil {
		return err
	}
	if err := os.RemoveAll(agentcfg.QueueDir(dir)); err != nil {
		return fmt.Errorf("remove %s: %w", agentcfg.QueueDir(dir), err)
	}
	return nil
}
