package semaphore_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/pkg/semaphore"
)

// --- Acquire + Release ---

func TestLocalSemaphore_AcquireAndRelease_Basic(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()

	if err := ls.Acquire(ctx, "key1", 2); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if err := ls.Release("key1"); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
}

func TestLocalSemaphore_Acquire_InvalidLimitReturnsError(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()

	err := ls.Acquire(ctx, "key", 0)
	if err == nil {
		t.Fatal("expected error for limit=0")
	}
	if !errors.Is(err, semaphore.ErrInvalidLimit) {
		t.Errorf("expected ErrInvalidLimit, got: %v", err)
	}

	err = ls.Acquire(ctx, "key", -1)
	if err == nil {
		t.Fatal("expected error for limit=-1")
	}
	if !errors.Is(err, semaphore.ErrInvalidLimit) {
		t.Errorf("expected ErrInvalidLimit, got: %v", err)
	}
}

func TestLocalSemaphore_Release_UnknownKeyReturnsError(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	err := ls.Release("never-acquired")
	if err == nil {
		t.Fatal("expected error releasing unknown key")
	}
	if !errors.Is(err, semaphore.ErrReleaseFailed) {
		t.Errorf("expected ErrReleaseFailed, got: %v", err)
	}
}

func TestLocalSemaphore_Release_DoubleReleaseReturnsError(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()

	if err := ls.Acquire(ctx, "k", 2); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := ls.Release("k"); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	// Second release — no slot held.
	if err := ls.Release("k"); err == nil {
		t.Error("expected error on double release")
	}
}

// --- Concurrency limiting ---

func TestLocalSemaphore_LimitEnforced(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()
	const limit = 3

	// Acquire `limit` slots.
	for i := 0; i < limit; i++ {
		if err := ls.Acquire(ctx, "bounded", limit); err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
	}

	if held := ls.Held("bounded"); held != limit {
		t.Errorf("expected %d held slots, got %d", limit, held)
	}

	// A non-blocking TryAcquire should fail.
	ok, err := ls.TryAcquire("bounded", limit)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if ok {
		t.Error("TryAcquire should have failed when all slots are held")
	}

	// Release one slot and try again.
	if err := ls.Release("bounded"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	ok2, err := ls.TryAcquire("bounded", limit)
	if err != nil {
		t.Fatalf("TryAcquire after release: %v", err)
	}
	if !ok2 {
		t.Error("TryAcquire should succeed after a slot was released")
	}
}

func TestLocalSemaphore_ConcurrentAcquire_LimitRespected(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	const limit = 4
	const goroutines = 20
	const key = "concurrent"

	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()

			if err := ls.Acquire(ctx, key, limit); err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}

			current := concurrent.Add(1)
			mu.Lock()
			if current > maxConcurrent.Load() {
				maxConcurrent.Store(current)
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)
			concurrent.Add(-1)

			if err := ls.Release(key); err != nil {
				t.Errorf("Release: %v", err)
			}
		}()
	}

	wg.Wait()

	if max := maxConcurrent.Load(); max > int64(limit) {
		t.Errorf("max concurrent exceeded limit: got %d, want <= %d", max, limit)
	}
}

// --- Context cancellation ---

func TestLocalSemaphore_Acquire_ContextCancellation(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()
	const limit = 1

	// Exhaust the semaphore.
	if err := ls.Acquire(ctx, "cancel-key", limit); err != nil {
		t.Fatalf("initial Acquire: %v", err)
	}

	// Try to acquire with a context that cancels quickly.
	cancelCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := ls.Acquire(cancelCtx, "cancel-key", limit)
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
	if !errors.Is(err, semaphore.ErrAcquireTimeout) {
		t.Errorf("expected ErrAcquireTimeout, got: %v", err)
	}
}

// --- Available / Held / Limit ---

func TestLocalSemaphore_Available_Held_Limit(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()
	const limit = 5

	// Before any acquire — key doesn't exist yet.
	if ls.Available("newkey") != 0 {
		t.Error("expected Available=0 for unregistered key")
	}

	if err := ls.Acquire(ctx, "newkey", limit); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := ls.Acquire(ctx, "newkey", limit); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if ls.Held("newkey") != 2 {
		t.Errorf("expected Held=2, got %d", ls.Held("newkey"))
	}
	if ls.Available("newkey") != 3 {
		t.Errorf("expected Available=3, got %d", ls.Available("newkey"))
	}
	if ls.Limit("newkey") != limit {
		t.Errorf("expected Limit=%d, got %d", limit, ls.Limit("newkey"))
	}
}

// --- TryAcquire ---

func TestLocalSemaphore_TryAcquire_SucceedsWhenAvailable(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ok, err := ls.TryAcquire("try", 3)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !ok {
		t.Error("expected TryAcquire to succeed when semaphore is empty")
	}
}

func TestLocalSemaphore_TryAcquire_InvalidLimitReturnsError(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	_, err := ls.TryAcquire("k", -5)
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
	if !errors.Is(err, semaphore.ErrInvalidLimit) {
		t.Errorf("expected ErrInvalidLimit, got: %v", err)
	}
}

// --- Keys + Delete ---

func TestLocalSemaphore_Keys_ReturnsAllKeys(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()

	_ = ls.Acquire(ctx, "alpha", 1)
	_ = ls.Acquire(ctx, "beta", 1)
	_ = ls.Acquire(ctx, "gamma", 1)

	keys := ls.Keys()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d: %v", len(keys), keys)
	}
}

func TestLocalSemaphore_Delete_RemovesKey(t *testing.T) {
	ls := semaphore.NewLocalSemaphore()
	ctx := context.Background()

	_ = ls.Acquire(ctx, "to-delete", 2)
	ls.Delete("to-delete")

	if ls.Limit("to-delete") != 0 {
		t.Error("expected key to be removed after Delete")
	}
}
