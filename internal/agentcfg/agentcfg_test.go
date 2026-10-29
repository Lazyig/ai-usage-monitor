package agentcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ai-usage-monitor/internal/event"
)

// setHome points every home-derived path at a temp dir so no test can read or
// write the real user's configuration.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv(EnvStateDir, "")
	t.Setenv(EnvServer, "")
	t.Setenv(EnvToken, "")
	return home
}

func TestStateDirPerOS(t *testing.T) {
	home := setHome(t)

	var want string
	switch runtime.GOOS {
	case "windows":
		// The LocalSystem service's state is machine wide, never in a profile.
		want = filepath.Join(os.Getenv("ProgramData"), AppName)
	case "darwin":
		want = filepath.Join(home, "Library", "Application Support", AppName)
	default:
		want = filepath.Join(home, ".config", AppName)
	}
	if got := StateDir(); got != want {
		t.Errorf("StateDir() = %q, want %q", got, want)
	}
}

func TestStateDirOverrides(t *testing.T) {
	home := setHome(t)

	t.Run("env wins over the OS default", func(t *testing.T) {
		t.Setenv(EnvStateDir, filepath.Join(home, "custom"))
		if got, want := StateDir(), filepath.Join(home, "custom"); got != want {
			t.Errorf("StateDir() = %q, want %q", got, want)
		}
	})

	if runtime.GOOS == "linux" {
		t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
			if got, want := StateDir(), filepath.Join(home, "xdg", AppName); got != want {
				t.Errorf("StateDir() = %q, want %q", got, want)
			}
		})
	}
	if runtime.GOOS == "windows" {
		t.Run("ProgramData, not APPDATA", func(t *testing.T) {
			t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
			t.Setenv("ProgramData", filepath.Join(home, "ProgramData"))
			if got, want := StateDir(), filepath.Join(home, "ProgramData", AppName); got != want {
				t.Errorf("StateDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestDefaultRoots(t *testing.T) {
	home := setHome(t)

	// The default source directory layout: identical relative layout on every OS, only the
	// home directory differs ($HOME vs %USERPROFILE%).
	cases := []struct {
		source event.Source
		want   string
	}{
		{event.SourceClaudeCode, filepath.Join(home, ".claude", "projects")},
		{event.SourceCodex, filepath.Join(home, ".codex", "sessions")},
		{event.SourceAntigravity, filepath.Join(home, ".gemini", "antigravity-cli")},
	}
	for _, tc := range cases {
		t.Run(string(tc.source), func(t *testing.T) {
			if got := DefaultRoot(tc.source); got != tc.want {
				t.Errorf("DefaultRoot(%s) = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
	if got := DefaultRoot(event.Source("nope")); got != "" {
		t.Errorf("DefaultRoot(unknown) = %q, want empty", got)
	}
}

func TestDefaultsCoversEverySource(t *testing.T) {
	setHome(t)
	cfg := Defaults()

	if cfg.IntervalSeconds != DefaultIntervalSeconds {
		t.Errorf("interval = %d, want %d", cfg.IntervalSeconds, DefaultIntervalSeconds)
	}
	if cfg.DeviceName == "" {
		t.Error("device name is empty")
	}
	if len(cfg.DeviceID) != 36 || strings.Count(cfg.DeviceID, "-") != 4 {
		t.Errorf("device id %q is not a UUID", cfg.DeviceID)
	}
	if cfg.DeviceID[14] != '4' {
		t.Errorf("device id %q is not version 4", cfg.DeviceID)
	}
	if a, b := Defaults().DeviceID, Defaults().DeviceID; a == b {
		t.Error("Defaults() returned the same device id twice")
	}
	for _, s := range Sources {
		sc, ok := cfg.Sources[s]
		if !ok {
			t.Fatalf("source %s missing from defaults", s)
		}
		if !sc.Enabled {
			t.Errorf("source %s should default to enabled", s)
		}
		if sc.Root != DefaultRoot(s) {
			t.Errorf("source %s root = %q, want %q", s, sc.Root, DefaultRoot(s))
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	setHome(t)
	dir := filepath.Join(t.TempDir(), "state") // not yet created: Save must mkdir

	want := Defaults()
	want.ServerURL = "https://usage.example.com"
	want.DeviceToken = "aum_secret"
	want.DeviceName = "laptop"
	want.IntervalSeconds = 120
	want.Source(event.SourceCodex).Enabled = false
	want.Source(event.SourceClaudeCode).Root = filepath.Join(dir, "logs")

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, existed, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !existed {
		t.Error("Load reported no config file after Save")
	}
	if got.ServerURL != want.ServerURL || got.DeviceToken != want.DeviceToken ||
		got.DeviceID != want.DeviceID || got.DeviceName != want.DeviceName ||
		got.IntervalSeconds != want.IntervalSeconds {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got.Redacted(), want.Redacted())
	}
	if got.Source(event.SourceCodex).Enabled {
		t.Error("codex should have stayed disabled")
	}
	if got, want := got.Source(event.SourceClaudeCode).Root, filepath.Join(dir, "logs"); got != want {
		t.Errorf("claude_code root = %q, want %q", got, want)
	}
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	setHome(t)
	dir := filepath.Join(t.TempDir(), "absent")

	cfg, existed, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if existed {
		t.Error("existed = true for a missing config")
	}
	if cfg.Source(event.SourceClaudeCode).Root != DefaultRoot(event.SourceClaudeCode) {
		t.Error("missing config did not fall back to the default root")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("Load created the state directory; it must stay read-only")
	}
}

func TestLoadFillsGapsFromAnOlderConfig(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	// A config written before antigravity existed, with no device id.
	raw := `{"server_url":"https://s","device_token":"t","sources":{"claude_code":{"enabled":true}}}`
	if err := os.WriteFile(ConfigPath(dir), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, existed, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !existed {
		t.Error("existed = false for a present config")
	}
	if cfg.DeviceID == "" {
		t.Error("device id was not generated")
	}
	if cfg.IntervalSeconds != DefaultIntervalSeconds {
		t.Errorf("interval = %d, want the default", cfg.IntervalSeconds)
	}
	if got := cfg.Source(event.SourceClaudeCode).Root; got != DefaultRoot(event.SourceClaudeCode) {
		t.Errorf("empty root was not defaulted: %q", got)
	}
	if _, ok := cfg.Sources[event.SourceAntigravity]; !ok {
		t.Error("a source added by a newer version was not backfilled")
	}
}

func TestLoadOrCreatePersistsGeneratedDeviceID(t *testing.T) {
	setHome(t)
	dir := t.TempDir()

	first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate (second): %v", err)
	}
	if first.DeviceID != second.DeviceID {
		t.Errorf("device id changed between runs: %q then %q", first.DeviceID, second.DeviceID)
	}
	if _, err := os.Stat(ConfigPath(dir)); err != nil {
		t.Errorf("config was not written: %v", err)
	}
}

func TestLoadOrCreateDoesNotPersistEnvOverrides(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	t.Setenv(EnvServer, "https://from-env")
	t.Setenv(EnvToken, "env-token")

	cfg, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.ServerURL != "https://from-env" || cfg.DeviceToken != "env-token" {
		t.Errorf("env overrides were not applied: %+v", cfg.Redacted())
	}

	raw, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.ServerURL != "" || onDisk.DeviceToken != "" {
		t.Errorf("environment secrets were written to disk: %s", raw)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	cfg := Defaults()
	cfg.ServerURL = "https://from-file"
	cfg.DeviceToken = "file-token"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	t.Setenv(EnvServer, "https://from-env")
	t.Setenv(EnvToken, "env-token")
	got, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerURL != "https://from-env" {
		t.Errorf("server = %q, want the env value", got.ServerURL)
	}
	if got.DeviceToken != "env-token" {
		t.Error("token was not overridden by the environment")
	}
}

func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits do not apply on windows")
	}
	setHome(t)
	dir := filepath.Join(t.TempDir(), "state")
	if err := Save(dir, Defaults()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(ConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.json mode = %04o, want 0600 (it holds the device token)", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("state dir mode = %04o, want 0700", perm)
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	for range 3 {
		if err := Save(dir, Defaults()); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("state dir holds %v, want just config.json", names)
	}
}

func TestRedactedHidesTheToken(t *testing.T) {
	setHome(t)
	cfg := Defaults()
	cfg.DeviceToken = "aum_supersecret"
	red := cfg.Redacted()

	if strings.Contains(red.DeviceToken, "supersecret") {
		t.Errorf("Redacted kept the token: %q", red.DeviceToken)
	}
	if cfg.DeviceToken != "aum_supersecret" {
		t.Error("Redacted mutated the original config")
	}
	// A redacted copy must not share source pointers with the original.
	red.Source(event.SourceCodex).Enabled = false
	if !cfg.Source(event.SourceCodex).Enabled {
		t.Error("Redacted shares SourceConfig pointers with the original")
	}
	blob, err := json.Marshal(red)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "supersecret") {
		t.Errorf("serialised redacted config leaked the token: %s", blob)
	}
}

func TestPaths(t *testing.T) {
	dir := filepath.Join("x", "y")
	if got, want := ConfigPath(dir), filepath.Join(dir, "config.json"); got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
	if got, want := CursorPath(dir), filepath.Join(dir, "cursors.json"); got != want {
		t.Errorf("CursorPath = %q, want %q", got, want)
	}
	if got, want := QueueDir(dir), filepath.Join(dir, "queue"); got != want {
		t.Errorf("QueueDir = %q, want %q", got, want)
	}
}

func TestInterval(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 60}, {-5, 60}, {30, 30}} {
		if got := (Config{IntervalSeconds: tc.in}).Interval(); got != tc.want {
			t.Errorf("Interval(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestDefaultsStartAtTheCurrentIDScheme(t *testing.T) {
	setHome(t)
	// A brand new device has no history to re-send, so it is already current.
	if got := Defaults().IDScheme; got != event.IDScheme {
		t.Errorf("Defaults().IDScheme = %d, want %d", got, event.IDScheme)
	}
}

// A config written before the field existed must read back as 0, so the agent
// notices it is behind and rescans once. fillDefaults must not "helpfully" set
// it to the current scheme.
func TestOlderConfigReportsNoIDScheme(t *testing.T) {
	setHome(t)
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"server_url":"https://example.test","device_token":"aum_x","device_id":"dev-1","interval_seconds":60}`
	if err := os.WriteFile(ConfigPath(dir), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, existed, err := Load(dir)
	if err != nil || !existed {
		t.Fatalf("Load = %v, existed=%v", err, existed)
	}
	if cfg.IDScheme != 0 {
		t.Fatalf("IDScheme = %d for a config that predates the field, want 0", cfg.IDScheme)
	}
	// LoadOrCreate must not quietly bump it either; only the agent's migration does.
	if cfg, err := LoadOrCreate(dir); err != nil || cfg.IDScheme != 0 {
		t.Fatalf("LoadOrCreate IDScheme = %d (err %v), want 0", cfg.IDScheme, err)
	}
}

func TestIDSchemeRoundTrips(t *testing.T) {
	setHome(t)
	dir := filepath.Join(t.TempDir(), "state")
	cfg := Defaults()
	cfg.IDScheme = 7
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["id_scheme"] != float64(7) {
		t.Errorf("id_scheme on disk = %v, want 7", onDisk["id_scheme"])
	}
	got, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.IDScheme != 7 {
		t.Errorf("IDScheme after a round trip = %d, want 7", got.IDScheme)
	}
}
