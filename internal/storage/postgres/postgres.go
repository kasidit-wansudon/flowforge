// Package postgres provides PostgreSQL-backed repository implementations for
// workflows, runs, and task runs using pgx/v5 connection pooling.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Domain models
// ---------------------------------------------------------------------------

// WorkflowStatus represents the lifecycle state of a workflow definition.
type WorkflowStatus string

const (
	WorkflowStatusDraft    WorkflowStatus = "draft"
	WorkflowStatusActive   WorkflowStatus = "active"
	WorkflowStatusArchived WorkflowStatus = "archived"
	WorkflowStatusDisabled WorkflowStatus = "disabled"
)

// RunStatus represents the lifecycle state of a workflow run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
	RunStatusTimedOut  RunStatus = "timed_out"
)

// TaskRunStatus represents the lifecycle state of an individual task within a run.
type TaskRunStatus string

const (
	TaskRunStatusPending   TaskRunStatus = "pending"
	TaskRunStatusRunning   TaskRunStatus = "running"
	TaskRunStatusCompleted TaskRunStatus = "completed"
	TaskRunStatusFailed    TaskRunStatus = "failed"
	TaskRunStatusSkipped   TaskRunStatus = "skipped"
	TaskRunStatusCancelled TaskRunStatus = "cancelled"
)

// TriggerType describes what initiated a workflow run.
type TriggerType string

const (
	TriggerManual   TriggerType = "manual"
	TriggerSchedule TriggerType = "schedule"
	TriggerEvent    TriggerType = "event"
	TriggerAPI      TriggerType = "api"
)

// Workflow is a versioned workflow definition stored in the database.
type Workflow struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Definition []byte         `json:"definition"` // JSON or YAML blob
	Version    int            `json:"version"`
	Status     WorkflowStatus `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// Run is a single execution of a workflow.
type Run struct {
	ID          string      `json:"id"`
	WorkflowID  string      `json:"workflow_id"`
	Status      RunStatus   `json:"status"`
	StartedAt   time.Time   `json:"started_at"`
	CompletedAt *time.Time  `json:"completed_at,omitempty"`
	Error       *string     `json:"error,omitempty"`
	TriggerType TriggerType `json:"trigger_type"`
}

// TaskRun tracks execution of a single task within a workflow run.
type TaskRun struct {
	ID          string        `json:"id"`
	RunID       string        `json:"run_id"`
	TaskID      string        `json:"task_id"`
	Status      TaskRunStatus `json:"status"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	Attempt     int           `json:"attempt"`
	Output      []byte        `json:"output,omitempty"`
	Error       *string       `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// DB — connection wrapper
// ---------------------------------------------------------------------------

// DB wraps a pgxpool.Pool and exposes repository accessors.
type DB struct {
	pool *pgxpool.Pool
}

// NewDB establishes a connection pool using the provided DSN and returns a
// ready-to-use DB. Example DSN:
//
//	"postgres://user:pass@localhost:5432/flowforge?sslmode=disable"
func NewDB(ctx context.Context, connString string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}

	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Close releases all connections.
func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Pool returns the underlying connection pool for advanced use cases.
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Workflows returns a WorkflowRepository backed by this database.
func (db *DB) Workflows() *WorkflowRepository {
	return &WorkflowRepository{pool: db.pool}
}

// Runs returns a RunRepository backed by this database.
func (db *DB) Runs() *RunRepository {
	return &RunRepository{pool: db.pool}
}

// TaskRuns returns a TaskRunRepository backed by this database.
func (db *DB) TaskRuns() *TaskRunRepository {
	return &TaskRunRepository{pool: db.pool}
}

// ---------------------------------------------------------------------------
// WorkflowRepository
// ---------------------------------------------------------------------------

// WorkflowRepository provides CRUD operations for Workflow records.
type WorkflowRepository struct {
	pool *pgxpool.Pool
}

// Create inserts a new workflow and returns it with generated ID and timestamps.
func (r *WorkflowRepository) Create(ctx context.Context, w *Workflow) (*Workflow, error) {
	if w.ID == "" {
		w.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	w.CreatedAt = now
	w.UpdatedAt = now
	if w.Version == 0 {
		w.Version = 1
	}
	if w.Status == "" {
		w.Status = WorkflowStatusDraft
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO workflows (id, name, definition, version, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		w.ID, w.Name, w.Definition, w.Version, w.Status, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: create workflow: %w", err)
	}
	return w, nil
}

// Get retrieves a workflow by name.
func (r *WorkflowRepository) Get(ctx context.Context, name string) (*Workflow, error) {
	w := &Workflow{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, definition, version, status, created_at, updated_at
		 FROM workflows WHERE name = $1`, name,
	).Scan(&w.ID, &w.Name, &w.Definition, &w.Version, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: workflow %q not found", name)
		}
		return nil, fmt.Errorf("postgres: get workflow: %w", err)
	}
	return w, nil
}

