package transport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultQueueMaxBytes caps the offline queue. A device that cannot reach the
// server for weeks must not fill its disk; past this point the oldest batches
// are dropped, which is the least bad option because the newest usage is the
// one the dashboard is missing.
const DefaultQueueMaxBytes int64 = 500 << 20 // 500 MB

// queueSuffix is the extension of a persisted batch file.
const queueSuffix = ".json"

// Queue is a directory of pending ingest payloads, one JSON file per batch,
// named after the nanosecond it was queued at so lexical order is chronological.
type Queue struct {
	dir      string
	maxBytes int64

	// Logf receives warnings (currently: batches dropped at the size cap).
	Logf func(format string, args ...any)

	mu  sync.Mutex
	seq int
}

// OpenQueue prepares dir. maxBytes <= 0 selects DefaultQueueMaxBytes.
//
// The directory itself is created on the first Enqueue, not here, so read-only
// callers such as `agent status` can inspect a queue without creating one.
func OpenQueue(dir string, maxBytes int64) (*Queue, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultQueueMaxBytes
	}
	if dir == "" {
		return nil, fmt.Errorf("transport: empty queue directory")
	}
	return &Queue{dir: dir, maxBytes: maxBytes}, nil
}

// Dir is the directory backing the queue.
func (q *Queue) Dir() string { return q.dir }

func (q *Queue) warnf(format string, args ...any) {
	if q.Logf != nil {
		q.Logf(format, args...)
	}
}

// Item is one queued batch.
type Item struct {
	Name  string // file name, sorts oldest first
	Path  string
	Bytes int64
}

// List returns the queued batches, oldest first.
func (q *Queue) List() ([]Item, error) {
	entries, err := os.ReadDir(q.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("transport: read queue dir: %w", err)
	}
	var out []Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), queueSuffix) || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished between ReadDir and Info: fine, it is gone
		}
		out = append(out, Item{Name: e.Name(), Path: filepath.Join(q.dir, e.Name()), Bytes: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Stats reports the number of queued batches and their total size on disk.
func (q *Queue) Stats() (count int, bytes int64, err error) {
	items, err := q.List()
	if err != nil {
		return 0, 0, err
	}
	for _, it := range items {
		bytes += it.Bytes
	}
	return len(items), bytes, nil
}

// Enqueue persists one ingest payload. Writing is atomic (temp + rename) so a
// half-written file is never picked up by List. If the queue would exceed its
// cap, the oldest batches are dropped first, with a warning.
func (q *Queue) Enqueue(payload []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if int64(len(payload)) > q.maxBytes {
		q.warnf("queue: dropping a %d byte batch, larger than the %d byte queue cap", len(payload), q.maxBytes)
		return fmt.Errorf("transport: batch of %d bytes exceeds queue cap %d", len(payload), q.maxBytes)
	}
	if err := q.prune(int64(len(payload))); err != nil {
		return err
	}
	if err := os.MkdirAll(q.dir, 0o700); err != nil {
		return fmt.Errorf("transport: create queue dir: %w", err)
	}

	name := q.nextName()
	tmp, err := os.CreateTemp(q.dir, ".batch-*.tmp")
	if err != nil {
		return fmt.Errorf("transport: queue temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("transport: write queued batch: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("transport: sync queued batch: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("transport: close queued batch: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(q.dir, name)); err != nil {
		return fmt.Errorf("transport: rename queued batch: %w", err)
	}
	return nil
}

// nextName is a zero padded nanosecond timestamp, with a per-process counter as
// a tiebreaker because two batches of the same tick can land in one nanosecond
// on coarse clocks (notably Windows).
func (q *Queue) nextName() string {
	q.seq++
	for {
		name := fmt.Sprintf("%019d-%04d%s", time.Now().UnixNano(), q.seq, queueSuffix)
		if _, err := os.Stat(filepath.Join(q.dir, name)); errors.Is(err, os.ErrNotExist) {
			return name
		}
		q.seq++
	}
}

// prune drops the oldest batches until incoming more bytes stays under the cap.
func (q *Queue) prune(incoming int64) error {
	items, err := q.List()
	if err != nil {
		return err
	}
	var total int64
	for _, it := range items {
		total += it.Bytes
	}
	for _, it := range items {
		if total+incoming <= q.maxBytes {
			break
		}
		if err := os.Remove(it.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("transport: drop queued batch: %w", err)
		}
		total -= it.Bytes
		q.warnf("queue: dropped oldest batch %s (%d bytes), queue over the %d byte cap", it.Name, it.Bytes, q.maxBytes)
	}
	return nil
}

// Read returns the payload of one queued batch.
func (q *Queue) Read(it Item) ([]byte, error) {
	raw, err := os.ReadFile(it.Path)
	if err != nil {
		return nil, fmt.Errorf("transport: read queued batch: %w", err)
	}
	return raw, nil
}

// Remove deletes a delivered batch.
func (q *Queue) Remove(it Item) error {
	if err := os.Remove(it.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("transport: remove queued batch: %w", err)
	}
	return nil
}
