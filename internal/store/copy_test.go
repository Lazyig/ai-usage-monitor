package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/store/storetest"
)

func dumpDevices(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT ` + deviceCopyCols + ` FROM devices ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		vals := make([]any, 12)
		ptrs := make([]any, 12)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprint(vals...))
	}
	return out
}

// fill gives s a revoked device, a device with a pending re-sync and events.
func fill(t *testing.T, s *Store) (token string) {
	t.Helper()
	ctx := context.Background()
	a, token, err := s.CreateDevice(ctx, "mac")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreateDevice(ctx, "old")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchDevice(ctx, DeviceMeta{ID: a.ID, Name: "host-a", OS: "darwin", Arch: "arm64", AgentVersion: "1.2"}, true, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestResync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeDevice(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	var evs []event.UsageEvent
	for i := range 2500 { // more than one copy batch
		dev := a.ID
		if i%7 == 0 {
			dev = b.ID
		}
		evs = append(evs, event.UsageEvent{EventID: fmt.Sprintf("e%05d", i), DeviceID: dev, Source: event.SourceClaudeCode,
			Timestamp: time.UnixMilli(int64(i) * 1000), Model: "claude-opus-5", SessionID: fmt.Sprint("s", i%5),
			Project: "/p", Input: int64(i), Output: 2, CacheRead: 3, CacheWrite5m: 4, CacheWrite1h: 5, CacheWrite: 6,
			Reasoning: 1, Confidence: event.ConfidenceExact, ToolVersion: "2.1"})
	}
	if _, _, _, err := s.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
	return token
}

func copyBetween(t *testing.T, src, dst *Store) CopyReport {
	t.Helper()
	rep, err := Copy(context.Background(), src, dst, false)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// SQLite -> target -> SQLite again: every row arrives unchanged, and an
// enrolled device's token still works on the copy.
func TestCopyRoundTrip(t *testing.T) {
	ctx := context.Background()
	srcPath := filepath.Join(t.TempDir(), "src.db")
	orig, err := Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	token := fill(t, orig)
	wantDev, wantEv := dumpDevices(t, orig), dumpEvents(t, orig)
	orig.Close()

	src, err := OpenSource(ctx, srcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	midDSN := storetest.DSN(t) // SQLite, or PostgreSQL in the PostgreSQL suite
	mid, err := Open(midDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer mid.Close()
	if rep := copyBetween(t, src, mid); rep.Devices != 2 || rep.Events != 2500 || rep.Cleaned != 0 {
		t.Fatalf("report %+v", rep)
	}
	if got := dumpDevices(t, mid); !reflect.DeepEqual(got, wantDev) {
		t.Errorf("devices differ\n got %v\nwant %v", got, wantDev)
	}
	if got := dumpEvents(t, mid); !reflect.DeepEqual(got, wantEv) {
		t.Errorf("events differ")
	}
	if d, err := mid.DeviceByTokenHash(ctx, HashToken(token)); err != nil || d.Name != "mac" {
		t.Errorf("token on the copy: %+v %v", d, err)
	}
	if ok, err := mid.ConsumeResync(ctx, dumpID(t, mid, "mac")); err != nil || !ok {
		t.Errorf("pending re-sync lost: %v %v", ok, err)
	}

	// And back: the copy is a valid source in turn.
	midSrc, err := OpenSource(ctx, midDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer midSrc.Close()
	back := openSQLiteTest(t)
	copyBetween(t, midSrc, back)
	if got := dumpEvents(t, back); !reflect.DeepEqual(got, wantEv) {
		t.Errorf("events differ after the way back")
	}
}

func dumpID(t *testing.T, s *Store, name string) string {
	t.Helper()
	var id string
	if err := s.db.QueryRow(`SELECT id FROM devices WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCopyRefusals(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// A missing source is not created.
	missing := filepath.Join(dir, "missing.db")
	if _, err := OpenSource(ctx, missing); err == nil {
		t.Error("a missing source opened")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Error("opening a missing source created it")
	}

	// A source behind this version's schema is not migrated, only refused.
	oldPath := filepath.Join(dir, "old.db")
	old, err := Open(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.db.Exec(`DELETE FROM schema_migrations WHERE version = '0004_drop_oversized_events.sql'`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	if _, err := OpenSource(ctx, oldPath); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Errorf("an outdated source: %v", err)
	}

	// The target must be empty.
	srcPath := filepath.Join(dir, "src.db")
	s, err := Open(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	fill(t, s)
	s.Close()
	src, err := OpenSource(ctx, srcPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	full := openTest(t)
	if _, _, err := full.CreateDevice(ctx, "there"); err != nil {
		t.Fatal(err)
	}
	if _, err := Copy(ctx, src, full, false); !errors.Is(err, ErrTargetNotEmpty) {
		t.Errorf("non-empty target: %v", err)
	}
}

// Old SQLite data may hold NUL in free text (cleaned on the way into
// PostgreSQL, counted) or in ids (refused: cleaning could merge two).
func TestCopyUncleanLegacyData(t *testing.T) {
	ctx := context.Background()
	mk := func(t *testing.T, sql string, args ...any) *Store {
		path := filepath.Join(t.TempDir(), "legacy.db")
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		d, _, err := s.CreateDevice(ctx, "x")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(sql, append([]any{d.ID}, args...)...); err != nil {
			t.Fatal(err)
		}
		s.Close()
		src, err := OpenSource(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { src.Close() })
		return src
	}
	const ins = `INSERT INTO usage_events(event_id, device_id, source, ts, model, session_id, project, received_at)
		VALUES($2, $1, 'codex', 1, $3, 's', 'p', 1)`

	text := mk(t, ins, "e1", "m\x00odel")
	rep, err := Copy(ctx, text, openTest(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[bool]int{true: 1, false: 0}[storetest.Postgres()]; rep.Cleaned != want {
		t.Errorf("cleaned %d values, want %d", rep.Cleaned, want)
	}

	id := mk(t, ins, "e\x00vil", "m")
	if _, err := Copy(ctx, id, openTest(t), false); err == nil || !strings.Contains(err.Error(), "ids") {
		t.Errorf("an unclean id was copied: %v", err)
	}
}

func TestSameDatabase(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.db")
	if err := os.WriteFile(a, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.db")
	if err := os.Symlink(a, link); err != nil {
		t.Skip(err)
	}
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{a, a, true},
		{a, link, true},
		{a, filepath.Join(dir, "b.db"), false},
		{"postgres://u:x@db:5432/aum", "postgresql://other@DB/aum?sslmode=require", true},
		{"postgres://db/aum", "postgres://db/other", false},
		{"postgres://db/aum?search_path=a", "postgres://db/aum?search_path=b", false},
		{"postgres://db/aum", a, false},
	} {
		if got := SameDatabase(c.a, c.b); got != c.same {
			t.Errorf("SameDatabase(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// An empty source is most likely a mistyped path: refused unless asked for.
func TestCopyEmptySource(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "empty.db")
	e, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	src, err := OpenSource(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := Copy(ctx, src, openTest(t), false); !errors.Is(err, ErrSourceEmpty) {
		t.Errorf("empty source: %v", err)
	}
	if _, err := Copy(ctx, src, openTest(t), true); err != nil {
		t.Errorf("empty source with allowEmpty: %v", err)
	}
}

// A SQLite path may contain characters the file: URI form gives a meaning.
func TestSQLitePathsWithURICharacters(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	names := []string{"my#1.db", "a?b.db", "p%41.db"}
	if runtime.GOOS == "windows" {
		names = []string{"my#1.db", "p%41.db"} // Windows file names cannot hold '?'
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		s, err := Open(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		fill(t, s)
		s.Close()
		src, err := OpenSource(ctx, path)
		if err != nil {
			t.Fatalf("%s as a source: %v", name, err)
		}
		if _, err := Copy(ctx, src, openSQLiteTest(t), false); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		src.Close()
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != len(names)*3 && len(entries) != len(names) { // each file, maybe with -wal and -shm
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Errorf("unexpected files: %v", got)
	}
}

// The copy names every column of both tables, so a column added by a later
// migration cannot be dropped silently.
func TestCopyColumnsCoverSchema(t *testing.T) {
	s := openTest(t)
	q := `SELECT p.name FROM pragma_table_info($1) p`
	if s.dialect == postgres {
		q = `SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1`
	}
	for table, cols := range map[string]string{"devices": deviceCopyCols, "usage_events": eventCols} {
		rows, err := s.db.Query(q, table)
		if err != nil {
			t.Fatal(err)
		}
		var have []string
		for rows.Next() {
			var c string
			rows.Scan(&c)
			have = append(have, c)
		}
		rows.Close()
		var want []string
		for _, c := range strings.Split(cols, ",") {
			want = append(want, strings.TrimSpace(c))
		}
		slices.Sort(have)
		slices.Sort(want)
		if !slices.Equal(have, want) {
			t.Errorf("%s: schema %v, copied %v", table, have, want)
		}
	}
}
