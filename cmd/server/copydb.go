package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"ai-usage-monitor/internal/store"
)

const copyDBUsage = `usage: ai-usage-server copy-db [-from DSN | -from-env VAR] [-to DSN | -to-env VAR] [-allow-empty]

Copies every device and event from one database to another, for example from
the SQLite file to PostgreSQL. Device tokens are copied as their hashes, so
enrolled agents keep working against the new database.

The target is created (and migrated) if needed and must hold no data. The
source is only read, never migrated; it must be at this version's schema
(start this version of the server on it once first). Stop the server while
copying: events it receives meanwhile would not be copied.

-to defaults to the database the server itself would open (-db flags aside:
DB_PATH, then DATABASE_URL, then data/usage.db). Prefer -from-env/-to-env and
PGPASSWORD over URLs on the command line, which other users can see in ps.

flags:
`

// runCopyDB implements `copy-db`; it returns the exit code.
func runCopyDB(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	flags := flag.NewFlagSet("copy-db", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		from       = flags.String("from", "", "source database: a SQLite file path or a postgres:// URL")
		fromEnv    = flags.String("from-env", "", "read the source from this environment variable instead")
		to         = flags.String("to", "", "target database (default: the server's own setting)")
		toEnv      = flags.String("to-env", "", "read the target from this environment variable instead")
		allowEmpty = flags.Bool("allow-empty", false, "copy a source that holds no devices and no events (refused otherwise: most likely the wrong file)")
	)
	flags.Usage = func() {
		fmt.Fprint(stderr, copyDBUsage)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(code int, format string, a ...any) int {
		fmt.Fprintf(stderr, "copy-db: "+format+"\n", a...)
		return code
	}
	if flags.NArg() > 0 {
		return fail(2, "unexpected argument %q", flags.Arg(0))
	}
	pick := func(value, env, what string) (string, error) {
		switch {
		case value != "" && env != "":
			return "", fmt.Errorf("give -%s or -%s-env, not both", what, what)
		case env != "":
			v := getenv(env)
			if v == "" {
				return "", fmt.Errorf("-%s-env %s is empty", what, env)
			}
			return v, nil
		}
		return value, nil
	}
	src, err := pick(*from, *fromEnv, "from")
	if err != nil {
		return fail(2, "%v", err)
	}
	if src == "" {
		return fail(2, "a source is required (-from or -from-env)")
	}
	dst, err := pick(*to, *toEnv, "to")
	if err != nil {
		return fail(2, "%v", err)
	}
	if dst == "" {
		// The server's own setting, without its guard against an existing
		// SQLite file: copying away from that file is the point.
		db, err := resolveDB("", false, getenv, func(string) bool { return false })
		if err != nil {
			return fail(2, "%v", err)
		}
		dst = db.dsn
	}
	if store.SameDatabase(src, dst) {
		return fail(2, "source and target are the same database (%s)", store.RedactDSN(dst))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	from2, err := store.OpenSource(ctx, src)
	if err != nil {
		return fail(1, "source %s: %v", store.RedactDSN(src), err)
	}
	defer from2.Close()
	// Checked before the target is opened, so a wrong -from creates nothing.
	if empty, err := from2.Empty(ctx); err != nil {
		return fail(1, "source %s: %v", store.RedactDSN(src), err)
	} else if empty && !*allowEmpty {
		return fail(1, "the source %s holds no devices and no events; check -from, or pass -allow-empty", store.RedactDSN(src))
	}
	conns, err := maxConns(getenv)
	if err != nil {
		return fail(2, "%v", err)
	}
	target, err := store.OpenContext(ctx, dst, store.Options{MaxConns: conns})
	if err != nil {
		return fail(1, "target %s: %v", store.RedactDSN(dst), err)
	}
	defer target.Close()

	fmt.Fprintf(stdout, "copying %s -> %s\n", store.RedactDSN(src), store.RedactDSN(dst))
	rep, err := store.Copy(ctx, from2, target, *allowEmpty)
	if errors.Is(err, store.ErrSourceEmpty) {
		return fail(1, "the source %s holds no devices and no events; check -from, or pass -allow-empty", store.RedactDSN(src))
	}
	if errors.Is(err, store.ErrTargetNotEmpty) {
		return fail(1, "the target %s already holds devices or events; copy-db only fills an empty database", store.RedactDSN(dst))
	}
	if err != nil {
		return fail(1, "%v", err)
	}
	fmt.Fprintf(stdout, "copied %d devices and %d events; counts verified\n", rep.Devices, rep.Events)
	if rep.Cleaned > 0 {
		fmt.Fprintf(stdout, "%d text values held NUL or invalid UTF-8 and were stored with U+FFFD in their place\n", rep.Cleaned)
	}
	return 0
}
