// Package cursor persists each collector's opaque progress state between agent
// runs. The whole thing is one small JSON object, source -> collector cursor,
// written atomically so a crash mid-write cannot leave a truncated file that
// would make the agent re-read (or worse, skip) history.
package cursor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"ai-usage-monitor/internal/collector"
	"ai-usage-monitor/internal/event"
)

// Store is the in-memory view of cursors.json. It is safe for concurrent use.
type Store struct {
	path string

	mu   sync.Mutex
	data map[string]json.RawMessage
}

// Open reads path. A missing file yields an empty store, and so does a corrupt
// one: losing cursors only costs one full re-read, and the server drops the
// duplicate events by EventID, so refusing to start would be the worse failure.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: map[string]json.RawMessage{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cursor: read %s: %w", path, err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil || got == nil {
		return s, nil // corrupt: start over rather than fail to run
	}
	s.data = got
	return s, nil
}

// Path is the file this store is backed by.
func (s *Store) Path() string { return s.path }

// Get returns the stored cursor for source, or nil for "from the beginning".
func (s *Store) Get(source event.Source) collector.Cursor {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.data[string(source)]
	if !ok || len(raw) == 0 {
		return nil
	}
	return collector.Cursor(append([]byte(nil), raw...))
}

// Set records a new cursor for source. An empty cursor clears the entry.
// Cursors are collector-internal JSON by contract; anything else is rejected
// rather than silently corrupting cursors.json.
func (s *Store) Set(source event.Source, cur collector.Cursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(cur) == 0 {
		delete(s.data, string(source))
		return nil
	}
	if !json.Valid(cur) {
		return fmt.Errorf("cursor: %s returned a non-JSON cursor (%d bytes)", source, len(cur))
	}
	s.data[string(source)] = append(json.RawMessage(nil), cur...)
	return nil
}

// Reset forgets every cursor, in memory and on disk, so the next scan starts
// from the beginning. Used when the server asks for a full resync and when the
// EventID scheme changes.
func (s *Store) Reset() error {
	s.mu.Lock()
	s.data = map[string]json.RawMessage{}
	s.mu.Unlock()
	return Clear(s.path)
}

// Clear deletes the cursor file at path without opening it, for callers that
// only want the next run to start from zero. A missing file is not an error.
func Clear(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cursor: remove %s: %w", path, err)
	}
	return nil
}

// Sources lists the sources that currently have a cursor, sorted.
func (s *Store) Sources() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Size reports how many bytes of cursor state are stored for source. It is only
// used by `status` to show that a collector has made progress without dumping
// the (large and meaningless to a human) cursor itself.
func (s *Store) Size(source event.Source) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data[string(source)])
}

// Save writes the store to disk atomically: a temp file in the same directory,
// fsync, then rename over the target.
func (s *Store) Save() error {
	s.mu.Lock()
	// Compact, not indented: the cursor of a busy device lists hundreds of
	// files, and storing each collector's JSON verbatim keeps the round trip
	// byte for byte identical.
	raw, err := json.Marshal(s.data)
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("cursor: encode: %w", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cursor: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".cursors-*.json")
	if err != nil {
		return fmt.Errorf("cursor: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("cursor: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("cursor: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cursor: close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("cursor: rename: %w", err)
	}
	return nil
}
