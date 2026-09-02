package agentcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ai-usage-monitor/internal/event"
)

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
