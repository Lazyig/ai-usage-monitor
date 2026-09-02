package cursor

import (
	"os"
	"path/filepath"
	"testing"

	"ai-usage-monitor/internal/event"
)

func TestResetForgetsEverything(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursors.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(event.SourceCodex, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(event.SourceCodex); got != nil {
		t.Errorf("cursor survived Reset in memory: %s", got)
	}
	if len(s.Sources()) != 0 {
		t.Errorf("Sources() = %v after Reset", s.Sources())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the cursor file survived Reset: %v", err)
	}
	// Reopening starts from zero, and the store still works.
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get(event.SourceCodex); got != nil {
		t.Errorf("reopened store has a cursor: %s", got)
	}
	if err := s.Save(); err != nil {
		t.Errorf("Save after Reset: %v", err)
	}
}

func TestClearIsForgivingOfAMissingFile(t *testing.T) {
	if err := Clear(filepath.Join(t.TempDir(), "nothing.json")); err != nil {
		t.Errorf("Clear on a missing file = %v, want nil", err)
	}
}
