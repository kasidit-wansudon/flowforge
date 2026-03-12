// Package main implements the FlowForge task execution worker.
//
// Workers connect to the orchestration server, claim tasks from a NATS queue,
// execute them with configurable concurrency, and report results back. Each
// worker sends a periodic heartbeat so the server can detect failures.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ---------------------------------------------------------------------------
// Build-time variables
// ---------------------------------------------------------------------------

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds all worker configuration.
type Config struct {
	ServerGRPCAddr    string
	WorkerID          string
	WorkerConcurrency int
	NATSURL           string
	LogLevel          string
	MetricsPort       string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() *Config {
	concurrency, _ := strconv.Atoi(envOrDefault("WORKER_CONCURRENCY", "10"))
	if concurrency <= 0 {
		concurrency = 10
	}

	return &Config{
		ServerGRPCAddr:    envOrDefault("SERVER_GRPC_ADDR", "localhost:9090"),
		WorkerID:          envOrDefault("WORKER_ID", ""),
		WorkerConcurrency: concurrency,
		NATSURL:           envOrDefault("NATS_URL", "nats://localhost:4222"),
		LogLevel:          envOrDefault("LOG_LEVEL", "info"),
		MetricsPort:       envOrDefault("WORKER_METRICS_PORT", "9091"),
	}
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// generateWorkerID produces a worker identifier from the hostname and a
// random suffix, suitable for use when WORKER_ID is not set.
func generateWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "worker"
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()%100000)
	}
	return fmt.Sprintf("%s-%s", hostname, hex.EncodeToString(b))
}

// ---------------------------------------------------------------------------
// Task Types
// ---------------------------------------------------------------------------

// TaskStatus represents the execution state of a task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
	TaskStatusRetrying  TaskStatus = "retrying"
)

// Task represents a unit of work claimed from the queue.
type Task struct {
	ID         string                 `json:"id"`
	RunID      string                 `json:"run_id"`
	WorkflowID string                 `json:"workflow_id"`
	Type       string                 `json:"type"`
	Config     map[string]interface{} `json:"config"`
	Attempt    int                    `json:"attempt"`
	MaxRetries int                    `json:"max_retries"`
	Timeout    time.Duration          `json:"timeout"`
	Priority   int                    `json:"priority"`
}

