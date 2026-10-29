package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

// keyValueDSN spots a libpq "key=value" connection string, which would
// otherwise be taken for a SQLite file name (password included).
var keyValueDSN = regexp.MustCompile(`(^|\s)(host|hostaddr|dbname|user|password|port|sslmode)=`)

// CheckSQLitePath refuses settings that are clearly not a SQLite file path:
// a URL of another scheme (a typo of postgres://, or a mysql:// URL) and a
// libpq key=value string. Either would otherwise create a file named after it.
func CheckSQLitePath(p string) error {
	if strings.Contains(p, "://") {
		return fmt.Errorf("%w: unsupported database URL scheme in %q (use postgres:// or a SQLite file path)",
			ErrConfig, RedactDSN(p))
	}
	if keyValueDSN.MatchString(p) {
		return fmt.Errorf("%w: key=value connection strings are not supported, use a postgres:// URL", ErrConfig)
	}
	return nil
}

// openSQLite opens (creating if needed) the SQLite database at path.
func openSQLite(ctx context.Context, path string) (*Store, error) {
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("store: create db dir: %w%s", err, permHint(err))
			}
		}
		// A new database is created owner-only (it holds project paths and
		// hostnames); SQLite gives its -wal and -shm files the same mode. An
		// existing file keeps the mode its operator chose.
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("store: create db: %w%s", err, permHint(err))
		}
		f.Close()
	}
	dsn := sqliteURI(path) +
		"_pragma=busy_timeout(5000)" +
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

	s := &Store{dialect: sqlite, db: db, rdb: rdb, ping: rdb, batch: sqliteBatch}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// sqliteURI is path as a SQLite "file:" URI ready for parameters: characters
// that the URI form gives a meaning (? starts the parameters, # a fragment,
// % an escape) are percent-encoded, so the file opened is the one named.
func sqliteURI(path string) string {
	return "file:" + strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path) + "?"
}

// permHint explains the usual cause of a permission error: the image runs as
// uid 65532, but a data volume created by an older (root) image is root-owned.
func permHint(err error) string {
	if !errors.Is(err, fs.ErrPermission) {
		return ""
	}
	return fmt.Sprintf(" (running as uid %d: the DB directory and files must be writable by this user; "+
		"a volume created by an older root-run image needs a one-time chown -R 65532:65532, see docs/DEPLOY.md)",
		os.Getuid())
}

// migrateSQLite applies the pending migrations one transaction each. SQLite
// has a single writer, so concurrent servers cannot race here.
func (s *Store) migrateSQLite(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: migrations table: %w", err)
	}
	applied, err := appliedVersions(ctx, s.db)
	if err != nil {
		return err
	}
	all, err := migrations(sqlite.String())
	if err != nil {
		return err
	}
	for _, m := range all {
		if applied[m.version] {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, tx, m); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %s: %w", m.version, err)
		}
	}
	return nil
}
