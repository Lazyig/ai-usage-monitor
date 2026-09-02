package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// TokenPrefix marks an agent device token.
const TokenPrefix = "aum_"

// Device is a registered agent machine. The token itself is never stored, only
// its sha256; TokenHash is the hex digest of the presented token string.
type Device struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	OS             string     `json:"os"`
	Arch           string     `json:"arch"`
	AgentVersion   string     `json:"agent_version"`
	TokenCreatedAt time.Time  `json:"token_created_at"`
	LastSeen       *time.Time `json:"last_seen"`
	LastIngest     *time.Time `json:"last_ingest"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	EventsTotal    int64      `json:"events_total"`
}

// DeviceMeta is the self-reported device description an agent sends with ingest.
type DeviceMeta struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	OS           string `json:"os,omitempty"`
	Arch         string `json:"arch,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
}

// HashToken is the storage form of a device token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken mints a random device token ("aum_" + 32 random bytes, base64url).
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// newUUID returns a random (version 4) UUID string without pulling in a dep.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

// CreateDevice registers a device and returns it plus the plaintext token,
// which is the only time the token is ever available.
func (s *Store) CreateDevice(ctx context.Context, name string) (Device, string, error) {
	id, err := newUUID()
	if err != nil {
		return Device{}, "", err
	}
	token, err := NewToken()
	if err != nil {
		return Device{}, "", err
	}
	now := nowMillis()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO devices(id, name, token_hash, token_created_at) VALUES(?,?,?,?)`,
		id, name, HashToken(token), now); err != nil {
		return Device{}, "", fmt.Errorf("store: create device: %w", err)
	}
	return Device{ID: id, Name: name, TokenCreatedAt: fromMillis(now)}, token, nil
}

const deviceCols = `d.id, d.name, d.os, d.arch, d.agent_version, d.token_created_at, d.last_seen, d.last_ingest, d.revoked_at`

func scanDevice(sc interface{ Scan(...any) error }) (Device, error) {
	var (
		d                             Device
		created                       int64
		lastSeen, lastIngest, revoked sql.NullInt64
	)
	if err := sc.Scan(&d.ID, &d.Name, &d.OS, &d.Arch, &d.AgentVersion, &created, &lastSeen, &lastIngest, &revoked); err != nil {
		return Device{}, err
	}
	d.TokenCreatedAt = fromMillis(created)
	d.LastSeen = nullMillis(lastSeen)
	d.LastIngest = nullMillis(lastIngest)
	d.RevokedAt = nullMillis(revoked)
	return d, nil
}

// DeviceByTokenHash looks a device up by the hash of its bearer token. Revoked
// devices are returned too; callers decide (ingest rejects them).
func (s *Store) DeviceByTokenHash(ctx context.Context, hash string) (Device, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.token_hash = ?`, hash)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// Device returns one device by id (without the event count).
func (s *Store) Device(ctx context.Context, id string) (Device, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.id = ?`, id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// ListDevices returns every device with its lifetime event count, newest token first.
func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+deviceCols+`, (SELECT COUNT(*) FROM usage_events e WHERE e.device_id = d.id)
		FROM devices d ORDER BY d.token_created_at DESC, d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var (
			d                             Device
			created                       int64
			lastSeen, lastIngest, revoked sql.NullInt64
			events                        int64
		)
		if err := rows.Scan(&d.ID, &d.Name, &d.OS, &d.Arch, &d.AgentVersion, &created,
			&lastSeen, &lastIngest, &revoked, &events); err != nil {
			return nil, err
		}
		d.TokenCreatedAt = fromMillis(created)
		d.LastSeen = nullMillis(lastSeen)
		d.LastIngest = nullMillis(lastIngest)
		d.RevokedAt = nullMillis(revoked)
		d.EventsTotal = events
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeDevice invalidates the device token. Events are kept; the token hash is
// rewritten so the old token can never be used again and the UNIQUE index stays
// happy if the same name is registered later.
func (s *Store) RevokeDevice(ctx context.Context, id string) error {
	now := nowMillis()
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET revoked_at = COALESCE(revoked_at, ?), token_hash = 'revoked:' || id || ':' || ?
		 WHERE id = ?`, now, now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RotateToken mints a fresh token for an existing device: the old one stops
// working immediately, the device id and every event stored under it stay put.
// This is the safe way to "re-register" a machine - creating a second device
// instead would split one machine's history across two ids.
//
// Like CreateDevice, the plaintext token is returned once and never stored.
// A revoked device is un-revoked by rotating it.
func (s *Store) RotateToken(ctx context.Context, id string) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET token_hash = ?, token_created_at = ?, revoked_at = NULL WHERE id = ?`,
		HashToken(token), nowMillis(), id)
	if err != nil {
		return "", fmt.Errorf("store: rotate token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", ErrNotFound
	}
	return token, nil
}

// PurgeEvents deletes every event of one device and flags it for a re-sync, so
// the next ingest tells the agent to forget its cursors and send everything
// again. The device and its token are untouched.
func (s *Store) PurgeEvents(ctx context.Context, id string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	deleted, err := purgeEventsTx(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE devices SET resync_requested = 1 WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("store: flag resync: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func purgeEventsTx(ctx context.Context, tx *sql.Tx, id string) (int64, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM usage_events WHERE device_id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("store: purge events: %w", err)
	}
	return res.RowsAffected()
}

// DeleteDevice removes a device. purge=false only revokes the token and keeps
// the events (they still count towards the totals, attributed to a device that
// can no longer report). purge=true erases the device for good: its events and
// its row go in one transaction, which is the only way to satisfy the
// usage_events -> devices foreign key.
//
// It returns how many events were deleted (always 0 when purge is false).
func (s *Store) DeleteDevice(ctx context.Context, id string, purge bool) (int64, error) {
	if !purge {
		return 0, s.RevokeDevice(ctx, id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	deleted, err := purgeEventsTx(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("store: delete device: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

// ConsumeResync reports whether a re-sync was requested for the device and
// clears the flag in the same transaction, so exactly one ingest response ever
// carries it: two agent processes (or a retried request) cannot both be told to
// rescan, and a crash between the two statements cannot lose the request.
func (s *Store) ConsumeResync(ctx context.Context, id string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var flag int64
	err = tx.QueryRowContext(ctx, `SELECT resync_requested FROM devices WHERE id = ?`, id).Scan(&flag)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if flag == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET resync_requested = 0 WHERE id = ?`, id); err != nil {
		return false, fmt.Errorf("store: clear resync: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// TouchDevice records agent metadata and heartbeat timestamps. Empty metadata
// fields are left untouched. ingest=true also bumps last_ingest.
func (s *Store) TouchDevice(ctx context.Context, m DeviceMeta, ingest bool, at time.Time) error {
	ms := toMillis(at)
	var lastIngest any
	if ingest {
		lastIngest = ms
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE devices SET
			name          = CASE WHEN ? <> '' THEN ? ELSE name END,
			os            = CASE WHEN ? <> '' THEN ? ELSE os END,
			arch          = CASE WHEN ? <> '' THEN ? ELSE arch END,
			agent_version = CASE WHEN ? <> '' THEN ? ELSE agent_version END,
			last_seen     = ?,
			last_ingest   = COALESCE(?, last_ingest)
		WHERE id = ?`,
		m.Name, m.Name, m.OS, m.OS, m.Arch, m.Arch, m.AgentVersion, m.AgentVersion,
		ms, lastIngest, m.ID)
	return err
}
