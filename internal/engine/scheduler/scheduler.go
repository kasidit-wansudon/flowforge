// Package scheduler implements a priority-based task scheduler that manages
// task readiness by resolving dependencies before dispatching work.
package scheduler

import (
	"container/heap"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Task priority
// ---------------------------------------------------------------------------

// TaskPriority represents the urgency level of a scheduled task. Lower numeric
// values indicate higher priority.
type TaskPriority int

const (
	PriorityCritical TaskPriority = 0
	PriorityHigh     TaskPriority = 1
	PriorityNormal   TaskPriority = 2
	PriorityLow      TaskPriority = 3
)

// String returns a human-readable label for the priority.
func (p TaskPriority) String() string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityNormal:
		return "normal"
	case PriorityLow:
		return "low"
	default:
		return fmt.Sprintf("priority(%d)", int(p))
	}
}

// ---------------------------------------------------------------------------
// Task status within the scheduler
// ---------------------------------------------------------------------------

// TaskState tracks where a task is in the scheduler's lifecycle.
type TaskState string

const (
	StatePending   TaskState = "pending"
	StateReady     TaskState = "ready"
	StateRunning   TaskState = "running"
	StateCompleted TaskState = "completed"
	StateFailed    TaskState = "failed"
)

// ---------------------------------------------------------------------------
// ScheduledTask
// ---------------------------------------------------------------------------

// ScheduledTask represents a unit of work submitted to the scheduler.
type ScheduledTask struct {
	TaskID       string       `json:"task_id"`
	WorkflowID   string       `json:"workflow_id"`
	Priority     TaskPriority `json:"priority"`
	Dependencies []string     `json:"dependencies,omitempty"`
	ScheduledAt  time.Time    `json:"scheduled_at"`

	// internal bookkeeping (not exported for JSON)
	state TaskState
	index int // position in the priority queue heap
}

// TaskResult holds the outcome of a completed or failed task.
type TaskResult struct {
	TaskID string
	Output interface{}
	Error  error
}

// ---------------------------------------------------------------------------
// Priority queue (min-heap)
// ---------------------------------------------------------------------------

type taskQueue []*ScheduledTask

func (tq taskQueue) Len() int { return len(tq) }

func (tq taskQueue) Less(i, j int) bool {
	// Lower priority value = higher urgency.
	if tq[i].Priority != tq[j].Priority {
		return tq[i].Priority < tq[j].Priority
	}
	// Break ties by scheduled time (earlier first).
	return tq[i].ScheduledAt.Before(tq[j].ScheduledAt)
}

func (tq taskQueue) Swap(i, j int) {
	tq[i], tq[j] = tq[j], tq[i]
	tq[i].index = i
	tq[j].index = j
}

func (tq *taskQueue) Push(x interface{}) {
	n := len(*tq)
	task := x.(*ScheduledTask)
	task.index = n
	*tq = append(*tq, task)
}

func (tq *taskQueue) Pop() interface{} {
	old := *tq
	n := len(old)
	task := old[n-1]
	old[n-1] = nil // GC
	task.index = -1
	*tq = old[:n-1]
	return task
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrTaskNotFound       = errors.New("scheduler: task not found")
	ErrTaskAlreadyExists  = errors.New("scheduler: task already exists")
	ErrNoReadyTasks       = errors.New("scheduler: no ready tasks available")
	ErrSchedulerStopped   = errors.New("scheduler: scheduler is stopped")
	ErrDependencyNotFound = errors.New("scheduler: dependency task not found")
)

// ---------------------------------------------------------------------------
// Scheduler
// ---------------------------------------------------------------------------

// Scheduler is a priority-based task scheduler with dependency resolution. It
// is safe for concurrent use.
type Scheduler struct {
	mu sync.Mutex

	// readyQueue contains tasks whose dependencies are fully satisfied and
	// are waiting to be dispatched.
	readyQueue taskQueue

	// all tracks every task by ID regardless of state.
	all map[string]*ScheduledTask

	// completed tracks tasks that have finished (successfully or not). The
	// value is nil for success or the error for failures.
	completed map[string]*TaskResult

	// running tracks tasks that are currently executing.
	running map[string]*ScheduledTask

	stopped bool
}

// NewScheduler creates a new Scheduler.
func NewScheduler() *Scheduler {
	s := &Scheduler{
		readyQueue: make(taskQueue, 0),
		all:        make(map[string]*ScheduledTask),
		completed:  make(map[string]*TaskResult),
		running:    make(map[string]*ScheduledTask),
	}
	heap.Init(&s.readyQueue)
	return s
}

