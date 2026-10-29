package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
)

// OpenSource opens the database dsn names for reading only, to copy it
// elsewhere: a SQLite file must already exist and is opened read-only, and
// nothing is migrated. Its schema must be exactly the one this binary
// writes, so every column is known.
func OpenSource(ctx context.Context, dsn string) (*Store, error) {
	var s *Store
	if IsPostgresURL(dsn) {
		cfg, err := parsePostgres(dsn)
		if err != nil {
			return nil, err
		}
		db := stdlib.OpenDB(*cfg)
		db.SetMaxOpenConns(2)
		s = &Store{dialect: postgres, db: db, rdb: db, ping: db}
	} else {
		if err := CheckSQLitePath(dsn); err != nil {
			return nil, err
		}
		if fi, err := os.Stat(dsn); err != nil || fi.IsDir() {
			return nil, fmt.Errorf("store: source database %s does not exist", dsn)
		}
		// mode=ro still lets SQLite read the WAL, so a database whose last
		// writes are not checkpointed yet is copied whole (it needs the
		// directory to be writable for the -shm file).
		// temp_store(MEMORY): the copy's sorting must not need a writable temp
		// directory (the image runs with a read-only root filesystem).
		db, err := sql.Open("sqlite", sqliteURI(dsn)+"mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)&_pragma=temp_store(MEMORY)")
		if err != nil {
			return nil, fmt.Errorf("store: open source: %w", err)
		}
		db.SetMaxOpenConns(1)
		s = &Store{dialect: sqlite, db: db, rdb: db, ping: db}
	}
	if err := s.db.PingContext(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := s.checkSchemaCurrent(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// checkSchemaCurrent refuses a database whose applied migrations are not
// exactly this binary's.
func (s *Store) checkSchemaCurrent(ctx context.Context) error {
	applied, err := appliedVersions(ctx, s.db)
	if err != nil {
		return fmt.Errorf("store: source has no schema this version knows: %w", err)
	}
	all, err := migrations(s.dialect.String())
	if err != nil {
		return err
	}
	var want, got []string
	for _, m := range all {
		want = append(want, m.version)
	}
	for v := range applied {
		got = append(got, v)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return fmt.Errorf("store: the source's schema (%d migrations) is not this version's (%d): "+
			"start this version of the server once on the source first", len(got), len(want))
	}
	return nil
}

// SameDatabase reports whether two settings name the same database: the same
// file (after resolving symlinks), or the same PostgreSQL host, port, database
// and search_path.
func SameDatabase(a, b string) bool {
	if IsPostgresURL(a) != IsPostgresURL(b) {
		return false
	}
	if !IsPostgresURL(a) {
		ra, rb := realPath(a), realPath(b)
		return ra != "" && ra == rb
	}
	ca, errA := parsePostgres(a)
	cb, errB := parsePostgres(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(ca.Host, cb.Host) && ca.Port == cb.Port && ca.Database == cb.Database &&
		ca.RuntimeParams["search_path"] == cb.RuntimeParams["search_path"]
}

func realPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// CopyReport says what Copy did.
type CopyReport struct {
	Devices, Events int64
	// Cleaned counts free-text values whose NUL or invalid UTF-8 had to become
	// U+FFFD because the target (PostgreSQL) cannot store them.
	Cleaned int
}

const deviceCopyCols = `id, name, os, arch, agent_version, token_hash, token_created_at,
	last_seen, last_ingest, revoked_at, resync_requested, hostname`

// ErrTargetNotEmpty is returned by Copy when the target already holds data.
var ErrTargetNotEmpty = errors.New("store: the target database is not empty")

// ErrSourceEmpty is returned by Copy for a source without devices or events
// (most likely the wrong file) unless allowEmpty is set.
var ErrSourceEmpty = errors.New("store: the source database holds no devices and no events")

// Copy copies every device (token hashes included, so enrolled agents keep
// working) and every event from src to dst, which must be empty. The source is
// read in one snapshot and the target written in one transaction, which
// checks both the emptiness and the final counts before it commits: either
// everything arrives or nothing does.
func Copy(ctx context.Context, src, dst *Store, allowEmpty bool) (CopyReport, error) {
	var rep CopyReport
	opts := &sql.TxOptions{ReadOnly: true}
	if src.dialect == postgres {
		opts.Isolation = sql.LevelRepeatableRead // one snapshot for both tables
	}
	stx, err := src.db.BeginTx(ctx, opts)
	if err != nil {
		return rep, fmt.Errorf("store: read source: %w", err)
	}
	defer stx.Rollback()
	var srcDev, srcEv int64
	if err := stx.QueryRowContext(ctx, countBoth).Scan(&srcDev, &srcEv); err != nil {
		return rep, err
	}
	if srcDev == 0 && srcEv == 0 && !allowEmpty {
		return rep, ErrSourceEmpty
	}
	if err := checkSourceIDs(ctx, stx); err != nil {
		return rep, err
	}

	clean := func(s string) string { return s }
	if dst.dialect == postgres {
		clean = func(s string) string {
			c := CleanText(s)
			if c != s {
				rep.Cleaned++
			}
			return c
		}
	}

	err = dst.writeTx(ctx, func(dtx *sql.Tx) error {
		rep = CopyReport{}
		if dst.dialect == postgres {
			// Nothing else may write while the copy runs (a server already
			// pointed at the target, say): its rows would break the count check.
			if _, err := dtx.ExecContext(ctx, `LOCK TABLE devices, usage_events IN EXCLUSIVE MODE`); err != nil {
				return fmt.Errorf("store: lock target: %w", err)
			}
		}
		var n, m int64
		if err := dtx.QueryRowContext(ctx, countBoth).Scan(&n, &m); err != nil {
			return err
		}
		if n+m != 0 {
			return ErrTargetNotEmpty
		}
		size := dst.batchSize()
		w := &stmtTx{tx: dtx, prepare: size.prepare, unnamed: size.unnamed}
		defer w.close()

		rows, err := stx.QueryContext(ctx, `SELECT `+deviceCopyCols+` FROM devices ORDER BY id`)
		if err != nil {
			return fmt.Errorf("store: read devices: %w", err)
		}
		var devs [][]any
		for rows.Next() {
			var (
				id, name, osName, arch, ver, hash, host string
				created, resync                         int64
				seen, ingest, revoked                   sql.NullInt64
			)
			if err := rows.Scan(&id, &name, &osName, &arch, &ver, &hash, &created, &seen, &ingest, &revoked, &resync, &host); err != nil {
				rows.Close()
				return err
			}
			devs = append(devs, []any{id, clean(name), clean(osName), clean(arch), clean(ver), hash, created,
				seen, ingest, revoked, resync, clean(host)})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if err := insertRows(ctx, w, "devices", deviceCopyCols, devs, size.rows); err != nil {
			return err
		}
		rep.Devices = int64(len(devs))

		rows, err = stx.QueryContext(ctx, `SELECT `+eventCols+` FROM usage_events ORDER BY event_id`)
		if err != nil {
			return fmt.Errorf("store: read events: %w", err)
		}
		defer rows.Close()
		var batch [][]any
		flush := func() error {
			err := insertRows(ctx, w, "usage_events", eventCols, batch, size.rows)
			rep.Events += int64(len(batch))
			batch = batch[:0]
			return err
		}
		for rows.Next() {
			var (
				id, dev, src, model, sess, proj, conf, tool string
				ts, in, out, cr, w5, w1, wg, reason, recv   int64
			)
			if err := rows.Scan(&id, &dev, &src, &ts, &model, &sess, &proj, &in, &out, &cr, &w5, &w1, &wg, &reason,
				&conf, &tool, &recv); err != nil {
				return err
			}
			batch = append(batch, []any{id, dev, clean(src), ts, clean(model), sess, clean(proj), in, out, cr, w5, w1, wg, reason,
				clean(conf), clean(tool), recv})
			if len(batch) == 1000 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := flush(); err != nil {
			return err
		}
		// The target must hold exactly what the source snapshot held, checked
		// before commit so a mismatch leaves nothing behind.
		var dstDev, dstEv int64
		if err := dtx.QueryRowContext(ctx, countBoth).Scan(&dstDev, &dstEv); err != nil {
			return err
		}
		if srcDev != dstDev || srcEv != dstEv || dstDev != rep.Devices || dstEv != rep.Events {
			return fmt.Errorf("store: copy incomplete: source %d devices / %d events, target %d / %d", srcDev, srcEv, dstDev, dstEv)
		}
		return nil
	})
	if err != nil {
		return CopyReport{}, err
	}
	return rep, nil
}

// Empty reports whether the database holds no devices and no events.
func (s *Store) Empty(ctx context.Context) (bool, error) {
	var d, e int64
	if err := s.db.QueryRowContext(ctx, countBoth).Scan(&d, &e); err != nil {
		return false, err
	}
	return d == 0 && e == 0, nil
}

const countBoth = `SELECT (SELECT COUNT(*) FROM devices), (SELECT COUNT(*) FROM usage_events)`

// checkSourceIDs refuses a source whose ids hold NUL or invalid UTF-8: they
// cannot be stored in PostgreSQL, and cleaning them could merge two ids.
// Ingest has rejected such ids since PostgreSQL was supported, so only old
// SQLite data can have them.
func checkSourceIDs(ctx context.Context, tx *sql.Tx) error {
	var bad []string
	for _, q := range []struct{ what, sql string }{
		{"device", `SELECT id FROM devices`},
		{"event", `SELECT event_id FROM usage_events`},
		{"session", `SELECT DISTINCT session_id FROM usage_events`},
	} {
		rows, err := tx.QueryContext(ctx, q.sql)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			if !IsCleanText(v) && len(bad) < 10 {
				bad = append(bad, q.what+" "+strconv.Quote(v))
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("store: the source has ids with NUL or invalid UTF-8, which cannot be copied as they are: %s "+
			"(delete those devices' events, or keep using this database)", strings.Join(bad, ", "))
	}
	return nil
}

// insertRows writes rows (each holding one value per column of cols) into
// table, perRow rows per statement.
func insertRows(ctx context.Context, tx *stmtTx, table, cols string, rows [][]any, perRow int) error {
	if len(rows) == 0 {
		return nil
	}
	ncols := len(rows[0])
	for start := 0; start < len(rows); start += perRow {
		part := rows[start:min(start+perRow, len(rows))]
		var b strings.Builder
		args := make([]any, 0, len(part)*ncols)
		b.WriteString("INSERT INTO " + table + "(" + cols + ") VALUES ")
		for n, r := range part {
			if n > 0 {
				b.WriteString(",")
			}
			b.WriteString("(")
			for c := range ncols {
				if c > 0 {
					b.WriteString(",")
				}
				b.WriteString("$" + strconv.Itoa(len(args)+c+1))
			}
			b.WriteString(")")
			args = append(args, r...)
		}
		if _, err := tx.exec(ctx, b.String(), tx.mode(args)...); err != nil {
			return fmt.Errorf("store: copy %s: %w", table, err)
		}
	}
	return nil
}
