package event

import (
	"testing"
	"time"
)

func TestValidRejectsNegativeReasoning(t *testing.T) {
	e := UsageEvent{EventID: "test", DeviceID: "test", Source: SourceCodex, Model: "gpt-6-sol", Timestamp: time.Now(), Output: 10}
	if !e.Valid() {
		t.Fatal("valid event rejected")
	}
	e.Reasoning = -1
	if e.Valid() {
		t.Fatal("negative reasoning must be rejected before storage")
	}
	e.Reasoning = 10
	if !e.Valid() {
		t.Fatal("reasoning equal to output must remain valid")
	}
}

// The whole point of scheme 2: the same model call hashes to the same id no
// matter which device, agent install or run reports it.
func TestNewEventIDIsDeviceIndependent(t *testing.T) {
	a := NewEventID(SourceClaudeCode, "msg_1", "req_1")
	b := NewEventID(SourceClaudeCode, "msg_1", "req_1")
	if a != b {
		t.Fatalf("same inputs gave %s and %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("id %q is not a sha256 hex digest", a)
	}
}

func TestNewEventIDSeparatesItsParts(t *testing.T) {
	// Without the NUL separators these two would collide.
	if x, y := NewEventID(SourceCodex, "ab", "c"), NewEventID(SourceCodex, "a", "bc"); x == y {
		t.Errorf("ab|c and a|bc both hash to %s", x)
	}
	// The source is part of the key, so two tools cannot share an id.
	if x, y := NewEventID(SourceCodex, "k"), NewEventID(SourceAntigravity, "k"); x == y {
		t.Errorf("codex and antigravity both hash %q to %s", "k", x)
	}
	// A different number of parts is a different id.
	if x, y := NewEventID(SourceCodex, "k"), NewEventID(SourceCodex, "k", ""); x == y {
		t.Errorf("one part and two parts both hash to %s", x)
	}
}
