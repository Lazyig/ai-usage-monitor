// Package codex reads Codex CLI rollout transcripts (~/.codex/sessions) and
// turns every token_count event into a normalized usage event.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

// unknownModel is used for token_count lines that precede the first
// turn_context and whose session_meta carries no model.
const unknownModel = "unknown"

// Collector reads one Codex sessions directory.
type Collector struct {
	root string
}

// New returns a collector for root, the Codex sessions directory (normally
// ~/.codex/sessions). A missing root yields empty scans, not errors.
func New(root string) collector.Collector { return &Collector{root: root} }

// Name implements collector.Collector.
func (c *Collector) Name() event.Source { return event.SourceCodex }

// line is the envelope shared by every rollout record.
type line struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMeta struct {
	ID             string `json:"id"`
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	CLIVersion     string `json:"cli_version"`
	ThreadSource   string `json:"thread_source"`    // user | subagent | guardian_review
	ParentThreadID string `json:"parent_thread_id"` // set for sub-agent sessions
	Model          string `json:"model"`
}

type turnContext struct {
	Model string `json:"model"`
}

// eventMsg covers the token_count payload.
//
// payload.rate_limits is present on these lines and is NOT emitted yet. The
// fields available for the planned plan-usage view are:
//
//	rate_limits.primary.used_percent    float
//	rate_limits.primary.window_minutes  int   (10080 = weekly, 43200 = monthly)
//	rate_limits.primary.resets_at       int64 (unix seconds)
//	rate_limits.plan_type               string ("free", "plus", ...)
//
// plus rate_limits.secondary with the same primary shape and rate_limits.credits.
type eventMsg struct {
	Type string     `json:"type"`
	Info *tokenInfo `json:"info"` // null on some token_count lines
}

type tokenInfo struct {
	LastTokenUsage *tokenUsage `json:"last_token_usage"`
}

// tokenUsage follows the OpenAI convention: InputTokens INCLUDES CachedInput,
// and OutputTokens INCLUDES ReasoningOutput.
type tokenUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	CacheWriteTokens  int64 `json:"cache_write_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	ReasoningTokens   int64 `json:"reasoning_output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
}

func (u tokenUsage) isZero() bool {
	return u.InputTokens == 0 && u.CachedInputTokens == 0 && u.CacheWriteTokens == 0 &&
		u.OutputTokens == 0 && u.ReasoningTokens == 0 && u.TotalTokens == 0
}

// Scan implements collector.Collector.
//
// The native dedupe key is session id + the byte offset of the token_count line
// inside its file. Rollout files are append-only, so a line never moves and the
// key is stable across scans and across a full re-read after truncation.
func (c *Collector) Scan(ctx context.Context, deviceID string, cur collector.Cursor) ([]event.UsageEvent, collector.Cursor, error) {
	state := decodeCursor(cur)

	files, err := jsonlFiles(c.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, cur, nil // tool not installed on this device
		}
		return nil, cur, fmt.Errorf("codex: walk %s: %w", c.root, err)
	}

	next := cursorState{Files: make(map[string]fileState, len(files))}
	var (
		events  []event.UsageEvent
		fileErr []error
		okFiles int
	)

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return events, cur, err
		}

		key := cursorKey(c.root, path)
		fi, err := os.Stat(path)
		if err != nil {
			fileErr = append(fileErr, fmt.Errorf("codex: stat %s: %w", path, err))
			continue
		}
		ino, hasIno := fileIdentity(fi)
		prev, seen := state.Files[key]
		start := startOffset(prev, seen, fi, ino, hasIno)

		// Session context either resumes from the cursor or is rebuilt from the
		// session_meta / turn_context lines at the top of the file.
		st := fileState{}
		if start > 0 {
			st = prev
		}

		end, err := readAppended(path, start, func(offset int64, raw []byte) error {
			if ev, ok := decodeLine(raw, offset, deviceID, &st); ok {
				events = append(events, ev)
			}
			return nil
		})
		if err != nil {
			fileErr = append(fileErr, fmt.Errorf("codex: read %s: %w", path, err))
		} else {
			okFiles++
		}

		st.Offset = end
		st.Size = fi.Size()
		st.ModNS = fi.ModTime().UnixNano()
		st.Inode, st.HasIno = ino, hasIno
		next.Files[key] = st
	}

	if okFiles == 0 && len(fileErr) > 0 {
		return events, cur, errors.Join(fileErr...)
	}

	encoded, err := json.Marshal(next)
	if err != nil {
		return events, cur, fmt.Errorf("codex: encode cursor: %w", err)
	}
	return events, encoded, nil
}

// cursorKey is the stable, OS independent cursor map key for path.
func cursorKey(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return filepath.ToSlash(rel)
}

// decodeLine folds one rollout line into st (session metadata, current model)
// and returns an event when the line is a usable token_count.
func decodeLine(raw []byte, offset int64, deviceID string, st *fileState) (event.UsageEvent, bool) {
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		return event.UsageEvent{}, false
	}

	switch l.Type {
	case "session_meta":
		var m sessionMeta
		if err := json.Unmarshal(l.Payload, &m); err != nil {
			return event.UsageEvent{}, false
		}
		st.SessionID = m.ID
		if st.SessionID == "" {
			st.SessionID = m.SessionID
		}
		st.CWD = m.CWD
		st.CLIVersion = m.CLIVersion
		if m.Model != "" && st.Model == "" {
			st.Model = m.Model
		}
		return event.UsageEvent{}, false

	case "turn_context":
		// The model can change between turns; the most recent turn_context wins
		// for every token_count that follows it.
		var tc turnContext
		if err := json.Unmarshal(l.Payload, &tc); err == nil && tc.Model != "" {
			st.Model = tc.Model
		}
		return event.UsageEvent{}, false

	case "event_msg":
		var em eventMsg
		if err := json.Unmarshal(l.Payload, &em); err != nil {
			return event.UsageEvent{}, false
		}
		if em.Type != "token_count" || em.Info == nil || em.Info.LastTokenUsage == nil {
			return event.UsageEvent{}, false // info:null lines carry no usage
		}
		u := *em.Info.LastTokenUsage
		if u.isZero() {
			return event.UsageEvent{}, false // heartbeat with no turn behind it
		}

		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(l.Timestamp))
		if err != nil {
			return event.UsageEvent{}, false
		}

		model := st.Model
		if model == "" {
			model = unknownModel
		}

		// OpenAI -> our convention: input includes cached, so subtract it out.
		input := u.InputTokens - u.CachedInputTokens
		if input < 0 {
			input = 0
		}

		ev := event.UsageEvent{
			EventID:     event.NewEventID(deviceID, event.SourceCodex, st.SessionID, strconv.FormatInt(offset, 10)),
			DeviceID:    deviceID,
			Source:      event.SourceCodex,
			Timestamp:   ts.UTC(),
			Model:       model,
			SessionID:   st.SessionID,
			Project:     st.CWD,
			Input:       input,
			Output:      nonNeg(u.OutputTokens),
			CacheRead:   nonNeg(u.CachedInputTokens),
			CacheWrite:  nonNeg(u.CacheWriteTokens),
			Reasoning:   nonNeg(u.ReasoningTokens),
			Confidence:  event.ConfidenceExact,
			ToolVersion: st.CLIVersion,
		}
		if ev.Reasoning > ev.Output {
			ev.Reasoning = ev.Output // reasoning is a subset of output by contract
		}
		if !ev.Valid() {
			return event.UsageEvent{}, false
		}
		return ev, true
	}

	return event.UsageEvent{}, false
}

func nonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
