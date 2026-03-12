// Package grpc implements the gRPC service layer for FlowForge worker
// communication. It provides task lifecycle management (submit, claim,
// complete, fail) and worker registration with heartbeat-based liveness
// tracking.
package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// Proto-equivalent request / response types
// ---------------------------------------------------------------------------

// SubmitTaskRequest is the payload for submitting a new task to the queue.
type SubmitTaskRequest struct {
	WorkflowID string            `json:"workflow_id"`
	RunID      string            `json:"run_id"`
	TaskID     string            `json:"task_id"`
	TaskName   string            `json:"task_name"`
	TaskType   string            `json:"task_type"`
	Payload    []byte            `json:"payload"`
	Priority   int32             `json:"priority"`
	Timeout    int64             `json:"timeout"` // seconds
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// SubmitTaskResponse is returned after a task is successfully submitted.
type SubmitTaskResponse struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	QueuedAt  int64  `json:"queued_at"`
	QueueSize int32  `json:"queue_size"`
}

// ClaimTaskRequest is sent by a worker to claim the next available task.
type ClaimTaskRequest struct {
	WorkerID   string   `json:"worker_id"`
	TaskTypes  []string `json:"task_types"`
	MaxTasks   int32    `json:"max_tasks"`
	LeaseTTL   int64    `json:"lease_ttl"` // seconds
}

// ClaimTaskResponse contains the tasks claimed by the worker.
type ClaimTaskResponse struct {
	Tasks []*TaskAssignment `json:"tasks"`
}

// TaskAssignment represents a task assigned to a worker.
type TaskAssignment struct {
	TaskID     string            `json:"task_id"`
	WorkflowID string           `json:"workflow_id"`
	RunID      string            `json:"run_id"`
	TaskName   string            `json:"task_name"`
	TaskType   string            `json:"task_type"`
	Payload    []byte            `json:"payload"`
	Priority   int32             `json:"priority"`
	Timeout    int64             `json:"timeout"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	LeaseExpiry int64            `json:"lease_expiry"`
}

// CompleteTaskRequest reports that a task finished successfully.
type CompleteTaskRequest struct {
	TaskID   string `json:"task_id"`
	WorkerID string `json:"worker_id"`
	Output   []byte `json:"output,omitempty"`
}

// CompleteTaskResponse is the acknowledgement of a task completion.
type CompleteTaskResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// FailTaskRequest reports that a task execution failed.
type FailTaskRequest struct {
	TaskID     string `json:"task_id"`
	WorkerID   string `json:"worker_id"`
	Error      string `json:"error"`
	Retryable  bool   `json:"retryable"`
	StackTrace string `json:"stack_trace,omitempty"`
}

// FailTaskResponse is the acknowledgement of a task failure.
type FailTaskResponse struct {
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	WillRetry    bool   `json:"will_retry"`
	RetryCount   int32  `json:"retry_count"`
	MaxRetries   int32  `json:"max_retries"`
	NextRetryAt  int64  `json:"next_retry_at,omitempty"`
}

// HeartbeatRequest is a periodic liveness signal from a worker.
type HeartbeatRequest struct {
	WorkerID    string   `json:"worker_id"`
	ActiveTasks []string `json:"active_tasks,omitempty"`
	CPUUsage    float64  `json:"cpu_usage"`
	MemUsage    float64  `json:"mem_usage"`
	TaskTypes   []string `json:"task_types,omitempty"`
}

// HeartbeatResponse acknowledges the heartbeat and may carry directives.
type HeartbeatResponse struct {
	Acknowledged bool     `json:"acknowledged"`
	ServerTime   int64    `json:"server_time"`
	CancelTasks  []string `json:"cancel_tasks,omitempty"`
}

// RegisterWorkerRequest is sent when a worker comes online.
type RegisterWorkerRequest struct {
	WorkerID   string            `json:"worker_id"`
	Hostname   string            `json:"hostname"`
	TaskTypes  []string          `json:"task_types"`
	MaxTasks   int32             `json:"max_tasks"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// RegisterWorkerResponse is returned after successful registration.
type RegisterWorkerResponse struct {
	WorkerID        string `json:"worker_id"`
	Registered      bool   `json:"registered"`
	HeartbeatInterval int64 `json:"heartbeat_interval"` // seconds
	LeaseInterval   int64  `json:"lease_interval"`      // seconds
}

// DeregisterWorkerRequest is sent when a worker shuts down gracefully.
type DeregisterWorkerRequest struct {
	WorkerID string `json:"worker_id"`
	Reason   string `json:"reason,omitempty"`
}

// DeregisterWorkerResponse acknowledges deregistration.
type DeregisterWorkerResponse struct {
	WorkerID     string `json:"worker_id"`
	Deregistered bool   `json:"deregistered"`
	OrphanedTasks int32 `json:"orphaned_tasks"`
}

// ListWorkersRequest asks for the current set of known workers.
type ListWorkersRequest struct {
	StatusFilter string `json:"status_filter,omitempty"`
}

// ListWorkersResponse contains the list of registered workers.
type ListWorkersResponse struct {
	Workers []*WorkerInfo `json:"workers"`
}

// WorkerInfo describes a registered worker.
type WorkerInfo struct {
	WorkerID      string            `json:"worker_id"`
	Hostname      string            `json:"hostname"`
	Status        string            `json:"status"`
	TaskTypes     []string          `json:"task_types"`
	MaxTasks      int32             `json:"max_tasks"`
	ActiveTasks   int32             `json:"active_tasks"`
	Labels        map[string]string `json:"labels,omitempty"`
	RegisteredAt  int64             `json:"registered_at"`
	LastHeartbeat int64             `json:"last_heartbeat"`
}

// ---------------------------------------------------------------------------
// Proto-equivalent service interfaces
// ---------------------------------------------------------------------------

// TaskServiceServer defines the gRPC task-lifecycle service.
type TaskServiceServer interface {
	SubmitTask(context.Context, *SubmitTaskRequest) (*SubmitTaskResponse, error)
	ClaimTask(context.Context, *ClaimTaskRequest) (*ClaimTaskResponse, error)
	CompleteTask(context.Context, *CompleteTaskRequest) (*CompleteTaskResponse, error)
	FailTask(context.Context, *FailTaskRequest) (*FailTaskResponse, error)
	Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error)
}

