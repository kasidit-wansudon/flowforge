// Package state defines the workflow and task state machines used by the
// FlowForge engine. It enforces valid status transitions and provides helpers
// for querying terminal and active states.
package state

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Workflow status
// ---------------------------------------------------------------------------

// WorkflowStatus represents the lifecycle status of a workflow execution.
type WorkflowStatus string

const (
	WorkflowPending   WorkflowStatus = "pending"
	WorkflowQueued    WorkflowStatus = "queued"
	WorkflowRunning   WorkflowStatus = "running"
	WorkflowPaused    WorkflowStatus = "paused"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowFailed    WorkflowStatus = "failed"
	WorkflowCancelled WorkflowStatus = "cancelled"
	WorkflowTimedOut  WorkflowStatus = "timed_out"
)

// allWorkflowStatuses enumerates every valid WorkflowStatus value.
var allWorkflowStatuses = []WorkflowStatus{
	WorkflowPending, WorkflowQueued, WorkflowRunning, WorkflowPaused,
	WorkflowCompleted, WorkflowFailed, WorkflowCancelled, WorkflowTimedOut,
}

// validWorkflowTransitions defines the set of legal (from -> to) transitions.
var validWorkflowTransitions = map[WorkflowStatus]map[WorkflowStatus]bool{
	WorkflowPending: {
		WorkflowQueued:    true,
		WorkflowRunning:   true,
		WorkflowCancelled: true,
	},
	WorkflowQueued: {
		WorkflowRunning:   true,
		WorkflowCancelled: true,
	},
	WorkflowRunning: {
		WorkflowPaused:    true,
		WorkflowCompleted: true,
		WorkflowFailed:    true,
		WorkflowCancelled: true,
		WorkflowTimedOut:  true,
	},
	WorkflowPaused: {
		WorkflowRunning:   true,
		WorkflowCancelled: true,
	},
	// Terminal states have no outgoing transitions.
	WorkflowCompleted: {},
	WorkflowFailed:    {},
	WorkflowCancelled: {},
	WorkflowTimedOut:  {},
}

// IsTerminalWorkflowStatus returns true if the workflow status is a terminal
// (final) state from which no further transitions are possible.
func IsTerminalWorkflowStatus(s WorkflowStatus) bool {
	switch s {
	case WorkflowCompleted, WorkflowFailed, WorkflowCancelled, WorkflowTimedOut:
		return true
	}
	return false
}

