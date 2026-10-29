package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-usage-monitor/internal/agentcfg"
	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/transport"
)

// testdataRoots are the collectors' own anonymised samples, reused here so the
// CLI is exercised against exactly the fixtures the parsers are tested with.
func testdataRoots(t *testing.T) map[event.Source]string {
	t.Helper()
	roots := map[event.Source]string{
		event.SourceClaudeCode: filepath.Join("..", "..", "internal", "collector", "claudecode", "testdata", "projects"),
		event.SourceCodex:      filepath.Join("..", "..", "internal", "collector", "codex", "testdata", "sessions"),
	}
	for src, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("%s testdata missing at %s: %v", src, abs, err)
		}
		roots[src] = abs
	}
	return roots
}

// setupState writes a config that points every collector at the testdata and
// returns the state directory. HOME is redirected too so nothing can reach the
// developer's real logs.
func setupState(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv(agentcfg.EnvServer, "")
	t.Setenv(agentcfg.EnvToken, "")
	t.Setenv(agentcfg.EnvClaudeConfigDir, "")

	dir := filepath.Join(home, "state")
	cfg := agentcfg.Defaults()
	cfg.DeviceName = "test-device"
	for src, root := range testdataRoots(t) {
		cfg.Source(src).Root = root
	}
	if err := agentcfg.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv(agentcfg.EnvStateDir, dir)
	return dir
}

// runCLI executes one command with stdout captured.
func runCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	defer func() { stdout = old }()
	code := run(args)
	return buf.String(), code
}

func TestScanAgainstCollectorTestdata(t *testing.T) {
	setupState(t)

	out, code := runCLI(t, "scan", "--since", "all")
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, out)
	}

	// Both collectors must be represented, with their fixture models.
	for _, want := range []string{"claude_code", "codex", "claude-opus-5", "gpt-5.6-terra", "TOTAL", "per day"} {
		if !strings.Contains(out, want) {
			t.Errorf("scan output is missing %q:\n%s", want, out)
		}
	}

	total := totalRow(t, out)
	if total.Calls == 0 {
		t.Errorf("TOTAL row reports no calls:\n%s", out)
	}
	if total.Input == 0 || total.Output == 0 {
		t.Errorf("TOTAL row has zero tokens (input=%d output=%d):\n%s", total.Input, total.Output, out)
	}
	if total.CacheRead == 0 || total.CacheWrite == 0 {
		t.Errorf("TOTAL row has no cache tokens; the claude fixtures carry both:\n%s", out)
	}

	// The per-day table must cover the fixture dates.
	for _, day := range []string{"2026-08-01", "2026-08-02"} {
		if !strings.Contains(out, day) {
			t.Errorf("per-day table is missing %s:\n%s", day, out)
		}
	}
}

// totalRow parses the grand TOTAL line of the model table.
func totalRow(t *testing.T, out string) totals {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 7 || fields[0] != "TOTAL" {
			continue
		}
		var got totals
		nums := make([]int64, 6)
		for i := range nums {
			nums[i] = parseNum(t, fields[i+1])
		}
		got.Calls = int(nums[0])
		got.Input, got.Output, got.CacheRead, got.CacheWrite, got.Reasoning = nums[1], nums[2], nums[3], nums[4], nums[5]
		return got
	}
	t.Fatalf("no TOTAL row in:\n%s", out)
	return totals{}
}

func parseNum(t *testing.T, s string) int64 {
	t.Helper()
	var v int64
	for _, c := range strings.ReplaceAll(s, ",", "") {
		if c < '0' || c > '9' {
			t.Fatalf("%q is not a number", s)
		}
		v = v*10 + int64(c-'0')
	}
	return v
}

func TestScanMatchesJSONOutput(t *testing.T) {
	setupState(t)

	out, code := runCLI(t, "scan", "--since", "all", "--json")
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, out)
	}
	var events []event.UsageEvent
	if err := json.Unmarshal([]byte(out), &events); err != nil {
		t.Fatalf("--json did not produce a JSON array: %v\n%s", err, out)
	}
	if len(events) == 0 {
		t.Fatal("--json produced no events")
	}

	var calls int
	table, _ := runCLI(t, "scan", "--since", "all")
	calls = totalRow(t, table).Calls
	if calls != len(events) {
		t.Errorf("table reports %d calls but --json emitted %d events", calls, len(events))
	}
	for _, e := range events {
		if !e.Valid() {
			t.Fatalf("scan emitted an invalid event: %+v", e)
		}
		if e.DeviceID == "" {
			t.Fatal("scan emitted an event with no device id")
		}
	}
	// Events must come out in chronological order.
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.Before(events[i-1].Timestamp) {
			t.Fatalf("events are not sorted: %s before %s", events[i].Timestamp, events[i-1].Timestamp)
		}
	}
}

