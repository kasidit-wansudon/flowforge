package pool

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewPool(t *testing.T) {
	p := New(4, 10)
	defer p.Shutdown()

	if p.MaxWorkers() != 4 {
		t.Errorf("MaxWorkers() = %d, want 4", p.MaxWorkers())
	}
}

func TestNewPoolMinValues(t *testing.T) {
	p := New(0, -1)
	defer p.Shutdown()

	if p.MaxWorkers() != 1 {
		t.Errorf("MaxWorkers() = %d, want 1 (minimum)", p.MaxWorkers())
	}
}

func TestSubmitAndComplete(t *testing.T) {
	p := New(2, 10)
	defer p.Shutdown()

	var executed atomic.Bool
	err := p.Submit(func() error {
		executed.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}

	// Wait for task to complete.
	time.Sleep(100 * time.Millisecond)

	if !executed.Load() {
		t.Error("task was not executed")
	}

	submitted, completed, failed, panics := p.Stats()
	if submitted != 1 {
		t.Errorf("submitted = %d, want 1", submitted)
	}
	if completed != 1 {
		t.Errorf("completed = %d, want 1", completed)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if panics != 0 {
		t.Errorf("panics = %d, want 0", panics)
	}
}

func TestSubmitMultipleTasks(t *testing.T) {
	p := New(4, 20)
	defer p.Shutdown()

	const n = 10
	var counter atomic.Int64

	for i := 0; i < n; i++ {
		err := p.Submit(func() error {
			counter.Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("Submit() returned error on task %d: %v", i, err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	if counter.Load() != n {
		t.Errorf("counter = %d, want %d", counter.Load(), n)
	}

	submitted, completed, _, _ := p.Stats()
	if submitted != n {
		t.Errorf("submitted = %d, want %d", submitted, n)
	}
	if completed != n {
		t.Errorf("completed = %d, want %d", completed, n)
	}
}

func TestSubmitFailedTask(t *testing.T) {
	p := New(2, 10)
	defer p.Shutdown()

	err := p.Submit(func() error {
		return errors.New("task error")
	})
	if err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_, _, failed, _ := p.Stats()
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}

func TestSubmitPanickingTask(t *testing.T) {
	p := New(2, 10)
	defer p.Shutdown()

	err := p.Submit(func() error {
		panic("test panic")
	})
	if err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_, _, failed, panics := p.Stats()
	if panics != 1 {
		t.Errorf("panics = %d, want 1", panics)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}

func TestShutdown(t *testing.T) {
	p := New(2, 10)

	// Submit a task before shutdown.
	err := p.Submit(func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}

	p.Shutdown()

	// After shutdown, Submit should return an error.
	err = p.Submit(func() error { return nil })
	if err == nil {
		t.Error("Submit() after Shutdown() should return error")
	}
}

func TestShutdownIdempotent(t *testing.T) {
	p := New(2, 10)

	// Calling Shutdown multiple times should not panic.
	p.Shutdown()
	p.Shutdown()
	p.Shutdown()
}

func TestShutdownWithTimeoutSuccess(t *testing.T) {
	p := New(2, 10)

	err := p.Submit(func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}

	ok := p.ShutdownWithTimeout(5 * time.Second)
	if !ok {
		t.Error("ShutdownWithTimeout() returned false, expected true (should complete within timeout)")
	}
}

func TestShutdownWithTimeoutExpired(t *testing.T) {
	p := New(1, 0)

	// Submit a task that takes longer than the timeout.
	err := p.Submit(func() error {
		time.Sleep(5 * time.Second)
		return nil
	})
	if err != nil {
		t.Fatalf("Submit() returned error: %v", err)
	}

	ok := p.ShutdownWithTimeout(50 * time.Millisecond)
	if ok {
		t.Error("ShutdownWithTimeout() returned true, expected false (task should still be running)")
	}
}

func TestConcurrentSubmit(t *testing.T) {
	p := New(4, 100)
	defer p.Shutdown()

	var wg sync.WaitGroup
	const numGoroutines = 10
	const tasksPerGoroutine = 5
	var counter atomic.Int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				err := p.Submit(func() error {
					counter.Add(1)
					return nil
				})
				if err != nil {
					t.Errorf("Submit() returned error: %v", err)
				}
			}
		}()
	}

	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	expected := int64(numGoroutines * tasksPerGoroutine)
	if counter.Load() != expected {
		t.Errorf("counter = %d, want %d", counter.Load(), expected)
	}
}

func TestStatsAccuracy(t *testing.T) {
	p := New(2, 20)
	defer p.Shutdown()

	// 3 successful, 2 failed, 1 panic
	for i := 0; i < 3; i++ {
		p.Submit(func() error { return nil })
	}
	for i := 0; i < 2; i++ {
		p.Submit(func() error { return errors.New("fail") })
	}
	p.Submit(func() error { panic("boom") })

	time.Sleep(200 * time.Millisecond)

	submitted, completed, failed, panics := p.Stats()
	if submitted != 6 {
		t.Errorf("submitted = %d, want 6", submitted)
	}
	if completed != 3 {
		t.Errorf("completed = %d, want 3", completed)
	}
	// failed includes panics
	if failed != 3 {
		t.Errorf("failed = %d, want 3 (2 errors + 1 panic)", failed)
	}
	if panics != 1 {
		t.Errorf("panics = %d, want 1", panics)
	}
}
