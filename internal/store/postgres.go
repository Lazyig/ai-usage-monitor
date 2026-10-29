package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// postgresBatch: every statement is a network round trip, so InsertEvents sends
// large multi-row statements, without naming them (see batchSize).
var postgresBatch = batchSize{rows: 500, ids: 500, unnamed: true}

// IsPostgresURL reports whether dsn selects PostgreSQL.
func IsPostgresURL(dsn string) bool {
	lower := strings.ToLower(dsn)
	return strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
}

// RedactDSN is dsn as it may appear in logs and errors: a URL loses its
// password, both the user:password@ form and password/sslpassword query
// parameters. A URL that does not parse is not shown at all, since the raw
// string may hold a password. Anything else (a SQLite path) is returned as is.
func RedactDSN(dsn string) string {
	if keyValueDSN.MatchString(dsn) {
		return "(connection string)"
	}
	if !strings.Contains(dsn, "://") {
		return dsn
	}
	u, err := url.Parse(dsn)
	// Opaque (jdbc:postgresql://...) keeps the user part out of u.User, and an
	// @ in the path is a password with an unencoded / in it: neither can be
	// shown safely.
	if err != nil || u.Opaque != "" || strings.Contains(u.Path, "@") {
		return "(invalid database URL)"
	}
	if u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	q := u.Query()
	for _, k := range []string{"password", "sslpassword"} {
		if q.Has(k) {
			q.Set(k, "xxxxx")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// parsePostgres validates dsn and turns it into a pgx config. Its errors never
// quote pgx's message: that can repeat the query string, password included.
func parsePostgres(dsn string) (*pgx.ConnConfig, error) {
	u, err := url.Parse(dsn)
	if err != nil || strings.Contains(u.Path, "@") {
		return nil, fmt.Errorf("%w: the database URL does not parse (percent-encode @, /, # and question marks in the password)", ErrConfig)
	}
	for _, k := range []string{"password", "sslpassword"} {
		if u.Query().Has(k) {
			return nil, fmt.Errorf("%w: put the password in the URL's user part, PGPASSWORD or a PGPASSFILE, not in the %q parameter", ErrConfig, k)
		}
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid database URL %s (check the host, port and parameters such as sslmode)", ErrConfig, RedactDSN(dsn))
	}
	// Without a timeout, one attempt at a host that drops packets waits for the
	// kernel to give up (minutes); the startup wait counts on attempts ending.
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = connectTimeout
	}
	return cfg, nil
}

// connectTimeout bounds one connection attempt unless the URL sets connect_timeout.
const connectTimeout = 10 * time.Second

// PostgresWarnings lists what is risky about a PostgreSQL setting, for the
// server to log once at startup. It returns nothing for other settings.
func PostgresWarnings(dsn string) []string {
	if !IsPostgresURL(dsn) {
		return nil
	}
	cfg, err := parsePostgres(dsn)
	if err != nil {
		return nil // Open reports it
	}
	if w := plaintextWarning(cfg, explicitlyPlain(dsn)); w != "" {
		return []string{w}
	}
	return nil
}

// explicitlyPlain reports sslmode=disable in the URL itself: the operator chose
// plaintext on purpose (a private network), so it is not warned about.
func explicitlyPlain(dsn string) bool {
	u, err := url.Parse(dsn)
	return err == nil && u.Query().Get("sslmode") == "disable"
}

// plaintextWarning says why the connection may travel unencrypted, or "" when
// it cannot: TLS is required, the server is on this machine, or plaintext was
// asked for explicitly. Every host of a multi-host URL counts.
func plaintextWarning(cfg *pgx.ConnConfig, explicit bool) string {
	if explicit {
		return ""
	}
	targets := []*pgconn.FallbackConfig{{Host: cfg.Host, TLSConfig: cfg.TLSConfig}}
	targets = append(targets, cfg.Fallbacks...)
	for _, t := range targets {
		if t.TLSConfig == nil && !localHost(t.Host) {
			return fmt.Sprintf("the connection to %s may be unencrypted; set sslmode=require (or verify-full) in the database URL", t.Host)
		}
	}
	return ""
}

// localHost: a unix socket directory or a loopback address.
func localHost(host string) bool {
	if strings.HasPrefix(host, "/") || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func openPostgres(ctx context.Context, dsn string, opt Options) (*Store, error) {
	cfg, err := parsePostgres(dsn)
	if err != nil {
		return nil, err
	}
	maxConns := opt.MaxConns
	if maxConns <= 0 {
		maxConns = 10
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	// Idle connections are dropped after a while, so a restarted or failed-over
	// server is picked up without errors piling up on stale connections.
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	ping := stdlib.OpenDB(*cfg)
	ping.SetMaxOpenConns(2)
	ping.SetMaxIdleConns(1)
	ping.SetConnMaxIdleTime(5 * time.Minute)

	s := &Store{dialect: postgres, db: db, rdb: db, ping: ping, batch: postgresBatch}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// migratePostgres applies every pending migration in one transaction. Several
// servers may start at once against the same database, so the transaction
// first takes an advisory lock (keyed by schema, so separate schemas in one
// database do not wait for each other), then re-reads what is applied. A
// replica that waits for another's migration must not time out while it
// waits; only the DDL itself gets a lock timeout, so a migration stuck behind a
// long query fails instead of blocking all traffic.
func (s *Store) migratePostgres(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: connect: %w", err)
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`SET LOCAL statement_timeout = 0`,
		`SELECT pg_advisory_xact_lock(hashtext('ai-usage-monitor:' || current_schema()))`,
		`SET LOCAL lock_timeout = '10s'`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT COLLATE "C" PRIMARY KEY,
			applied_at BIGINT NOT NULL
		)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("store: prepare migrations: %w", err)
		}
	}
	applied, err := appliedVersions(ctx, tx)
	if err != nil {
		return err
	}
	all, err := migrations(postgres.String())
	if err != nil {
		return err
	}
	for _, m := range all {
		if !applied[m.version] {
			if err := applyMigration(ctx, tx, m); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migrations: %w", err)
	}
	return nil
}

// Permanent reports whether a connection failure will not go away by waiting:
// the server rejected the login (class 28), the database does not exist
// (3D000) or the user lacks a privilege (42501).
func Permanent(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return strings.HasPrefix(pgErr.Code, "28") || pgErr.Code == "3D000" || pgErr.Code == "42501"
}

// retryable reports whether a PostgreSQL transaction failed only because of
// concurrency (deadlock, serialization), so running it again can succeed.
func retryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40P01" || pgErr.Code == "40001")
}

// lockDevices takes the device rows of ids before a write touches their events
// (PostgreSQL only). Every write that changes both a device and its events
// locks the device first, so they cannot deadlock on each other, and a purge
// cannot run while an ingest for the same device is in flight: ingest takes a
// shared lock (FOR KEY SHARE, the lock the foreign key takes anyway), purge
// and delete an exclusive one (FOR UPDATE).
func (s *Store) lockDevices(ctx context.Context, tx *sql.Tx, exclusive bool, ids ...string) error {
	if s.dialect != postgres || len(ids) == 0 {
		return nil
	}
	mode := "FOR KEY SHARE"
	if exclusive {
		mode = "FOR UPDATE"
	}
	var b strings.Builder
	args := make([]any, len(ids))
	b.WriteString("SELECT id FROM devices WHERE id IN (")
	for i, id := range ids {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "$%d", i+1)
		args[i] = id
	}
	b.WriteString(") ORDER BY id " + mode)
	rows, err := tx.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return fmt.Errorf("store: lock devices: %w", err)
	}
	rows.Close()
	return rows.Err()
}