// GetByID retrieves a workflow by its unique identifier.
func (r *WorkflowRepository) GetByID(ctx context.Context, id string) (*Workflow, error) {
	w := &Workflow{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, definition, version, status, created_at, updated_at
		 FROM workflows WHERE id = $1`, id,
	).Scan(&w.ID, &w.Name, &w.Definition, &w.Version, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: workflow id=%s not found", id)
		}
		return nil, fmt.Errorf("postgres: get workflow by id: %w", err)
	}
	return w, nil
}

// List returns workflows with optional pagination.
func (r *WorkflowRepository) List(ctx context.Context, limit, offset int) ([]*Workflow, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, definition, version, status, created_at, updated_at
		 FROM workflows ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list workflows: %w", err)
	}
	defer rows.Close()

	var workflows []*Workflow
	for rows.Next() {
		w := &Workflow{}
		if err := rows.Scan(&w.ID, &w.Name, &w.Definition, &w.Version, &w.Status, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan workflow: %w", err)
		}
		workflows = append(workflows, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list workflows rows: %w", err)
	}
	return workflows, nil
}

// Update modifies an existing workflow. The version is auto-incremented and
// UpdatedAt is refreshed.
func (r *WorkflowRepository) Update(ctx context.Context, w *Workflow) (*Workflow, error) {
	w.UpdatedAt = time.Now().UTC()
	w.Version++

	tag, err := r.pool.Exec(ctx,
		`UPDATE workflows
		 SET name = $1, definition = $2, version = $3, status = $4, updated_at = $5
		 WHERE id = $6`,
		w.Name, w.Definition, w.Version, w.Status, w.UpdatedAt, w.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: update workflow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("postgres: workflow id=%s not found", w.ID)
	}
	return w, nil
}

// Delete removes a workflow by ID.
func (r *WorkflowRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM workflows WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("postgres: delete workflow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: workflow id=%s not found", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// RunRepository
// ---------------------------------------------------------------------------

// RunRepository provides CRUD operations for Run records.
type RunRepository struct {
	pool *pgxpool.Pool
}

// Create inserts a new run record.
func (r *RunRepository) Create(ctx context.Context, run *Run) (*Run, error) {
	if run.ID == "" {
		run.ID = uuid.New().String()
	}
	if run.Status == "" {
		run.Status = RunStatusPending
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO runs (id, workflow_id, status, started_at, completed_at, error, trigger_type)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		run.ID, run.WorkflowID, run.Status, run.StartedAt, run.CompletedAt, run.Error, run.TriggerType,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: create run: %w", err)
	}
	return run, nil
}

// Get retrieves a run by its ID.
func (r *RunRepository) Get(ctx context.Context, id string) (*Run, error) {
	run := &Run{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, workflow_id, status, started_at, completed_at, error, trigger_type
		 FROM runs WHERE id = $1`, id,
	).Scan(&run.ID, &run.WorkflowID, &run.Status, &run.StartedAt, &run.CompletedAt, &run.Error, &run.TriggerType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: run id=%s not found", id)
		}
		return nil, fmt.Errorf("postgres: get run: %w", err)
	}
	return run, nil
}

// List returns runs with optional pagination.
func (r *RunRepository) List(ctx context.Context, limit, offset int) ([]*Run, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, workflow_id, status, started_at, completed_at, error, trigger_type
		 FROM runs ORDER BY started_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list runs: %w", err)
	}
	defer rows.Close()

	var runs []*Run
	for rows.Next() {
		run := &Run{}
		if err := rows.Scan(&run.ID, &run.WorkflowID, &run.Status, &run.StartedAt, &run.CompletedAt, &run.Error, &run.TriggerType); err != nil {
			return nil, fmt.Errorf("postgres: scan run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list runs rows: %w", err)
	}
	return runs, nil
}

// UpdateStatus transitions a run to a new status. When the new status is
// terminal (completed, failed, cancelled, timed_out) the CompletedAt
// timestamp is set automatically.
func (r *RunRepository) UpdateStatus(ctx context.Context, id string, status RunStatus, runErr *string) error {
	var completedAt *time.Time
	switch status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCancelled, RunStatusTimedOut:
		now := time.Now().UTC()
		completedAt = &now
	}

	tag, err := r.pool.Exec(ctx,
		`UPDATE runs SET status = $1, completed_at = $2, error = $3 WHERE id = $4`,
		status, completedAt, runErr, id,
	)
	if err != nil {
		return fmt.Errorf("postgres: update run status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: run id=%s not found", id)
	}
	return nil
}

