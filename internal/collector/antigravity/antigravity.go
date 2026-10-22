// Package antigravity reads the Antigravity CLI's local conversation databases
// (~/.gemini/antigravity-cli) and turns every model call into a UsageEvent.
//
// Layout and field numbers come from the reverse engineering recorded in
// PLAN.md §6 (scripts under research/antigravity/). In short:
//
//	<root>/conversations/<conversation-id>.db   one SQLite file per conversation
//	  gen_metadata(idx, data BLOB)              one row per model call, tokens in
//	                                            a protobuf blob (PLAN.md §6.2)
//	  steps(idx, step_type, metadata BLOB)      step timestamps + a copy of the
//	                                            last call's usage (PLAN.md §6.3)
//	<root>/conversation_summaries.db            conversation -> workspace URIs
//
// Two safety rules shape the implementation:
//
//   - The blobs contain full system prompts, user messages and tool output
//     (1.1, 1.2.*, 1.8.*, 1.16.*). Only numeric fields and short ids are ever
//     read, never those paths, and no raw blob leaves the process.
//   - The CLI keeps the files open while running, so they are opened read-only
//     and immutable and a file that fails to open is skipped, not fatal.
package antigravity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"

	_ "modernc.org/sqlite" // pure Go driver, registered as "sqlite" (no cgo)
)

// internalModel labels calls that only exist in steps.metadata.9 (title and
// summary generation, step_type 23) or in gen rows that lost their model id.
// PLAN.md §6.4: 128 such calls on the reference machine.
const internalModel = "antigravity-internal"

// Collector implements collector.Collector for the Antigravity CLI.
type Collector struct {
	root string

	mu    sync.Mutex
	diags []Diagnostic
}

// New returns a collector reading root, normally ~/.gemini/antigravity-cli.
// A missing root is not an error, Scan just returns no events.
func New(root string) collector.Collector { return &Collector{root: root} }

// Name implements collector.Collector.
func (c *Collector) Name() event.Source { return event.SourceAntigravity }

// Diagnostic records the self-checks of PLAN.md §6.6 for one conversation.
// A Diagnostic is only produced when at least one check failed; every event of
// that conversation is then emitted with Confidence=unknown.
type Diagnostic struct {
	ConversationID    string   `json:"conversation_id"`
	Path              string   `json:"path"`
	GenRows           int      `json:"gen_rows"`
	DecodeFailures    int      `json:"decode_failures"`
	IdentityFailures  int      `json:"identity_failures"`  // 1.4.3 != 1.4.9 + 1.4.10
	MonotonicChecks   int      `json:"monotonic_checks"`   // comparisons made
	MonotonicFailures int      `json:"monotonic_failures"` // prompt total dropped >10%
	JoinAttempts      int      `json:"join_attempts"`
	JoinFailures      int      `json:"join_failures"` // gen.4 not found in steps.12
	UnknownModels     []string `json:"unknown_models,omitempty"`
	Reasons           []string `json:"reasons"`
}

// Diagnostics returns the self-check failures of the most recent Scan.
//
// The agent holds a collector.Collector, so a caller that wants to log these
// has to ask for them through a type assertion:
//
//	if d, ok := c.(interface{ Diagnostics() []antigravity.Diagnostic }); ok {
//	        for _, x := range d.Diagnostics() { log(x) }
//	}
//
// Nothing in the agent does that today; the events themselves already carry the
// verdict as Confidence=unknown, and these are for investigating why.
func (c *Collector) Diagnostics() []Diagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Diagnostic, len(c.diags))
	copy(out, c.diags)
	return out
}

// fileCursor is the per-conversation resume point. It stays deliberately small:
// request ids are not persisted, re-emitting a few events is harmless because
// the server deduplicates on EventID.
type fileCursor struct {
	GenIdx  int64 `json:"gen_idx"`  // highest gen_metadata.idx already emitted
	StepIdx int64 `json:"step_idx"` // highest steps.idx already emitted
	Size    int64 `json:"size"`
	ModTime int64 `json:"mtime"` // unix nanoseconds
}

type cursorState struct {
	Files map[string]fileCursor `json:"files"`
}

const freshIdx = int64(-1) // idx values start at 0, so -1 means "nothing yet"

func parseCursor(c collector.Cursor) cursorState {
	st := cursorState{Files: map[string]fileCursor{}}
	if len(c) == 0 {
		return st
	}
	var parsed cursorState
	if err := json.Unmarshal(c, &parsed); err != nil || parsed.Files == nil {
		return st // unreadable cursor: start over, the server dedupes
	}
	return parsed
}

