// Package agentcfg holds the collection agent's on-disk configuration and the
// per-OS locations of the state directory and of each tool's log root.
//
// Layout of the state directory (see StateDir):
//
//	config.json    this Config, written 0600 (it holds the device token)
//	cursors.json   internal/cursor state, one entry per source
//	queue/         internal/transport offline batches
package agentcfg

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ai-usage-monitor/internal/event"
)

// AppName is the directory name used under every OS config location.
const AppName = "ai-usage-agent"

// DefaultIntervalSeconds is the scan/push period used when none is configured.
const DefaultIntervalSeconds = 60

// Environment overrides. Command line flags take precedence over these.
const (
	EnvServer   = "AI_USAGE_SERVER"
	EnvToken    = "AI_USAGE_TOKEN"
	EnvStateDir = "AI_USAGE_STATE_DIR"
)

// Sources lists every known collector source in display order. A source that
// has no collector compiled in yet is simply skipped by the agent.
var Sources = []event.Source{
	event.SourceClaudeCode,
	event.SourceCodex,
	event.SourceAntigravity,
}

// SourceConfig is the per-tool configuration. Root is the directory the
// matching collector reads; a missing root is not an error, it just means the
// tool is not installed on this device.
type SourceConfig struct {
	Enabled bool   `json:"enabled"`
	Root    string `json:"root"`
}

// Config is config.json.
type Config struct {
	ServerURL       string                         `json:"server_url"`
	DeviceToken     string                         `json:"device_token"`
	DeviceID        string                         `json:"device_id"`
	DeviceName      string                         `json:"device_name"`
	IntervalSeconds int                            `json:"interval_seconds"`
	Sources         map[event.Source]*SourceConfig `json:"sources"`

	// IDScheme is the event.IDScheme the stored cursors were collected under.
	// It is deliberately NOT filled in by fillDefaults: a config written before
	// the field existed must read back as 0 so the agent notices it is behind
	// and rescans once. Only Defaults() (a brand new device, which has no
	// history to re-send) starts at the current scheme.
	IDScheme int `json:"id_scheme"`
}

// Redacted returns a copy safe to print or log: the device token is replaced by
// a fixed placeholder. The real token must never reach stdout or a log line.
func (c Config) Redacted() Config {
	out := c
	if out.DeviceToken != "" {
		out.DeviceToken = "***redacted***"
	}
	out.Sources = make(map[event.Source]*SourceConfig, len(c.Sources))
	for k, v := range c.Sources {
		cp := *v
		out.Sources[k] = &cp
	}
	return out
}

// Interval returns the configured period, clamped to something sane.
func (c Config) Interval() int {
	if c.IntervalSeconds <= 0 {
		return DefaultIntervalSeconds
	}
	return c.IntervalSeconds
}

// Source returns the configuration for s, creating a defaulted entry if the
// config file predates that source.
func (c *Config) Source(s event.Source) *SourceConfig {
	if c.Sources == nil {
		c.Sources = map[event.Source]*SourceConfig{}
	}
	sc, ok := c.Sources[s]
	if !ok || sc == nil {
		sc = &SourceConfig{Enabled: true, Root: DefaultRoot(s)}
		c.Sources[s] = sc
	}
	return sc
}

// homeDir is the user's home directory. On Windows os.UserHomeDir already
// resolves %USERPROFILE%, which is what the PLAN.md path table uses.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "."
}

// DefaultRoot is the per-source log directory from the PLAN.md path table.
// The relative layout is identical on every OS; only the home directory differs
// ($HOME on linux/macOS, %USERPROFILE% on Windows).
func DefaultRoot(s event.Source) string {
	home := homeDir()
	switch s {
	case event.SourceClaudeCode:
		return filepath.Join(home, ".claude", "projects")
	case event.SourceCodex:
		return filepath.Join(home, ".codex", "sessions")
	case event.SourceAntigravity:
		return filepath.Join(home, ".gemini", "antigravity-cli")
	}
	return ""
}

// StateDir is where config.json, cursors.json and queue/ live:
//
//	linux    $XDG_CONFIG_HOME/ai-usage-agent, else ~/.config/ai-usage-agent
//	darwin   ~/Library/Application Support/ai-usage-agent
//	windows  %APPDATA%\ai-usage-agent
//
// AI_USAGE_STATE_DIR overrides all of it.
func StateDir() string {
	if d := strings.TrimSpace(os.Getenv(EnvStateDir)); d != "" {
		return d
	}
	switch runtime.GOOS {
	case "windows":
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, AppName)
		}
		return filepath.Join(homeDir(), "AppData", "Roaming", AppName)
	case "darwin":
		return filepath.Join(homeDir(), "Library", "Application Support", AppName)
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, AppName)
		}
		return filepath.Join(homeDir(), ".config", AppName)
	}
}

