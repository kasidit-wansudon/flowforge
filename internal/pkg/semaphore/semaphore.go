// Package semaphore provides a distributed semaphore abstraction. The
// LocalSemaphore implementation uses in-process channels, suitable for
// single-node deployments or testing. Implementations backed by Redis or etcd
// can satisfy the same interface for multi-node coordination.
package semaphore

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrAcquireTimeout is returned when acquiring the semaphore exceeds the
	// context deadline.
	ErrAcquireTimeout = errors.New("semaphore: acquire timed out or context cancelled")

	// ErrReleaseFailed is returned when releasing a semaphore slot that was
	// not held (e.g., double release or releasing an unknown key).
	ErrReleaseFailed = errors.New("semaphore: release failed — slot not held or key unknown")

	// ErrInvalidLimit is returned when a non-positive limit is passed.
	ErrInvalidLimit = errors.New("semaphore: limit must be greater than zero")
)

// ---------------------------------------------------------------------------
// Semaphore interface
// ---------------------------------------------------------------------------

// Semaphore provides a concurrency-limiting primitive keyed by arbitrary
// string keys. Each key has an independent capacity (limit).
type Semaphore interface {
	// Acquire blocks until a slot becomes available for the given key (up to
	// limit concurrent holders), or until the context is cancelled.
	Acquire(ctx context.Context, key string, limit int) error

	// Release frees a slot for the given key so that another caller may
	// acquire it.
	Release(key string) error
}

// ---------------------------------------------------------------------------
// LocalSemaphore — channel-based, in-process implementation
// ---------------------------------------------------------------------------

// LocalSemaphore implements Semaphore using per-key buffered channels. It is
// safe for concurrent use and suitable for single-process deployments.
type LocalSemaphore struct {
	mu   sync.Mutex
	sems map[string]*keySem
}

// keySem holds the buffered channel and metadata for a single semaphore key.
type keySem struct {
	ch    chan struct{}
	limit int
}

// NewLocalSemaphore creates a LocalSemaphore.
func NewLocalSemaphore() *LocalSemaphore {
	return &LocalSemaphore{
		sems: make(map[string]*keySem),
	}
}

// Acquire blocks until a slot is available for key (up to limit), or the
// context is done. If the key does not yet exist, a semaphore with the
// requested limit is created lazily. If the key already exists with a
// different limit, the existing limit is preserved (first writer wins).
func (ls *LocalSemaphore) Acquire(ctx context.Context, key string, limit int) error {
	if limit <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidLimit, limit)
	}

	ls.mu.Lock()
	ks, ok := ls.sems[key]
	if !ok {
		ks = &keySem{
			ch:    make(chan struct{}, limit),
			limit: limit,
		}
		ls.sems[key] = ks
	}
	ls.mu.Unlock()

	select {
	case ks.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: key=%s: %v", ErrAcquireTimeout, key, ctx.Err())
	}
}

// Release frees one slot for the given key. It returns ErrReleaseFailed if the
// key is unknown or there are no held slots to release.
func (ls *LocalSemaphore) Release(key string) error {
	ls.mu.Lock()
	ks, ok := ls.sems[key]
	ls.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: unknown key %s", ErrReleaseFailed, key)
	}

	select {
	case <-ks.ch:
		return nil
	default:
		return fmt.Errorf("%w: no held slots for key %s", ErrReleaseFailed, key)
	}
}

// TryAcquire attempts a non-blocking acquire. It returns true if the slot was
// obtained and false otherwise.
func (ls *LocalSemaphore) TryAcquire(key string, limit int) (bool, error) {
	if limit <= 0 {
		return false, fmt.Errorf("%w: %d", ErrInvalidLimit, limit)
	}

	ls.mu.Lock()
	ks, ok := ls.sems[key]
	if !ok {
		ks = &keySem{
			ch:    make(chan struct{}, limit),
			limit: limit,
		}
		ls.sems[key] = ks
	}
	ls.mu.Unlock()

	select {
	case ks.ch <- struct{}{}:
		return true, nil
	default:
		return false, nil
	}
}

// Available returns the number of slots currently free for the given key. If
// the key has not been created yet, it returns 0 with no error.
func (ls *LocalSemaphore) Available(key string) int {
	ls.mu.Lock()
	ks, ok := ls.sems[key]
	ls.mu.Unlock()

	if !ok {
		return 0
	}
	return ks.limit - len(ks.ch)
}

// Held returns the number of slots currently occupied for the given key.
func (ls *LocalSemaphore) Held(key string) int {
	ls.mu.Lock()
	ks, ok := ls.sems[key]
	ls.mu.Unlock()

	if !ok {
		return 0
	}
	return len(ks.ch)
}

// Limit returns the capacity of the semaphore for the given key, or 0 if the
// key does not exist.
func (ls *LocalSemaphore) Limit(key string) int {
	ls.mu.Lock()
	ks, ok := ls.sems[key]
	ls.mu.Unlock()

	if !ok {
		return 0
	}
	return ks.limit
}

// Keys returns all registered semaphore keys.
func (ls *LocalSemaphore) Keys() []string {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	keys := make([]string, 0, len(ls.sems))
	for k := range ls.sems {
		keys = append(keys, k)
	}
	return keys
}

// Delete removes the semaphore for the given key, releasing any internal
// resources. Callers must ensure no goroutines are blocked on Acquire for
// this key.
func (ls *LocalSemaphore) Delete(key string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	delete(ls.sems, key)
}

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

var _ Semaphore = (*LocalSemaphore)(nil)
