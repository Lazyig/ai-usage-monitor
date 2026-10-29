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
	"strconv"
	"time"
	"unicode/utf8"
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
	// Hostname is what the agent last reported; Name is operator-owned.
	Hostname string `json:"hostname"`
}

// DeviceMeta is the self-reported device description an agent sends with ingest.
type DeviceMeta struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	OS           string `json:"os,omitempty"`
	Arch         string `json:"arch,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
}

// maxMetaLen caps each field an agent reports about its device. They are
// shown in every device list, and a device token holder controls them.
const maxMetaLen = 256

// Clipped returns m with every reported field cut to maxMetaLen bytes.
func (m DeviceMeta) Clipped() DeviceMeta {
	clip := func(s string) string {
		if len(s) <= maxMetaLen {
			return s
		}
		s = s[:maxMetaLen]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1] // do not leave half a character
		}
		return s
	}
	m.Name, m.OS, m.Arch, m.AgentVersion = clip(m.Name), clip(m.OS), clip(m.Arch), clip(m.AgentVersion)
	return m
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
		`INSERT INTO devices(id, name, token_hash, token_created_at) VALUES($1, $2, $3, $4)`,
		id, CleanText(name), HashToken(token), now); err != nil {
		return Device{}, "", fmt.Errorf("store: create device: %w", err)
	}
	return Device{ID: id, Name: CleanText(name), TokenCreatedAt: fromMillis(now)}, token, nil
}

const deviceCols = `d.id, d.name, d.os, d.arch, d.agent_version, d.token_created_at, d.last_seen, d.last_ingest, d.revoked_at, d.hostname`

// scanDevice reads the deviceCols of one row; extra receives any columns the
// query selects after them.
func scanDevice(sc interface{ Scan(...any) error }, extra ...any) (Device, error) {
	var (
		d                             Device
		created                       int64
		lastSeen, lastIngest, revoked sql.NullInt64
	)
	dest := append([]any{&d.ID, &d.Name, &d.OS, &d.Arch, &d.AgentVersion, &created, &lastSeen, &lastIngest, &revoked, &d.Hostname}, extra...)
	if err := sc.Scan(dest...); err != nil {
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
	row := s.rdb.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.token_hash = $1`, hash)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// Device returns one device by id (without the event count).
func (s *Store) Device(ctx context.Context, id string) (Device, error) {
	row := s.rdb.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.id = $1`, id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// ListDevices returns every device with its lifetime event count, newest token first.
func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.rdb.QueryContext(ctx, `
		SELECT `+deviceCols+`, (SELECT COUNT(*) FROM usage_events e WHERE e.device_id = d.id)
		FROM devices d ORDER BY d.token_created_at DESC, d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var events int64
		d, err := scanDevice(rows, &events)
		if err != nil {
			return nil, err
		}
		d.EventsTotal = events
		out = append(out, d)
	}
	return out, rows.Err()
}

// requireRow turns an UPDATE or DELETE that matched no row into ErrNotFound.
func requireRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// flagResyncSQL sets the flag ConsumeResync reads and clears.
const flagResyncSQL = `UPDATE devices SET resync_requested = 1 WHERE id = $1`

// RevokeDevice invalidates the device token. Events are kept; the token hash is
// rewritten to a per-device placeholder so the old token can never be used
// again, and the placeholder stays unique under the token_hash UNIQUE index.
func (s *Store) RevokeDevice(ctx context.Context, id string) error {
	now := nowMillis()
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET revoked_at = COALESCE(revoked_at, $1), token_hash = 'revoked:' || id || ':' || $2
		 WHERE id = $3`, now, strconv.FormatInt(now, 10), id)
	if err != nil {
		return err
	}
	return requireRow(res)
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
		`UPDATE devices SET token_hash = $1, token_created_at = $2, revoked_at = NULL WHERE id = $3`,
		HashToken(token), nowMillis(), id)
	if err != nil {
		return "", fmt.Errorf("store: rotate token: %w", err)
	}
	if err := requireRow(res); err != nil {
		return "", err
	}
	return token, nil
}

// PurgeEvents deletes every event of one device and flags it for a re-sync, so
// the next ingest tells the agent to forget its cursors and send everything
// again. The device and its token are untouched.
func (s *Store) PurgeEvents(ctx context.Context, id string) (int64, error) {
	var deleted int64
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		var err error
		if deleted, err = purgeEventsTx(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, flagResyncSQL, id)
		if err != nil {
			return fmt.Errorf("store: flag resync: %w", err)
		}
		return requireRow(res)
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func purgeEventsTx(ctx context.Context, tx *sql.Tx, id string) (int64, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM usage_events WHERE device_id = $1`, id)
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
	var deleted int64
	err := s.writeTx(ctx, func(tx *sql.Tx) error {
		var err error
		if deleted, err = purgeEventsTx(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("store: delete device: %w", err)
		}
		return requireRow(res)
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// RequestResync flags a device so its next ingest answers `"resync":true` and
// the agent re-reads every log from zero. Nothing is deleted: re-sent events
// are dropped as duplicates or, when a copy is more complete than what was
// stored (a streamed response caught mid-stream), refresh it. See InsertEvents.
func (s *Store) RequestResync(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, flagResyncSQL, id)
	if err != nil {
		return fmt.Errorf("store: flag resync: %w", err)
	}
	return requireRow(res)
}

// ConsumeResync reports whether a re-sync was requested for the device and
// clears the flag in the same statement, so exactly one ingest response ever
// carries it: two agent processes (or a retried request) cannot both be told to
// rescan, whatever the isolation level, and a crash cannot lose the request.
func (s *Store) ConsumeResync(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET resync_requested = 0 WHERE id = $1 AND resync_requested <> 0`, id)
	if err != nil {
		return false, fmt.Errorf("store: clear resync: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	// Nothing was cleared: either no request is pending or there is no such device.
	var one int
	err = s.rdb.QueryRowContext(ctx, `SELECT 1 FROM devices WHERE id = $1`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return false, err
}

// TouchDevice records agent metadata and heartbeat timestamps. Empty metadata
// fields are left untouched. The reported name goes to hostname; it only becomes
// the display name when the operator never gave one. ingest=true also bumps
// last_ingest.
func (s *Store) TouchDevice(ctx context.Context, m DeviceMeta, ingest bool, at time.Time) error {
	ms := toMillis(at)
	var lastIngest any
	if ingest {
		lastIngest = ms
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE devices SET
			name          = CASE WHEN name = '' AND $1 <> '' THEN $1 ELSE name END,
			hostname      = CASE WHEN $1 <> '' THEN $1 ELSE hostname END,
			os            = CASE WHEN $2 <> '' THEN $2 ELSE os END,
			arch          = CASE WHEN $3 <> '' THEN $3 ELSE arch END,
			agent_version = CASE WHEN $4 <> '' THEN $4 ELSE agent_version END,
			last_seen     = $5,
			last_ingest   = COALESCE($6, last_ingest)
		WHERE id = $7`,
		CleanText(m.Name), CleanText(m.OS), CleanText(m.Arch), CleanText(m.AgentVersion),
		ms, lastIngest, m.ID)
	return err
}

// RenameDevice sets the operator-owned display name.
func (s *Store) RenameDevice(ctx context.Context, id, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET name = $1 WHERE id = $2`, CleanText(name), id)
	if err != nil {
		return fmt.Errorf("store: rename device: %w", err)
	}
	return requireRow(res)
}