// Scan implements collector.Collector. It walks every conversation database,
// emits the calls newer than the cursor and returns the updated cursor.
func (c *Collector) Scan(ctx context.Context, deviceID string, cur collector.Cursor) ([]event.UsageEvent, collector.Cursor, error) {
	state := parseCursor(cur)
	next := cursorState{Files: map[string]fileCursor{}}
	var events []event.UsageEvent
	var diags []Diagnostic

	defer func() {
		c.mu.Lock()
		c.diags = diags
		c.mu.Unlock()
	}()

	paths := conversationFiles(c.root)
	if len(paths) == 0 {
		// Tool not installed on this device: empty result, no error.
		return nil, marshalCursor(next, cur), nil
	}

	projects := loadProjects(c.root)

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return events, marshalCursor(next, cur), err
		}
		name := filepath.Base(path)
		convID := strings.TrimSuffix(name, ".db")
		prev, ok := state.Files[name]
		if !ok {
			prev = fileCursor{GenIdx: freshIdx, StepIdx: freshIdx}
		}
		evs, fc, diag, err := c.scanConversation(ctx, deviceID, path, convID, projects[convID], prev)
		if err != nil {
			// Unreadable conversation (locked, corrupt, mid-write): keep the
			// old cursor so the next run retries it, and carry on.
			next.Files[name] = prev
			continue
		}
		events = append(events, evs...)
		next.Files[name] = fc
		if diag != nil {
			diags = append(diags, *diag)
		}
	}
	return events, marshalCursor(next, cur), nil
}

func marshalCursor(st cursorState, fallback collector.Cursor) collector.Cursor {
	b, err := json.Marshal(st)
	if err != nil {
		return fallback
	}
	return b
}

// conversationFiles lists <root>/conversations/*.db. A missing or unreadable
// directory yields no files and no error: PLAN.md §2.5 requires a device
// without the tool installed to produce an empty result, not a failure.
func conversationFiles(root string) []string {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(root, "conversations"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		out = append(out, filepath.Join(root, "conversations", e.Name()))
	}
	sort.Strings(out)
	return out
}

// openRO opens a SQLite file read-only and immutable, which is what makes it
// safe to read while the CLI holds the file open (PLAN.md §6.1).
//
// The conversation files are WAL databases. immutable=1 makes SQLite ignore
// any -wal companion, so calls the CLI has written but not yet checkpointed
// are invisible until a later scan; that costs a little freshness and buys
// never taking a lock the CLI could trip over. A file that is being rewritten
// underneath us may fail to open or query, in which case the caller keeps the
// old cursor and retries next time.
func openRO(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", roDSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// roDSN builds the SQLite URI. Windows paths are converted to the forward
// slash form SQLite expects, and characters with a meaning in URIs are escaped.
func roDSN(path string) string {
	p := filepath.ToSlash(path)
	// SQLite URIs only decode %XX; escape the delimiters that would otherwise
	// terminate the path. url.PathEscape would also escape '/' segments' safe
	// characters differently, so do it by hand.
	r := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23", " ", "%20")
	p = r.Replace(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/x -> /C:/x, the form SQLite documents for Windows
	}
	return "file://" + p + "?mode=ro&immutable=1"
}

// loadProjects reads <root>/conversation_summaries.db, mapping conversation id
// to the first workspace URI (PLAN.md §6.5, Project column). Absent or
// unreadable summaries simply mean "no project".
func loadProjects(root string) map[string]string {
	out := map[string]string{}
	if root == "" {
		return out
	}
	path := filepath.Join(root, "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		return out
	}
	db, err := openRO(path)
	if err != nil {
		return out
	}
	defer db.Close()
	rows, err := db.Query(`SELECT conversation_id, workspace_uris FROM conversation_summaries`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, uris sql.NullString
		if err := rows.Scan(&id, &uris); err != nil {
			return out
		}
		if !id.Valid || id.String == "" {
			continue
		}
		if u := firstWorkspaceURI(uris.String); u != "" {
			out[id.String] = u
		}
	}
	return out
}

// firstWorkspaceURI takes the first entry of the JSON array stored in
// conversation_summaries.workspace_uris (e.g. `["file:///home/me/proj"]`).
func firstWorkspaceURI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return ""
	}
	for _, u := range list {
		if u = strings.TrimSpace(u); u != "" {
			return u
		}
	}
	return ""
}

