// Package claudecode reads Claude Code's JSONL transcripts (~/.claude/projects,
// or <CLAUDE_CONFIG_DIR>/projects for every extra config directory) and turns
// every assistant message into a normalized usage event.
package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/collector/jsonlscan"
	"ai-usage-monitor/internal/event"
)

// syntheticModel marks locally generated placeholder messages (API errors,
// cancelled turns). They carry no real usage and must not be billed.
const syntheticModel = "<synthetic>"

// Collector reads one or more Claude Code projects directories.
type Collector struct {
	roots []string
}

// New returns a collector for roots, Claude Code projects directories (normally
// ~/.claude/projects, plus one per extra CLAUDE_CONFIG_DIR). Roots may be glob
// patterns; they are expanded on every scan (see collector.ExpandRoots). A
// missing root yields empty scans, not errors, because most devices only have a
// subset of the tools installed.
func New(roots ...string) collector.Collector { return &Collector{roots: roots} }

// Name implements collector.Collector.
func (c *Collector) Name() event.Source { return event.SourceClaudeCode }

// fileState is what the cursor keeps per transcript. Claude Code carries every
// field an event needs on the line itself, so the shared resume point is all
// there is to remember.
type fileState = jsonlscan.FileState

type (
	scanner     = jsonlscan.Scanner[fileState, *fileState]
	cursorState = jsonlscan.Cursor[fileState]
)

// multiCursor is the stored cursor: one jsonlscan cursor per root directory,
// keyed by the root's cleaned path. Keying by path (not position) keeps the
// progress when roots are reordered or moved between root and extra_roots.
type multiCursor struct {
	Roots map[string]cursorState `json:"roots"`
}

// decodeCursor parses a stored cursor. A cursor written before multiple roots
// existed is one bare jsonlscan cursor ({"files": ...}); it belonged to the
// configured root, which is first, so it is handed to that root and nothing is
// re-read after an upgrade. Anything unreadable yields an empty state.
func decodeCursor(raw []byte, first string) map[string]cursorState {
	out := map[string]cursorState{}
	if len(raw) == 0 {
		return out
	}
	var mc multiCursor
	if err := json.Unmarshal(raw, &mc); err == nil && mc.Roots != nil {
		for k, v := range mc.Roots {
			if v.Files != nil {
				out[k] = v
			}
		}
		return out
	}
	if legacy := jsonlscan.DecodeCursor[fileState](raw); len(legacy.Files) > 0 && first != "" {
		out[filepath.Clean(first)] = legacy
	}
	return out
}

// skipDir prunes memory/ subtrees: they only ever hold markdown notes.
func skipDir(name string) bool { return name == "memory" }

// line is the subset of a transcript record we care about. Everything else,
// including all prompt and response text, is deliberately left undecoded.
type line struct {
	Type              string   `json:"type"`
	UUID              string   `json:"uuid"`
	RequestID         string   `json:"requestId"`
	Timestamp         string   `json:"timestamp"`
	SessionID         string   `json:"sessionId"`
	CWD               string   `json:"cwd"`
	Version           string   `json:"version"`
	IsAPIErrorMessage bool     `json:"isApiErrorMessage"`
	Message           *message `json:"message"`
}

type message struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage *usage `json:"usage"`
}

