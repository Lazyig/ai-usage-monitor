package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"ai-usage-monitor/internal/store"
)

// defaultDBPath is the SQLite file used when nothing names a database.
const defaultDBPath = "data/usage.db"

// dbSetting is the database the server opens and where that came from.
type dbSetting struct {
	dsn    string
	source string   // "-db", "DB_PATH", "DATABASE_URL" or "default"
	notes  []string // warnings to log once the logger exists
}

// resolveDB picks the database: the -db flag, then DB_PATH, then DATABASE_URL,
// then the default SQLite file. DATABASE_URL is what hosting platforms inject
// when a PostgreSQL add-on is attached, so it is only consulted when nothing
// more specific is set, and an explicit setting wins over it with a warning
// rather than an error (it may belong to another service in a shared env).
func resolveDB(flagValue string, flagSet bool, getenv func(string) string, exists func(string) bool) (dbSetting, error) {
	dbURL := getenv("DATABASE_URL")
	pick := func(dsn, source string) dbSetting {
		s := dbSetting{dsn: dsn, source: source}
		if dbURL != "" && dbURL != dsn {
			s.notes = append(s.notes, "DATABASE_URL is set but ignored because "+source+" is set")
		}
		return s
	}
	switch {
	case flagSet && flagValue != "":
		return pick(flagValue, "-db"), nil
	case getenv("DB_PATH") != "":
		return pick(getenv("DB_PATH"), "DB_PATH"), nil
	case dbURL != "":
		if !store.IsPostgresURL(dbURL) {
			return dbSetting{}, fmt.Errorf("DATABASE_URL is not a postgres:// URL; set DB_PATH for a SQLite file")
		}
		// A server that ran on the default SQLite file must not switch to an
		// empty PostgreSQL database just because DATABASE_URL appeared.
		if exists(defaultDBPath) {
			return dbSetting{}, fmt.Errorf("DATABASE_URL is set, but the SQLite database %s exists: set DB_PATH=%s to keep using it, "+
				"or set DB_PATH to the PostgreSQL URL to switch deliberately", defaultDBPath, defaultDBPath)
		}
		return dbSetting{dsn: dbURL, source: "DATABASE_URL"}, nil
	}
	return dbSetting{dsn: defaultDBPath, source: "default"}, nil
}

// maxConns reads DB_MAX_CONNS (PostgreSQL pool size; default 10).
func maxConns(getenv func(string) string) (int, error) {
	v := getenv("DB_MAX_CONNS")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("DB_MAX_CONNS must be a positive integer (got %q)", v)
	}
	return n, nil
}

// startupWait is how long the server keeps trying to reach PostgreSQL at
// start, so it does not crash-loop while the database container starts.
const startupWait = 60 * time.Second

// openStore opens the database. PostgreSQL gets retried for startupWait
// unless the setting itself is wrong or the server refused for good (bad
// password, no such database); ctx (SIGTERM) ends the wait. Each attempt
// ends within the connect timeout, so the whole wait stays near startupWait.
func openStore(ctx context.Context, log *slog.Logger, dsn string, opt store.Options) (*store.Store, error) {
	if !store.IsPostgresURL(dsn) {
		return store.OpenContext(ctx, dsn, opt)
	}
	deadline := time.Now().Add(startupWait)
	delay := time.Second
	for {
		st, err := store.OpenContext(ctx, dsn, opt)
		if err == nil || errors.Is(err, store.ErrConfig) || store.Permanent(err) || ctx.Err() != nil || time.Now().After(deadline) {
			return st, err
		}
		log.Warn("database not reachable yet, retrying", "in", delay, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
