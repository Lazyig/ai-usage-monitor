// Package claudecode reads Claude Code's JSONL transcripts (~/.claude/projects)
// and turns every assistant message into a normalized usage event.
package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/collector/jsonlscan"
	"ai-usage-monitor/internal/event"
)

// syntheticModel marks locally generated placeholder messages (API errors,
// cancelled turns). They carry no real usage and must not be billed.
const syntheticModel = "<synthetic>"

// Collector reads one Claude Code projects directory.
type Collector struct {
	root string
}

// New returns a collector for root, the Claude Code projects directory
// (normally ~/.claude/projects). A missing root yields empty scans, not errors,
// because most devices only have a subset of the tools installed.
func New(root string) collector.Collector { return &Collector{root: root} }

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

func decodeCursor(raw []byte) cursorState { return jsonlscan.DecodeCursor[fileState](raw) }

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
	sc := scanner{
		Root:    c.root,
		Name:    "claudecode",
		SkipDir: skipDir,
		Line: func(_ *fileState, _ int64, raw []byte) error {
			ev, nativeKey, ok := decodeLine(raw, deviceID)
			if !ok {
				return nil
			}
			if idx, dup := byKey[nativeKey]; dup {
				events[idx] = ev
				return nil
			}
			byKey[nativeKey] = len(events)
			events = append(events, ev)
			return nil
		},
	}

	next, err := sc.Scan(ctx, decodeCursor(cur))
	switch {
	case errors.Is(err, jsonlscan.ErrNoRoot):
		return nil, cur, nil // tool not installed on this device
	case err != nil:
		return events, cur, err
	}

	encoded, err := json.Marshal(next)
	if err != nil {
		return events, cur, fmt.Errorf("claudecode: encode cursor: %w", err)
	}
	return events, encoded, nil
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
