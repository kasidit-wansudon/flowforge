// Package pool provides a bounded worker pool with graceful shutdown and panic
// recovery for the FlowForge engine.
package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Task is a unit of work submitted to the pool.
type Task func() error

// Pool manages a fixed set of worker goroutines that process submitted tasks.
// It is safe for concurrent use.
type Pool struct {
	maxWorkers int
	taskCh     chan Task
	wg         sync.WaitGroup
	once       sync.Once
	cancel     context.CancelFunc
	ctx        context.Context

	submitted atomic.Int64
	completed atomic.Int64
	failed    atomic.Int64
	panics    atomic.Int64
}

// New creates a worker pool with the given maximum number of concurrent workers
// and task channel buffer size.
func New(maxWorkers, queueSize int) *Pool {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	if queueSize < 0 {
		queueSize = 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		maxWorkers: maxWorkers,
		taskCh:     make(chan Task, queueSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	for i := 0; i < maxWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	return p
}

// Submit enqueues a task for execution. It blocks if the task queue is full.
// Returns an error if the pool has been shut down.
func (p *Pool) Submit(task Task) error {
	select {
	case <-p.ctx.Done():
		return fmt.Errorf("pool: pool is shut down")
	default:
	}

	select {
	case p.taskCh <- task:
		p.submitted.Add(1)
		return nil
	case <-p.ctx.Done():
		return fmt.Errorf("pool: pool is shut down")
	}
}

// Shutdown gracefully shuts down the pool, waiting for all in-flight tasks to
// complete. No new tasks will be accepted after Shutdown is called.
func (p *Pool) Shutdown() {
	p.once.Do(func() {
		p.cancel()
		close(p.taskCh)
		p.wg.Wait()
	})
}

// ShutdownWithTimeout shuts down the pool and waits up to the given duration
// for in-flight tasks to finish. Returns true if the shutdown completed within
// the timeout, false otherwise.
func (p *Pool) ShutdownWithTimeout(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		p.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Stats returns pool statistics.
func (p *Pool) Stats() (submitted, completed, failed, panics int64) {
	return p.submitted.Load(), p.completed.Load(), p.failed.Load(), p.panics.Load()
}

// MaxWorkers returns the configured maximum number of workers.
func (p *Pool) MaxWorkers() int {
	return p.maxWorkers
}

// worker is the main loop for a single worker goroutine.
func (p *Pool) worker() {
	defer p.wg.Done()

	for task := range p.taskCh {
		p.runTask(task)
	}
}

// runTask executes a single task with panic recovery.
func (p *Pool) runTask(task Task) {
	defer func() {
		if r := recover(); r != nil {
			p.panics.Add(1)
			p.failed.Add(1)
		}
	}()

	err := task()
	if err != nil {
		p.failed.Add(1)
	} else {
		p.completed.Add(1)
	}
}
