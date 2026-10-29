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

// ConfigVersion is the layout version of config.json, stored as
// config_version. A file from an older layout is migrated on load (see
// fillDefaults):
//
//	0  before the field existed
//	1  Claude Code reads extra config directories: an empty claude_code
//	   extra_roots, which was the only value older agents ever wrote, gets the
//	   defaults (DefaultExtraRoots)
//	2  plain http:// servers need allow_insecure_http (loopback excepted). A
//	   config already pointing at one keeps working: the flag is set for it
const ConfigVersion = 2

// Environment overrides. Command line flags take precedence over these.
const (
	EnvServer   = "AI_USAGE_SERVER"
	EnvToken    = "AI_USAGE_TOKEN"
	EnvStateDir = "AI_USAGE_STATE_DIR"
	// EnvAllowHTTP=1 permits a plain http:// server for this process only
	// (an ephemeral run against a test server); install --allow-http stores it.
	EnvAllowHTTP = "AI_USAGE_ALLOW_HTTP"
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
//
// ExtraRoots are further directories with Root's layout that the same
// collector also reads: Claude Code's other config directories
// (CLAUDE_CONFIG_DIR), the Antigravity desktop app's conversations beside the
// CLI's. Codex reads Root only. nil means "the defaults" and is filled in on
// load; an explicit empty list turns them off. Root and ExtraRoots may be glob
// patterns, expanded on every pass, so a folder created later is picked up.
type SourceConfig struct {
	Enabled    bool     `json:"enabled"`
	Root       string   `json:"root"`
	ExtraRoots []string `json:"extra_roots"`
}

// Roots is Root followed by ExtraRoots.
func (sc SourceConfig) Roots() []string {
	return append([]string{sc.Root}, sc.ExtraRoots...)
}

// Config is config.json.
type Config struct {
	ServerURL       string `json:"server_url"`
	DeviceToken     string `json:"device_token"`
	DeviceID        string `json:"device_id"`
	DeviceName      string `json:"device_name"`
	IntervalSeconds int    `json:"interval_seconds"`
	// AllowHTTP permits a plain http:// server that is not on this machine:
	// the device token and the installed binary would cross the network in
	// the clear. See CheckServerURL.
	AllowHTTP bool                           `json:"allow_insecure_http,omitempty"`
	Sources   map[event.Source]*SourceConfig `json:"sources"`

	// Version is the ConfigVersion this file was migrated to.
	Version int `json:"config_version"`

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
		sc = defaultSource(s)
		c.Sources[s] = sc
	}
	return sc
}

// defaultSource is an enabled source at its default roots.
func defaultSource(s event.Source) *SourceConfig {
	return &SourceConfig{Enabled: true, Root: DefaultRoot(s), ExtraRoots: DefaultExtraRoots(s)}
}

// homeDir is the user's home directory. On Windows os.UserHomeDir already
// resolves %USERPROFILE%, which is what the default source directory layout uses.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "."
}

// DefaultRoot is the per-source log directory from the default source directory layout.
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

// DefaultExtraRoots are the directories read in addition to DefaultRoot:
//
//   - Claude Code: $XDG_CONFIG_HOME/claude/projects (~/.config/claude/projects),
//     where some Claude Code versions keep their config, and the pattern
//     ~/.claude-*/projects, the usual names for extra CLAUDE_CONFIG_DIRs.
//   - Antigravity: the desktop app writes the same conversation databases as
//     the CLI, under ~/.gemini/antigravity.
//
// Never nil, so a filled-in config is told apart from one written before the
// field existed.
func DefaultExtraRoots(s event.Source) []string {
	switch s {
	case event.SourceClaudeCode:
		return claudeExtraRoots(homeDir(), os.Getenv("XDG_CONFIG_HOME"))
	case event.SourceAntigravity:
		return []string{filepath.Join(homeDir(), ".gemini", "antigravity")}
	}
	return []string{}
}

// claudeExtraRoots are Claude Code's default extra roots below home.
func claudeExtraRoots(home, xdgConfig string) []string {
	cfgHome := filepath.Join(home, ".config")
	if strings.TrimSpace(xdgConfig) != "" {
		cfgHome = xdgConfig
	}
	return []string{
		filepath.Join(cfgHome, "claude", "projects"),
		filepath.Join(home, ".claude-*", "projects"),
	}
}

// StateDir is where config.json, cursors.json and queue/ live:
//
//	linux    $XDG_CONFIG_HOME/ai-usage-agent, else ~/.config/ai-usage-agent
//	darwin   ~/Library/Application Support/ai-usage-agent
//	windows  %ProgramData%\ai-usage-agent (SYSTEM and Administrators only)
//
// AI_USAGE_STATE_DIR overrides all of it. On Windows the service runs as
// LocalSystem, so its state must not live in a folder the user can write:
// agents before that used %APPDATA% (see LegacyWindowsStateDir).
func StateDir() string {
	if d := strings.TrimSpace(os.Getenv(EnvStateDir)); d != "" {
		return d
	}
	switch runtime.GOOS {
	case "windows":
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, AppName)
		}
		return filepath.Join(`C:\ProgramData`, AppName)
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
		Version:         ConfigVersion,
		IDScheme:        event.IDScheme,
	}
	for _, s := range Sources {
		cfg.Sources[s] = defaultSource(s)
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

// LoadFile returns the config exactly as stored on disk, completed with
// defaults, plus whether the file existed. No environment overrides are applied
// so the result is safe to write back.
func LoadFile(dir string) (Config, bool, error) {
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
	cfg, existed, err := LoadFile(dir)
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
	stored, existed, err := LoadFile(dir)
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
			c.Sources[s] = defaultSource(s)
			continue
		}
		if sc.Root == "" {
			sc.Root = DefaultRoot(s)
		}
		if sc.ExtraRoots == nil {
			sc.ExtraRoots = DefaultExtraRoots(s)
		}
	}
	if c.Version < 1 {
		// Older agents always wrote [] here, so it says nothing about what the
		// user wants. The defaults are taken relative to the configured root:
		// a system service reads a config written by the installing user, and
		// its own home is not where that user's Claude Code lives.
		if sc := c.Sources[event.SourceClaudeCode]; len(sc.ExtraRoots) == 0 {
			if home, ok := claudeHome(sc.Root); ok && home != homeDir() {
				sc.ExtraRoots = claudeExtraRoots(home, "")
			} else {
				sc.ExtraRoots = DefaultExtraRoots(event.SourceClaudeCode)
			}
		}
	}
	if c.Version < 2 && CheckServerURL(c.ServerURL, false) != nil && CheckServerURL(c.ServerURL, true) == nil {
		// Installed before http was refused: keep delivering rather than
		// stop on upgrade. `status` keeps pointing it out.
		c.AllowHTTP = true
	}
	if c.Version < ConfigVersion {
		c.Version = ConfigVersion
	}
}

// claudeHome is the home directory a default Claude Code root (<home>/.claude/projects)
// sits in.
func claudeHome(root string) (string, bool) {
	root = filepath.Clean(root)
	parent := filepath.Dir(root)
	if filepath.Base(root) != "projects" || filepath.Base(parent) != ".claude" {
		return "", false
	}
	return filepath.Dir(parent), true
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
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvAllowHTTP))) {
	case "1", "true", "yes", "on":
		c.AllowHTTP = true
	}
}

// Save writes config.json atomically with 0600 permissions, creating the state
// directory (0700) if needed. The file holds the device token, so it must never
// be group or world readable.
func Save(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("agentcfg: create %s: %w", dir, err)
	}
	if err := SecureStateDir(dir); err != nil {
		return err
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