// IsActiveWorkflowStatus returns true if the workflow is in an active
// (non-terminal, non-pending) state.
func IsActiveWorkflowStatus(s WorkflowStatus) bool {
	switch s {
	case WorkflowRunning, WorkflowPaused, WorkflowQueued:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Task status
// ---------------------------------------------------------------------------

// TaskStatus represents the lifecycle status of an individual task.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskSuccess   TaskStatus = "success"
	TaskFailed    TaskStatus = "failed"
	TaskSkipped   TaskStatus = "skipped"
	TaskCancelled TaskStatus = "cancelled"
	TaskTimedOut  TaskStatus = "timed_out"
	TaskRetrying  TaskStatus = "retrying"
)

// allTaskStatuses enumerates every valid TaskStatus value.
var allTaskStatuses = []TaskStatus{
	TaskPending, TaskQueued, TaskRunning, TaskSuccess, TaskFailed,
	TaskSkipped, TaskCancelled, TaskTimedOut, TaskRetrying,
}

// validTaskTransitions defines the set of legal (from -> to) transitions.
var validTaskTransitions = map[TaskStatus]map[TaskStatus]bool{
	TaskPending: {
		TaskQueued:    true,
		TaskRunning:   true,
		TaskSkipped:   true,
		TaskCancelled: true,
	},
	TaskQueued: {
		TaskRunning:   true,
		TaskCancelled: true,
		TaskSkipped:   true,
	},
	TaskRunning: {
		TaskSuccess:   true,
		TaskFailed:    true,
		TaskCancelled: true,
		TaskTimedOut:  true,
		TaskRetrying:  true,
	},
	TaskRetrying: {
		TaskRunning:   true,
		TaskCancelled: true,
		TaskFailed:    true,
	},
	// Terminal states.
	TaskSuccess:   {},
	TaskFailed:    {},
	TaskSkipped:   {},
	TaskCancelled: {},
	TaskTimedOut:  {},
}

// IsTerminalTaskStatus returns true if the task status is terminal.
func IsTerminalTaskStatus(s TaskStatus) bool {
	switch s {
	case TaskSuccess, TaskFailed, TaskSkipped, TaskCancelled, TaskTimedOut:
		return true
	}
	return false
}

// IsActiveTaskStatus returns true if the task is actively executing or about
// to execute.
func IsActiveTaskStatus(s TaskStatus) bool {
	switch s {
	case TaskRunning, TaskQueued, TaskRetrying:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// State structs
// ---------------------------------------------------------------------------

// TaskState holds the runtime state of a single task within a workflow
// execution.
type TaskState struct {
	TaskID      string      `json:"task_id"`
	Status      TaskStatus  `json:"status"`
	StartedAt   *time.Time  `json:"started_at,omitempty"`
	CompletedAt *time.Time  `json:"completed_at,omitempty"`
	Attempt     int         `json:"attempt"`
	Error       string      `json:"error,omitempty"`
	Output      interface{} `json:"output,omitempty"`
}

// WorkflowState holds the runtime state of an entire workflow execution.
type WorkflowState struct {
	WorkflowID  string                `json:"workflow_id"`
	Status      WorkflowStatus        `json:"status"`
	StartedAt   *time.Time            `json:"started_at,omitempty"`
	CompletedAt *time.Time            `json:"completed_at,omitempty"`
	TaskStates  map[string]*TaskState `json:"task_states"`
	Error       string                `json:"error,omitempty"`
}

// NewWorkflowState creates a new WorkflowState in the Pending status.
func NewWorkflowState(workflowID string) *WorkflowState {
	return &WorkflowState{
		WorkflowID: workflowID,
		Status:     WorkflowPending,
		TaskStates: make(map[string]*TaskState),
	}
}

// NewTaskState creates a new TaskState in the Pending status.
func NewTaskState(taskID string) *TaskState {
	return &TaskState{
		TaskID:  taskID,
		Status:  TaskPending,
		Attempt: 0,
	}
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrInvalidTransition = errors.New("state: invalid status transition")
	ErrTaskNotFound      = errors.New("state: task not found")
)

// ---------------------------------------------------------------------------
// StateMachine — thread-safe state management
// ---------------------------------------------------------------------------

// StateMachine manages the state of a single workflow execution, including all
// of its task states. All methods are safe for concurrent use.
type StateMachine struct {
	mu    sync.RWMutex
	state *WorkflowState
}

// NewStateMachine creates a StateMachine for the given workflow ID. The
// initial workflow status is Pending.
func NewStateMachine(workflowID string) *StateMachine {
	return &StateMachine{
		state: NewWorkflowState(workflowID),
	}
}

// State returns a snapshot of the current workflow state. The returned value
// is a shallow copy; callers should not mutate the TaskStates map.
func (sm *StateMachine) State() WorkflowState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	// Copy the top-level struct (TaskStates map is shared — read-only use).
	cp := *sm.state
	return cp
}

// ---------------------------------------------------------------------------
// Workflow transitions
// ---------------------------------------------------------------------------

// TransitionWorkflow attempts to move the workflow from its current status to
// the target status. It returns ErrInvalidTransition if the transition is not
// allowed.
func (sm *StateMachine) TransitionWorkflow(to WorkflowStatus) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if err := ValidateWorkflowTransition(sm.state.Status, to); err != nil {
		return err
	}

	now := time.Now()
	sm.state.Status = to

	switch to {
	case WorkflowRunning:
		if sm.state.StartedAt == nil {
			sm.state.StartedAt = &now
		}
	case WorkflowCompleted, WorkflowFailed, WorkflowCancelled, WorkflowTimedOut:
		sm.state.CompletedAt = &now
	}

	return nil
}

// SetWorkflowError records an error message on the workflow state.
func (sm *StateMachine) SetWorkflowError(errMsg string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.state.Error = errMsg
}

// ---------------------------------------------------------------------------
// Task transitions
// ---------------------------------------------------------------------------

// AddTask registers a new task with Pending status. If the task already
// exists, this is a no-op.
func (sm *StateMachine) AddTask(taskID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if _, ok := sm.state.TaskStates[taskID]; !ok {
		sm.state.TaskStates[taskID] = NewTaskState(taskID)
	}
}

// TransitionTask attempts to move the specified task from its current status
// to the target status.
func (sm *StateMachine) TransitionTask(taskID string, to TaskStatus) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ts, ok := sm.state.TaskStates[taskID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	if err := ValidateTaskTransition(ts.Status, to); err != nil {
		return fmt.Errorf("task %s: %w", taskID, err)
	}

	now := time.Now()
	ts.Status = to

	switch to {
	case TaskRunning:
		if ts.StartedAt == nil {
			ts.StartedAt = &now
		}
		ts.Attempt++
	case TaskRetrying:
		// Attempt count is incremented when transitioning to Running.
	case TaskSuccess, TaskFailed, TaskSkipped, TaskCancelled, TaskTimedOut:
		ts.CompletedAt = &now
	}

	return nil
}

// SetTaskError records an error message on the given task.
func (sm *StateMachine) SetTaskError(taskID string, errMsg string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ts, ok := sm.state.TaskStates[taskID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	ts.Error = errMsg
	return nil
}

// SetTaskOutput records the output for the given task.
func (sm *StateMachine) SetTaskOutput(taskID string, output interface{}) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ts, ok := sm.state.TaskStates[taskID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	ts.Output = output
	return nil
}

// GetTaskState returns the state of a task, or ErrTaskNotFound.
func (sm *StateMachine) GetTaskState(taskID string) (TaskState, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ts, ok := sm.state.TaskStates[taskID]
	if !ok {
		return TaskState{}, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	return *ts, nil
}

// ---------------------------------------------------------------------------
// Standalone validation helpers
// ---------------------------------------------------------------------------

// ValidateWorkflowTransition checks whether a workflow transition from -> to
// is valid without actually performing it.
func ValidateWorkflowTransition(from, to WorkflowStatus) error {
	allowed, ok := validWorkflowTransitions[from]
	if !ok {
		return fmt.Errorf("%w: unknown status %s", ErrInvalidTransition, from)
	}
	if !allowed[to] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

// ValidateTaskTransition checks whether a task transition from -> to is valid.
func ValidateTaskTransition(from, to TaskStatus) error {
	allowed, ok := validTaskTransitions[from]
	if !ok {
		return fmt.Errorf("%w: unknown status %s", ErrInvalidTransition, from)
	}
	if !allowed[to] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

// IsTerminal returns true if the given status (workflow or task) is terminal.
// It accepts both WorkflowStatus and TaskStatus via the fmt.Stringer
// interface and raw strings.
func IsTerminal(status interface{}) bool {
	switch s := status.(type) {
	case WorkflowStatus:
		return IsTerminalWorkflowStatus(s)
	case TaskStatus:
		return IsTerminalTaskStatus(s)
	default:
		return false
	}
}

// IsActive returns true if the given status (workflow or task) is active.
func IsActive(status interface{}) bool {
	switch s := status.(type) {
	case WorkflowStatus:
		return IsActiveWorkflowStatus(s)
	case TaskStatus:
		return IsActiveTaskStatus(s)
	default:
		return false
	}
}

// AllWorkflowStatuses returns every defined WorkflowStatus value.
func AllWorkflowStatuses() []WorkflowStatus {
	out := make([]WorkflowStatus, len(allWorkflowStatuses))
	copy(out, allWorkflowStatuses)
	return out
}

// AllTaskStatuses returns every defined TaskStatus value.
func AllTaskStatuses() []TaskStatus {
	out := make([]TaskStatus, len(allTaskStatuses))
	copy(out, allTaskStatuses)
	return out
}
