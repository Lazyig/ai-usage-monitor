// Package storetest gives tests a fresh, empty database to open with
// store.Open. It imports nothing from store, so store's own tests can use it.
//
// By default that is a SQLite file in the test's temp directory. With
// AUM_TEST_POSTGRES_URL set (a postgres:// URL of a database the tests may
// create schemas in), every test gets its own PostgreSQL schema instead, so
// the same suites run on both backends.
package storetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx"
)

// EnvPostgres names the variable that switches the tests to PostgreSQL.
const EnvPostgres = "AUM_TEST_POSTGRES_URL"

// Postgres reports whether the tests run against PostgreSQL.
func Postgres() bool { return os.Getenv(EnvPostgres) != "" }

// SkipPostgres skips a test that checks SQLite specifics (files, modes, pools).
func SkipPostgres(t testing.TB) {
	t.Helper()
	if Postgres() {
		t.Skip("SQLite only")
	}
}

// DSN returns the location of a new, empty database that lives as long as the
// test: a SQLite file path, or a postgres:// URL whose search_path is a schema
// created for this test and dropped when it ends.
func DSN(t testing.TB) string {
	t.Helper()
	base := os.Getenv(EnvPostgres)
	if base == "" {
		return filepath.Join(t.TempDir(), "test.db")
	}
	var b [6]byte
	rand.Read(b[:])
	schema := "t_" + hex.EncodeToString(b[:])
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}
	// Registered first, so it runs after the test closed its store.
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
