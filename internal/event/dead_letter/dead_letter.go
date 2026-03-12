// Package deadletter provides a dead-letter queue (DLQ) for events that fail
// processing after exhausting retry attempts. It supports age-based expiry and
// selective retry of individual messages.
package deadletter

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds dead-letter queue parameters.
type Config struct {
	// MaxRetries is the maximum number of times a message may be retried
	// from the DLQ before it is permanently discarded.
	MaxRetries int
	// MaxAge is the maximum duration a dead letter may remain in the queue.
	// Entries older than MaxAge are eligible for automatic expiry. A zero
	// value means entries never expire.
	MaxAge time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxRetries: 3,
		MaxAge:     72 * time.Hour,
	}
}

// ---------------------------------------------------------------------------
// DeadLetter
// ---------------------------------------------------------------------------

// DeadLetter wraps a failed message together with error information and
// retry metadata.
type DeadLetter struct {
	// ID uniquely identifies this dead-letter entry.
	ID string
	// OriginalMessage is the raw payload that failed processing.
	OriginalMessage []byte
	// Subject is the original event subject.
	Subject string
	// Error describes why processing failed.
	Error string
	// Attempts counts the total number of processing attempts (original +
	// retries from the DLQ).
	Attempts int
	// FirstFailedAt records the timestamp of the very first failure.
	FirstFailedAt time.Time
	// LastFailedAt records the timestamp of the most recent failure.
	LastFailedAt time.Time
	// Headers preserves the original message headers.
	Headers map[string]string
}

// Expired returns true when the dead letter has been in the queue longer
// than maxAge. A zero maxAge is treated as "never expires".
func (dl *DeadLetter) Expired(maxAge time.Duration) bool {
	if maxAge <= 0 {
		return false
	}
	return time.Since(dl.FirstFailedAt) > maxAge
}

// ---------------------------------------------------------------------------
// DeadLetterQueue interface
// ---------------------------------------------------------------------------

// DeadLetterQueue defines the operations supported by a dead-letter store.
type DeadLetterQueue interface {
	// Push adds a dead-letter entry to the queue.
	Push(dl *DeadLetter) error
	// Pop removes and returns the oldest entry. Returns nil, nil when the
	// queue is empty.
	Pop() (*DeadLetter, error)
	// Peek returns the oldest entry without removing it. Returns nil, nil
	// when the queue is empty.
	Peek() (*DeadLetter, error)
	// Len returns the number of entries currently in the queue.
	Len() int
	// Retry marks the entry identified by id for reprocessing. The
	// implementation should increment the attempt counter and return an
	// error when retries are exhausted.
	Retry(id string) (*DeadLetter, error)
	// Get returns the dead-letter entry with the given id.
	Get(id string) (*DeadLetter, error)
	// List returns all entries currently in the queue.
	List() []*DeadLetter
	// Remove permanently deletes the entry with the given id.
	Remove(id string) error
	// Purge removes all expired entries and returns the number removed.
	Purge() int
}

// ---------------------------------------------------------------------------
// InMemoryDLQ
// ---------------------------------------------------------------------------

// InMemoryDLQ is a mutex-protected, slice-backed dead-letter queue suitable
// for single-process deployments and testing.
type InMemoryDLQ struct {
	mu     sync.Mutex
	items  []*DeadLetter
	index  map[string]int // id -> position in items slice
	config Config
}

// NewInMemoryDLQ creates an InMemoryDLQ with the given configuration.
func NewInMemoryDLQ(cfg Config) *InMemoryDLQ {
	return &InMemoryDLQ{
		items:  make([]*DeadLetter, 0),
		index:  make(map[string]int),
		config: cfg,
	}
}

