package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRotateToken(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	d, old, err := s.CreateDevice(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	created := d.TokenCreatedAt
	time.Sleep(2 * time.Millisecond) // token_created_at has millisecond resolution

	fresh, err := s.RotateToken(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == old {
		t.Fatal("rotate returned the same token")
	}
	if !strings.HasPrefix(fresh, TokenPrefix) {
		t.Fatalf("rotated token %q has no %s prefix", fresh, TokenPrefix)
	}
	if _, err := s.DeviceByTokenHash(ctx, HashToken(old)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old token still resolves: %v", err)
	}
	got, err := s.DeviceByTokenHash(ctx, HashToken(fresh))
	if err != nil {
		t.Fatalf("the new token does not resolve: %v", err)
	}
	if got.ID != d.ID {
		t.Fatalf("rotate moved the device: %s -> %s", d.ID, got.ID)
	}
	if !got.TokenCreatedAt.After(created) {
		t.Errorf("token_created_at = %s, want later than %s", got.TokenCreatedAt, created)
	}

	// Rotating un-revokes: it is how an operator gets a machine reporting again
	// after revoking it, without splitting its history across a second device.
	if err := s.RevokeDevice(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	third, err := s.RotateToken(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.DeviceByTokenHash(ctx, HashToken(third))
	if err != nil {
		t.Fatal(err)
	}
	if got.RevokedAt != nil {
		t.Errorf("revoked_at = %v after a rotate, want nil", got.RevokedAt)
	}

	if _, err := s.RotateToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rotate unknown = %v, want ErrNotFound", err)
	}
}

func TestPurgeEventsKeepsTheDevice(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	d1, d2 := seed(t, s)

	deleted, err := s.PurgeEvents(ctx, d1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted %d events, want the 3 belonging to d1", deleted)
	}
	devs, err := s.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		want := int64(2) // d2 keeps its own events
		if d.ID == d1 {
			want = 0
		}
		if d.EventsTotal != want {
			t.Errorf("device %s has %d events, want %d", d.ID, d.EventsTotal, want)
		}
	}
	if _, err := s.Device(ctx, d1); err != nil {
		t.Fatalf("purge removed the device row: %v", err)
	}

	// The flag is set by the purge, and reading it once clears it.
	got, err := s.ConsumeResync(ctx, d1)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("PurgeEvents did not set the resync flag")
	}
	got, err = s.ConsumeResync(ctx, d1)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("the resync flag survived being consumed")
	}
	// A device nobody purged never asks for a resync.
	if got, err := s.ConsumeResync(ctx, d2); err != nil || got {
		t.Fatalf("ConsumeResync(untouched) = %v, %v", got, err)
	}
	if _, err := s.ConsumeResync(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConsumeResync unknown = %v, want ErrNotFound", err)
	}
	if _, err := s.PurgeEvents(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PurgeEvents unknown = %v, want ErrNotFound", err)
	}
}

func TestDeleteDevice(t *testing.T) {
	ctx := context.Background()

	// purge=false is the old behaviour: revoke, keep the row and the events.
	s := openTest(t)
	d1, _ := seed(t, s)
	deleted, err := s.DeleteDevice(ctx, d1, false)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("a revoke deleted %d events, want 0", deleted)
	}
	got, err := s.Device(ctx, d1)
	if err != nil {
		t.Fatalf("revoke removed the device row: %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("revoked_at is nil after DeleteDevice(purge=false)")
	}
	c, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Events != 5 {
		t.Errorf("events = %d after a revoke, want all 5 kept", c.Events)
	}

	// purge=true erases both the events and the row, in one transaction.
	s2 := openTest(t)
	e1, e2 := seed(t, s2)
	deleted, err = s2.DeleteDevice(ctx, e1, true)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("purge deleted %d events, want 3", deleted)
	}
	if _, err := s2.Device(ctx, e1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the device row survived a purge: %v", err)
	}
	c, err = s2.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Events != 2 || c.Devices != 1 {
		t.Errorf("counts after purge = %+v, want 2 events and 1 device", c)
	}
	if _, err := s2.Device(ctx, e2); err != nil {
		t.Errorf("the other device was purged too: %v", err)
	}

	for _, purge := range []bool{false, true} {
		if _, err := s2.DeleteDevice(ctx, "nope", purge); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteDevice(unknown, purge=%v) = %v, want ErrNotFound", purge, err)
		}
	}
}

// An operator-given name must survive agent heartbeats; the reported hostname
// is kept separately and only fills an empty name.
func TestTouchDeviceKeepsOperatorName(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	dev, _, err := s.CreateDevice(ctx, "home-pc")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchDevice(ctx, DeviceMeta{ID: dev.ID, Name: "DESKTOP-XYZ", OS: "windows"}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Device(ctx, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "home-pc" || got.Hostname != "DESKTOP-XYZ" || got.OS != "windows" {
		t.Fatalf("after touch: name=%q hostname=%q os=%q", got.Name, got.Hostname, got.OS)
	}
	if err := s.RenameDevice(ctx, dev.ID, "living-room"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Device(ctx, dev.ID); got.Name != "living-room" {
		t.Fatalf("rename = %q", got.Name)
	}
	if err := s.RenameDevice(ctx, "missing", "x"); err != ErrNotFound {
		t.Fatalf("rename missing = %v, want ErrNotFound", err)
	}
	list, _ := s.ListDevices(ctx)
	if len(list) != 1 || list[0].Hostname != "DESKTOP-XYZ" {
		t.Fatalf("list = %+v", list)
	}
}
