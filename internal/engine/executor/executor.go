// Package executor provides the task execution framework for FlowForge. It
// supports registering typed task handlers, executing tasks with context-based
// timeouts and cancellation, and optionally wrapping execution with a retry
// policy.
package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/engine/retry"
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

// Task represents a unit of work to be executed.
type Task struct {
	ID         string         `json:"id"`
	WorkflowID string         `json:"workflow_id"`
	Type       string         `json:"type"`
	Config     map[string]any `json:"config,omitempty"`
	Timeout    time.Duration  `json:"timeout,omitempty"`
	Retry      *retry.RetryPolicy `json:"retry,omitempty"`
}

// TaskResult captures the outcome of executing a task.
type TaskResult struct {
	Output   interface{}   `json:"output,omitempty"`
	Duration time.Duration `json:"duration"`
	Attempt  int           `json:"attempt"`
	Error    error         `json:"error,omitempty"`
}

// HandlerFunc is the function signature that task handlers must implement. The
// context carries the deadline/cancellation for the individual execution
// attempt. The map is the task's configuration. The returned interface{} is
// the task output.
type HandlerFunc func(ctx context.Context, config map[string]any) (interface{}, error)

// ---------------------------------------------------------------------------
// Executor interface
// ---------------------------------------------------------------------------

// Executor defines the contract for running tasks.
type Executor interface {
	// Execute runs a task synchronously, honouring its timeout.
	Execute(ctx context.Context, task Task) (*TaskResult, error)
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrHandlerNotFound  = errors.New("executor: no handler registered for task type")
	ErrTaskTimeout      = errors.New("executor: task execution timed out")
	ErrTaskCancelled    = errors.New("executor: task execution cancelled")
	ErrTaskPanicked     = errors.New("executor: task handler panicked")
	ErrHandlerExists    = errors.New("executor: handler already registered for task type")
)

// ---------------------------------------------------------------------------
// DefaultExecutor
// ---------------------------------------------------------------------------

// DefaultExecutor is the standard Executor implementation. It dispatches tasks
// to registered handlers and enforces per-task timeouts.
type DefaultExecutor struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

// NewDefaultExecutor creates a DefaultExecutor with no registered handlers.
func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{
		handlers: make(map[string]HandlerFunc),
	}
}

// Register associates a handler function with a task type. If a handler for
// the given type already exists, ErrHandlerExists is returned.
func (e *DefaultExecutor) Register(taskType string, handler HandlerFunc) error {
	if taskType == "" {
		return fmt.Errorf("executor: task type must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("executor: handler must not be nil")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.handlers[taskType]; exists {
		return fmt.Errorf("%w: %s", ErrHandlerExists, taskType)
	}
	e.handlers[taskType] = handler
	return nil
}

// ReplaceHandler registers or replaces a handler for the given task type.
func (e *DefaultExecutor) ReplaceHandler(taskType string, handler HandlerFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[taskType] = handler
}

// getHandler returns the handler for the given task type.
func (e *DefaultExecutor) getHandler(taskType string) (HandlerFunc, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	h, ok := e.handlers[taskType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrHandlerNotFound, taskType)
	}
	return h, nil
}

// Execute runs the task by looking up the appropriate handler, wrapping the
// call in a timeout context, and capturing the result. It is safe for
// concurrent use.
func (e *DefaultExecutor) Execute(ctx context.Context, task Task) (*TaskResult, error) {
	handler, err := e.getHandler(task.Type)
	if err != nil {
		return nil, err
	}

	// Derive a context with the task's timeout, if configured.
	execCtx := ctx
	var cancel context.CancelFunc
	if task.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, task.Timeout)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	start := time.Now()

	// Run the handler in a goroutine to capture panics.
	type handlerResult struct {
		output interface{}
		err    error
	}
	resultCh := make(chan handlerResult, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- handlerResult{
					err: fmt.Errorf("%w: %v", ErrTaskPanicked, r),
				}
			}
		}()
		out, herr := handler(execCtx, task.Config)
		resultCh <- handlerResult{output: out, err: herr}
	}()

	select {
	case res := <-resultCh:
		duration := time.Since(start)
		if res.err != nil {
			return &TaskResult{
				Duration: duration,
				Attempt:  1,
				Error:    res.err,
			}, res.err
		}
		return &TaskResult{
			Output:   res.output,
			Duration: duration,
			Attempt:  1,
		}, nil

	case <-execCtx.Done():
		duration := time.Since(start)
		ctxErr := execCtx.Err()

		taskErr := ErrTaskTimeout
		if errors.Is(ctxErr, context.Canceled) {
			taskErr = ErrTaskCancelled
		}

		return &TaskResult{
			Duration: duration,
			Attempt:  1,
			Error:    taskErr,
		}, taskErr
	}
}

// ---------------------------------------------------------------------------
// ExecuteWithRetry
// ---------------------------------------------------------------------------

// ExecuteWithRetry wraps Execute with the retry policy attached to the task
// (or the provided fallback policy). Each retry calls Execute independently,
// so per-attempt timeouts apply.
func (e *DefaultExecutor) ExecuteWithRetry(ctx context.Context, task Task, fallbackPolicy *retry.RetryPolicy) (*TaskResult, error) {
	// Determine the retry policy to use.
	rp := task.Retry
	if rp == nil {
		if fallbackPolicy != nil {
			rp = fallbackPolicy
		} else {
			// No retry — execute once.
			return e.Execute(ctx, task)
		}
	}

	policy := retry.PolicyFromConfig(*rp)

	var lastResult *TaskResult
	var lastErr error

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastResult != nil {
				lastResult.Error = fmt.Errorf("context cancelled after attempt %d: %w", attempt, err)
				return lastResult, lastResult.Error
			}
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		result, err := e.Execute(ctx, task)
		if result != nil {
			result.Attempt = attempt + 1
		}

		if err == nil {
			return result, nil
		}

		lastResult = result
		lastErr = err

		if !policy.ShouldRetry(attempt, err) {
			if result != nil {
				result.Error = fmt.Errorf("max retries exceeded: %w", err)
				return result, result.Error
			}
			return nil, fmt.Errorf("max retries exceeded: %w", err)
		}

		delay := policy.NextDelay(attempt)
		if delay > 0 {
			select {
			case <-ctx.Done():
				return lastResult, fmt.Errorf("context cancelled during retry backoff: %w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(delay):
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Convenience: compile-time interface check
// ---------------------------------------------------------------------------

var _ Executor = (*DefaultExecutor)(nil)
