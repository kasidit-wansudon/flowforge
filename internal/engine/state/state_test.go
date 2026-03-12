package state

import (
	"testing"
)

func TestNewStateMachine(t *testing.T) {
	sm := NewStateMachine("wf-1")
	s := sm.State()
	if s.WorkflowID != "wf-1" {
		t.Errorf("expected workflow ID wf-1, got %s", s.WorkflowID)
	}
	if s.Status != WorkflowPending {
		t.Errorf("expected status Pending, got %s", s.Status)
	}
	if s.TaskStates == nil {
		t.Error("TaskStates should be initialized")
	}
}

func TestWorkflowTransitionPendingToRunning(t *testing.T) {
	sm := NewStateMachine("wf-1")
	if err := sm.TransitionWorkflow(WorkflowRunning); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := sm.State()
	if s.Status != WorkflowRunning {
		t.Errorf("expected Running, got %s", s.Status)
	}
	if s.StartedAt == nil {
		t.Error("StartedAt should be set")
	}
}

func TestWorkflowTransitionPendingToQueued(t *testing.T) {
	sm := NewStateMachine("wf-1")
	if err := sm.TransitionWorkflow(WorkflowQueued); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sm.State().Status != WorkflowQueued {
		t.Errorf("expected Queued, got %s", sm.State().Status)
	}
}

func TestWorkflowTransitionInvalid(t *testing.T) {
	sm := NewStateMachine("wf-1")
	// Pending -> Completed is not valid
	err := sm.TransitionWorkflow(WorkflowCompleted)
	if err == nil {
		t.Fatal("expected ErrInvalidTransition")
	}
}

