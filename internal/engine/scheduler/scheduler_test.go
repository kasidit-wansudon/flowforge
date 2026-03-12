package scheduler

import (
	"errors"
	"testing"
	"time"
)

func TestNewScheduler(t *testing.T) {
	s := NewScheduler()
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
}

func TestScheduleAndNext(t *testing.T) {
	s := NewScheduler()
	err := s.Schedule(ScheduledTask{
		TaskID:     "t-1",
		WorkflowID: "wf-1",
		Priority:   PriorityNormal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	task, err := s.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.TaskID != "t-1" {
		t.Errorf("expected t-1, got %s", task.TaskID)
	}
}

func TestScheduleDuplicateTask(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "t-1", WorkflowID: "wf-1", Priority: PriorityNormal})
	err := s.Schedule(ScheduledTask{TaskID: "t-1", WorkflowID: "wf-1", Priority: PriorityNormal})
	if err == nil {
		t.Fatal("expected ErrTaskAlreadyExists")
	}
}

func TestPriorityOrdering(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "low", WorkflowID: "wf-1", Priority: PriorityLow})
	_ = s.Schedule(ScheduledTask{TaskID: "critical", WorkflowID: "wf-1", Priority: PriorityCritical})
	_ = s.Schedule(ScheduledTask{TaskID: "high", WorkflowID: "wf-1", Priority: PriorityHigh})

	task, _ := s.Next()
	if task.TaskID != "critical" {
		t.Errorf("expected critical first, got %s", task.TaskID)
	}
	task, _ = s.Next()
	if task.TaskID != "high" {
		t.Errorf("expected high second, got %s", task.TaskID)
	}
	task, _ = s.Next()
	if task.TaskID != "low" {
		t.Errorf("expected low third, got %s", task.TaskID)
	}
}

func TestNextNoReadyTasks(t *testing.T) {
	s := NewScheduler()
	_, err := s.Next()
	if !errors.Is(err, ErrNoReadyTasks) {
		t.Errorf("expected ErrNoReadyTasks, got %v", err)
	}
}

func TestMarkComplete(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "t-1", WorkflowID: "wf-1", Priority: PriorityNormal})
	_, _ = s.Next() // move to running

	err := s.MarkComplete("t-1", "result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, err := s.GetTaskState("t-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != StateCompleted {
		t.Errorf("expected completed, got %s", state)
	}
}

func TestMarkFailed(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "t-1", WorkflowID: "wf-1", Priority: PriorityNormal})
	_, _ = s.Next()

	err := s.MarkFailed("t-1", errors.New("boom"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, _ := s.GetTaskState("t-1")
	if state != StateFailed {
		t.Errorf("expected failed, got %s", state)
	}
}

func TestMarkCompleteNotFound(t *testing.T) {
	s := NewScheduler()
	err := s.MarkComplete("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDependencyResolution(t *testing.T) {
	s := NewScheduler()
	// Schedule task-a with no deps (immediately ready)
	_ = s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	// Schedule task-b that depends on a
	_ = s.Schedule(ScheduledTask{TaskID: "b", WorkflowID: "wf-1", Priority: PriorityNormal, Dependencies: []string{"a"}})

	// b should be pending
	state, _ := s.GetTaskState("b")
	if state != StatePending {
		t.Errorf("expected b to be pending, got %s", state)
	}

	// Process a
	task, _ := s.Next()
	if task.TaskID != "a" {
		t.Fatalf("expected a, got %s", task.TaskID)
	}
	_ = s.MarkComplete("a", nil)

	// Now b should be ready
	task, err := s.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.TaskID != "b" {
		t.Errorf("expected b, got %s", task.TaskID)
	}
}

func TestFailedDependencyBlocksDownstream(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	_ = s.Schedule(ScheduledTask{TaskID: "b", WorkflowID: "wf-1", Priority: PriorityNormal, Dependencies: []string{"a"}})

	task, _ := s.Next()
	_ = s.MarkFailed(task.TaskID, errors.New("fail"))

	// b should still be pending since a failed
	_, err := s.Next()
	if !errors.Is(err, ErrNoReadyTasks) {
		t.Errorf("expected no ready tasks, got %v", err)
	}
}

func TestGetPendingTasks(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	_ = s.Schedule(ScheduledTask{TaskID: "b", WorkflowID: "wf-1", Priority: PriorityNormal, Dependencies: []string{"a"}})

	pending := s.GetPendingTasks()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending task, got %d", len(pending))
	}
	if pending[0].TaskID != "b" {
		t.Errorf("expected b, got %s", pending[0].TaskID)
	}
}

func TestGetRunningTasks(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	_, _ = s.Next()

	running := s.GetRunningTasks()
	if len(running) != 1 {
		t.Fatalf("expected 1 running task, got %d", len(running))
	}
	if running[0].TaskID != "a" {
		t.Errorf("expected a, got %s", running[0].TaskID)
	}
}

func TestGetReadyTasks(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	_ = s.Schedule(ScheduledTask{TaskID: "b", WorkflowID: "wf-1", Priority: PriorityHigh})

	ready := s.GetReadyTasks()
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready tasks, got %d", len(ready))
	}
}

func TestStop(t *testing.T) {
	s := NewScheduler()
	s.Stop()

	err := s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	if !errors.Is(err, ErrSchedulerStopped) {
		t.Errorf("expected ErrSchedulerStopped, got %v", err)
	}

	_, err = s.Next()
	if !errors.Is(err, ErrSchedulerStopped) {
		t.Errorf("expected ErrSchedulerStopped, got %v", err)
	}
}

func TestResolveDependencies(t *testing.T) {
	s := NewScheduler()
	_ = s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	_ = s.Schedule(ScheduledTask{TaskID: "b", WorkflowID: "wf-1", Priority: PriorityNormal, Dependencies: []string{"a"}})

	resolved, err := s.ResolveDependencies("b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("b should not be resolved yet (a not complete)")
	}

	// Complete a and resolve again
	task, _ := s.Next()
	_ = s.MarkComplete(task.TaskID, nil)

	resolved, err = s.ResolveDependencies("b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After MarkComplete, promotePendingTasks already ran, so b is already ready
	if !resolved {
		t.Error("b should be resolved after a completes")
	}
}

func TestPriorityString(t *testing.T) {
	tests := []struct {
		p    TaskPriority
		want string
	}{
		{PriorityCritical, "critical"},
		{PriorityHigh, "high"},
		{PriorityNormal, "normal"},
		{PriorityLow, "low"},
		{TaskPriority(99), "priority(99)"},
	}
	for _, tt := range tests {
		got := tt.p.String()
		if got != tt.want {
			t.Errorf("Priority(%d).String() = %s, want %s", int(tt.p), got, tt.want)
		}
	}
}

func TestScheduleAutoSetsTime(t *testing.T) {
	s := NewScheduler()
	before := time.Now()
	_ = s.Schedule(ScheduledTask{TaskID: "a", WorkflowID: "wf-1", Priority: PriorityNormal})
	after := time.Now()

	ready := s.GetReadyTasks()
	if len(ready) != 1 {
		t.Fatal("expected 1 ready task")
	}
	if ready[0].ScheduledAt.Before(before) || ready[0].ScheduledAt.After(after) {
		t.Error("ScheduledAt should be auto-set to approximately now")
	}
}
