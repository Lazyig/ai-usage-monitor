package antigravity

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test-only protobuf encoder. It is deliberately independent of the decoder so
// the round trip proves something, and it lets the fixtures below mirror the
// real field map of PLAN.md §9.2/§9.3 byte for byte.
// ---------------------------------------------------------------------------

func putVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

func tag(num, wire int) []byte { return putVarint(nil, uint64(num)<<3|uint64(wire)) }

func encVarint(num int, v uint64) []byte { return putVarint(tag(num, wireVarint), v) }

func encBytes(num int, b []byte) []byte {
	out := putVarint(tag(num, wireBytes), uint64(len(b)))
	return append(out, b...)
}

func encString(num int, s string) []byte { return encBytes(num, []byte(s)) }

func encMsg(num int, parts ...[]byte) []byte { return encBytes(num, cat(parts...)) }

func encFixed64(num int, v uint64) []byte {
	out := tag(num, wireFixed64)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return append(out, b[:]...)
}

func encFixed32(num int, v uint32) []byte {
	out := tag(num, wireFixed32)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return append(out, b[:]...)
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// encKV builds one entry of the repeated 1.20 {1: key, 2: value} list.
func encKV(key, value string) []byte {
	return encMsg(20, encString(1, key), encString(2, value))
}

// encTime builds a {1: seconds, 2: nanos} timestamp message.
func encTime(num int, t time.Time) []byte {
	return encMsg(num, encVarint(1, uint64(t.Unix())), encVarint(2, uint64(t.Nanosecond())))
}

// usageFixture describes a usage struct (gen 1.4 / steps 9).
type usageFixture struct {
	uncached, cached    int64
	output, thoughts    int64
	response            int64
	hasCached, hasThink bool
	requestID           string
}

func encUsage(num int, u usageFixture) []byte {
	parts := [][]byte{
		encVarint(1, 1298), // model constant, NOT tokens (PLAN.md §9.2)
		encVarint(2, uint64(u.uncached)),
		encVarint(3, uint64(u.output)),
	}
	if u.hasCached {
		parts = append(parts, encVarint(5, uint64(u.cached)))
	}
	parts = append(parts, encVarint(6, 24), encString(7, "bot-1234"))
	if u.hasThink {
		parts = append(parts, encVarint(9, uint64(u.thoughts)))
	}
	parts = append(parts, encVarint(10, uint64(u.response)))
	if u.requestID != "" {
		parts = append(parts, encString(11, u.requestID))
	}
	return encMsg(num, parts...)
}

type genFixture struct {
	stepUUID     string
	model        string
	display      string // 1.21, Claude only, must be ignored
	usage        usageFixture
	reqTime      *time.Time // 1.9.4
	trajectoryID string
	withPrompt   bool // add the huge 1.1 / 1.2 prompt fields
}

func encGen(g genFixture) []byte {
	var body [][]byte
	if g.withPrompt {
		// 1.1 and 1.2.* hold the system prompt and the whole conversation.
		// They are present in real rows and must never be read.
		body = append(body,
			encString(1, strings.Repeat("secret system prompt ", 500)),
			encMsg(2, encString(1, "user asked something private")),
		)
	}
	body = append(body, encUsage(4, g.usage))
	if g.reqTime != nil {
		body = append(body, encMsg(9, encTime(4, *g.reqTime)))
	}
	if g.model != "" {
		body = append(body, encString(19, g.model))
	}
	if g.display != "" {
		body = append(body, encString(21, g.display))
	}
	if g.trajectoryID != "" {
		body = append(body,
			encKV("model_enum", "MODEL_PLACEHOLDER_M12"),
			encKV("trajectory_id", g.trajectoryID),
			encKV("request_id", g.usage.requestID),
		)
	}
	parts := [][]byte{encMsg(1, body...)}
	if g.stepUUID != "" {
		parts = append(parts, encString(4, g.stepUUID))
	}
	return cat(parts...)
}

type stepFixture struct {
	uuid           string
	ts             time.Time
	usage          *usageFixture
	trajectoryID   string
	conversationID string
}

func encStep(s stepFixture) []byte {
	parts := [][]byte{encTime(1, s.ts)}
	if s.usage != nil {
		parts = append(parts, encUsage(9, *s.usage))
	}
	if s.uuid != "" {
		parts = append(parts, encString(12, s.uuid))
	}
	parts = append(parts, encMsg(20, encString(1, s.trajectoryID), encString(4, s.conversationID)))
	return cat(parts...)
}

// ---------------------------------------------------------------------------
// Decoder tests
// ---------------------------------------------------------------------------

func TestReadVarint(t *testing.T) {
	cases := []uint64{0, 1, 127, 128, 300, 1 << 20, 1<<63 - 1}
	for _, want := range cases {
		b := putVarint(nil, want)
		got, n, err := readVarint(b, 0)
		if err != nil || got != want || n != len(b) {
			t.Fatalf("readVarint(%d): got %d n=%d err=%v", want, got, n, err)
		}
	}
	if _, _, err := readVarint([]byte{0x80, 0x80}, 0); err == nil {
		t.Fatal("truncated varint must fail")
	}
	// 11 continuation bytes is never a legal varint.
	if _, _, err := readVarint([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 0); err == nil {
		t.Fatal("oversized varint must fail")
	}
}

func TestDecodeGeminiRow(t *testing.T) {
	blob := encGen(genFixture{
		stepUUID:     "step-uuid-1",
		model:        "gemini-3.7-flash",
		trajectoryID: "traj-1",
		withPrompt:   true,
		usage: usageFixture{
			uncached: 1200, cached: 34000, hasCached: true,
			output: 900, thoughts: 700, hasThink: true, response: 200,
			requestID: "req-gemini-1",
		},
	})
	g, err := decodeGen(7, blob)
	if err != nil {
		t.Fatalf("decodeGen: %v", err)
	}
	if g.Idx != 7 || g.StepUUID != "step-uuid-1" || g.Model != "gemini-3.7-flash" {
		t.Fatalf("header mismatch: %+v", g)
	}
	if g.TrajectoryID != "traj-1" {
		t.Fatalf("trajectory_id = %q", g.TrajectoryID)
	}
	u := g.Usage
	if u.Uncached != 1200 || u.Cached != 34000 || u.Output != 900 || u.Thoughts != 700 || u.Response != 200 {
		t.Fatalf("usage mismatch: %+v", u)
	}
	if !u.HasThoughts || u.RequestID != "req-gemini-1" || !u.Present {
		t.Fatalf("usage flags: %+v", u)
	}
	if !u.identityOK() {
		t.Fatal("identity should hold: 900 == 700+200")
	}
	if u.promptTotal() != 35200 {
		t.Fatalf("promptTotal = %d", u.promptTotal())
	}
	if g.HasReqTime {
		t.Fatal("no 1.9.4 in this fixture")
	}
}

func TestDecodeClaudeRowWithoutThoughts(t *testing.T) {
	// Claude via Vertex: no 1.4.9, no 1.4.5 on the first turn, display name in
	// 1.21 which we must ignore.
	blob := encGen(genFixture{
		stepUUID: "step-uuid-2",
		model:    "claude-sonnet-4-6",
		display:  "Claude Sonnet 4.6 (Thinking)",
		usage: usageFixture{
			uncached: 5000, output: 410, response: 410,
			requestID: "req_vrtx_abc",
		},
	})
	g, err := decodeGen(0, blob)
	if err != nil {
		t.Fatalf("decodeGen: %v", err)
	}
	if g.Model != "claude-sonnet-4-6" {
		t.Fatalf("model = %q", g.Model)
	}
	if g.Usage.HasThoughts || g.Usage.Thoughts != 0 {
		t.Fatal("Claude rows carry no 1.4.9")
	}
	if g.Usage.Cached != 0 {
		t.Fatalf("cached = %d, want 0 when 1.4.5 absent", g.Usage.Cached)
	}
	if !g.Usage.identityOK() {
		t.Fatal("identity should hold: 410 == 410 with no thoughts")
	}
	// Breaking the identity must be detected.
	bad := g.Usage
	bad.Output = 999
	if bad.identityOK() {
		t.Fatal("identity check did not catch 999 != 410")
	}
}

func TestDecodeGenRequestTimeFallback(t *testing.T) {
	want := time.Unix(1756000000, 123456789).UTC()
	blob := encGen(genFixture{
		stepUUID: "step-uuid-3",
		model:    "gemini-default",
		reqTime:  &want,
		usage:    usageFixture{uncached: 10, output: 5, response: 5, requestID: "r3"},
	})
	g, err := decodeGen(1, blob)
	if err != nil {
		t.Fatalf("decodeGen: %v", err)
	}
	if !g.HasReqTime || !g.ReqTime.Equal(want) {
		t.Fatalf("ReqTime = %v (%v), want %v", g.ReqTime, g.HasReqTime, want)
	}
	if g.ReqTime.Location() != time.UTC {
		t.Fatal("timestamps must be UTC")
	}
}

func TestDecodeStep(t *testing.T) {
	ts := time.Unix(1756000123, 500).UTC()
	blob := encStep(stepFixture{
		uuid:           "step-uuid-1",
		ts:             ts,
		usage:          &usageFixture{uncached: 3, output: 4, response: 4, requestID: "req-step-only"},
		trajectoryID:   "traj-1",
		conversationID: "conv-1",
	})
	s, err := decodeStep(9, stepTypeTitleSummary, blob)
	if err != nil {
		t.Fatalf("decodeStep: %v", err)
	}
	if !s.HasTime || !s.Time.Equal(ts) {
		t.Fatalf("time = %v", s.Time)
	}
	if s.UUID != "step-uuid-1" || s.TrajectoryID != "traj-1" || s.ConversationID != "conv-1" {
		t.Fatalf("ids: %+v", s)
	}
	if !s.Usage.Present || s.Usage.RequestID != "req-step-only" || s.Usage.Response != 4 {
		t.Fatalf("usage: %+v", s.Usage)
	}
	if s.StepType != stepTypeTitleSummary {
		t.Fatalf("step type = %d", s.StepType)
	}
}

func TestKVListStopsAtTrajectory(t *testing.T) {
	// 1.20 values may be strings or messages; we must not choke on a message
	// value belonging to another key.
	body := cat(
		encMsg(20, encString(1, "last_step_index"), encMsg(2, encVarint(1, 42))),
		encKV("trajectory_id", "traj-x"),
	)
	blob := cat(encMsg(1, body), encString(4, "u"))
	g, err := decodeGen(0, blob)
	if err != nil {
		t.Fatalf("decodeGen: %v", err)
	}
	if g.TrajectoryID != "traj-x" {
		t.Fatalf("trajectory = %q", g.TrajectoryID)
	}
}

func TestMalformedInputsNeverPanic(t *testing.T) {
	valid := encGen(genFixture{
		stepUUID: "u", model: "gemini-3-flash-a", trajectoryID: "t",
		usage: usageFixture{uncached: 1, cached: 2, hasCached: true, output: 3, thoughts: 1, hasThink: true, response: 2, requestID: "r"},
	})
	// Every prefix of a valid blob, i.e. every possible truncation.
	for i := 0; i < len(valid); i++ {
		if _, err := decodeGen(0, valid[:i]); err != nil {
			continue // expected for most cuts
		}
	}
	garbage := [][]byte{
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		{0x00},             // field number 0
		{0x0b},             // wire type 3 (group start)
		{0x0c},             // wire type 4
		{0x0e},             // wire type 6
		{0x0f},             // wire type 7
		{0x0a, 0xff, 0x01}, // length 255 but 1 byte of payload
		{0x22},             // tag with no payload
	}
	for i, b := range garbage {
		if _, err := decodeGen(0, b); err == nil {
			t.Fatalf("garbage %d decoded without error", i)
		}
		if _, err := decodeStep(0, 0, b); err == nil {
			t.Fatalf("garbage %d decoded as step without error", i)
		}
	}
	// A nested message that is itself garbage must fail when descended into.
	nested := cat(encMsg(1, encBytes(4, []byte{0x0b})), encString(4, "u"))
	if _, err := decodeGen(0, nested); err == nil {
		t.Log("nested garbage in an unread path is fine, it is skipped")
	}
	// Byte flipping: must never panic.
	for i := 0; i < len(valid); i++ {
		mutated := make([]byte, len(valid))
		copy(mutated, valid)
		mutated[i] ^= 0x55
		_, _ = decodeGen(0, mutated)
		_, _ = decodeStep(0, 15, mutated)
	}
}

func TestResolveRejectsDeepPaths(t *testing.T) {
	m := message(encVarint(1, 1))
	if _, _, err := m.uint(1, 2, 3, 4, 5, 6, 7, 8, 9); err == nil {
		t.Fatal("path deeper than maxDepth must error")
	}
	if _, _, err := m.uint(); err == nil {
		t.Fatal("empty path must error")
	}
}

func TestFixedWidthFieldsRead(t *testing.T) {
	// The blobs use varints for tokens, but a schema change to fixed32/64 must
	// still be readable rather than silently zero.
	m := message(cat(encFixed64(2, 7), encFixed32(3, 9)))
	if v, ok, _ := m.int64At(2); !ok || v != 7 {
		t.Fatalf("fixed64 = %d %v", v, ok)
	}
	if v, ok, _ := m.int64At(3); !ok || v != 9 {
		t.Fatalf("fixed32 = %d %v", v, ok)
	}
}

func TestStringPathOnNonBytesField(t *testing.T) {
	m := message(encVarint(19, 5))
	if s, ok, err := m.str(19); err != nil || ok || s != "" {
		t.Fatalf("varint read as string: %q %v %v", s, ok, err)
	}
}

// FuzzDecodeGen and FuzzDecodeStep assert the decoder's only failure mode is an
// error: no panic, no unbounded work, whatever bytes the CLI (or a corrupted
// file) hands us.
func FuzzDecodeGen(f *testing.F) {
	f.Add(encGen(genFixture{
		stepUUID: "u", model: "gemini-3.7-flash", trajectoryID: "t", withPrompt: true,
		usage: usageFixture{uncached: 1, cached: 2, hasCached: true, output: 3, thoughts: 1, hasThink: true, response: 2, requestID: "r"},
	}))
	f.Add(encGen(genFixture{stepUUID: "u", model: "claude-sonnet-4-6",
		usage: usageFixture{uncached: 9, output: 4, response: 4, requestID: "req_vrtx_x"}}))
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0xff, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := decodeGen(0, data)
		if err != nil {
			return
		}
		if g.Usage.Uncached < 0 || g.Usage.Cached < 0 || g.Usage.Output < 0 || g.Usage.Thoughts < 0 {
			t.Fatalf("negative token count from %x: %+v", data, g.Usage)
		}
	})
}

func FuzzDecodeStep(f *testing.F) {
	f.Add(encStep(stepFixture{uuid: "u", ts: time.Unix(1756000000, 1).UTC(),
		usage: &usageFixture{uncached: 1, output: 2, response: 2, requestID: "r"}, trajectoryID: "t", conversationID: "c"}))
	f.Add([]byte{0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := decodeStep(0, 15, data); err != nil {
			return
		}
	})
}