type usage struct {
	InputTokens              int64          `json:"input_tokens"`
	OutputTokens             int64          `json:"output_tokens"`
	CacheCreationInputTokens int64          `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64          `json:"cache_read_input_tokens"`
	CacheCreation            *cacheCreation `json:"cache_creation"`
	OutputTokensDetails      *outputDetails `json:"output_tokens_details"`
}

type cacheCreation struct {
	Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
	Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
}

type outputDetails struct {
	ThinkingTokens int64 `json:"thinking_tokens"`
}

// Scan implements collector.Collector.
//
// Dedupe: streaming writes the same message.id several times inside one file,
// each line carrying a larger usage snapshot than the last. The native key is
// message.id + requestId and the LAST occurrence within a scan wins.
//
// Known and accepted consequence: if the duplicate lines of one message straddle
// two scans (the message was still streaming when we hit EOF), the message is
// emitted once per scan with different token counts. Both events share the same
// EventID, and the server keeps the first write, so the total is never inflated -
// it can only be slightly low for that one message until a later full re-read.
func (c *Collector) Scan(ctx context.Context, deviceID string, cur collector.Cursor) ([]event.UsageEvent, collector.Cursor, error) {
	var (
		events []event.UsageEvent
		byKey  = map[string]int{} // native key -> index in events (last write wins)
	)
	line := func(_ *fileState, _ int64, raw []byte) error {
		ev, nativeKey, ok := decodeLine(raw, deviceID)
		if !ok {
			return nil
		}
		// The key is shared by every root: the same transcript copied into
		// two config directories still yields one event per scan.
		if idx, dup := byKey[nativeKey]; dup {
			events[idx] = ev
			return nil
		}
		byKey[nativeKey] = len(events)
		events = append(events, ev)
		return nil
	}

	var first string
	if len(c.roots) > 0 && !collector.IsGlob(c.roots[0]) {
		first = c.roots[0]
	}
	prev := decodeCursor(cur, first)
	next := map[string]cursorState{}
	var (
		errs  []error
		found bool
	)
	for _, root := range collector.ExpandRoots(c.roots) {
		sc := scanner{Root: root, Name: "claudecode", SkipDir: skipDir, Line: line}
		st, err := sc.Scan(ctx, prev[root])
		switch {
		case errors.Is(err, jsonlscan.ErrNoRoot):
			// Not there (yet, or an unmounted disk): keep what we knew, so a
			// folder that comes back is not re-read from the start.
			if old, ok := prev[root]; ok {
				next[root] = old
			}
			continue
		case ctx.Err() != nil:
			return events, cur, ctx.Err()
		case err != nil:
			errs = append(errs, err)
			if old, ok := prev[root]; ok {
				next[root] = old // retried from the same place next pass
			}
			continue
		}
		found = true
		next[root] = st
	}
	// A folder a pattern matched last time but not now (renamed, unmounted)
	// keeps its progress like a missing plain root does, so it is not re-read
	// from the start when it comes back. Folders no longer covered by any
	// configured root are dropped.
	for key, old := range prev {
		if _, ok := next[key]; !ok && c.matchesPattern(key) {
			next[key] = old
		}
	}
	if !found && len(errs) == 0 && len(next) == 0 {
		return nil, cur, nil // tool not installed on this device
	}

	encoded, err := json.Marshal(multiCursor{Roots: next})
	if err != nil {
		return events, cur, fmt.Errorf("claudecode: encode cursor: %w", err)
	}
	if len(errs) > 0 {
		// Only a root that could not be read at all keeps its old cursor; the
		// others advance, so their events are not sent again next pass.
		return events, encoded, errors.Join(errs...)
	}
	return events, encoded, nil
}

// matchesPattern reports whether dir is one a configured glob pattern can match.
func (c *Collector) matchesPattern(dir string) bool {
	for _, r := range c.roots {
		if collector.IsGlob(r) {
			if ok, _ := filepath.Match(filepath.Clean(r), dir); ok {
				return true
			}
		}
	}
	return false
}

// decodeLine converts one transcript line into an event. ok is false for every
// line that is not a billable assistant message.
func decodeLine(raw []byte, deviceID string) (ev event.UsageEvent, nativeKey string, ok bool) {
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		return ev, "", false // half written or non-JSON line
	}
	switch {
	case l.Type != "assistant", l.Message == nil, l.Message.Usage == nil:
		return ev, "", false
	case l.IsAPIErrorMessage, l.Message.Model == syntheticModel:
		return ev, "", false
	}

	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(l.Timestamp))
	if err != nil {
		return ev, "", false
	}

	// message.id is the real identity; the line uuid is a per-line id that only
	// serves as a fallback when a record was written without a message id.
	msgID := l.Message.ID
	if msgID == "" {
		msgID = l.UUID
	}
	if msgID == "" {
		return ev, "", false
	}

	u := l.Message.Usage
	// input_tokens already excludes cache tokens (Anthropic convention), so the
	// four buckets can be priced independently.
	var write5m, write1h int64
	if u.CacheCreation != nil {
		write5m, write1h = u.CacheCreation.Ephemeral5m, u.CacheCreation.Ephemeral1h
	} else {
		// Older transcripts have only the flat total; 5m is the default TTL.
		write5m = u.CacheCreationInputTokens
	}
	var reasoning int64
	if u.OutputTokensDetails != nil {
		reasoning = u.OutputTokensDetails.ThinkingTokens
	}

	ev = event.UsageEvent{
		EventID:      event.NewEventID(event.SourceClaudeCode, msgID, l.RequestID),
		DeviceID:     deviceID,
		Source:       event.SourceClaudeCode,
		Timestamp:    ts.UTC(),
		Model:        l.Message.Model,
		SessionID:    l.SessionID,
		Project:      l.CWD,
		Input:        nonNeg(u.InputTokens),
		Output:       nonNeg(u.OutputTokens),
		CacheRead:    nonNeg(u.CacheReadInputTokens),
		CacheWrite5m: nonNeg(write5m),
		CacheWrite1h: nonNeg(write1h),
		Reasoning:    nonNeg(reasoning),
		Confidence:   event.ConfidenceExact,
		ToolVersion:  l.Version,
	}
	if ev.Reasoning > ev.Output {
		ev.Reasoning = ev.Output // thinking tokens are a subset of output by contract
	}
	if !ev.Valid() {
		return event.UsageEvent{}, "", false
	}
	return ev, msgID + "\x00" + l.RequestID, true
}

func nonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
