// Package collector defines the interface every per-tool log reader implements.
package collector

import (
	"context"

	"ai-usage-monitor/internal/event"
)

// Cursor is opaque per-collector progress state (file offsets, last step idx, ...).
// It is persisted by the agent between runs as raw JSON.
type Cursor []byte

// Collector reads a tool's local logs incrementally.
type Collector interface {
	// Name is the stable identifier used for cursor storage and metrics.
	Name() event.Source
	// Scan returns every event that appeared since cursor, plus the new cursor.
	// A nil cursor means "from the beginning". Scan must be safe to call repeatedly;
	// re-emitting an event is fine because the server is idempotent, missing one is not.
	Scan(ctx context.Context, deviceID string, cursor Cursor) ([]event.UsageEvent, Cursor, error)
}
