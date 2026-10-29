package store

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ai-usage-monitor/internal/event"
)

func TestMigrationsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Re-running migrate on the same handle must be a no-op.
	if err := s.migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if _, _, err := s.CreateDevice(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopening an existing database must not re-apply anything either.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	devs, err := s2.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 1 {
		t.Fatalf("devices after reopen = %d, want 1", len(devs))
	}
	// Exactly one row per embedded migration, however many there are by now.
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(files) {
		t.Fatalf("schema_migrations rows = %d, want %d (one per migrations/*.sql)", n, len(files))
	}
}

// An event stored before the token cap would overflow every SUM over it; the
// migration removes it.
func TestMigrationDropsOversizedEvents(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.InsertEvents(ctx, []event.UsageEvent{
		ev(t, "ok", d.ID, event.SourceCodex, "2026-10-01T01:00:00Z", "gpt-5.6-terra", 10, 0, 0, 0),
		ev(t, "huge", d.ID, event.SourceCodex, "2026-10-01T02:00:00Z", "gpt-5.6-terra", 9_000_000_000_000_000_000, 0, 0, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = '0004_drop_oversized_events.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d events left, want only the sane one", n)
	}
}

func TestDeviceLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	d, token, err := s.CreateDevice(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 20 || token[:4] != TokenPrefix {
		t.Fatalf("token %q has no aum_ prefix", token)
	}
	got, err := s.DeviceByTokenHash(ctx, HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != d.ID || got.Name != "macbook" {
		t.Fatalf("lookup = %+v", got)
	}

	now := mustTime(t, "2026-09-02T00:00:00Z")
	if err := s.TouchDevice(ctx, DeviceMeta{ID: d.ID, OS: "darwin", Arch: "arm64", AgentVersion: "0.1.0"}, true, now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Device(ctx, d.ID)
	if got.Name != "macbook" || got.OS != "darwin" || got.Arch != "arm64" || got.AgentVersion != "0.1.0" {
		t.Fatalf("after touch = %+v", got)
	}
	if got.LastSeen == nil || !got.LastSeen.Equal(now) || got.LastIngest == nil {
		t.Fatalf("heartbeat not recorded: %+v", got)
	}

	if err := s.RevokeDevice(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeviceByTokenHash(ctx, HashToken(token)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token still resolves: %v", err)
	}
	if err := s.RevokeDevice(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown = %v, want ErrNotFound", err)
	}
	// Events survive revocation.
	if _, err := s.Device(ctx, d.ID); err != nil {
		t.Fatalf("device row removed: %v", err)
	}
}

func TestSummary(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d1, d2 := seed(t, s)
	f := Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-03T00:00:00Z")}

	sum, err := s.Summary(ctx, f, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Totals.Events != 5 {
		t.Fatalf("events = %d, want 5", sum.Totals.Events)
	}
	if sum.Totals.Input != 5_500_000 || sum.Totals.Output != 400_000 ||
		sum.Totals.CacheRead != 2_000_000 || sum.Totals.CacheWrite != 500_000 {
		t.Fatalf("totals = %+v", sum.Totals)
	}
	approx(t, "totals.cost_usd", sum.Totals.CostUSD, 20.125)
	approx(t, "totals.cost_known_ratio", sum.Totals.CostKnownRatio, round6(7_400_000.0/8_400_000.0))

	bySource := map[string]Group{}
	for _, g := range sum.BySource {
		bySource[g.Key] = g
	}
	if len(bySource) != 3 {
		t.Fatalf("by_source keys = %v", bySource)
	}
	approx(t, "by_source[claude_code]", bySource["claude_code"].CostUSD, 14.125)
	approx(t, "by_source[codex]", bySource["codex"].CostUSD, 3.6)
	approx(t, "by_source[antigravity]", bySource["antigravity"].CostUSD, 2.4)
	if bySource["claude_code"].Events != 2 {
		t.Errorf("claude_code events = %d, want 2", bySource["claude_code"].Events)
	}
	if sum.BySource[0].Key != "claude_code" {
		t.Errorf("by_source not sorted by cost: %v", sum.BySource)
	}

	byDevice := map[string]Group{}
	for _, g := range sum.ByDevice {
		byDevice[g.Key] = g
	}
	approx(t, "by_device[d1]", byDevice[d1].CostUSD, 17.725)
	approx(t, "by_device[d2]", byDevice[d2].CostUSD, 2.4)
	if byDevice[d1].Name != "mac" || byDevice[d2].Name != "linux" {
		t.Errorf("device names missing: %+v", sum.ByDevice)
	}

	byModel := map[string]Group{}
	for _, g := range sum.ByModel {
		byModel[g.Key] = g
	}
	if len(byModel) != 4 {
		t.Fatalf("by_model = %+v", sum.ByModel)
	}
	approx(t, "by_model[claude-opus-5]", byModel["claude-opus-5"].CostUSD, 14.125)
	if byModel["claude-opus-5"].Source != "claude_code" {
		t.Errorf("model row lost its source: %+v", byModel["claude-opus-5"])
	}
	if !byModel["gemini-default"].Estimated {
		t.Errorf("alias-priced model should be estimated: %+v", byModel["gemini-default"])
	}
	if byModel["unpriced-model"].CostUSD != 0 || byModel["unpriced-model"].CostKnownRatio != 0 {
		t.Errorf("unpriced model = %+v, want zero cost and ratio", byModel["unpriced-model"])
	}

	// A device filter narrows every section.
	only, err := s.Summary(ctx, Filter{From: f.From, To: f.To, Device: d2}, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if only.Totals.Events != 2 || len(only.ByDevice) != 1 {
		t.Fatalf("device filter = %+v", only.Totals)
	}
}

func TestBreakdown(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	_, d2 := seed(t, s)
	f := Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-03T00:00:00Z")}

	b, err := s.Breakdown(ctx, f, "model", 2, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Rows) != 2 {
		t.Fatalf("limit ignored: %d rows", len(b.Rows))
	}
	if b.Rows[0].Key != "claude-opus-5" || b.Rows[1].Key != "gpt-5.6-terra" {
		t.Fatalf("rows not ordered by cost: %+v", b.Rows)
	}
	approx(t, "rows[0].cost", b.Rows[0].CostUSD, 14.125)
	if b.Rows[0].FirstSeen == nil || !b.Rows[0].FirstSeen.Equal(mustTime(t, "2026-09-01T10:00:00Z")) {
		t.Errorf("first_seen = %v", b.Rows[0].FirstSeen)
	}
	if b.Rows[0].LastSeen == nil || !b.Rows[0].LastSeen.Equal(mustTime(t, "2026-09-01T15:30:00Z")) {
		t.Errorf("last_seen = %v", b.Rows[0].LastSeen)
	}

	byProject, err := s.Breakdown(ctx, f, "project", 50, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(byProject.Rows) != 4 {
		t.Fatalf("project rows = %+v", byProject.Rows)
	}

	bySession, err := s.Breakdown(ctx, Filter{From: f.From, To: f.To, Device: d2}, "session", 50, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession.Rows) != 2 || bySession.Rows[0].Device != d2 {
		t.Fatalf("session rows = %+v", bySession.Rows)
	}

	byDevice, err := s.Breakdown(ctx, f, "device", 50, testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if byDevice.Rows[0].Name == "" {
		t.Errorf("device breakdown has no name: %+v", byDevice.Rows[0])
	}
	if _, err := s.Breakdown(ctx, f, "bogus", 50, nil); err == nil {
		t.Error("bogus group_by accepted")
	}
}

func TestEventsPagination(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	seed(t, s)
	f := Filter{From: mustTime(t, "2026-09-01T00:00:00Z"), To: mustTime(t, "2026-09-03T00:00:00Z")}

	var ids []string
	cursor := ""
	for range 10 {
		page, err := s.Events(ctx, f, 2, cursor, testTable(t))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Events {
			ids = append(ids, e.EventID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	want := []string{"e5", "e4", "e3", "e2", "e1"} // newest first
	if len(ids) != len(want) {
		t.Fatalf("paged ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("paged ids = %v, want %v", ids, want)
		}
	}

	page, err := s.Events(ctx, Filter{From: f.From, To: f.To, Source: "codex"}, 200, "", testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.NextCursor != "" {
		t.Fatalf("filtered page = %+v", page)
	}
	if page.Events[0].CostUSD == nil {
		t.Fatal("cost_usd missing for a priced event")
	}
	approx(t, "event cost", *page.Events[0].CostUSD, 3.6)

	unpriced, err := s.Events(ctx, Filter{From: f.From, To: f.To, Model: "unpriced-model"}, 200, "", testTable(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(unpriced.Events) != 1 || unpriced.Events[0].CostUSD != nil {
		t.Fatalf("unpriced event = %+v", unpriced.Events)
	}
	if _, err := s.Events(ctx, f, 200, "!!not-base64!!", nil); err == nil {
		t.Error("bad cursor accepted")
	}
}

func TestOpenPermissionErrorHint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only mode on a Windows directory does not stop file creation")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	db, err := Open(filepath.Join(dir, "usage.db"))
	if err == nil {
		db.Close()
		t.Fatal("Open in a read-only directory succeeded")
	}
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "chown") {
		t.Fatalf("err = %v, want a permission error with an ownership hint", err)
	}
}

func TestPermHintOnlyForPermissionErrors(t *testing.T) {
	if got := permHint(fs.ErrNotExist); got != "" {
		t.Fatalf("permHint(ErrNotExist) = %q, want empty", got)
	}
	if got := permHint(fs.ErrPermission); !strings.Contains(got, "65532") {
		t.Fatalf("permHint(ErrPermission) = %q, want an ownership hint", got)
	}
}
