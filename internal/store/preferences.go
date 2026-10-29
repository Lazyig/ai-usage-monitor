package store

import (
	"context"
	"database/sql"
	"errors"
)

// Preference returns the stored JSON document name, or ok=false if none was
// saved yet.
func (s *Store) Preference(ctx context.Context, name string) (value string, ok bool, err error) {
	err = s.rdb.QueryRowContext(ctx, `SELECT value FROM preferences WHERE name = $1`, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetPreference stores the JSON document name, replacing what was there. The
// caller validates value; it must be clean text.
func (s *Store) SetPreference(ctx context.Context, name, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO preferences(name, value, updated_at) VALUES($1, $2, $3)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		name, CleanText(value), nowMillis())
	return err
}