// scanConversation reads one conversation database.
//
// Rows are always read in full even when the cursor is ahead, because two
// things need whole-file knowledge: the step UUID -> timestamp join map, and
// the set of gen request ids that tells apart the steps-only calls. Only the
// event emission is limited to rows past the cursor. The steps.metadata and
// gen_metadata.data blobs are the only columns fetched; step_payload (the bulk
// of an 80 MB file) is never touched.
func (c *Collector) scanConversation(ctx context.Context, deviceID, path, convID, project string, prev fileCursor) ([]event.UsageEvent, fileCursor, *Diagnostic, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, prev, nil, err
	}
	next := fileCursor{GenIdx: prev.GenIdx, StepIdx: prev.StepIdx, Size: info.Size(), ModTime: info.ModTime().UnixNano()}
	// Truncated or rewound file: the idx space may have been reused, so read
	// everything again (the server deduplicates).
	if info.Size() < prev.Size || info.ModTime().UnixNano() < prev.ModTime {
		next.GenIdx, next.StepIdx = freshIdx, freshIdx
		prev = fileCursor{GenIdx: freshIdx, StepIdx: freshIdx}
	}

	db, err := openRO(path)
	if err != nil {
		return nil, prev, nil, err
	}
	defer db.Close()

	steps, err := readSteps(ctx, db)
	if err != nil {
		return nil, prev, nil, err
	}
	gens, decodeFailures, err := readGens(ctx, db)
	if err != nil {
		return nil, prev, nil, err
	}

	stepTime := make(map[string]time.Time, len(steps))
	for _, s := range steps {
		if s.UUID != "" && s.HasTime {
			stepTime[s.UUID] = s.Time
		}
	}
	genRequestIDs := make(map[string]struct{}, len(gens))
	for _, g := range gens {
		if g.Usage.RequestID != "" {
			genRequestIDs[g.Usage.RequestID] = struct{}{}
		}
	}

	diag := runSelfChecks(convID, path, gens, stepTime, decodeFailures)
	confidence := event.ConfidenceExact
	if diag != nil {
		confidence = event.ConfidenceUnknown
	}

	var events []event.UsageEvent

	// (1) gen_metadata rows: the primary source, they carry the model id.
	for _, g := range gens {
		if g.Idx > next.GenIdx {
			next.GenIdx = g.Idx
		}
		if g.Idx <= prev.GenIdx {
			continue
		}
		if g.Model == "" && !g.Usage.Present {
			continue // PLAN.md §6.4: failed/aborted call, nothing to report
		}
		ts, ok := stepTime[g.StepUUID]
		if !ok {
			if !g.HasReqTime {
				continue // no timestamp at all: cannot place the event
			}
			ts = g.ReqTime // fallback: gen 1.9.4
		}
		key := g.Usage.RequestID
		if key == "" {
			key = fmt.Sprintf("gen:%s:%d", convID, g.Idx)
		}
		model := g.Model
		if model == "" {
			model = internalModel
		}
		events = append(events, event.UsageEvent{
			EventID:    event.NewEventID(event.SourceAntigravity, convID, key),
			DeviceID:   deviceID,
			Source:     event.SourceAntigravity,
			Timestamp:  ts,
			Model:      model,
			SessionID:  g.TrajectoryID,
			Project:    project,
			Input:      g.Usage.Uncached,
			CacheRead:  g.Usage.Cached,
			Output:     g.Usage.Output,
			Reasoning:  clampReasoning(g.Usage), // 0 for Claude rows, which omit 1.4.9
			Confidence: confidence,
		})
	}

	// (2) steps rows whose usage never showed up in gen_metadata: title and
	// summary generation, model unknown (PLAN.md §6.4).
	for _, s := range steps {
		if s.Idx > next.StepIdx {
			next.StepIdx = s.Idx
		}
		if s.Idx <= prev.StepIdx || !s.Usage.Present || !s.HasTime {
			continue
		}
		id := s.Usage.RequestID
		if id == "" {
			continue // cannot tell it apart from a gen row: skip, never double count
		}
		if _, dup := genRequestIDs[id]; dup {
			continue
		}
		events = append(events, event.UsageEvent{
			EventID:    event.NewEventID(event.SourceAntigravity, convID, id),
			DeviceID:   deviceID,
			Source:     event.SourceAntigravity,
			Timestamp:  s.Time,
			Model:      internalModel,
			SessionID:  s.TrajectoryID,
			Project:    project,
			Input:      s.Usage.Uncached,
			CacheRead:  s.Usage.Cached,
			Output:     s.Usage.Output,
			Reasoning:  clampReasoning(s.Usage),
			Confidence: confidence,
		})
	}

	return events, next, diag, nil
}

// clampReasoning keeps Reasoning within Output. Reasoning is a subset of Output
// in every observed row (the identity of PLAN.md §6.6 check 1); should a schema
// change break that, the self-check reports it and the event stays valid per
// event.UsageEvent.Valid instead of being dropped downstream.
func clampReasoning(u usage) int64 {
	if u.Thoughts > u.Output {
		return u.Output
	}
	return u.Thoughts
}

