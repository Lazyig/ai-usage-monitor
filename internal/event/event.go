// Package event defines the normalized usage record shared by every collector,
// the agent transport and the server.
package event

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Source identifies the tool a usage event came from.
type Source string

const (
	SourceClaudeCode  Source = "claude_code"
	SourceCodex       Source = "codex"
	SourceAntigravity Source = "antigravity"
)

// Confidence says how trustworthy the token numbers are.
type Confidence string

const (
	ConfidenceExact     Confidence = "exact"     // read directly from the tool's own accounting
	ConfidenceEstimated Confidence = "estimated" // derived (e.g. text length), shown with a badge
	ConfidenceUnknown   Confidence = "unknown"   // decoder self-check failed; tokens may be wrong
)

// UsageEvent is one model call. Token fields follow the Anthropic convention:
// Input excludes cache tokens, CacheRead/CacheWrite* are separate, and Output
// includes reasoning. Collectors convert other providers' conventions on the way in.
type UsageEvent struct {
	EventID   string    `json:"event_id"` // sha256(device|source|native key) - idempotency key
	DeviceID  string    `json:"device_id"`
	Source    Source    `json:"source"`
	Timestamp time.Time `json:"ts"` // UTC
	Model     string    `json:"model"`
	SessionID string    `json:"session_id,omitempty"`
	Project   string    `json:"project,omitempty"` // cwd or workspace URI

	Input        int64 `json:"input"`
	Output       int64 `json:"output"`
	CacheRead    int64 `json:"cache_read"`
	CacheWrite5m int64 `json:"cache_write_5m"` // Claude only; other sources put everything in CacheWrite
	CacheWrite1h int64 `json:"cache_write_1h"`
	CacheWrite   int64 `json:"cache_write"`
	Reasoning    int64 `json:"reasoning"` // subset of Output, informational

	Confidence  Confidence `json:"confidence"`
	ToolVersion string     `json:"tool_version,omitempty"`
}

// NewEventID derives the idempotency key. parts are the source-native identifiers
// (e.g. message id + request id) in a stable order.
func NewEventID(device string, source Source, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(device))
	h.Write([]byte{0})
	h.Write([]byte(source))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TotalInput is every prompt token regardless of cache state.
func (e UsageEvent) TotalInput() int64 {
	return e.Input + e.CacheRead + e.CacheWrite5m + e.CacheWrite1h + e.CacheWrite
}

// Valid reports obvious inconsistencies a collector should never emit.
func (e UsageEvent) Valid() bool {
	switch {
	case e.EventID == "", e.DeviceID == "", e.Model == "" && e.Source != SourceAntigravity:
		return false
	case e.Timestamp.IsZero():
		return false
	case e.Input < 0 || e.Output < 0 || e.CacheRead < 0 || e.CacheWrite < 0 || e.CacheWrite5m < 0 || e.CacheWrite1h < 0:
		return false
	case e.Reasoning > e.Output:
		return false
	}
	return strings.TrimSpace(string(e.Source)) != ""
}
