package store

import (
	"context"
	"testing"
	"time"
)

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
