package agentcfg

import (
	"errors"
	"os"
	"testing"
)

func TestCheckServerURL(t *testing.T) {
	for _, tc := range []struct {
		url   string
		allow bool
		ok    bool
	}{
		{"https://usage.example.com", false, true},
		{"http://localhost:8080", false, true},
		{"http://127.0.0.1:18081", false, true},
		{"http://[::1]:8080", false, true},
		{"http://usage.example.com", false, false},
		{"http://127.0.0.1.evil.example", false, false},
		{"http://192.168.1.10:8080", false, false},
		{"http://192.168.1.10:8080", true, true},
		{"ftp://usage.example.com", true, false},
		{"usage.example.com", false, false},
	} {
		err := CheckServerURL(tc.url, tc.allow)
		if (err == nil) != tc.ok {
			t.Errorf("CheckServerURL(%q, %v) = %v, want ok=%v", tc.url, tc.allow, err, tc.ok)
		}
	}
	if err := CheckServerURL("http://lan-host", false); !errors.Is(err, ErrInsecureServer) {
		t.Errorf("want ErrInsecureServer, got %v", err)
	}
}

// A config that already pointed at an http server before it was refused keeps
// working after the upgrade; a fresh one does not get the exemption.
func TestOlderHTTPConfigKeepsWorking(t *testing.T) {
	setHome(t)
	dir := t.TempDir()
	raw := `{"server_url":"http://192.168.1.10:8080","device_token":"t","config_version":1}`
	if err := os.WriteFile(ConfigPath(dir), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowHTTP || CheckServerURL(cfg.ServerURL, cfg.AllowHTTP) != nil {
		t.Errorf("an existing http install was cut off: %+v", cfg.Redacted())
	}
	if fresh := Defaults(); fresh.AllowHTTP {
		t.Error("a fresh config must not allow http")
	}
}
