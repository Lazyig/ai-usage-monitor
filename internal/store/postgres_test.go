package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/store/storetest"
)

func requirePostgres(t *testing.T) {
	t.Helper()
	if !storetest.Postgres() {
		t.Skip("set " + storetest.EnvPostgres + " to run the PostgreSQL tests")
	}
}

func TestRedactDSN(t *testing.T) {
	for in, want := range map[string]string{
		"data/usage.db":                                "data/usage.db",
		"postgres://u:secret@db:5432/aum":              "postgres://u:xxxxx@db:5432/aum",
		"postgres://u@db/aum?sslmode=require":          "postgres://u@db/aum?sslmode=require",
		"postgres://db/aum?password=secret&x=1":        "postgres://db/aum?password=xxxxx&x=1",
		"postgres://db/aum?sslpassword=secret":         "postgres://db/aum?sslpassword=xxxxx",
		"postgres://u:p@ss@db/aum":                     "postgres://u:xxxxx@db/aum",
		"postgres://u:se%2Fcret@db/aum":                "postgres://u:xxxxx@db/aum",
		"postgres://u:bad%zz@db/aum":                   "(invalid database URL)",
		"postgresql://u:secret@[::1]:5432/aum?a=b":     "postgresql://u:xxxxx@[::1]:5432/aum?a=b",
		"mysql://root:secret@db/aum":                   "mysql://root:xxxxx@db/aum",
		"host=db user=u password=secret":               "(connection string)",
		"jdbc:postgresql://u:secret@db/aum":            "(invalid database URL)",
		"postgres://u:123/secret@db/aum":               "(invalid database URL)",
		"postgres://u:secret@db/aum?options=-c%20x%3D": "postgres://u:xxxxx@db/aum?options=-c+x%3D",
		// An unencoded # or ? turns the user into the host and the password
		// into the fragment or query.
		"postgres://u:#secret@db/aum":     "(invalid database URL)",
		"postgres://u:2024#secret@db/aum": "(invalid database URL)",
		"postgres://u:?secret@db/aum":     "(invalid database URL)",
		"postgres://u:12?secret@db/aum":   "(invalid database URL)",
		"mysql://u:#secret@db/aum":        "(invalid database URL)",
	} {
		got := RedactDSN(in)
		if got != want {
			t.Errorf("RedactDSN(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("RedactDSN(%q) leaks the password: %q", in, got)
		}
	}
}

// Settings that cannot work are refused before anything is created, and the
// errors never repeat a password.
func TestOpenRefusesBadSettings(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // a setting taken for a relative SQLite path would land here
	for _, dsn := range []string{
		"postgress://u:secret@db/aum",       // typo: not a SQLite file name either
		"mysql://u:secret@db/aum",           // another database
		"host=db user=u password=secret",    // libpq key=value
		"postgres://db/aum?password=secret", // password as a parameter
		"postgres://u:secret@db:notaport/aum",
		"postgres://u:secret@db/aum?sslmode=bogus",
		"postgres://u:bad%zzsecret@db/aum",
		"postgres://u:123/secret@db/aum", // unencoded / in the password
		"postgres://u:#secret@db/aum",    // unencoded # in the password
		"postgres://u:12?secret@db/aum",  // unencoded ? in the password
		"jdbc:postgresql://u:secret@db/aum",
	} {
		_, err := OpenContext(context.Background(), dsn, Options{})
		if !errors.Is(err, ErrConfig) {
			t.Errorf("Open(%q) = %v, want ErrConfig", RedactDSN(dsn), err)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("Open(%q) error leaks the password: %v", RedactDSN(dsn), err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("refused settings created files: %v", entries)
	}
}

func TestPlaintextWarning(t *testing.T) {
	for dsn, warn := range map[string]bool{
		"postgres://u@db.example/aum":                      true,  // sslmode=prefer may fall back
		"postgres://u@db.example/aum?sslmode=disable":      false, // chosen explicitly
		"postgres://u@db.example/aum?sslmode=allow":        true,
		"postgres://u@127.0.0.1:5999,db.example:5432/aum":  true, // a remote fallback
		"postgres://u@db.example/aum?sslmode=require":      false,
		"postgres://u@db.example/aum?sslmode=verify-full":  false,
		"postgres://u@localhost/aum?sslmode=disable":       false,
		"postgres://u@127.0.0.1/aum?sslmode=disable":       false,
		"postgres://u@[::1]/aum?sslmode=disable":           false,
		"postgres://u@/aum?host=%2Fvar%2Frun%2Fpostgresql": false, // unix socket
	} {
		cfg, err := parsePostgres(dsn)
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		if got := plaintextWarning(cfg, explicitlyPlain(dsn)) != ""; got != warn {
			t.Errorf("%s: warning %v, want %v", dsn, got, warn)
		}
	}
}

// Both dialects ship the same migration versions.
func TestMigrationVersionsMatch(t *testing.T) {
	names := func(d dialect) []string {
		ms, err := migrations(d.String())
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range ms {
			out = append(out, m.version)
		}
		return out
	}
	if a, b := names(sqlite), names(postgres); !slices.Equal(a, b) || len(a) == 0 {
		t.Errorf("sqlite migrations %v, postgres %v", a, b)
	}
}

// The PostgreSQL schema has the same tables, columns, nullability, defaults
// and indexes as the SQLite one.
func TestSchemaParity(t *testing.T) {
	requirePostgres(t)
	ctx := context.Background()
	pg, lite := openTest(t), openSQLiteTest(t)

	describe := func(s *Store) []string {
		var q string
		if s.dialect == postgres {
			q = `SELECT c.table_name || '.' || c.column_name || ' null=' || (c.is_nullable = 'YES')::text ||
				' default=' || COALESCE(c.column_default, '')
				FROM information_schema.columns c WHERE c.table_schema = current_schema()
				UNION ALL
				SELECT 'index ' || indexname FROM pg_indexes WHERE schemaname = current_schema() AND indexname LIKE 'idx_%'`
		} else {
			q = `SELECT m.name || '.' || p.name || ' null=' || CASE WHEN p."notnull" = 0 AND p.pk = 0 THEN 'true' ELSE 'false' END ||
				' default=' || COALESCE(p.dflt_value, '')
				FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type = 'table'
				UNION ALL
				SELECT 'index ' || name FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_%'`
		}
		rows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			// PostgreSQL spells defaults with their type ('':text, 0); SQLite as written.
			line = strings.ReplaceAll(line, "''::text", "''")
			out = append(out, line)
		}
		slices.Sort(out)
		return out
	}
	if a, b := describe(lite), describe(pg); !slices.Equal(a, b) {
		t.Errorf("schemas differ\nsqlite:   %v\npostgres: %v", a, b)
	}
}

// Several servers starting at once against one database migrate it once.
func TestConcurrentOpensMigrateOnce(t *testing.T) {
	requirePostgres(t)
	dsn := storetest.DSN(t)
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(dsn)
			if err == nil {
				s.Close()
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if all, _ := migrations("postgres"); n != len(all) {
		t.Errorf("%d migrations recorded, want %d", n, len(all))
	}
}

// Two ingests of overlapping events in opposite orders (two agent processes,
// or a re-sync next to normal traffic) neither deadlock nor lose an update.
func TestConcurrentOverlappingIngest(t *testing.T) {
	requirePostgres(t)
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(out int64, reverse bool) []event.UsageEvent {
		evs := make([]event.UsageEvent, 200)
		for i := range evs {
			id := i
			if reverse {
				id = len(evs) - 1 - i
			}
			evs[i] = event.UsageEvent{EventID: fmt.Sprintf("e%03d", id), DeviceID: d.ID, Source: event.SourceCodex,
				Timestamp: time.Unix(int64(id), 0), Model: "m", Output: out}
		}
		return evs
	}
	if _, _, _, err := s.InsertEvents(ctx, mk(1, false)); err != nil {
		t.Fatal(err)
	}
	for round := range 10 {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for g := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, _, errs[g] = s.InsertEvents(ctx, mk(int64(2+2*round+g), g == 1))
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
	// The lock order is what keeps them apart; a retry would hide a deadlock.
	if n := s.retries.Load(); n != 0 {
		t.Errorf("%d transactions were retried after a deadlock", n)
	}
	var low int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_events WHERE output <> 21`).Scan(&low); err != nil {
		t.Fatal(err)
	}
	if low != 0 {
		t.Errorf("%d events missed the largest output", low)
	}
}

// A purge racing an ingest of the same device ends with a consistent state: no
// foreign key error, and no event of the ingest survives a purge that ran after it.
func TestPurgeRacesIngest(t *testing.T) {
	requirePostgres(t)
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	for round := range 20 {
		evs := make([]event.UsageEvent, 50)
		for i := range evs {
			evs[i] = event.UsageEvent{EventID: fmt.Sprintf("r%d-%d", round, i), DeviceID: d.ID, Source: event.SourceCodex,
				Timestamp: time.Unix(int64(i), 0), Model: "m", Output: 1}
		}
		var wg sync.WaitGroup
		var ingestErr, purgeErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, _, _, ingestErr = s.InsertEvents(ctx, evs) }()
		go func() { defer wg.Done(); _, purgeErr = s.PurgeEvents(ctx, d.ID) }()
		wg.Wait()
		if ingestErr != nil || purgeErr != nil {
			t.Fatalf("round %d: ingest %v, purge %v", round, ingestErr, purgeErr)
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 && n != len(evs) {
			t.Fatalf("round %d: %d events left, want all or none", round, n)
		}
		if _, err := s.PurgeEvents(ctx, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Deleting the device for good while ingesting must not trip the foreign key.
	evs := []event.UsageEvent{{EventID: "last", DeviceID: d.ID, Source: event.SourceCodex, Timestamp: time.Unix(1, 0), Model: "m"}}
	var wg sync.WaitGroup
	var ingestErr, delErr error
	wg.Add(2)
	go func() { defer wg.Done(); _, _, _, ingestErr = s.InsertEvents(ctx, evs) }()
	go func() { defer wg.Done(); _, delErr = s.DeleteDevice(ctx, d.ID, true) }()
	wg.Wait()
	if delErr != nil {
		t.Fatalf("delete: %v", delErr)
	}
	// The ingest either ran first (and was deleted) or found no device: a
	// foreign key violation is the only acceptable kind of failure.
	if ingestErr != nil && !strings.Contains(ingestErr.Error(), "foreign key") {
		t.Fatalf("ingest: %v", ingestErr)
	}
}

// Exactly one of many concurrent consumers sees a re-sync request.
func TestConsumeResyncOnce(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d, _, err := s.CreateDevice(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestResync(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.ConsumeResync(ctx, d.ID)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				got++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if got != 1 {
		t.Errorf("%d consumers saw the request, want 1", got)
	}
	if _, err := s.ConsumeResync(ctx, "no-such-device"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown device: %v", err)
	}
}

// Text sorts bytewise on both backends whatever the database locale (the
// PostgreSQL schema says COLLATE "C"), so keyset pages and group order match
// SQLite. The ids differ only in case and punctuation, which a linguistic
// collation would reorder.
func TestOrderingMatchesSQLite(t *testing.T) {
	ctx := context.Background()
	ids := []string{"a", "B", "_", "a-", "A", "b", "-", "a_"}
	order := func(s *Store) (pages, keys []string) {
		d, _, err := s.CreateDevice(ctx, "x")
		if err != nil {
			t.Fatal(err)
		}
		var evs []event.UsageEvent
		for _, id := range ids {
			evs = append(evs, event.UsageEvent{EventID: id, DeviceID: d.ID, Source: event.SourceCodex,
				Timestamp: time.Unix(100, 0), Model: "m", Project: "p" + id, Output: 1})
		}
		if _, _, _, err := s.InsertEvents(ctx, evs); err != nil {
			t.Fatal(err)
		}
		cursor := ""
		for {
			page, err := s.Events(ctx, Filter{}, 1, cursor, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range page.Events {
				pages = append(pages, e.EventID)
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		rows, err := s.aggregate(ctx, Filter{}, "project", 0, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			keys = append(keys, r.key)
		}
		return pages, keys
	}
	gotPages, gotKeys := order(openTest(t))
	wantPages, wantKeys := order(openSQLiteTest(t))
	if !slices.Equal(gotPages, wantPages) || len(gotPages) != len(ids) {
		t.Errorf("paged order %v, SQLite %v", gotPages, wantPages)
	}
	if !slices.Equal(gotKeys, wantKeys) {
		t.Errorf("group order %v, SQLite %v", gotKeys, wantKeys)
	}
}
