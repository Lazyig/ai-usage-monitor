// Package store is the persistence layer: schema migrations, event ingestion
// and every aggregate query behind docs/API.md, on SQLite (the default) or
// PostgreSQL.
//
// On SQLite a single writer connection serializes writes and a separate
// read-only pool serves dashboard queries alongside it using WAL; in-memory
// tests share one connection because their databases are local to it. On
// PostgreSQL one pool serves both, and writes that touch a device and its
// events lock the device row first (see InsertEvents and PurgeEvents).
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"ai-usage-monitor/internal/event"
	"ai-usage-monitor/internal/pricing"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
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

// dialect is the database the Store talks to. SQL is written once in the
// subset both accept; what differs (schema files, locking, pools, retries)
// branches on this.
type dialect int

const (
	sqlite dialect = iota
	postgres
)

// String is the dialect's name, which is also its migrations directory.
func (d dialect) String() string {
	if d == postgres {
		return "postgres"
	}
	return "sqlite"
}

// Store owns the database handles.
type Store struct {
	dialect dialect
	db      *sql.DB // SQLite: the single writer. PostgreSQL: the one pool for everything
	rdb     *sql.DB // SQLite: the read-only pool (WAL lets it run beside the writer)
	ping    *sql.DB // the readiness check's own handle, so it never queues behind requests

	batch batchSize // statement batching for InsertEvents

	retries atomic.Int64 // write transactions run again after a deadlock (tests check it stays 0)
}

// Options tunes Open.
type Options struct {
	// MaxConns caps the PostgreSQL pool (default 10). SQLite ignores it.
	MaxConns int
}

// ErrConfig marks an error in the database setting itself (a malformed URL, a
// refused option): retrying the same setting cannot succeed.
var ErrConfig = errors.New("store: invalid database setting")

// Open opens the database dsn names and applies all embedded migrations. A
// postgres:// or postgresql:// URL selects PostgreSQL; anything else is a SQLite
// file path, created if needed (":memory:" for tests).
func Open(dsn string) (*Store, error) {
	return OpenContext(context.Background(), dsn, Options{})
}

// OpenContext is Open with a context that bounds connecting and migrating, and
// with options.
func OpenContext(ctx context.Context, dsn string, opt Options) (*Store, error) {
	if IsPostgresURL(dsn) {
		return openPostgres(ctx, dsn, opt)
	}
	if err := CheckSQLitePath(dsn); err != nil {
		return nil, err
	}
	return openSQLite(ctx, dsn)
}

// DB exposes the handle to the tests, which check the schema directly.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error {
	for _, h := range []*sql.DB{s.rdb, s.ping} {
		if h != nil && h != s.db {
			h.Close()
		}
	}
	return s.db.Close()
}

// Ping is the readiness check: a trivial read on a handle of its own, so it
// reflects whether requests can be served without queueing behind them.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.ping.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// migrate applies every embedded migration of the dialect, in filename order,
// exactly once. Each dialect has its own directory with the same file names,
// and the file name is the version recorded in schema_migrations.
func (s *Store) migrate(ctx context.Context) error {
	if s.dialect == postgres {
		return s.migratePostgres(ctx)
	}
	return s.migrateSQLite(ctx)
}

// migration is one embedded schema change.
type migration struct {
	version string
	body    string
}

// migrations lists a dialect's embedded files in order.
func migrations(dir string) ([]migration, error) {
	entries, err := fs.Glob(migrationsFS, "migrations/"+dir+"/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(entries)
	out := make([]migration, 0, len(entries))
	for _, name := range entries {
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: path.Base(name), body: string(body)})
	}
	return out, nil
}

// appliedVersions reads schema_migrations.
func appliedVersions(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("store: read migrations: %w", err)
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// applyMigration runs one migration and records it, inside tx.
func applyMigration(ctx context.Context, tx *sql.Tx, m migration) error {
	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return fmt.Errorf("store: migration %s: %w", m.version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, applied_at) VALUES($1, $2)`,
		m.version, nowMillis()); err != nil {
		return fmt.Errorf("store: record migration %s: %w", m.version, err)
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

// writeTx runs fn in one write transaction, committing when it returns nil.
// On PostgreSQL a transaction that lost a deadlock or a serialization check is
// run again from the start (fn must therefore recompute everything it returns);
// SQLite serializes writers and never needs it.
func (s *Store) writeTx(ctx context.Context, fn func(*sql.Tx) error) error {
	for attempt := 0; ; attempt++ {
		err := s.tryTx(ctx, fn)
		if err == nil || s.dialect != postgres || attempt == 3 || !retryable(err) {
			return err
		}
		s.retries.Add(1)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(10+rand.IntN(40)) * time.Millisecond << attempt):
		}
	}
}

func (s *Store) tryTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// unmatchable reports whether a read argument can match no stored value. On
// PostgreSQL such an argument (NUL, invalid UTF-8) would be an error rather
// than a miss, so the caller answers "nothing found" without asking. SQLite
// compares the bytes as they are: older rows may still hold them.
func (s *Store) unmatchable(args ...string) bool {
	if s.dialect != postgres {
		return false
	}
	for _, a := range args {
		if !IsCleanText(a) {
			return true
		}
	}
	return false
}

// CleanText makes s storable as text on every backend: invalid UTF-8 and NUL
// (which PostgreSQL text cannot hold) become U+FFFD. Valid text is returned
// unchanged.
func CleanText(s string) string {
	if IsCleanText(s) {
		return s
	}
	return strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", "\uFFFD")
}

// IsCleanText reports whether s is valid UTF-8 without NUL bytes, that is,
// whether CleanText would return it unchanged.
func IsCleanText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
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