// TaskResult is the outcome of executing a single task.
type TaskResult struct {
	TaskID      string                 `json:"task_id"`
	RunID       string                 `json:"run_id"`
	WorkerID    string                 `json:"worker_id"`
	Status      TaskStatus             `json:"status"`
	Output      map[string]interface{} `json:"output,omitempty"`
	Error       string                 `json:"error,omitempty"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt time.Time              `json:"completed_at"`
	Attempt     int                    `json:"attempt"`
	DurationMs  int64                  `json:"duration_ms"`
}

// ---------------------------------------------------------------------------
// Task Handlers
// ---------------------------------------------------------------------------

// TaskHandler is a function that executes a specific type of task.
type TaskHandler func(ctx context.Context, task *Task) (map[string]interface{}, error)

// TaskRegistry holds registered task handlers keyed by task type.
type TaskRegistry struct {
	mu       sync.RWMutex
	handlers map[string]TaskHandler
}

// NewTaskRegistry creates a new registry.
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{handlers: make(map[string]TaskHandler)}
}

// Register adds a handler for the given task type.
func (r *TaskRegistry) Register(taskType string, handler TaskHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[taskType] = handler
}

// Get returns the handler for the given task type, or nil.
func (r *TaskRegistry) Get(taskType string) TaskHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[taskType]
}

// Types returns all registered task type names.
func (r *TaskRegistry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}

// registerBuiltinHandlers installs the standard task handlers.
func registerBuiltinHandlers(registry *TaskRegistry, logger *zap.Logger) {
	registry.Register("http", httpTaskHandler(logger))
	registry.Register("script", scriptTaskHandler(logger))
	registry.Register("condition", conditionTaskHandler(logger))
	registry.Register("parallel", parallelTaskHandler(logger))
	registry.Register("delay", delayTaskHandler(logger))
}

func httpTaskHandler(logger *zap.Logger) TaskHandler {
	return func(ctx context.Context, task *Task) (map[string]interface{}, error) {
		method, _ := task.Config["method"].(string)
		url, _ := task.Config["url"].(string)
		if method == "" {
			method = "GET"
		}
		if url == "" {
			return nil, fmt.Errorf("http task: url is required")
		}

		logger.Info("executing HTTP task",
			zap.String("task_id", task.ID),
			zap.String("method", method),
			zap.String("url", url),
		)

		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("http task: failed to create request: %w", err)
		}

		// Apply custom headers.
		if headers, ok := task.Config["headers"].(map[string]interface{}); ok {
			for k, v := range headers {
				if sv, ok := v.(string); ok {
					req.Header.Set(k, sv)
				}
			}
		}

		client := &http.Client{Timeout: 30 * time.Second}
		if task.Timeout > 0 {
			client.Timeout = task.Timeout
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http task: request failed: %w", err)
		}
		defer resp.Body.Close()

		var body interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)

		return map[string]interface{}{
			"status_code": resp.StatusCode,
			"headers":     resp.Header,
			"body":        body,
		}, nil
	}
}

func scriptTaskHandler(logger *zap.Logger) TaskHandler {
	return func(ctx context.Context, task *Task) (map[string]interface{}, error) {
		command, _ := task.Config["command"].(string)
		if command == "" {
			return nil, fmt.Errorf("script task: command is required")
		}

		logger.Info("executing script task",
			zap.String("task_id", task.ID),
			zap.String("command", command),
		)

		// In production this would use os/exec.CommandContext to run the
		// script in a sandboxed environment with resource limits.
		//
		// cmd := exec.CommandContext(ctx, "sh", "-c", command)
		// output, err := cmd.CombinedOutput()
		// ...

		return map[string]interface{}{
			"exit_code": 0,
			"stdout":    "script execution placeholder",
			"stderr":    "",
		}, nil
	}
}

func conditionTaskHandler(logger *zap.Logger) TaskHandler {
	return func(ctx context.Context, task *Task) (map[string]interface{}, error) {
		expression, _ := task.Config["expression"].(string)
		if expression == "" {
			return nil, fmt.Errorf("condition task: expression is required")
		}

		logger.Info("evaluating condition",
			zap.String("task_id", task.ID),
			zap.String("expression", expression),
		)

		// In production this would evaluate the expression against the
		// workflow context using a safe expression evaluator (e.g. expr or cel).
		result := true

		return map[string]interface{}{
			"result":     result,
			"expression": expression,
		}, nil
	}
}

func parallelTaskHandler(logger *zap.Logger) TaskHandler {
	return func(ctx context.Context, task *Task) (map[string]interface{}, error) {
		tasks, _ := task.Config["tasks"].([]interface{})

		logger.Info("executing parallel task group",
			zap.String("task_id", task.ID),
			zap.Int("subtask_count", len(tasks)),
		)

		// In production this would fan-out sub-tasks to the queue and
		// wait for all of them to complete (or fail).
		return map[string]interface{}{
			"completed": len(tasks),
			"failed":    0,
		}, nil
	}
}

func delayTaskHandler(logger *zap.Logger) TaskHandler {
	return func(ctx context.Context, task *Task) (map[string]interface{}, error) {
		durationStr, _ := task.Config["duration"].(string)
		if durationStr == "" {
			return nil, fmt.Errorf("delay task: duration is required")
		}
		d, err := time.ParseDuration(durationStr)
		if err != nil {
			return nil, fmt.Errorf("delay task: invalid duration %q: %w", durationStr, err)
		}

		logger.Info("delay task sleeping",
			zap.String("task_id", task.ID),
			zap.Duration("duration", d),
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
		}

		return map[string]interface{}{
			"delayed_for": d.String(),
		}, nil
	}
}

// ---------------------------------------------------------------------------
// Task Executor
// ---------------------------------------------------------------------------

// TaskExecutor wraps task execution with timeout, retry, and result reporting.
type TaskExecutor struct {
	registry *TaskRegistry
	workerID string
	logger   *zap.Logger
}

// NewTaskExecutor creates a new executor.
func NewTaskExecutor(registry *TaskRegistry, workerID string, logger *zap.Logger) *TaskExecutor {
	return &TaskExecutor{
		registry: registry,
		workerID: workerID,
		logger:   logger,
	}
}

// Execute runs a task through its registered handler.
func (e *TaskExecutor) Execute(ctx context.Context, task *Task) *TaskResult {
	result := &TaskResult{
		TaskID:    task.ID,
		RunID:     task.RunID,
		WorkerID:  e.workerID,
		Attempt:   task.Attempt,
		StartedAt: time.Now().UTC(),
	}

	handler := e.registry.Get(task.Type)
	if handler == nil {
		result.Status = TaskStatusFailed
		result.Error = fmt.Sprintf("no handler registered for task type %q", task.Type)
		result.CompletedAt = time.Now().UTC()
		result.DurationMs = time.Since(result.StartedAt).Milliseconds()
		return result
	}

	// Apply per-task timeout if set.
	execCtx := ctx
	if task.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, task.Timeout)
		defer cancel()
	}

	e.logger.Info("executing task",
		zap.String("task_id", task.ID),
		zap.String("type", task.Type),
		zap.Int("attempt", task.Attempt),
	)

	output, err := handler(execCtx, task)
	result.CompletedAt = time.Now().UTC()
	result.DurationMs = time.Since(result.StartedAt).Milliseconds()

	if err != nil {
		result.Status = TaskStatusFailed
		result.Error = err.Error()

		// Check if a retry is warranted.
		if task.Attempt < task.MaxRetries {
			result.Status = TaskStatusRetrying
			e.logger.Warn("task failed, will retry",
				zap.String("task_id", task.ID),
				zap.Int("attempt", task.Attempt),
				zap.Int("max_retries", task.MaxRetries),
				zap.Error(err),
			)
		} else {
			e.logger.Error("task failed permanently",
				zap.String("task_id", task.ID),
				zap.Int("attempt", task.Attempt),
				zap.Error(err),
			)
		}
	} else {
		result.Status = TaskStatusCompleted
		result.Output = output
		e.logger.Info("task completed",
			zap.String("task_id", task.ID),
			zap.Int64("duration_ms", result.DurationMs),
		)
	}

	return result
}

// ---------------------------------------------------------------------------
// Worker Pool
// ---------------------------------------------------------------------------

// WorkerPool manages a fixed number of concurrent task executors.
type WorkerPool struct {
	executor    *TaskExecutor
	concurrency int
	workerID    string
	logger      *zap.Logger

	taskCh     chan *Task
	resultCh   chan *TaskResult
	inFlight   int64
	totalTasks int64

	wg sync.WaitGroup
}

// NewWorkerPool creates a pool with the given concurrency limit.
func NewWorkerPool(executor *TaskExecutor, concurrency int, logger *zap.Logger) *WorkerPool {
	return &WorkerPool{
		executor:    executor,
		concurrency: concurrency,
		workerID:    executor.workerID,
		logger:      logger,
		taskCh:      make(chan *Task, concurrency*2),
		resultCh:    make(chan *TaskResult, concurrency*2),
	}
}

// Start launches the worker goroutines. They run until ctx is cancelled.
func (p *WorkerPool) Start(ctx context.Context) {
	p.logger.Info("starting worker pool",
		zap.Int("concurrency", p.concurrency),
	)

	for i := 0; i < p.concurrency; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

func (p *WorkerPool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	p.logger.Debug("worker goroutine started", zap.Int("worker_goroutine", id))

	for {
		select {
		case <-ctx.Done():
			p.logger.Debug("worker goroutine stopping", zap.Int("worker_goroutine", id))
			return
		case task, ok := <-p.taskCh:
			if !ok {
				return
			}
			atomic.AddInt64(&p.inFlight, 1)
			atomic.AddInt64(&p.totalTasks, 1)

			result := p.executor.Execute(ctx, task)

			atomic.AddInt64(&p.inFlight, -1)

			// Non-blocking send to result channel.
			select {
			case p.resultCh <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Submit enqueues a task for execution.
func (p *WorkerPool) Submit(task *Task) {
	p.taskCh <- task
}

// Results returns the channel on which task results are published.
func (p *WorkerPool) Results() <-chan *TaskResult {
	return p.resultCh
}

// InFlight returns the number of currently executing tasks.
func (p *WorkerPool) InFlight() int64 {
	return atomic.LoadInt64(&p.inFlight)
}

// TotalProcessed returns the total number of tasks processed.
func (p *WorkerPool) TotalProcessed() int64 {
	return atomic.LoadInt64(&p.totalTasks)
}

// Shutdown closes the task channel and waits for all workers to finish.
func (p *WorkerPool) Shutdown() {
	p.logger.Info("shutting down worker pool, waiting for in-flight tasks",
		zap.Int64("in_flight", p.InFlight()),
	)
	close(p.taskCh)
	p.wg.Wait()
	close(p.resultCh)
	p.logger.Info("worker pool shut down",
		zap.Int64("total_processed", p.TotalProcessed()),
	)
}

// ---------------------------------------------------------------------------
// NATS Consumer (stub)
// ---------------------------------------------------------------------------

// NATSConsumer subscribes to the task distribution subject and feeds tasks
// into the worker pool.
type NATSConsumer struct {
	natsURL  string
	workerID string
	pool     *WorkerPool
	logger   *zap.Logger
}

// NewNATSConsumer creates a new consumer.
func NewNATSConsumer(natsURL, workerID string, pool *WorkerPool, logger *zap.Logger) *NATSConsumer {
	return &NATSConsumer{
		natsURL:  natsURL,
		workerID: workerID,
		pool:     pool,
		logger:   logger,
	}
}

// Start connects to NATS and subscribes to the task subject. Blocks until
// ctx is cancelled.
func (c *NATSConsumer) Start(ctx context.Context) error {
	c.logger.Info("connecting to NATS", zap.String("url", c.natsURL))

	// In production:
	//
	// nc, err := nats.Connect(c.natsURL,
	//     nats.RetryOnFailedConnect(true),
	//     nats.MaxReconnects(-1),
	//     nats.ReconnectWait(2*time.Second),
	//     nats.Name(c.workerID),
	// )
	// if err != nil {
	//     return fmt.Errorf("nats connect: %w", err)
	// }
	// defer nc.Close()
	//
	// js, err := nc.JetStream()
	// if err != nil {
	//     return fmt.Errorf("jetstream init: %w", err)
	// }
	//
	// sub, err := js.QueueSubscribe("flowforge.tasks.>", "workers",
	//     func(msg *nats.Msg) {
	//         var task Task
	//         if err := json.Unmarshal(msg.Data, &task); err != nil {
	//             c.logger.Error("failed to decode task", zap.Error(err))
	//             _ = msg.Nak()
	//             return
	//         }
	//         c.pool.Submit(&task)
	//         _ = msg.Ack()
	//     },
	//     nats.Durable(c.workerID),
	//     nats.ManualAck(),
	//     nats.AckWait(5*time.Minute),
	//     nats.MaxDeliver(3),
	// )
	// if err != nil {
	//     return fmt.Errorf("subscribe: %w", err)
	// }
	// defer func() { _ = sub.Unsubscribe() }()

	c.logger.Info("NATS consumer started (stub mode)")

	<-ctx.Done()
	c.logger.Info("NATS consumer stopping")
	return nil
}

// ---------------------------------------------------------------------------
// Result Reporter (stub)
// ---------------------------------------------------------------------------

// ResultReporter reads task results from the pool and sends them to the
// server via gRPC.
type ResultReporter struct {
	serverAddr string
	workerID   string
	pool       *WorkerPool
	logger     *zap.Logger
}

// NewResultReporter creates a new reporter.
func NewResultReporter(serverAddr, workerID string, pool *WorkerPool, logger *zap.Logger) *ResultReporter {
	return &ResultReporter{
		serverAddr: serverAddr,
		workerID:   workerID,
		pool:       pool,
		logger:     logger,
	}
}

// Start reads results and sends them upstream. Blocks until the result
// channel is closed or ctx is cancelled.
func (r *ResultReporter) Start(ctx context.Context) {
	r.logger.Info("result reporter started")

	// In production, establish a gRPC connection:
	//
	// conn, err := grpc.Dial(r.serverAddr,
	//     grpc.WithTransportCredentials(insecure.NewCredentials()),
	//     grpc.WithKeepaliveParams(keepalive.ClientParameters{
	//         Time:                30 * time.Second,
	//         Timeout:             10 * time.Second,
	//         PermitWithoutStream: true,
	//     }),
	// )
	// if err != nil { ... }
	// defer conn.Close()
	// client := pb.NewWorkerServiceClient(conn)

	for {
		select {
		case <-ctx.Done():
			// Drain remaining results before exiting.
			for result := range r.pool.Results() {
				r.report(result)
			}
			return
		case result, ok := <-r.pool.Results():
			if !ok {
				return
			}
			r.report(result)
		}
	}
}

func (r *ResultReporter) report(result *TaskResult) {
	// In production this calls the gRPC ReportTaskResult endpoint.
	r.logger.Info("reporting task result",
		zap.String("task_id", result.TaskID),
		zap.String("status", string(result.Status)),
		zap.Int64("duration_ms", result.DurationMs),
	)
}

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

// Heartbeat periodically notifies the server that this worker is alive.
type Heartbeat struct {
	serverAddr string
	workerID   string
	interval   time.Duration
	pool       *WorkerPool
	logger     *zap.Logger
}

// NewHeartbeat creates a heartbeat sender.
func NewHeartbeat(serverAddr, workerID string, pool *WorkerPool, logger *zap.Logger) *Heartbeat {
	return &Heartbeat{
		serverAddr: serverAddr,
		workerID:   workerID,
		interval:   15 * time.Second,
		pool:       pool,
		logger:     logger,
	}
}

// Start sends heartbeats at the configured interval until ctx is cancelled.
func (h *Heartbeat) Start(ctx context.Context) {
	h.logger.Info("heartbeat started", zap.Duration("interval", h.interval))
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("heartbeat stopped")
			return
		case <-ticker.C:
			h.send(ctx)
		}
	}
}

func (h *Heartbeat) send(ctx context.Context) {
	payload := map[string]interface{}{
		"worker_id":       h.workerID,
		"in_flight_tasks": h.pool.InFlight(),
		"total_processed": h.pool.TotalProcessed(),
		"timestamp":       time.Now().UTC().Format(time.RFC3339Nano),
	}

	// In production this calls the gRPC WorkerHeartbeat endpoint:
	//
	// _, err := client.Heartbeat(ctx, &pb.HeartbeatRequest{...})
	// if err != nil {
	//     h.logger.Warn("heartbeat failed", zap.Error(err))
	// }

	h.logger.Debug("heartbeat sent", zap.Any("payload", payload))
}

// ---------------------------------------------------------------------------
// Logger
// ---------------------------------------------------------------------------

func newLogger(level string) (*zap.Logger, error) {
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zapcore.InfoLevel
	}

	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(lvl),
		Development: lvl == zapcore.DebugLevel,
		Encoding:    "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			FunctionKey:    zapcore.OmitKey,
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	return cfg.Build()
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	// ---- Configuration --------------------------------------------------
	cfg := LoadConfig()

	if cfg.WorkerID == "" {
		cfg.WorkerID = generateWorkerID()
	}

	// ---- Logger ---------------------------------------------------------
	logger, err := newLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("starting flowforge worker",
		zap.String("version", version),
		zap.String("worker_id", cfg.WorkerID),
		zap.Int("concurrency", cfg.WorkerConcurrency),
		zap.String("server", cfg.ServerGRPCAddr),
	)

	// ---- Task registry --------------------------------------------------
	registry := NewTaskRegistry()
	registerBuiltinHandlers(registry, logger.Named("handler"))
	logger.Info("task handlers registered", zap.Strings("types", registry.Types()))

	// ---- Task executor --------------------------------------------------
	executor := NewTaskExecutor(registry, cfg.WorkerID, logger.Named("executor"))

	// ---- Worker pool ----------------------------------------------------
	pool := NewWorkerPool(executor, cfg.WorkerConcurrency, logger.Named("pool"))

	// ---- Graceful shutdown context --------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// ---- Start pool -----------------------------------------------------
	pool.Start(ctx)

	// ---- NATS consumer --------------------------------------------------
	consumer := NewNATSConsumer(cfg.NATSURL, cfg.WorkerID, pool, logger.Named("nats"))
	go func() {
		if err := consumer.Start(ctx); err != nil {
			logger.Error("NATS consumer error", zap.Error(err))
			cancel()
		}
	}()

	// ---- Result reporter ------------------------------------------------
	reporter := NewResultReporter(cfg.ServerGRPCAddr, cfg.WorkerID, pool, logger.Named("reporter"))
	go reporter.Start(ctx)

	// ---- Heartbeat ------------------------------------------------------
	heartbeat := NewHeartbeat(cfg.ServerGRPCAddr, cfg.WorkerID, pool, logger.Named("heartbeat"))
	go heartbeat.Start(ctx)

	// ---- Metrics (lightweight HTTP for Prometheus scraping) -------------
	metricsMux := http.NewServeMux()
	metricsMux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"worker_id": cfg.WorkerID,
			"in_flight": pool.InFlight(),
			"processed": pool.TotalProcessed(),
		})
	})
	metricsServer := &http.Server{
		Addr:              ":" + cfg.MetricsPort,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("worker metrics server listening", zap.String("addr", metricsServer.Addr))
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", zap.Error(err))
		}
	}()

	logger.Info("worker is ready and waiting for tasks")

	// ---- Wait for shutdown signal ---------------------------------------
	select {
	case sig := <-sigCh:
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	case <-ctx.Done():
		logger.Info("context cancelled, shutting down")
	}

	// ---- Graceful shutdown ----------------------------------------------
	logger.Info("initiating graceful shutdown")

	// 1. Stop accepting new tasks from NATS.
	cancel()

	// 2. Shut down the metrics server.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	_ = metricsServer.Shutdown(shutdownCtx)

	// 3. Wait for in-flight tasks to finish and shut down the pool.
	pool.Shutdown()

	// 4. Deregister worker from server.
	logger.Info("deregistering worker from server")
	// In production:
	// _, err = client.Deregister(shutdownCtx, &pb.DeregisterRequest{WorkerId: cfg.WorkerID})

	logger.Info("flowforge worker shut down cleanly",
		zap.Int64("total_processed", pool.TotalProcessed()),
	)
}
