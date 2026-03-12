package parallel

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewParallelHandler(t *testing.T) {
	h := NewParallelHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestExecuteAllTasksSucceed(t *testing.T) {
	h := NewParallelHandler()
	cfg := ParallelConfig{
		Tasks: []string{"task-1", "task-2", "task-3"},
	}

	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		return fmt.Sprintf("output-%s", taskID), nil
	}

	result, err := h.Execute(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true when all tasks succeed")
	}
	if result.Completed != 3 {
		t.Errorf("expected 3 completed, got %d", result.Completed)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}
	if result.Total != 3 {
		t.Errorf("expected Total=3, got %d", result.Total)
	}
	for _, taskID := range cfg.Tasks {
		tr, ok := result.Results[taskID]
		if !ok {
			t.Errorf("expected result for task %q", taskID)
			continue
		}
		if !tr.Success {
			t.Errorf("task %q should have succeeded", taskID)
		}
	}
}

func TestExecuteSomeTasksFail(t *testing.T) {
	h := NewParallelHandler()
	cfg := ParallelConfig{
		Tasks:    []string{"ok-1", "fail-1", "ok-2"},
		FailFast: false,
	}

	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		if taskID == "fail-1" {
			return nil, fmt.Errorf("task %s failed", taskID)
		}
		return "ok", nil
	}

	result, err := h.Execute(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when a task fails")
	}
	if result.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Failed)
	}
	if result.Completed != 2 {
		t.Errorf("expected 2 completed, got %d", result.Completed)
	}
	tr := result.Results["fail-1"]
	if tr == nil {
		t.Fatal("expected result for 'fail-1'")
	}
	if tr.Success {
		t.Error("expected fail-1 to have Success=false")
	}
	if tr.Error == "" {
		t.Error("expected non-empty Error for failed task")
	}
}

func TestExecuteMaxConcurrency(t *testing.T) {
	const maxConcurrent = 2
	const totalTasks = 6

	h := NewParallelHandler()

	tasks := make([]string, totalTasks)
	for i := range tasks {
		tasks[i] = fmt.Sprintf("task-%d", i)
	}

	cfg := ParallelConfig{
		Tasks:          tasks,
		MaxConcurrency: maxConcurrent,
	}

	var concurrent int64
	var maxObserved int64

	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		cur := atomic.AddInt64(&concurrent, 1)
		// Track peak concurrency.
		for {
			old := atomic.LoadInt64(&maxObserved)
			if cur <= old || atomic.CompareAndSwapInt64(&maxObserved, old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&concurrent, -1)
		return "done", nil
	}

	result, err := h.Execute(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected all tasks to succeed")
	}
	if maxObserved > int64(maxConcurrent) {
		t.Errorf("max concurrent executions exceeded: observed %d, limit %d", maxObserved, maxConcurrent)
	}
}

func TestExecuteFailFast(t *testing.T) {
	h := NewParallelHandler()

	tasks := make([]string, 5)
	for i := range tasks {
		tasks[i] = fmt.Sprintf("task-%d", i)
	}

	cfg := ParallelConfig{
		Tasks:    tasks,
		FailFast: true,
	}

	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		if taskID == "task-0" {
			return nil, fmt.Errorf("task-0 deliberately failed")
		}
		// Other tasks wait a bit to allow cancellation to propagate.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return "done", nil
		}
	}

	result, err := h.Execute(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// At least one task should have failed.
	if result.Failed == 0 {
		t.Error("expected at least one failure in FailFast mode")
	}
	// Overall result should be unsuccessful.
	if result.Success {
		t.Error("expected Success=false in FailFast mode with a failing task")
	}
}

func TestExecuteNoTasks(t *testing.T) {
	h := NewParallelHandler()
	cfg := ParallelConfig{Tasks: []string{}}
	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		return nil, nil
	}
	_, err := h.Execute(context.Background(), cfg, executor)
	if err == nil {
		t.Fatal("expected error when no tasks are provided")
	}
}

func TestExecuteDuplicateTaskIDs(t *testing.T) {
	h := NewParallelHandler()
	cfg := ParallelConfig{Tasks: []string{"task-1", "task-1"}}
	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		return nil, nil
	}
	_, err := h.Execute(context.Background(), cfg, executor)
	if err == nil {
		t.Fatal("expected error for duplicate task IDs")
	}
}

func TestExecuteNilExecutor(t *testing.T) {
	h := NewParallelHandler()
	cfg := ParallelConfig{Tasks: []string{"task-1"}}
	_, err := h.Execute(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error for nil executor")
	}
}

func TestExecuteResultContainsTaskOutputs(t *testing.T) {
	h := NewParallelHandler()
	cfg := ParallelConfig{
		Tasks: []string{"alpha", "beta"},
	}

	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		return map[string]string{"id": taskID, "status": "done"}, nil
	}

	result, err := h.Execute(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, taskID := range cfg.Tasks {
		tr, ok := result.Results[taskID]
		if !ok {
			t.Errorf("expected result for task %q", taskID)
			continue
		}
		out, ok := tr.Output.(map[string]string)
		if !ok {
			t.Errorf("expected map output for task %q, got %T", taskID, tr.Output)
			continue
		}
		if out["id"] != taskID {
			t.Errorf("task %q: expected output id=%q, got %q", taskID, taskID, out["id"])
		}
	}
}

func TestExecuteDuration(t *testing.T) {
	h := NewParallelHandler()
	cfg := ParallelConfig{
		Tasks: []string{"t1", "t2"},
	}
	executor := func(ctx context.Context, taskID string) (interface{}, error) {
		return nil, nil
	}
	result, err := h.Execute(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Duration <= 0 {
		t.Error("expected Duration > 0")
	}
}

func TestParseConfig(t *testing.T) {
	raw := map[string]any{
		"tasks":           []interface{}{"task-a", "task-b"},
		"max_concurrency": float64(3),
		"fail_fast":       true,
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(cfg.Tasks))
	}
	if cfg.MaxConcurrency != 3 {
		t.Errorf("expected MaxConcurrency=3, got %d", cfg.MaxConcurrency)
	}
	if !cfg.FailFast {
		t.Error("expected FailFast=true")
	}
}