func TestWorkflowTransitionToTerminal(t *testing.T) {
	sm := NewStateMachine("wf-1")
	_ = sm.TransitionWorkflow(WorkflowRunning)
	if err := sm.TransitionWorkflow(WorkflowCompleted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := sm.State()
	if s.Status != WorkflowCompleted {
		t.Errorf("expected Completed, got %s", s.Status)
	}
	if s.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
}

func TestWorkflowTransitionFromTerminalFails(t *testing.T) {
	sm := NewStateMachine("wf-1")
	_ = sm.TransitionWorkflow(WorkflowRunning)
	_ = sm.TransitionWorkflow(WorkflowCompleted)

	err := sm.TransitionWorkflow(WorkflowRunning)
	if err == nil {
		t.Fatal("expected error transitioning from terminal state")
	}
}

func TestWorkflowPauseResume(t *testing.T) {
	sm := NewStateMachine("wf-1")
	_ = sm.TransitionWorkflow(WorkflowRunning)
	if err := sm.TransitionWorkflow(WorkflowPaused); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sm.State().Status != WorkflowPaused {
		t.Error("expected Paused")
	}
	if err := sm.TransitionWorkflow(WorkflowRunning); err != nil {
		t.Fatalf("unexpected error resuming: %v", err)
	}
	if sm.State().Status != WorkflowRunning {
		t.Error("expected Running after resume")
	}
}

func TestSetWorkflowError(t *testing.T) {
	sm := NewStateMachine("wf-1")
	sm.SetWorkflowError("something went wrong")
	s := sm.State()
	if s.Error != "something went wrong" {
		t.Errorf("expected error message, got %s", s.Error)
	}
}

func TestAddAndTransitionTask(t *testing.T) {
	sm := NewStateMachine("wf-1")
	sm.AddTask("task-1")
	ts, err := sm.GetTaskState("task-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts.Status != TaskPending {
		t.Errorf("expected Pending, got %s", ts.Status)
	}

	if err := sm.TransitionTask("task-1", TaskRunning); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts, _ = sm.GetTaskState("task-1")
	if ts.Status != TaskRunning {
		t.Errorf("expected Running, got %s", ts.Status)
	}
	if ts.Attempt != 1 {
		t.Errorf("expected Attempt 1, got %d", ts.Attempt)
	}
}

func TestTaskTransitionInvalid(t *testing.T) {
	sm := NewStateMachine("wf-1")
	sm.AddTask("task-1")
	// Pending -> Success is not valid (must go through Running)
	err := sm.TransitionTask("task-1", TaskSuccess)
	if err == nil {
		t.Fatal("expected ErrInvalidTransition")
	}
}

func TestTaskTransitionNotFound(t *testing.T) {
	sm := NewStateMachine("wf-1")
	err := sm.TransitionTask("nonexistent", TaskRunning)
	if err == nil {
		t.Fatal("expected ErrTaskNotFound")
	}
}

func TestSetTaskError(t *testing.T) {
	sm := NewStateMachine("wf-1")
	sm.AddTask("task-1")
	if err := sm.SetTaskError("task-1", "timeout"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts, _ := sm.GetTaskState("task-1")
	if ts.Error != "timeout" {
		t.Errorf("expected 'timeout', got %s", ts.Error)
	}
}

func TestSetTaskOutput(t *testing.T) {
	sm := NewStateMachine("wf-1")
	sm.AddTask("task-1")
	if err := sm.SetTaskOutput("task-1", map[string]string{"key": "val"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts, _ := sm.GetTaskState("task-1")
	output, ok := ts.Output.(map[string]string)
	if !ok {
		t.Fatal("expected map[string]string output")
	}
	if output["key"] != "val" {
		t.Errorf("expected val, got %s", output["key"])
	}
}

func TestTaskRetryTransition(t *testing.T) {
	sm := NewStateMachine("wf-1")
	sm.AddTask("task-1")
	_ = sm.TransitionTask("task-1", TaskRunning)
	if err := sm.TransitionTask("task-1", TaskRetrying); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts, _ := sm.GetTaskState("task-1")
	if ts.Status != TaskRetrying {
		t.Errorf("expected Retrying, got %s", ts.Status)
	}
	// Retrying -> Running
	if err := sm.TransitionTask("task-1", TaskRunning); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts, _ = sm.GetTaskState("task-1")
	if ts.Attempt != 2 {
		t.Errorf("expected attempt 2, got %d", ts.Attempt)
	}
}

func TestIsTerminalWorkflow(t *testing.T) {
	terminals := []WorkflowStatus{WorkflowCompleted, WorkflowFailed, WorkflowCancelled, WorkflowTimedOut}
	for _, s := range terminals {
		if !IsTerminalWorkflowStatus(s) {
			t.Errorf("expected %s to be terminal", s)
		}
	}
	nonTerminals := []WorkflowStatus{WorkflowPending, WorkflowQueued, WorkflowRunning, WorkflowPaused}
	for _, s := range nonTerminals {
		if IsTerminalWorkflowStatus(s) {
			t.Errorf("expected %s to be non-terminal", s)
		}
	}
}

func TestIsTerminalTask(t *testing.T) {
	terminals := []TaskStatus{TaskSuccess, TaskFailed, TaskSkipped, TaskCancelled, TaskTimedOut}
	for _, s := range terminals {
		if !IsTerminalTaskStatus(s) {
			t.Errorf("expected %s to be terminal", s)
		}
	}
	nonTerminals := []TaskStatus{TaskPending, TaskQueued, TaskRunning, TaskRetrying}
	for _, s := range nonTerminals {
		if IsTerminalTaskStatus(s) {
			t.Errorf("expected %s to be non-terminal", s)
		}
	}
}

func TestIsActiveWorkflow(t *testing.T) {
	active := []WorkflowStatus{WorkflowRunning, WorkflowPaused, WorkflowQueued}
	for _, s := range active {
		if !IsActiveWorkflowStatus(s) {
			t.Errorf("expected %s to be active", s)
		}
	}
	if IsActiveWorkflowStatus(WorkflowPending) {
		t.Error("Pending should not be active")
	}
}

func TestIsActiveTask(t *testing.T) {
	active := []TaskStatus{TaskRunning, TaskQueued, TaskRetrying}
	for _, s := range active {
		if !IsActiveTaskStatus(s) {
			t.Errorf("expected %s to be active", s)
		}
	}
}

func TestValidateWorkflowTransitionFunction(t *testing.T) {
	if err := ValidateWorkflowTransition(WorkflowPending, WorkflowRunning); err != nil {
		t.Fatalf("expected valid transition, got %v", err)
	}
	if err := ValidateWorkflowTransition(WorkflowCompleted, WorkflowRunning); err == nil {
		t.Fatal("expected error for invalid transition from terminal")
	}
}

func TestValidateTaskTransitionFunction(t *testing.T) {
	if err := ValidateTaskTransition(TaskPending, TaskRunning); err != nil {
		t.Fatalf("expected valid transition, got %v", err)
	}
	if err := ValidateTaskTransition(TaskSuccess, TaskRunning); err == nil {
		t.Fatal("expected error for invalid transition from terminal")
	}
}

func TestIsTerminalGeneric(t *testing.T) {
	if !IsTerminal(WorkflowCompleted) {
		t.Error("expected IsTerminal(WorkflowCompleted) == true")
	}
	if !IsTerminal(TaskFailed) {
		t.Error("expected IsTerminal(TaskFailed) == true")
	}
	if IsTerminal("random") {
		t.Error("expected IsTerminal(string) == false")
	}
}

func TestIsActiveGeneric(t *testing.T) {
	if !IsActive(WorkflowRunning) {
		t.Error("expected IsActive(WorkflowRunning) == true")
	}
	if !IsActive(TaskRunning) {
		t.Error("expected IsActive(TaskRunning) == true")
	}
	if IsActive("random") {
		t.Error("expected IsActive(string) == false")
	}
}

func TestAllWorkflowStatuses(t *testing.T) {
	all := AllWorkflowStatuses()
	if len(all) != 8 {
		t.Errorf("expected 8 workflow statuses, got %d", len(all))
	}
}

func TestAllTaskStatuses(t *testing.T) {
	all := AllTaskStatuses()
	if len(all) != 9 {
		t.Errorf("expected 9 task statuses, got %d", len(all))
	}
}

func TestAddTaskIdempotent(t *testing.T) {
	sm := NewStateMachine("wf-1")
	sm.AddTask("task-1")
	_ = sm.TransitionTask("task-1", TaskRunning)
	sm.AddTask("task-1") // should be a no-op

	ts, _ := sm.GetTaskState("task-1")
	if ts.Status != TaskRunning {
		t.Error("AddTask should not reset existing task state")
	}
}