// WorkerServiceServer defines the gRPC worker-management service.
type WorkerServiceServer interface {
	Register(context.Context, *RegisterWorkerRequest) (*RegisterWorkerResponse, error)
	Deregister(context.Context, *DeregisterWorkerRequest) (*DeregisterWorkerResponse, error)
	ListWorkers(context.Context, *ListWorkersRequest) (*ListWorkersResponse, error)
}

// ---------------------------------------------------------------------------
// Internal task record
// ---------------------------------------------------------------------------

// taskRecord is the in-memory representation of a queued task.
type taskRecord struct {
	SubmitTaskRequest
	ID          string
	Status      string // queued, claimed, completed, failed
	WorkerID    string
	RetryCount  int32
	MaxRetries  int32
	QueuedAt    time.Time
	ClaimedAt   time.Time
	CompletedAt time.Time
	LeaseExpiry time.Time
	Output      []byte
	Error       string
}

// workerRecord is the in-memory representation of a registered worker.
type workerRecord struct {
	Info          WorkerInfo
	activeTasks   map[string]bool
	cancelPending []string
}

// ---------------------------------------------------------------------------
// TaskServer (implements TaskServiceServer)
// ---------------------------------------------------------------------------

// TaskServer implements TaskServiceServer with in-memory state. In production
// the task queue would be backed by a durable store (e.g. Redis, PostgreSQL,
// NATS JetStream).
type TaskServer struct {
	mu     sync.RWMutex
	tasks  map[string]*taskRecord
	queue  []*taskRecord // simple FIFO; production would use a priority queue
	logger *zap.Logger

	// workerMgr is a reference to the worker server so heartbeats can
	// update worker liveness.
	workerMgr *WorkerServer

	defaultMaxRetries int32
	defaultLeaseSeconds int64
}

