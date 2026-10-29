package main

import (
	"strings"
	"testing"
)

func TestResolveDB(t *testing.T) {
	const pg = "postgres://u:secret@db/aum"
	cases := []struct {
		name       string
		flag       string
		flagSet    bool
		env        map[string]string
		sqliteFile bool
		dsn        string
		source     string
		note       bool
		err        string
	}{
		{name: "default", dsn: defaultDBPath, source: "default"},
		{name: "flag", flag: "/x.db", flagSet: true, env: map[string]string{"DB_PATH": "/y.db"}, dsn: "/x.db", source: "-db"},
		{name: "DB_PATH", env: map[string]string{"DB_PATH": "/data/usage.db"}, dsn: "/data/usage.db", source: "DB_PATH"},
		{name: "DATABASE_URL", env: map[string]string{"DATABASE_URL": pg}, dsn: pg, source: "DATABASE_URL"},
		// An explicit setting wins over an injected DATABASE_URL, with a warning.
		{name: "DB_PATH beats DATABASE_URL", env: map[string]string{"DB_PATH": "/data/usage.db", "DATABASE_URL": pg},
			dsn: "/data/usage.db", source: "DB_PATH", note: true},
		{name: "foreign DATABASE_URL ignored", env: map[string]string{"DB_PATH": "/d.db", "DATABASE_URL": "mysql://x"},
			dsn: "/d.db", source: "DB_PATH", note: true},
		{name: "DATABASE_URL not postgres", env: map[string]string{"DATABASE_URL": "mysql://u:secret@x/y"}, err: "not a postgres"},
		{name: "DATABASE_URL key=value", env: map[string]string{"DATABASE_URL": "host=db password=secret"}, err: "not a postgres"},
		// A server that used the default SQLite file does not silently switch.
		{name: "DATABASE_URL over existing sqlite", env: map[string]string{"DATABASE_URL": pg}, sqliteFile: true, err: "exists"},
		{name: "deliberate switch", env: map[string]string{"DB_PATH": pg}, sqliteFile: true, dsn: pg, source: "DB_PATH"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveDB(c.flag, c.flagSet, func(k string) string { return c.env[k] }, func(string) bool { return c.sqliteFile })
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				if strings.Contains(err.Error(), "secret") {
					t.Errorf("error leaks the password: %v", err)
				}
				return
			}
			if err != nil || got.dsn != c.dsn || got.source != c.source || (len(got.notes) > 0) != c.note {
				t.Fatalf("got %+v, %v; want dsn %q source %q note %v", got, err, c.dsn, c.source, c.note)
			}
		})
	}
}

func TestMaxConns(t *testing.T) {
	for v, want := range map[string]int{"": 0, "1": 1, "25": 25} {
		if n, err := maxConns(func(string) string { return v }); err != nil || n != want {
			t.Errorf("%q: %d, %v", v, n, err)
		}
	}
	for _, v := range []string{"0", "-3", "ten", "1.5"} {
		if _, err := maxConns(func(string) string { return v }); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
}
