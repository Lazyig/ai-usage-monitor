package antigravity

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

// ---------------------------------------------------------------------------
// Synthetic fixture: the same tables the CLI creates (see the schema dumped in
// PLAN.md §6.1), filled with hand-encoded protobuf blobs.
// ---------------------------------------------------------------------------

const (
	genSchema  = "CREATE TABLE `gen_metadata` (`idx` integer,`data` blob,`size` integer NOT NULL DEFAULT 0,PRIMARY KEY (`idx`))"
	stepSchema = "CREATE TABLE `steps` (`idx` integer,`step_type` integer NOT NULL DEFAULT 0,`status` integer NOT NULL DEFAULT 0," +
		"`has_subtrajectory` numeric NOT NULL DEFAULT false,`metadata` blob,`error_details` blob,`permissions` blob,`task_details` blob," +
		"`render_info` blob,`step_payload` blob,`step_format` integer NOT NULL DEFAULT 0,PRIMARY KEY (`idx`))"
	summarySchema = "CREATE TABLE `conversation_summaries` (`conversation_id` text,`title` text NOT NULL DEFAULT \"\"," +
		"`workspace_uris` text NOT NULL,PRIMARY KEY (`conversation_id`))"
)

func openRW(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newConversation creates <root>/conversations/<id>.db with empty tables.
func newConversation(t *testing.T, root, id string) *sql.DB {
	t.Helper()
	dir := filepath.Join(root, "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := openRW(t, filepath.Join(dir, id+".db"))
	for _, stmt := range []string{genSchema, stepSchema} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func insertGen(t *testing.T, db *sql.DB, idx int64, blob []byte) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO gen_metadata(idx, data, size) VALUES (?,?,?)", idx, blob, len(blob)); err != nil {
		t.Fatalf("insert gen: %v", err)
	}
}

func insertStep(t *testing.T, db *sql.DB, idx, stepType int64, blob []byte) {
	t.Helper()
	// step_payload is filled with junk so a test would notice if the collector
	// ever started selecting it (it must not: real payloads are megabytes).
	_, err := db.Exec("INSERT INTO steps(idx, step_type, status, metadata, step_payload) VALUES (?,?,?,?,?)",
		idx, stepType, 0, blob, make([]byte, 1024))
	if err != nil {
		t.Fatalf("insert step: %v", err)
	}
}

func writeSummaries(t *testing.T, root string, uris map[string]string) {
	t.Helper()
	db := openRW(t, filepath.Join(root, "conversation_summaries.db"))
	if _, err := db.Exec(summarySchema); err != nil {
		t.Fatal(err)
	}
	for id, uri := range uris {
		raw, _ := json.Marshal([]string{uri})
		if _, err := db.Exec("INSERT INTO conversation_summaries(conversation_id, title, workspace_uris) VALUES (?,?,?)", id, "t", string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

const (
	testConvID = "11111111-2222-3333-4444-555555555555"
	testDevice = "device-1"
)

var baseTime = time.Unix(1756000000, 0).UTC()

// buildStandardConversation writes the reference fixture:
//
//	gen 0 + step 0 : gemini call with thoughts and cache hits
//	gen 1 + step 1 : claude call, no 1.4.9
//	step 2         : title generation, usage only in metadata.9 (step_type 23)
//	gen 2          : row with neither model nor usage -> skipped
func buildStandardConversation(t *testing.T, root string) *sql.DB {
	t.Helper()
	db := newConversation(t, root, testConvID)

	insertGen(t, db, 0, encGen(genFixture{
		stepUUID: "uuid-0", model: "gemini-3.7-flash", trajectoryID: "traj-1", withPrompt: true,
		usage: usageFixture{uncached: 1200, cached: 34000, hasCached: true,
			output: 900, thoughts: 700, hasThink: true, response: 200, requestID: "req-gem-0"},
	}))
	insertGen(t, db, 1, encGen(genFixture{
		stepUUID: "uuid-1", model: "claude-sonnet-4-6", display: "Claude Sonnet 4.6 (Thinking)", trajectoryID: "traj-1",
		usage: usageFixture{uncached: 5000, cached: 60000, hasCached: true,
			output: 410, response: 410, requestID: "req_vrtx_1"},
	}))
	// Neither model nor usage: an aborted call (PLAN.md §6.4, 43 such rows).
	insertGen(t, db, 2, cat(encMsg(1, encString(21, "ignored")), encString(4, "uuid-2")))

	insertStep(t, db, 0, stepTypeModelGeneration, encStep(stepFixture{
		uuid: "uuid-0", ts: baseTime, trajectoryID: "traj-1", conversationID: testConvID,
		usage: &usageFixture{uncached: 1200, cached: 34000, hasCached: true,
			output: 900, thoughts: 700, hasThink: true, response: 200, requestID: "req-gem-0"},
	}))
	insertStep(t, db, 1, stepTypeModelGeneration, encStep(stepFixture{
		uuid: "uuid-1", ts: baseTime.Add(time.Minute), trajectoryID: "traj-1", conversationID: testConvID,
		usage: &usageFixture{uncached: 5000, cached: 60000, hasCached: true,
			output: 410, response: 410, requestID: "req_vrtx_1"},
	}))
	insertStep(t, db, 2, stepTypeTitleSummary, encStep(stepFixture{
		uuid: "uuid-2-step", ts: baseTime.Add(2 * time.Minute), trajectoryID: "traj-1", conversationID: testConvID,
		usage: &usageFixture{uncached: 800, output: 12, thoughts: 4, hasThink: true, response: 8, requestID: "req-title-1"},
	}))
	return db
}

func scan(t *testing.T, c collector.Collector, cur collector.Cursor) ([]event.UsageEvent, collector.Cursor) {
	t.Helper()
	evs, next, err := c.Scan(context.Background(), testDevice, cur)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return evs, next
}

func byModel(evs []event.UsageEvent) map[string]event.UsageEvent {
	m := map[string]event.UsageEvent{}
	for _, e := range evs {
		m[e.Model] = e
	}
	return m
}

func TestScanEmitsExpectedEvents(t *testing.T) {
	root := t.TempDir()
	buildStandardConversation(t, root)
	writeSummaries(t, root, map[string]string{testConvID: "file:///config/workspace"})

	c := New(root)
	if c.Name() != event.SourceAntigravity {
		t.Fatalf("Name = %q", c.Name())
	}
	evs, cur := scan(t, c, nil)
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(evs), evs)
	}
	for _, e := range evs {
		if !e.Valid() {
			t.Fatalf("invalid event: %+v", e)
		}
		if e.Project != "file:///config/workspace" {
			t.Fatalf("project = %q", e.Project)
		}
		if e.SessionID != "traj-1" {
			t.Fatalf("session = %q", e.SessionID)
		}
		if e.Confidence != event.ConfidenceExact {
			t.Fatalf("confidence = %q", e.Confidence)
		}
		if e.DeviceID != testDevice || e.Source != event.SourceAntigravity {
			t.Fatalf("identity fields wrong: %+v", e)
		}
		if e.CacheWrite != 0 || e.CacheWrite5m != 0 || e.CacheWrite1h != 0 {
			t.Fatal("Antigravity never records cache writes (PLAN.md §6.5)")
		}
	}

	m := byModel(evs)
	gem, ok := m["gemini-3.7-flash"]
	if !ok {
		t.Fatal("gemini event missing")
	}
	if gem.Input != 1200 || gem.CacheRead != 34000 || gem.Output != 900 || gem.Reasoning != 700 {
		t.Fatalf("gemini tokens: %+v", gem)
	}
	if !gem.Timestamp.Equal(baseTime) {
		t.Fatalf("gemini ts = %v want %v (step join)", gem.Timestamp, baseTime)
	}
	wantID := event.NewEventID(event.SourceAntigravity, testConvID, "req-gem-0")
	if gem.EventID != wantID {
		t.Fatalf("event id = %s want %s", gem.EventID, wantID)
	}

	cl, ok := m["claude-sonnet-4-6"]
	if !ok {
		t.Fatal("claude event missing")
	}
	if cl.Input != 5000 || cl.CacheRead != 60000 || cl.Output != 410 || cl.Reasoning != 0 {
		t.Fatalf("claude tokens: %+v", cl)
	}

	// The step-only call (title generation) becomes an internal event.
	in, ok := m[internalModel]
	if !ok {
		t.Fatal("antigravity-internal event missing")
	}
	if in.Input != 800 || in.Output != 12 || in.Reasoning != 4 {
		t.Fatalf("internal tokens: %+v", in)
	}
	if !in.Timestamp.Equal(baseTime.Add(2 * time.Minute)) {
		t.Fatalf("internal ts = %v", in.Timestamp)
	}

	// Steps 0 and 1 repeat the gen request ids and must not be emitted twice.
	seen := map[string]bool{}
	for _, e := range evs {
		if seen[e.EventID] {
			t.Fatalf("duplicate event id %s", e.EventID)
		}
		seen[e.EventID] = true
	}

	if d := c.(*Collector).Diagnostics(); len(d) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", d)
	}

	// Cursor round trip: nothing new on a second pass.
	again, cur2 := scan(t, c, cur)
	if len(again) != 0 {
		t.Fatalf("rescan emitted %d events: %+v", len(again), again)
	}
	var st cursorState
	if err := json.Unmarshal(cur2, &st); err != nil {
		t.Fatalf("cursor json: %v", err)
	}
	fc := st.Files[testConvID+".db"]
	if fc.GenIdx != 2 || fc.StepIdx != 2 {
		t.Fatalf("cursor = %+v", fc)
	}
	if fc.Size == 0 || fc.ModTime == 0 {
		t.Fatalf("cursor lacks file identity: %+v", fc)
	}
}

func TestScanIncremental(t *testing.T) {
	root := t.TempDir()
	db := buildStandardConversation(t, root)
	c := New(root)
	_, cur := scan(t, c, nil)

	// A new call appended after the cursor was taken.
	insertGen(t, db, 3, encGen(genFixture{
		stepUUID: "uuid-3", model: "gemini-3-flash-a", trajectoryID: "traj-1",
		usage: usageFixture{uncached: 100, cached: 90000, hasCached: true,
			output: 60, thoughts: 40, hasThink: true, response: 20, requestID: "req-gem-3"},
	}))
	insertStep(t, db, 3, stepTypeModelGeneration, encStep(stepFixture{
		uuid: "uuid-3", ts: baseTime.Add(3 * time.Minute), trajectoryID: "traj-1", conversationID: testConvID,
	}))

	evs, _ := scan(t, c, cur)
	if len(evs) != 1 {
		t.Fatalf("incremental scan gave %d events: %+v", len(evs), evs)
	}
	if evs[0].Model != "gemini-3-flash-a" || evs[0].Input != 100 || evs[0].CacheRead != 90000 {
		t.Fatalf("incremental event: %+v", evs[0])
	}
}

func TestScanRereadsWhenFileShrank(t *testing.T) {
	root := t.TempDir()
	buildStandardConversation(t, root)
	c := New(root)
	first, cur := scan(t, c, nil)

	// Pretend the recorded file was bigger than what is on disk now: the file
	// was rewritten, idx values may have been reused, so read from scratch.
	var st cursorState
	if err := json.Unmarshal(cur, &st); err != nil {
		t.Fatal(err)
	}
	fc := st.Files[testConvID+".db"]
	fc.Size += 1 << 20
	st.Files[testConvID+".db"] = fc
	shrunk, _ := json.Marshal(st)

	evs, _ := scan(t, c, shrunk)
	if len(evs) != len(first) {
		t.Fatalf("after shrink got %d events, want the full %d", len(evs), len(first))
	}

	// Same for a file whose mtime moved backwards.
	path := filepath.Join(root, "conversations", testConvID+".db")
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	evs, _ = scan(t, c, cur)
	if len(evs) != len(first) {
		t.Fatalf("after mtime rewind got %d events, want %d", len(evs), len(first))
	}
}

func TestSelfCheckDowngradesConfidence(t *testing.T) {
	root := t.TempDir()
	db := newConversation(t, root, testConvID)
	// 1.4.3 != 1.4.9 + 1.4.10 -> schema drift (PLAN.md §6.6 check 1).
	insertGen(t, db, 0, encGen(genFixture{
		stepUUID: "uuid-0", model: "gemini-3.7-flash", trajectoryID: "traj-1",
		usage: usageFixture{uncached: 10, output: 999, thoughts: 700, hasThink: true, response: 200, requestID: "r0"},
	}))
	insertStep(t, db, 0, stepTypeModelGeneration, encStep(stepFixture{
		uuid: "uuid-0", ts: baseTime, trajectoryID: "traj-1", conversationID: testConvID,
	}))

	c := New(root)
	evs, _ := scan(t, c, nil)
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	if evs[0].Confidence != event.ConfidenceUnknown {
		t.Fatalf("confidence = %q, want unknown", evs[0].Confidence)
	}
	diags := c.(*Collector).Diagnostics()
	if len(diags) != 1 || diags[0].IdentityFailures != 1 || len(diags[0].Reasons) == 0 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if diags[0].ConversationID != testConvID {
		t.Fatalf("diagnostic conversation = %q", diags[0].ConversationID)
	}
}

func TestSelfCheckUnknownModelAndJoinFailure(t *testing.T) {
	root := t.TempDir()
	db := newConversation(t, root, testConvID)
	// Unknown provider prefix + no matching step row: two failing checks.
	insertGen(t, db, 0, encGen(genFixture{
		stepUUID: "missing-uuid", model: "gpt-9-turbo", trajectoryID: "traj-1",
		reqTime: &baseTime,
		usage:   usageFixture{uncached: 10, output: 5, response: 5, requestID: "r0"},
	}))
	c := New(root)
	evs, _ := scan(t, c, nil)
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	// Timestamp fell back to gen 1.9.4.
	if !evs[0].Timestamp.Equal(baseTime) {
		t.Fatalf("ts = %v, want the 1.9.4 fallback %v", evs[0].Timestamp, baseTime)
	}
	if evs[0].Confidence != event.ConfidenceUnknown {
		t.Fatal("unknown model + join failure must downgrade confidence")
	}
	d := c.(*Collector).Diagnostics()
	if len(d) != 1 || d[0].JoinFailures != 1 || len(d[0].UnknownModels) != 1 {
		t.Fatalf("diagnostics = %+v", d)
	}
}

func TestRowWithoutTimestampIsSkipped(t *testing.T) {
	root := t.TempDir()
	db := newConversation(t, root, testConvID)
	insertGen(t, db, 0, encGen(genFixture{
		stepUUID: "nowhere", model: "gemini-3.7-flash",
		usage: usageFixture{uncached: 1, output: 1, response: 1, requestID: "r"},
	}))
	evs, _ := scan(t, New(root), nil)
	if len(evs) != 0 {
		t.Fatalf("row without any timestamp must be skipped, got %+v", evs)
	}
}

func TestMissingRootIsEmpty(t *testing.T) {
	for _, root := range []string{"", filepath.Join(t.TempDir(), "nope")} {
		c := New(root)
		evs, cur, err := c.Scan(context.Background(), testDevice, nil)
		if err != nil {
			t.Fatalf("root %q: %v", root, err)
		}
		if len(evs) != 0 {
			t.Fatalf("root %q gave events", root)
		}
		if len(cur) == 0 {
			t.Fatalf("root %q gave no cursor", root)
		}
	}
}

func TestUnreadableConversationIsSkipped(t *testing.T) {
	root := t.TempDir()
	buildStandardConversation(t, root)
	// A file that is not SQLite at all must not break the whole scan.
	bad := filepath.Join(root, "conversations", "00000000-0000-0000-0000-000000000000.db")
	if err := os.WriteFile(bad, []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	evs, _ := scan(t, New(root), nil)
	if len(evs) != 3 {
		t.Fatalf("got %d events, want the 3 from the good conversation", len(evs))
	}
}

func TestRODSN(t *testing.T) {
	if got := roDSN("/tmp/a b/c#1.db"); got != "file:///tmp/a%20b/c%231.db?mode=ro&immutable=1" {
		t.Fatalf("dsn = %q", got)
	}
	// Windows drive letters become file:///C:/... , the form SQLite documents.
	if got := roDSN(`C:\Users\me\c.db`); filepath.Separator == '\\' && got != "file:///C:/Users/me/c.db?mode=ro&immutable=1" {
		t.Fatalf("windows dsn = %q", got)
	}
}

// TestReadOnlyImmutableOpen proves the URI form actually reaches SQLite: a
// database opened through openRO must reject writes. (If the URI parameters
// were treated as part of the file name, openRO would have created an empty
// database instead and the SELECT below would fail.)
func TestReadOnlyImmutableOpen(t *testing.T) {
	// A directory with a space and a '#' exercises the DSN escaping.
	root := filepath.Join(t.TempDir(), "we ird#dir")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	buildStandardConversation(t, root)

	path := filepath.Join(root, "conversations", testConvID+".db")
	db, err := openRO(path)
	if err != nil {
		t.Fatalf("openRO: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM gen_metadata").Scan(&n); err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 3 {
		t.Fatalf("gen rows = %d, want 3", n)
	}
	if _, err := db.Exec("INSERT INTO gen_metadata(idx, data, size) VALUES (99, x'00', 1)"); err == nil {
		t.Fatal("write succeeded on a mode=ro handle")
	}

	// And the collector reaches the same file through the same DSN.
	evs, _ := scan(t, New(root), nil)
	if len(evs) != 3 {
		t.Fatalf("got %d events from a path with a space, want 3", len(evs))
	}
}

func TestFirstWorkspaceURI(t *testing.T) {
	cases := map[string]string{
		`["file:///a","file:///b"]`: "file:///a",
		`[]`:                        "",
		``:                          "",
		`not json`:                  "",
		`["", "file:///c"]`:         "file:///c",
	}
	for in, want := range cases {
		if got := firstWorkspaceURI(in); got != want {
			t.Fatalf("firstWorkspaceURI(%q) = %q want %q", in, got, want)
		}
	}
}

func TestContextCancellation(t *testing.T) {
	root := t.TempDir()
	buildStandardConversation(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := New(root).Scan(ctx, testDevice, nil); err == nil {
		t.Fatal("cancelled context must surface an error")
	}
}
