// Package parallel provides a parallel fan-out/fan-in task handler for workflow execution.
package parallel

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// TaskResult represents the outcome of a single task in a parallel group.
type TaskResult struct {
	TaskID   string        `json:"task_id"`
	Output   interface{}   `json:"output"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
	Success  bool          `json:"success"`
}

// Result represents the combined outcome of all parallel tasks.
type Result struct {
	Results   map[string]*TaskResult `json:"results"`
	Duration  time.Duration          `json:"duration"`
	Completed int                    `json:"completed"`
	Failed    int                    `json:"failed"`
	Total     int                    `json:"total"`
	Success   bool                   `json:"success"`
}

// ParallelConfig defines the configuration for a parallel task.
type ParallelConfig struct {
	Tasks          []string `json:"tasks" yaml:"tasks"`
	MaxConcurrency int      `json:"max_concurrency" yaml:"max_concurrency"`
	FailFast       bool     `json:"fail_fast" yaml:"fail_fast"`
}

// TaskExecutor is a function type that executes a single task and returns its result.
type TaskExecutor func(ctx context.Context, taskID string) (interface{}, error)

// ParallelHandler executes multiple tasks concurrently with configurable concurrency limits.
type ParallelHandler struct{}

// NewParallelHandler creates a new ParallelHandler.
func NewParallelHandler() *ParallelHandler {
	return &ParallelHandler{}
}

// Execute runs all configured tasks concurrently, collecting their results.
// It respects MaxConcurrency to limit the number of tasks running at the same time.
// If FailFast is true, the first error will cancel all remaining tasks.
func (h *ParallelHandler) Execute(ctx context.Context, config ParallelConfig, executor TaskExecutor) (*Result, error) {
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid parallel config: %w", err)
	}

	if executor == nil {
		return nil, fmt.Errorf("task executor is required")
	}

	applyDefaults(&config)

	results := &Result{
		Results: make(map[string]*TaskResult, len(config.Tasks)),
		Total:   len(config.Tasks),
	}

	var mu sync.Mutex
	start := time.Now()

	if config.FailFast {
		results = h.executeFailFast(ctx, config, executor, results, &mu)
	} else {
		results = h.executeAll(ctx, config, executor, results, &mu)
	}

	results.Duration = time.Since(start)
	results.Success = results.Failed == 0

	return results, nil
}

// executeFailFast runs tasks concurrently and cancels on first error.
func (h *ParallelHandler) executeFailFast(
	ctx context.Context,
	config ParallelConfig,
	executor TaskExecutor,
	results *Result,
	mu *sync.Mutex,
) *Result {
	g, gCtx := errgroup.WithContext(ctx)
	sem := semaphore.NewWeighted(int64(config.MaxConcurrency))

	for _, taskID := range config.Tasks {
		taskID := taskID // capture loop variable

		g.Go(func() error {
			if err := sem.Acquire(gCtx, 1); err != nil {
				mu.Lock()
				results.Results[taskID] = &TaskResult{
					TaskID:  taskID,
					Success: false,
					Error:   "cancelled before execution",
				}
				results.Failed++
				mu.Unlock()
				return err
			}
			defer sem.Release(1)

			taskStart := time.Now()
			output, err := executor(gCtx, taskID)
			duration := time.Since(taskStart)

			mu.Lock()
			defer mu.Unlock()

			tr := &TaskResult{
				TaskID:   taskID,
				Output:   output,
				Duration: duration,
			}

			if err != nil {
				tr.Success = false
				tr.Error = err.Error()
				results.Failed++
				results.Results[taskID] = tr
				return err // This cancels the group context.
			}

			tr.Success = true
			results.Completed++
			results.Results[taskID] = tr
			return nil
		})
	}

	// Wait for all goroutines. Ignore the error since we've captured it in results.
	_ = g.Wait()

	return results
}

// executeAll runs all tasks concurrently and collects all results regardless of errors.
func (h *ParallelHandler) executeAll(
	ctx context.Context,
	config ParallelConfig,
	executor TaskExecutor,
	results *Result,
	mu *sync.Mutex,
) *Result {
	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(int64(config.MaxConcurrency))

	for _, taskID := range config.Tasks {
		taskID := taskID
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := sem.Acquire(ctx, 1); err != nil {
				mu.Lock()
				results.Results[taskID] = &TaskResult{
					TaskID:  taskID,
					Success: false,
					Error:   "cancelled before execution",
				}
				results.Failed++
				mu.Unlock()
				return
			}
			defer sem.Release(1)

			taskStart := time.Now()
			output, err := executor(ctx, taskID)
			duration := time.Since(taskStart)

			mu.Lock()
			defer mu.Unlock()

			tr := &TaskResult{
				TaskID:   taskID,
				Output:   output,
				Duration: duration,
			}

			if err != nil {
				tr.Success = false
				tr.Error = err.Error()
				results.Failed++
			} else {
				tr.Success = true
				results.Completed++
			}

			results.Results[taskID] = tr
		}()
	}

	wg.Wait()

	return results
}

// validateConfig validates the parallel task configuration.
func validateConfig(config *ParallelConfig) error {
	if len(config.Tasks) == 0 {
		return fmt.Errorf("at least one task is required")
	}

	// Check for duplicate task IDs.
	seen := make(map[string]bool, len(config.Tasks))
	for _, taskID := range config.Tasks {
		if taskID == "" {
			return fmt.Errorf("task ID cannot be empty")
		}
		if seen[taskID] {
			return fmt.Errorf("duplicate task ID %q", taskID)
		}
		seen[taskID] = true
	}

	if config.MaxConcurrency < 0 {
		return fmt.Errorf("max_concurrency must be >= 0")
	}

	return nil
}

// applyDefaults sets default values for unset config fields.
func applyDefaults(config *ParallelConfig) {
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = len(config.Tasks)
	}
}

// ParseConfig parses a generic config map into a ParallelConfig.
func ParseConfig(config map[string]any) (ParallelConfig, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return ParallelConfig{}, fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfg ParallelConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ParallelConfig{}, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}
