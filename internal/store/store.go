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
}

// Store owns the SQLite handle.
type Store struct {
	db *sql.DB
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

	s := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the handle for tests and future extensions.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

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
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events`).Scan(&c.Events); err != nil {
		return c, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE revoked_at IS NULL`).Scan(&c.Devices); err != nil {
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