// GetByWorkflow returns runs belonging to a specific workflow.
func (r *RunRepository) GetByWorkflow(ctx context.Context, workflowID string, limit, offset int) ([]*Run, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, workflow_id, status, started_at, completed_at, error, trigger_type
		 FROM runs WHERE workflow_id = $1 ORDER BY started_at DESC LIMIT $2 OFFSET $3`,
		workflowID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get runs by workflow: %w", err)
	}
	defer rows.Close()

	var runs []*Run
	for rows.Next() {
		run := &Run{}
		if err := rows.Scan(&run.ID, &run.WorkflowID, &run.Status, &run.StartedAt, &run.CompletedAt, &run.Error, &run.TriggerType); err != nil {
			return nil, fmt.Errorf("postgres: scan run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: get runs by workflow rows: %w", err)
	}
	return runs, nil
}

// ---------------------------------------------------------------------------
// TaskRunRepository
// ---------------------------------------------------------------------------

// TaskRunRepository provides CRUD operations for TaskRun records.
type TaskRunRepository struct {
	pool *pgxpool.Pool
}

// Create inserts a new task run record.
func (r *TaskRunRepository) Create(ctx context.Context, tr *TaskRun) (*TaskRun, error) {
	if tr.ID == "" {
		tr.ID = uuid.New().String()
	}
	if tr.Status == "" {
		tr.Status = TaskRunStatusPending
	}
	if tr.StartedAt.IsZero() {
		tr.StartedAt = time.Now().UTC()
	}
	if tr.Attempt == 0 {
		tr.Attempt = 1
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO task_runs (id, run_id, task_id, status, started_at, completed_at, attempt, output, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tr.ID, tr.RunID, tr.TaskID, tr.Status, tr.StartedAt, tr.CompletedAt, tr.Attempt, tr.Output, tr.Error,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: create task run: %w", err)
	}
	return tr, nil
}

// Get retrieves a task run by its ID.
func (r *TaskRunRepository) Get(ctx context.Context, id string) (*TaskRun, error) {
	tr := &TaskRun{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, run_id, task_id, status, started_at, completed_at, attempt, output, error
		 FROM task_runs WHERE id = $1`, id,
	).Scan(&tr.ID, &tr.RunID, &tr.TaskID, &tr.Status, &tr.StartedAt, &tr.CompletedAt, &tr.Attempt, &tr.Output, &tr.Error)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: task run id=%s not found", id)
		}
		return nil, fmt.Errorf("postgres: get task run: %w", err)
	}
	return tr, nil
}

// UpdateStatus transitions a task run to a new status, optionally recording
// output and an error message.
func (r *TaskRunRepository) UpdateStatus(ctx context.Context, id string, status TaskRunStatus, output []byte, taskErr *string) error {
	var completedAt *time.Time
	switch status {
	case TaskRunStatusCompleted, TaskRunStatusFailed, TaskRunStatusSkipped, TaskRunStatusCancelled:
		now := time.Now().UTC()
		completedAt = &now
	}

	tag, err := r.pool.Exec(ctx,
		`UPDATE task_runs SET status = $1, completed_at = $2, output = $3, error = $4 WHERE id = $5`,
		status, completedAt, output, taskErr, id,
	)
	if err != nil {
		return fmt.Errorf("postgres: update task run status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: task run id=%s not found", id)
	}
	return nil
}

// GetByRun returns all task runs for a given workflow run.
func (r *TaskRunRepository) GetByRun(ctx context.Context, runID string) ([]*TaskRun, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, run_id, task_id, status, started_at, completed_at, attempt, output, error
		 FROM task_runs WHERE run_id = $1 ORDER BY started_at ASC`, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get task runs by run: %w", err)
	}
	defer rows.Close()

	var taskRuns []*TaskRun
	for rows.Next() {
		tr := &TaskRun{}
		if err := rows.Scan(&tr.ID, &tr.RunID, &tr.TaskID, &tr.Status, &tr.StartedAt, &tr.CompletedAt, &tr.Attempt, &tr.Output, &tr.Error); err != nil {
			return nil, fmt.Errorf("postgres: scan task run: %w", err)
		}
		taskRuns = append(taskRuns, tr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: get task runs by run rows: %w", err)
	}
	return taskRuns, nil
}