// TaskServerConfig carries parameters for constructing a TaskServer.
type TaskServerConfig struct {
	Logger            *zap.Logger
	WorkerServer      *WorkerServer
	DefaultMaxRetries int32
	DefaultLeaseTTL   int64 // seconds
}

// NewTaskServer creates a TaskServer ready to accept RPCs.
func NewTaskServer(cfg TaskServerConfig) *TaskServer {
	logger := cfg.Logger
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	maxRetries := cfg.DefaultMaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	leaseTTL := cfg.DefaultLeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = 300 // 5 minutes
	}
	return &TaskServer{
		tasks:               make(map[string]*taskRecord),
		queue:               make([]*taskRecord, 0),
		logger:              logger,
		workerMgr:           cfg.WorkerServer,
		defaultMaxRetries:   maxRetries,
		defaultLeaseSeconds: leaseTTL,
	}
}

// SubmitTask places a new task on the queue.
func (s *TaskServer) SubmitTask(_ context.Context, req *SubmitTaskRequest) (*SubmitTaskResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if req.TaskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.TaskType == "" {
		return nil, status.Error(codes.InvalidArgument, "task_type is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[req.TaskID]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "task %s already exists", req.TaskID)
	}

	now := time.Now().UTC()
	rec := &taskRecord{
		SubmitTaskRequest: *req,
		ID:                req.TaskID,
		Status:            "queued",
		MaxRetries:        s.defaultMaxRetries,
		QueuedAt:          now,
	}
	s.tasks[rec.ID] = rec
	s.queue = append(s.queue, rec)

	s.logger.Info("task submitted",
		zap.String("task_id", rec.ID),
		zap.String("task_type", rec.TaskType),
		zap.String("workflow_id", rec.WorkflowID),
	)

	return &SubmitTaskResponse{
		TaskID:    rec.ID,
		Status:    rec.Status,
		QueuedAt:  now.Unix(),
		QueueSize: int32(len(s.queue)),
	}, nil
}

// ClaimTask lets a worker claim one or more pending tasks that match the
// worker's declared task types.
func (s *TaskServer) ClaimTask(_ context.Context, req *ClaimTaskRequest) (*ClaimTaskResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if req.WorkerID == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}

	maxClaim := int(req.MaxTasks)
	if maxClaim <= 0 {
		maxClaim = 1
	}

	leaseTTL := req.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = s.defaultLeaseSeconds
	}

	typesSet := make(map[string]bool, len(req.TaskTypes))
	for _, t := range req.TaskTypes {
		typesSet[t] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	var claimed []*TaskAssignment
	remaining := make([]*taskRecord, 0, len(s.queue))

	for _, rec := range s.queue {
		if len(claimed) >= maxClaim {
			remaining = append(remaining, rec)
			continue
		}
		// Match on task type (empty types = accept anything).
		if len(typesSet) > 0 && !typesSet[rec.TaskType] {
			remaining = append(remaining, rec)
			continue
		}
		if rec.Status != "queued" {
			remaining = append(remaining, rec)
			continue
		}

		rec.Status = "claimed"
		rec.WorkerID = req.WorkerID
		rec.ClaimedAt = now
		rec.LeaseExpiry = now.Add(time.Duration(leaseTTL) * time.Second)

		claimed = append(claimed, &TaskAssignment{
			TaskID:      rec.ID,
			WorkflowID:  rec.WorkflowID,
			RunID:       rec.RunID,
			TaskName:    rec.TaskName,
			TaskType:    rec.TaskType,
			Payload:     rec.Payload,
			Priority:    rec.Priority,
			Timeout:     rec.Timeout,
			Metadata:    rec.Metadata,
			LeaseExpiry: rec.LeaseExpiry.Unix(),
		})

		// Register active task with worker manager.
		if s.workerMgr != nil {
			s.workerMgr.addActiveTask(req.WorkerID, rec.ID)
		}
	}
	s.queue = remaining

	s.logger.Info("tasks claimed",
		zap.String("worker_id", req.WorkerID),
		zap.Int("count", len(claimed)),
	)

	return &ClaimTaskResponse{Tasks: claimed}, nil
}