// ConfigPath, CursorPath and QueueDir name the files inside a state directory.
func ConfigPath(dir string) string { return filepath.Join(dir, "config.json") }
func CursorPath(dir string) string { return filepath.Join(dir, "cursors.json") }
func QueueDir(dir string) string   { return filepath.Join(dir, "queue") }

// Defaults builds a fresh configuration: a new device id, this machine's
// hostname as the device name and every known source enabled at its default
// root. Server URL and token stay empty; only `install` (or the environment)
// fills those in.
func Defaults() Config {
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "unknown"
	}
	cfg := Config{
		DeviceID:        NewDeviceID(),
		DeviceName:      name,
		IntervalSeconds: DefaultIntervalSeconds,
		Sources:         map[event.Source]*SourceConfig{},
		IDScheme:        event.IDScheme,
	}
	for _, s := range Sources {
		cfg.Sources[s] = &SourceConfig{Enabled: true, Root: DefaultRoot(s)}
	}
	return cfg
}

// NewDeviceID returns a random UUIDv4. It is generated once and then persisted
// in config.json, because the server keys every event by it.
func NewDeviceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; a time based fallback would be
		// worse than a loud, obviously broken id.
		panic(fmt.Sprintf("agentcfg: read random: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// loadFile returns the config exactly as stored on disk, completed with
// defaults, plus whether the file existed. No environment overrides are applied
// so the result is safe to write back.
func loadFile(dir string) (Config, bool, error) {
	raw, err := os.ReadFile(ConfigPath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("agentcfg: read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, true, fmt.Errorf("agentcfg: parse %s: %w", ConfigPath(dir), err)
	}
	cfg.fillDefaults()
	return cfg, true, nil
}

// Load reads dir/config.json, fills every unset field with its default and
// layers the environment overrides on top. It reports whether a config file was
// actually present, so callers can tell a fresh device from a configured one.
// A missing file is not an error: `scan` must work before anything is installed.
func Load(dir string) (Config, bool, error) {
	cfg, existed, err := loadFile(dir)
	if err != nil {
		return cfg, existed, err
	}
	cfg.applyEnv()
	return cfg, existed, nil
}

// LoadOrCreate is Load plus persistence of anything that had to be generated:
// the device id on first run, or a source added by a newer agent version.
// Values coming from the environment are never written back to disk.
func LoadOrCreate(dir string) (Config, error) {
	stored, existed, err := loadFile(dir)
	if err != nil {
		return stored, err
	}
	changed := !existed
	if existed {
		raw, _ := os.ReadFile(ConfigPath(dir))
		var onDisk Config
		if err := json.Unmarshal(raw, &onDisk); err == nil {
			before, _ := json.Marshal(onDisk)
			after, _ := json.Marshal(stored)
			changed = string(before) != string(after)
		}
	}
	if changed {
		if err := Save(dir, stored); err != nil {
			return stored, err
		}
	}
	cfg := stored
	cfg.applyEnv()
	return cfg, nil
}

// fillDefaults completes a config parsed from disk.
func (c *Config) fillDefaults() {
	if c.DeviceID == "" {
		c.DeviceID = NewDeviceID()
	}
	if c.DeviceName == "" {
		if n, err := os.Hostname(); err == nil && n != "" {
			c.DeviceName = n
		} else {
			c.DeviceName = "unknown"
		}
	}
	if c.IntervalSeconds <= 0 {
		c.IntervalSeconds = DefaultIntervalSeconds
	}
	if c.Sources == nil {
		c.Sources = map[event.Source]*SourceConfig{}
	}
	for _, s := range Sources {
		sc, ok := c.Sources[s]
		if !ok || sc == nil {
			c.Sources[s] = &SourceConfig{Enabled: true, Root: DefaultRoot(s)}
			continue
		}
		if sc.Root == "" {
			sc.Root = DefaultRoot(s)
		}
	}
}

// applyEnv layers AI_USAGE_SERVER / AI_USAGE_TOKEN over the file. Flags are
// applied by the caller afterwards and therefore win.
func (c *Config) applyEnv() {
	if v := strings.TrimSpace(os.Getenv(EnvServer)); v != "" {
		c.ServerURL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvToken)); v != "" {
		c.DeviceToken = v
	}
}

// Save writes config.json atomically with 0600 permissions, creating the state
// directory (0700) if needed. The file holds the device token, so it must never
// be group or world readable.
func Save(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("agentcfg: create %s: %w", dir, err)
	}
	cfg.fillDefaults()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("agentcfg: encode config: %w", err)
	}
	raw = append(raw, '\n')

	path := ConfigPath(dir)
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("agentcfg: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded

	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return fmt.Errorf("agentcfg: chmod temp: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("agentcfg: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("agentcfg: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("agentcfg: rename config: %w", err)
	}
	return os.Chmod(path, 0o600)
}
