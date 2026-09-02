// Package store is the SQLite persistence layer: schema migrations, event
// ingestion and every aggregate query behind docs/API.md.
//
// The database is opened with a single connection (SetMaxOpenConns(1)) so there
// is exactly one writer and no SQLITE_BUSY dance; WAL keeps reads cheap anyway
// and the workload is a handful of agents pushing batches every minute.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/pricing"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned when a lookup by id finds nothing.
var ErrNotFound = errors.New("store: not found")

// Pricer prices one (aggregated) event. *pricing.Table implements it.
type Pricer interface {
	Cost(event.UsageEvent) pricing.Cost
	// CostFor prices with an explicit request prompt size (long-context tiers).
	CostFor(event.UsageEvent, int64) pricing.Cost
	// Thresholds are the long-context boundaries the aggregator must group by.
	Thresholds() []int64
}

// Store owns the SQLite handle.
type Store struct {
	db  *sql.DB // the single writer (and the reader for in-memory databases)
	rdb *sql.DB // read-only pool: WAL lets readers run beside the writer
}

// Open opens (creating if needed) the SQLite database at path and applies all
// embedded migrations. path may be ":memory:" for tests.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("store: create db dir: %w", err)
			}
		}
	}
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(on)" +
		// Sorting/grouping spills go to memory instead of a temp file: the container
		// runs with a read-only root filesystem (SQLITE_IOERR_GETTEMPPATH otherwise).
		"&_pragma=temp_store(MEMORY)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// One connection: single writer, and every connection shares the same
	// pragmas and (for ":memory:") the same database.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Reads get their own small pool so a dashboard load (five aggregations at
	// once) and the readiness probe never queue behind ingest writes or each
	// other. An in-memory database is per-connection, so tests keep one handle.
	rdb := db
	if !strings.Contains(path, ":memory:") && !strings.Contains(path, "mode=memory") {
		rdb, err = sql.Open("sqlite", dsn+"&_pragma=query_only(1)")
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("store: open reader: %w", err)
		}
		rdb.SetMaxOpenConns(4)
		rdb.SetMaxIdleConns(4)
		rdb.SetConnMaxLifetime(0)
	}

	s := &Store{db: db, rdb: rdb}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the handle to the tests, which check the schema directly.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error {
	if s.rdb != nil && s.rdb != s.db {
		s.rdb.Close()
	}
	return s.db.Close()
}

// Ping is the readiness check: a trivial read on the reader pool, so it reflects
// whether requests can be served without measuring how busy the writer is.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.rdb.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// migrate applies every migrations/*.sql file, in filename order, exactly once.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: migrations table: %w", err)
	}
	applied := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		version := filepath.Base(name)
		if applied[version] {
			continue
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			version, nowMillis()); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %s: %w", version, err)
		}
	}
	return nil
}

// Counts holds the numbers /health reports.
type Counts struct {
	Events  int64 `json:"events"`
	Devices int64 `json:"devices"`
}

// Counts returns row counts for /health.
func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	if err := s.rdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events`).Scan(&c.Events); err != nil {
		return c, err
	}
	if err := s.rdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE revoked_at IS NULL`).Scan(&c.Devices); err != nil {
		return c, err
	}
	return c, nil
}

func nowMillis() int64 { return time.Now().UTC().UnixMilli() }

func toMillis(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// nullMillis converts a nullable millis column to a *time.Time.
func nullMillis(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := fromMillis(n.Int64)
	return &t
}