// Push adds a dead-letter entry. If the entry has no ID one is generated.
func (q *InMemoryDLQ) Push(dl *DeadLetter) error {
	if dl == nil {
		return errors.New("deadletter: cannot push nil entry")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if dl.ID == "" {
		dl.ID = uuid.New().String()
	}
	if dl.FirstFailedAt.IsZero() {
		dl.FirstFailedAt = time.Now().UTC()
	}
	if dl.LastFailedAt.IsZero() {
		dl.LastFailedAt = dl.FirstFailedAt
	}
	if dl.Attempts == 0 {
		dl.Attempts = 1
	}

	q.items = append(q.items, dl)
	q.index[dl.ID] = len(q.items) - 1
	return nil
}

// Pop removes and returns the oldest entry. Returns (nil, nil) when empty.
func (q *InMemoryDLQ) Pop() (*DeadLetter, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return nil, nil
	}

	dl := q.items[0]
	q.removeLocked(0)
	return dl, nil
}

// Peek returns the oldest entry without removing it.
func (q *InMemoryDLQ) Peek() (*DeadLetter, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return nil, nil
	}
	return q.items[0], nil
}

// Len returns the current queue depth.
func (q *InMemoryDLQ) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Retry marks the entry for reprocessing by incrementing its attempt counter
// and updating LastFailedAt. If the maximum retry count is exceeded an error
// is returned and the entry is removed from the queue.
func (q *InMemoryDLQ) Retry(id string) (*DeadLetter, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	idx, ok := q.index[id]
	if !ok {
		return nil, fmt.Errorf("deadletter: entry %s not found", id)
	}

	dl := q.items[idx]
	dl.Attempts++
	dl.LastFailedAt = time.Now().UTC()

	if q.config.MaxRetries > 0 && dl.Attempts > q.config.MaxRetries {
		q.removeLocked(idx)
		return dl, fmt.Errorf("deadletter: entry %s exceeded max retries (%d)", id, q.config.MaxRetries)
	}

	// Remove from queue — caller is expected to re-publish.
	q.removeLocked(idx)
	return dl, nil
}

// Get returns the entry with the given ID.
func (q *InMemoryDLQ) Get(id string) (*DeadLetter, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	idx, ok := q.index[id]
	if !ok {
		return nil, fmt.Errorf("deadletter: entry %s not found", id)
	}
	return q.items[idx], nil
}

// List returns a snapshot of all entries.
func (q *InMemoryDLQ) List() []*DeadLetter {
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]*DeadLetter, len(q.items))
	copy(out, q.items)
	return out
}

// Remove permanently deletes the entry with the given ID.
func (q *InMemoryDLQ) Remove(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	idx, ok := q.index[id]
	if !ok {
		return fmt.Errorf("deadletter: entry %s not found", id)
	}
	q.removeLocked(idx)
	return nil
}

// Purge removes all entries whose age exceeds Config.MaxAge and returns the
// number of entries purged.
func (q *InMemoryDLQ) Purge() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.config.MaxAge <= 0 {
		return 0
	}

	purged := 0
	remaining := make([]*DeadLetter, 0, len(q.items))
	for _, dl := range q.items {
		if dl.Expired(q.config.MaxAge) {
			delete(q.index, dl.ID)
			purged++
		} else {
			remaining = append(remaining, dl)
		}
	}

	q.items = remaining
	q.rebuildIndex()
	return purged
}

// removeLocked removes the item at position idx and rebuilds the index.
// Caller must hold q.mu.
func (q *InMemoryDLQ) removeLocked(idx int) {
	id := q.items[idx].ID
	delete(q.index, id)

	// Swap with last element for O(1) removal, then shrink.
	last := len(q.items) - 1
	if idx != last {
		q.items[idx] = q.items[last]
		q.index[q.items[idx].ID] = idx
	}
	q.items = q.items[:last]
}

// rebuildIndex reconstructs the id -> position mapping.
func (q *InMemoryDLQ) rebuildIndex() {
	q.index = make(map[string]int, len(q.items))
	for i, dl := range q.items {
		q.index[dl.ID] = i
	}
}