// Schedule adds a task to the scheduler. If all dependencies are already
// satisfied the task becomes immediately ready; otherwise it stays pending
// until ResolveDependencies marks it ready.
func (s *Scheduler) Schedule(task ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return ErrSchedulerStopped
	}

	if _, exists := s.all[task.TaskID]; exists {
		return fmt.Errorf("%w: %s", ErrTaskAlreadyExists, task.TaskID)
	}

	t := &ScheduledTask{
		TaskID:       task.TaskID,
		WorkflowID:   task.WorkflowID,
		Priority:     task.Priority,
		Dependencies: make([]string, len(task.Dependencies)),
		ScheduledAt:  task.ScheduledAt,
		state:        StatePending,
	}
	copy(t.Dependencies, task.Dependencies)

	if t.ScheduledAt.IsZero() {
		t.ScheduledAt = time.Now()
	}

	s.all[t.TaskID] = t

	// If no dependencies or all already completed, mark ready immediately.
	if s.areDependenciesSatisfied(t) {
		t.state = StateReady
		heap.Push(&s.readyQueue, t)
	}

	return nil
}

// Next removes and returns the highest-priority ready task. It returns
// ErrNoReadyTasks if no tasks are ready.
func (s *Scheduler) Next() (*ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return nil, ErrSchedulerStopped
	}
	if s.readyQueue.Len() == 0 {
		return nil, ErrNoReadyTasks
	}

	task := heap.Pop(&s.readyQueue).(*ScheduledTask)
	task.state = StateRunning
	s.running[task.TaskID] = task
	return task, nil
}

// MarkComplete records a task as successfully completed and resolves
// downstream dependencies.
func (s *Scheduler) MarkComplete(taskID string, result interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.running[taskID]
	if !ok {
		// Also check if it exists at all.
		if _, exists := s.all[taskID]; !exists {
			return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
		}
		return fmt.Errorf("scheduler: task %s is not running", taskID)
	}

	task.state = StateCompleted
	delete(s.running, taskID)
	s.completed[taskID] = &TaskResult{TaskID: taskID, Output: result}

	// Promote pending tasks whose deps are now satisfied.
	s.promotePendingTasks()
	return nil
}

// MarkFailed records a task as failed. Downstream tasks that depend on this
// task will NOT be promoted.
func (s *Scheduler) MarkFailed(taskID string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.running[taskID]
	if !ok {
		if _, exists := s.all[taskID]; !exists {
			return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
		}
		return fmt.Errorf("scheduler: task %s is not running", taskID)
	}

	task.state = StateFailed
	delete(s.running, taskID)
	s.completed[taskID] = &TaskResult{TaskID: taskID, Error: err}

	return nil
}

// ResolveDependencies checks whether all dependencies for the given task are
// satisfied. If they are and the task is pending, it is promoted to ready.
func (s *Scheduler) ResolveDependencies(taskID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.all[taskID]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	if task.state != StatePending {
		return task.state == StateReady || task.state == StateRunning || task.state == StateCompleted, nil
	}

	if s.areDependenciesSatisfied(task) {
		task.state = StateReady
		heap.Push(&s.readyQueue, task)
		return true, nil
	}

	return false, nil
}

// GetPendingTasks returns a snapshot of all tasks in the pending state.
func (s *Scheduler) GetPendingTasks() []*ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	var tasks []*ScheduledTask
	for _, t := range s.all {
		if t.state == StatePending {
			cp := *t
			tasks = append(tasks, &cp)
		}
	}
	return tasks
}

// GetRunningTasks returns a snapshot of all tasks in the running state.
func (s *Scheduler) GetRunningTasks() []*ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := make([]*ScheduledTask, 0, len(s.running))
	for _, t := range s.running {
		cp := *t
		tasks = append(tasks, &cp)
	}
	return tasks
}

// GetReadyTasks returns a snapshot of all tasks in the ready queue without
// removing them.
func (s *Scheduler) GetReadyTasks() []*ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := make([]*ScheduledTask, 0, s.readyQueue.Len())
	for _, t := range s.readyQueue {
		cp := *t
		tasks = append(tasks, &cp)
	}
	return tasks
}

// GetTaskState returns the current scheduler state for the given task.
func (s *Scheduler) GetTaskState(taskID string) (TaskState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.all[taskID]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	return t.state, nil
}

// Stop prevents new tasks from being scheduled and new dispatches from
// happening. Already-running tasks are unaffected.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// areDependenciesSatisfied returns true if every dependency of t has completed
// successfully. Must be called with s.mu held.
func (s *Scheduler) areDependenciesSatisfied(t *ScheduledTask) bool {
	for _, dep := range t.Dependencies {
		result, ok := s.completed[dep]
		if !ok {
			return false
		}
		// A failed dependency does not satisfy the requirement.
		if result.Error != nil {
			return false
		}
	}
	return true
}

// promotePendingTasks scans all pending tasks and moves those whose
// dependencies are now met into the ready queue. Must be called with s.mu
// held.
func (s *Scheduler) promotePendingTasks() {
	for _, t := range s.all {
		if t.state != StatePending {
			continue
		}
		if s.areDependenciesSatisfied(t) {
			t.state = StateReady
			heap.Push(&s.readyQueue, t)
		}
	}
}
