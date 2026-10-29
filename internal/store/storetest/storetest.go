// Package storetest gives tests a fresh, empty database to open with
// store.Open. It imports nothing from store, so store's own tests can use it.
package storetest

import (
	"path/filepath"
	"testing"
)

// DSN returns the location of a new database that lives as long as the test.
func DSN(t testing.TB) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}