// CompleteTask marks a task as successfully completed.
func (s *TaskServer) CompleteTask(_ context.Context, req *CompleteTaskRequest) (*CompleteTaskResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if req.TaskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.WorkerID == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, exists := s.tasks[req.TaskID]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "task %s not found", req.TaskID)
	}
	if rec.Status != "claimed" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"task %s is in state %s, expected claimed", req.TaskID, rec.Status)
	}
	if rec.WorkerID != req.WorkerID {
		return nil, status.Errorf(codes.PermissionDenied,
			"task %s is assigned to worker %s, not %s", req.TaskID, rec.WorkerID, req.WorkerID)
	}

	rec.Status = "completed"
	rec.Output = req.Output
	rec.CompletedAt = time.Now().UTC()

	if s.workerMgr != nil {
		s.workerMgr.removeActiveTask(req.WorkerID, req.TaskID)
	}

	s.logger.Info("task completed",
		zap.String("task_id", req.TaskID),
		zap.String("worker_id", req.WorkerID),
	)

	return &CompleteTaskResponse{
		TaskID: req.TaskID,
		Status: "completed",
	}, nil
}

// FailTask marks a task as failed and optionally re-queues it for retry.
func (s *TaskServer) FailTask(_ context.Context, req *FailTaskRequest) (*FailTaskResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if req.TaskID == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.WorkerID == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, exists := s.tasks[req.TaskID]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "task %s not found", req.TaskID)
	}
	if rec.Status != "claimed" {
		return nil, status.Errorf(codes.FailedPrecondition,
			"task %s is in state %s, expected claimed", req.TaskID, rec.Status)
	}
	if rec.WorkerID != req.WorkerID {
		return nil, status.Errorf(codes.PermissionDenied,
			"task %s is assigned to worker %s, not %s", req.TaskID, rec.WorkerID, req.WorkerID)
	}

	if s.workerMgr != nil {
		s.workerMgr.removeActiveTask(req.WorkerID, req.TaskID)
	}

	rec.RetryCount++
	rec.Error = req.Error
	willRetry := req.Retryable && rec.RetryCount <= rec.MaxRetries

	resp := &FailTaskResponse{
		TaskID:     req.TaskID,
		RetryCount: rec.RetryCount,
		MaxRetries: rec.MaxRetries,
	}

	if willRetry {
		// Exponential back-off: 2^retryCount seconds, capped at 5 minutes.
		backoff := time.Duration(1<<rec.RetryCount) * time.Second
		const maxBackoff = 5 * time.Minute
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		rec.Status = "queued"
		rec.WorkerID = ""
		rec.ClaimedAt = time.Time{}
		rec.LeaseExpiry = time.Time{}
		s.queue = append(s.queue, rec)

		resp.Status = "queued"
		resp.WillRetry = true
		resp.NextRetryAt = time.Now().UTC().Add(backoff).Unix()

		s.logger.Info("task re-queued for retry",
			zap.String("task_id", req.TaskID),
			zap.Int32("retry", rec.RetryCount),
			zap.Int32("max", rec.MaxRetries),
		)
	} else {
		rec.Status = "failed"
		rec.CompletedAt = time.Now().UTC()
		resp.Status = "failed"
		resp.WillRetry = false

		s.logger.Warn("task permanently failed",
			zap.String("task_id", req.TaskID),
			zap.String("error", req.Error),
		)
	}

	return resp, nil
}

// Heartbeat updates a worker's liveness timestamp and returns any pending
// cancel directives.
func (s *TaskServer) Heartbeat(_ context.Context, req *HeartbeatRequest) (*HeartbeatResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if req.WorkerID == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}

	var cancelTasks []string
	if s.workerMgr != nil {
		cancelTasks = s.workerMgr.processHeartbeat(req)
	}

	return &HeartbeatResponse{
		Acknowledged: true,
		ServerTime:   time.Now().UTC().Unix(),
		CancelTasks:  cancelTasks,
	}, nil
}