func TestScanSourceFilter(t *testing.T) {
	setupState(t)

	out, code := runCLI(t, "scan", "--since", "all", "--source", "codex")
	if code != 0 {
		t.Fatalf("exit code %d\n%s", code, out)
	}
	if strings.Contains(out, "claude_code") {
		t.Errorf("--source codex still scanned claude_code:\n%s", out)
	}
	if !strings.Contains(out, "gpt-5.6-terra") {
		t.Errorf("--source codex produced no codex rows:\n%s", out)
	}
}

func TestScanRejectsUnknownSource(t *testing.T) {
	setupState(t)
	if _, code := runCLI(t, "scan", "--source", "nope"); code != 2 {
		t.Errorf("exit code %d for an unknown source, want 2", code)
	}
}

func TestScanNarrowWindowIsEmpty(t *testing.T) {
	setupState(t)
	// The fixtures are dated 2026-08; nothing can fall inside the last minute.
	out, code := runCLI(t, "scan", "--since", "1m")
	if code != 0 {
		t.Fatalf("exit code %d\n%s", code, out)
	}
	if !strings.Contains(out, "no events in this window") {
		t.Errorf("expected an empty-window notice:\n%s", out)
	}
}

func TestScanWorksWithoutAConfigFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv(agentcfg.EnvStateDir, filepath.Join(home, "absent"))
	t.Setenv(agentcfg.EnvServer, "")
	t.Setenv(agentcfg.EnvToken, "")

	// No config, no logs: the offline validation tool must still run cleanly.
	out, code := runCLI(t, "scan", "--since", "all")
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "not installed") {
		t.Errorf("expected the missing roots to be reported:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, "absent")); err == nil {
		t.Error("scan created a state directory; it must not write anything")
	}
}

func TestScanStateDirFlagBeatsEnv(t *testing.T) {
	dir := setupState(t)
	// Point the env at an empty directory and the flag at the real one.
	t.Setenv(agentcfg.EnvStateDir, filepath.Join(t.TempDir(), "empty"))

	out, code := runCLI(t, "scan", "--since", "all", "--state-dir", dir)
	if code != 0 {
		t.Fatalf("exit code %d\n%s", code, out)
	}
	if !strings.Contains(out, "claude-opus-5") {
		t.Errorf("--state-dir did not win over the environment:\n%s", out)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in    string
		zero  bool
		about time.Duration
	}{
		{in: "all", zero: true},
		{in: "0", zero: true},
		{in: "", zero: true},
		{in: "24h", about: 24 * time.Hour},
		{in: "7d", about: 7 * 24 * time.Hour},
		{in: "4w", about: 28 * 24 * time.Hour},
		{in: "90m", about: 90 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseSince(tc.in)
			if err != nil {
				t.Fatalf("parseSince(%q): %v", tc.in, err)
			}
			if tc.zero {
				if !got.IsZero() {
					t.Errorf("parseSince(%q) = %v, want the zero time", tc.in, got)
				}
				return
			}
			delta := now.Sub(got) - tc.about
			if delta < -time.Minute || delta > time.Minute {
				t.Errorf("parseSince(%q) is %s off", tc.in, delta)
			}
		})
	}
	if _, err := parseSince("banana"); err == nil {
		t.Error("parseSince accepted nonsense")
	}
}