// readSteps loads idx, step_type and the (small) metadata blob of every step.
func readSteps(ctx context.Context, db *sql.DB) ([]stepRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT idx, step_type, metadata FROM steps ORDER BY idx`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stepRow
	for rows.Next() {
		var idx, stepType int64
		var blob []byte
		if err := rows.Scan(&idx, &stepType, &blob); err != nil {
			return nil, err
		}
		if len(blob) == 0 {
			continue
		}
		s, err := decodeStep(idx, stepType, blob)
		if err != nil {
			continue // malformed step: no timestamp from it, keep going
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// readGens loads every gen_metadata row and returns how many failed to decode.
func readGens(ctx context.Context, db *sql.DB) ([]genRow, int, error) {
	rows, err := db.QueryContext(ctx, `SELECT idx, data FROM gen_metadata ORDER BY idx`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []genRow
	failures := 0
	for rows.Next() {
		var idx int64
		var blob []byte
		if err := rows.Scan(&idx, &blob); err != nil {
			return nil, failures, err
		}
		if len(blob) == 0 {
			continue
		}
		g, err := decodeGen(idx, blob)
		if err != nil {
			failures++
			continue
		}
		out = append(out, g)
	}
	return out, failures, rows.Err()
}

// runSelfChecks implements PLAN.md §6.6. It returns nil when every check
// passed, otherwise a Diagnostic naming the failures; the caller then drops the
// conversation to Confidence=unknown but still emits its events.
func runSelfChecks(convID, path string, gens []genRow, stepTime map[string]time.Time, decodeFailures int) *Diagnostic {
	d := Diagnostic{ConversationID: convID, Path: path, GenRows: len(gens), DecodeFailures: decodeFailures}

	var prevPrompt int64
	for _, g := range gens {
		// 1. total output == thoughts + response (or == response for Claude).
		if g.Usage.Present && !g.Usage.identityOK() {
			d.IdentityFailures++
		}
		// 2. uncached+cached prompt must not collapse between calls; context
		//    compaction makes the occasional drop legitimate.
		if p := g.Usage.promptTotal(); p > 0 {
			if prevPrompt > 0 {
				d.MonotonicChecks++
				if float64(p) < 0.9*float64(prevPrompt) {
					d.MonotonicFailures++
				}
			}
			prevPrompt = p
		}
		// 3. model id must look like a known provider.
		if g.Model != "" && !knownModel(g.Model) {
			if !containsString(d.UnknownModels, g.Model) {
				d.UnknownModels = append(d.UnknownModels, g.Model)
			}
		}
		// 4. step UUID join. Only rows that would become an event are counted:
		// an aborted call with neither model nor usage is skipped anyway, so
		// its missing step is not evidence of drift.
		if g.StepUUID != "" && (g.Model != "" || g.Usage.Present) {
			d.JoinAttempts++
			if _, ok := stepTime[g.StepUUID]; !ok {
				d.JoinFailures++
			}
		}
	}

	if d.IdentityFailures > 0 {
		d.Reasons = append(d.Reasons, fmt.Sprintf("output identity failed on %d/%d rows (1.4.3 != 1.4.9+1.4.10)", d.IdentityFailures, d.GenRows))
	}
	if d.MonotonicChecks > 0 && float64(d.MonotonicFailures) >= 0.05*float64(d.MonotonicChecks) {
		d.Reasons = append(d.Reasons, fmt.Sprintf("prompt total dropped on %d/%d rows (>=5%%)", d.MonotonicFailures, d.MonotonicChecks))
	}
	if d.JoinAttempts > 0 && float64(d.JoinAttempts-d.JoinFailures) < 0.99*float64(d.JoinAttempts) {
		d.Reasons = append(d.Reasons, fmt.Sprintf("step UUID join succeeded on %d/%d rows (<99%%)", d.JoinAttempts-d.JoinFailures, d.JoinAttempts))
	}
	if len(d.UnknownModels) > 0 {
		d.Reasons = append(d.Reasons, "unrecognised model ids: "+strings.Join(d.UnknownModels, ", "))
	}
	if d.GenRows > 0 && float64(decodeFailures) > 0.01*float64(d.GenRows+decodeFailures) {
		d.Reasons = append(d.Reasons, fmt.Sprintf("%d/%d rows failed to decode", decodeFailures, d.GenRows+decodeFailures))
	}
	if len(d.Reasons) == 0 {
		return nil
	}
	return &d
}

// knownModel is self-check 3 of PLAN.md §6.6: an unexpected provider prefix is
// treated as possible schema drift.
func knownModel(m string) bool {
	return strings.HasPrefix(m, "gemini-") || strings.HasPrefix(m, "claude-") || m == internalModel
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