// QueueSize returns the current number of queued (unclaimed) tasks.
func (s *TaskServer) QueueSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.queue)
}

// ReapExpiredLeases moves tasks with expired leases back to the queue so
// they can be claimed by another worker. This should be called periodically.
func (s *TaskServer) ReapExpiredLeases() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	reaped := 0

	for _, rec := range s.tasks {
		if rec.Status == "claimed" && !rec.LeaseExpiry.IsZero() && now.After(rec.LeaseExpiry) {
			s.logger.Warn("lease expired, re-queuing task",
				zap.String("task_id", rec.ID),
				zap.String("worker_id", rec.WorkerID),
			)

			if s.workerMgr != nil {
				s.workerMgr.removeActiveTask(rec.WorkerID, rec.ID)
			}

			rec.Status = "queued"
			rec.WorkerID = ""
			rec.ClaimedAt = time.Time{}
			rec.LeaseExpiry = time.Time{}
			s.queue = append(s.queue, rec)
			reaped++
		}
	}
	return reaped
}

// ---------------------------------------------------------------------------
// WorkerServer (implements WorkerServiceServer)
// ---------------------------------------------------------------------------

// WorkerServer manages worker registration and liveness.
type WorkerServer struct {
	mu      sync.RWMutex
	workers map[string]*workerRecord
	logger  *zap.Logger

	heartbeatInterval time.Duration
	leaseInterval     time.Duration
	heartbeatTimeout  time.Duration
}

// WorkerServerConfig carries parameters for constructing a WorkerServer.
type WorkerServerConfig struct {
	Logger            *zap.Logger
	HeartbeatInterval time.Duration
	LeaseInterval     time.Duration
	HeartbeatTimeout  time.Duration
}

// NewWorkerServer creates a WorkerServer.
func NewWorkerServer(cfg WorkerServerConfig) *WorkerServer {
	logger := cfg.Logger
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	hbInterval := cfg.HeartbeatInterval
	if hbInterval == 0 {
		hbInterval = 30 * time.Second
	}
	leaseInterval := cfg.LeaseInterval
	if leaseInterval == 0 {
		leaseInterval = 300 * time.Second
	}
	hbTimeout := cfg.HeartbeatTimeout
	if hbTimeout == 0 {
		hbTimeout = 90 * time.Second
	}
	return &WorkerServer{
		workers:           make(map[string]*workerRecord),
		logger:            logger,
		heartbeatInterval: hbInterval,
		leaseInterval:     leaseInterval,
		heartbeatTimeout:  hbTimeout,
	}
}

// Register adds a worker to the pool. If the worker is already registered,
// its metadata is updated.
func (s *WorkerServer) Register(_ context.Context, req *RegisterWorkerRequest) (*RegisterWorkerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	workerID := req.WorkerID
	if workerID == "" {
		workerID = uuid.New().String()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()

	rec, exists := s.workers[workerID]
	if exists {
		// Re-registration: update metadata, keep active tasks.
		rec.Info.Hostname = req.Hostname
		rec.Info.TaskTypes = req.TaskTypes
		rec.Info.MaxTasks = req.MaxTasks
		rec.Info.Labels = req.Labels
		rec.Info.Status = "active"
		rec.Info.LastHeartbeat = now
	} else {
		rec = &workerRecord{
			Info: WorkerInfo{
				WorkerID:      workerID,
				Hostname:      req.Hostname,
				Status:        "active",
				TaskTypes:     req.TaskTypes,
				MaxTasks:      req.MaxTasks,
				Labels:        req.Labels,
				RegisteredAt:  now,
				LastHeartbeat: now,
			},
			activeTasks: make(map[string]bool),
		}
		s.workers[workerID] = rec
	}

	s.logger.Info("worker registered",
		zap.String("worker_id", workerID),
		zap.String("hostname", req.Hostname),
		zap.Strings("task_types", req.TaskTypes),
	)

	return &RegisterWorkerResponse{
		WorkerID:          workerID,
		Registered:        true,
		HeartbeatInterval: int64(s.heartbeatInterval.Seconds()),
		LeaseInterval:     int64(s.leaseInterval.Seconds()),
	}, nil
}

// Deregister removes a worker from the pool and returns the number of tasks
// it still had active (orphaned).
func (s *WorkerServer) Deregister(_ context.Context, req *DeregisterWorkerRequest) (*DeregisterWorkerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request must not be nil")
	}
	if req.WorkerID == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rec, exists := s.workers[req.WorkerID]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "worker %s not found", req.WorkerID)
	}

	orphaned := int32(len(rec.activeTasks))
	delete(s.workers, req.WorkerID)

	s.logger.Info("worker deregistered",
		zap.String("worker_id", req.WorkerID),
		zap.String("reason", req.Reason),
		zap.Int32("orphaned_tasks", orphaned),
	)

	return &DeregisterWorkerResponse{
		WorkerID:      req.WorkerID,
		Deregistered:  true,
		OrphanedTasks: orphaned,
	}, nil
}