func TestNumFormatting(t *testing.T) {
	cases := map[int64]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 1234567: "1,234,567", -4321: "-4,321"}
	for in, want := range cases {
		if got := num(in); got != want {
			t.Errorf("num(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	if code := run([]string{"frobnicate"}); code != 2 {
		t.Errorf("exit code %d for an unknown command, want 2", code)
	}
	if code := run(nil); code != 2 {
		t.Errorf("exit code %d for no command, want 2", code)
	}
}

func TestVersionCommand(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
}

func TestRegistryCoversTheConfiguredSources(t *testing.T) {
	// Every compiled-in collector must have a default root, otherwise the agent
	// would scan the working directory.
	for _, s := range known() {
		if agentcfg.DefaultRoot(s) == "" {
			t.Errorf("source %s has no default root in agentcfg", s)
		}
	}
	// And every source agentcfg knows about must be either compiled in or
	// simply absent from the registry (antigravity, until it lands).
	for _, s := range agentcfg.Sources {
		if _, ok := registry[s]; !ok && s != event.SourceAntigravity {
			t.Errorf("source %s is configured but has no collector", s)
		}
	}
}

func TestRunOncePushesAndSavesCursors(t *testing.T) {
	dir := setupState(t)

	var batches int
	var events int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ingest" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		batches++
		var body struct {
			Events []event.UsageEvent `json:"events"`
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("request body is not gzip: %v", err)
		}
		defer zr.Close()
		if err := json.NewDecoder(zr).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		events += len(body.Events)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":0,"device_id":"dev"}`, len(body.Events))
	}))
	defer srv.Close()

	out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok")
	if code != 0 {
		t.Fatalf("exit code %d, want 0\n%s", code, out)
	}
	if batches != 1 || events == 0 {
		t.Fatalf("%d batches with %d events reached the server", batches, events)
	}

	// The cursor file must exist afterwards, and a second pass must find nothing
	// new (the collectors resumed where they stopped).
	raw, err := os.ReadFile(agentcfg.CursorPath(dir))
	if err != nil {
		t.Fatalf("cursors were not saved: %v", err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("cursors.json is not JSON: %v", err)
	}
	for _, s := range known() {
		if _, ok := stored[string(s)]; !ok {
			t.Errorf("no cursor stored for %s", s)
		}
	}

	before := events
	if _, code := runCLI(t, "run", "--once", "--server", srv.URL, "--token", "tok"); code != 0 {
		t.Fatalf("second pass exit code %d", code)
	}
	if events != before {
		t.Errorf("second pass re-sent %d events; the cursor was not honoured", events-before)
	}
}

func TestRunOnceQueuesWhenTheServerIsDown(t *testing.T) {
	dir := setupState(t)

	// Port 1 on the loopback interface refuses connections immediately.
	out, code := runCLI(t, "run", "--once", "--server", "http://127.0.0.1:1", "--token", "tok")
	if code != 0 {
		t.Fatalf("exit code %d, want 0: queued events count as handled\n%s", code, out)
	}
	q, err := transport.OpenQueue(agentcfg.QueueDir(dir), 0)
	if err != nil {
		t.Fatal(err)
	}
	n, bytes, err := q.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || bytes == 0 {
		t.Fatalf("nothing was queued (%d batches, %d bytes)", n, bytes)
	}
	// Cursors advanced, because the events are safely on disk.
	if _, err := os.Stat(agentcfg.CursorPath(dir)); err != nil {
		t.Errorf("cursors were not saved after queueing: %v", err)
	}
}

func TestRunRequiresAServer(t *testing.T) {
	setupState(t)
	if _, code := runCLI(t, "run", "--once"); code != 2 {
		t.Errorf("exit code %d with no server configured, want 2", code)
	}
}

func TestStatusRedactsTheToken(t *testing.T) {
	dir := setupState(t)
	cfg, _, err := agentcfg.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ServerURL = "http://127.0.0.1:1"
	cfg.DeviceToken = "aum_supersecret"
	if err := agentcfg.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}

	out, code := runCLI(t, "status", "--timeout", "200ms")
	if code != 0 {
		t.Fatalf("exit code %d\n%s", code, out)
	}
	if strings.Contains(out, "supersecret") {
		t.Errorf("status printed the device token:\n%s", out)
	}
	for _, want := range []string{"state dir", dir, "queue", "health", "claude_code", "codex", "antigravity"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output is missing %q:\n%s", want, out)
		}
	}
}

// A run pointed at another server through flags or the environment (a debug
// run, a token from a secret) must not rewrite config.json with those values,
// even when it does persist something: the server-issued id, a new id scheme.
func TestRunDoesNotPersistOverrides(t *testing.T) {
	dir := setupState(t)
	stored, _, err := agentcfg.LoadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	stored.ServerURL, stored.DeviceToken, stored.IDScheme = "https://prod.example", "aum_prod", 0
	if err := agentcfg.Save(dir, stored); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/agent/whoami" {
			fmt.Fprint(w, `{"device_id":"server-issued-id"}`)
			return
		}
		fmt.Fprint(w, `{"accepted":0,"duplicates":0,"device_id":"server-issued-id"}`)
	}))
	defer srv.Close()
	t.Setenv(agentcfg.EnvToken, "aum_from_env")

	if out, code := runCLI(t, "run", "--once", "--server", srv.URL, "--interval", "5"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	got, _, err := agentcfg.LoadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerURL != "https://prod.example" || got.DeviceToken != "aum_prod" || got.IntervalSeconds == 5 {
		t.Errorf("overrides leaked into config.json: server %q token %q interval %d", got.ServerURL, got.DeviceToken, got.IntervalSeconds)
	}
	if got.DeviceID != "server-issued-id" || got.IDScheme != event.IDScheme {
		t.Errorf("device id %q / id scheme %d were not persisted", got.DeviceID, got.IDScheme)
	}
}
