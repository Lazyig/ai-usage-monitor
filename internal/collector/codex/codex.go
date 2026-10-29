// Package codex reads Codex CLI rollout transcripts (~/.codex/sessions) and
// turns every token_count event into a normalized usage event.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/collector/jsonlscan"
	"ai-usage-monitor/internal/event"
)

// Collector reads one Codex sessions directory.
type Collector struct {
	root string
}

// New returns a collector for root, the Codex sessions directory (normally
// ~/.codex/sessions). A missing root yields empty scans, not errors.
func New(root string) collector.Collector { return &Collector{root: root} }

// Name implements collector.Collector.
func (c *Collector) Name() event.Source { return event.SourceCodex }

// fileState is the per-file cursor entry: the shared resume point plus the
// session context.
//
// The session fields are carried in the cursor on purpose: session_meta and
// turn_context appear once near the top of a rollout file, so a resumed scan
// that only reads appended bytes would otherwise have no model, cwd or session
// id for the token_count lines it finds. Embedding jsonlscan.FileState keeps
// them all in one flat JSON object, exactly as older agents wrote it.
type fileState struct {
	jsonlscan.FileState

	SessionID  string `json:"session_id,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	CLIVersion string `json:"cli_version,omitempty"`
	Model      string `json:"model,omitempty"` // most recent turn_context model
}

type (
	scanner     = jsonlscan.Scanner[fileState, *fileState]
	cursorState = jsonlscan.Cursor[fileState]
)

func decodeCursor(raw []byte) cursorState { return jsonlscan.DecodeCursor[fileState](raw) }

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
	Model          string `json:"model"`            // older CLIs only

	// Newer CLIs and Codex Desktop name the session's starting model here
	// instead ({"provenance":{"type":"model","model":"gpt-6.1-sol"}}).
	BaseInstructions *struct {
		Provenance *struct {
			Type  string `json:"type"`
			Model string `json:"model"`
		} `json:"provenance"`
	} `json:"base_instructions"`
}

// startModel is the model the session started on, from whichever field this
// CLI version writes.
func (m sessionMeta) startModel() string {
	if m.Model != "" {
		return m.Model
	}
	if bi := m.BaseInstructions; bi != nil && bi.Provenance != nil && (bi.Provenance.Type == "" || bi.Provenance.Type == "model") {
		return bi.Provenance.Model
	}
	return ""
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
//
// The thread_settings_applied payload is read for its model: Codex Desktop
// writes it when a session starts or switches model, often with no
// turn_context at all.
type eventMsg struct {
	Type           string     `json:"type"`
	Info           *tokenInfo `json:"info"` // null on some token_count lines
	ThreadSettings *struct {
		Model string `json:"model"`
	} `json:"thread_settings"`
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
	var events []event.UsageEvent
	sc := scanner{
		Root: c.root,
		Name: "codex",
		// Codex compresses finished rollouts in place (rollout-*.jsonl.zst).
		Compressed: true,
		Line: func(st *fileState, offset int64, raw []byte) error {
			if ev, ok := decodeLine(raw, offset, deviceID, st); ok {
				events = append(events, ev)
			}
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
		return events, cur, fmt.Errorf("codex: encode cursor: %w", err)
	}
	return events, encoded, nil
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
		if model := m.startModel(); model != "" && st.Model == "" {
			st.Model = model
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
		if em.Type == "thread_settings_applied" {
			// Like turn_context: the latest applied model covers what follows.
			if em.ThreadSettings != nil && em.ThreadSettings.Model != "" {
				st.Model = em.ThreadSettings.Model
			}
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

		// token_count lines can precede every record naming a model
		// (session_meta, thread_settings_applied, turn_context).
		model := st.Model
		if model == "" {
			model = event.UnknownModel
		}

		// OpenAI -> our convention: input includes cached, so subtract it out.
		input := u.InputTokens - u.CachedInputTokens
		if input < 0 {
			input = 0
		}

		ev := event.UsageEvent{
			EventID:     event.NewEventID(event.SourceCodex, st.SessionID, strconv.FormatInt(offset, 10)),
			DeviceID:    deviceID,
			Source:      event.SourceCodex,
			Timestamp:   ts.UTC(),
			Model:       model,
			SessionID:   st.SessionID,
			Project:     st.CWD,
			Input:       input,
			Output:      max(u.OutputTokens, 0),
			CacheRead:   max(u.CachedInputTokens, 0),
			CacheWrite:  max(u.CacheWriteTokens, 0),
			Reasoning:   max(u.ReasoningTokens, 0),
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