// ListWorkers returns all known workers, optionally filtered by status.
func (s *WorkerServer) ListWorkers(_ context.Context, req *ListWorkersRequest) (*ListWorkersResponse, error) {
	if req == nil {
		req = &ListWorkersRequest{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	workers := make([]*WorkerInfo, 0, len(s.workers))
	for _, rec := range s.workers {
		if req.StatusFilter != "" && rec.Info.Status != req.StatusFilter {
			continue
		}
		info := rec.Info // copy
		info.ActiveTasks = int32(len(rec.activeTasks))
		workers = append(workers, &info)
	}

	return &ListWorkersResponse{Workers: workers}, nil
}

// processHeartbeat updates worker liveness and returns any cancel directives.
func (s *WorkerServer) processHeartbeat(req *HeartbeatRequest) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, exists := s.workers[req.WorkerID]
	if !exists {
		s.logger.Warn("heartbeat from unknown worker", zap.String("worker_id", req.WorkerID))
		return nil
	}

	rec.Info.LastHeartbeat = time.Now().UTC().Unix()
	rec.Info.Status = "active"

	if len(req.TaskTypes) > 0 {
		rec.Info.TaskTypes = req.TaskTypes
	}

	// Drain any pending cancel directives.
	var cancels []string
	if len(rec.cancelPending) > 0 {
		cancels = rec.cancelPending
		rec.cancelPending = nil
	}

	return cancels
}

// addActiveTask records that a task has been claimed by a worker.
func (s *WorkerServer) addActiveTask(workerID, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec, ok := s.workers[workerID]; ok {
		rec.activeTasks[taskID] = true
	}
}

// removeActiveTask records that a task is no longer active on a worker.
func (s *WorkerServer) removeActiveTask(workerID, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec, ok := s.workers[workerID]; ok {
		delete(rec.activeTasks, taskID)
	}
}

// CancelTaskOnWorker queues a cancel directive that will be delivered to the
// worker on its next heartbeat.
func (s *WorkerServer) CancelTaskOnWorker(workerID, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.workers[workerID]
	if !ok {
		return fmt.Errorf("worker %s not found", workerID)
	}
	rec.cancelPending = append(rec.cancelPending, taskID)
	return nil
}

// ReapDeadWorkers marks workers that have not sent a heartbeat within the
// timeout window as "dead" and returns their IDs.
func (s *WorkerServer) ReapDeadWorkers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().UTC().Add(-s.heartbeatTimeout).Unix()
	var dead []string

	for id, rec := range s.workers {
		if rec.Info.Status == "active" && rec.Info.LastHeartbeat < cutoff {
			rec.Info.Status = "dead"
			dead = append(dead, id)

			s.logger.Warn("worker marked dead",
				zap.String("worker_id", id),
				zap.Int64("last_heartbeat", rec.Info.LastHeartbeat),
			)
		}
	}
	return dead
}

// ActiveWorkerCount returns the number of workers currently in "active" state.
func (s *WorkerServer) ActiveWorkerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, rec := range s.workers {
		if rec.Info.Status == "active" {
			count++
		}
	}
	return count
}
