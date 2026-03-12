// Package rest provides a production-grade REST API for the FlowForge workflow
// orchestration engine. It exposes workflow management, run tracking, and system
// health endpoints behind standard middleware (logging, recovery, CORS, auth,
// request-id, metrics).
package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Domain types (local to the REST package to avoid circular imports)
// ---------------------------------------------------------------------------

// Workflow represents a workflow definition stored in the system.
type Workflow struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Version     int                    `json:"version"`
	Status      string                 `json:"status"`
	Definition  map[string]interface{} `json:"definition,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// TaskState captures the runtime state of a single task inside a run.
type TaskState struct {
	TaskID    string    `json:"task_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// Run represents a single execution instance of a workflow.
type Run struct {
	ID         string      `json:"id"`
	WorkflowID string      `json:"workflow_id"`
	Status     string      `json:"status"`
	TaskStates []TaskState `json:"task_states,omitempty"`
	Input      interface{} `json:"input,omitempty"`
	Output     interface{} `json:"output,omitempty"`
	Error      string      `json:"error,omitempty"`
	StartedAt  time.Time   `json:"started_at"`
	EndedAt    time.Time   `json:"ended_at,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// ListOptions carries pagination and sorting parameters for list queries.
type ListOptions struct {
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	Sort       string `json:"sort"`
	WorkflowID string `json:"workflow_id,omitempty"`
	Status     string `json:"status,omitempty"`
}

// APIResponse is the standard envelope for all successful JSON responses.
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

// Meta carries pagination metadata in list responses.
type Meta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ErrorResponse is the standard envelope for error JSON responses.
type ErrorResponse struct {
	Success   bool   `json:"success"`
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// LogEntry represents a single log line for a workflow run.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	TaskID    string    `json:"task_id,omitempty"`
	Message   string    `json:"message"`
}

// TriggerRequest is the payload for triggering a workflow.
type TriggerRequest struct {
	Input map[string]interface{} `json:"input,omitempty"`
}

// ---------------------------------------------------------------------------
// Repository interfaces
// ---------------------------------------------------------------------------

// WorkflowRepository abstracts persistence for workflow definitions.
type WorkflowRepository interface {
	Create(ctx context.Context, w *Workflow) error
	GetByID(ctx context.Context, id string) (*Workflow, error)
	List(ctx context.Context, opts ListOptions) ([]*Workflow, int, error)
	Update(ctx context.Context, w *Workflow) error
	Delete(ctx context.Context, id string) error
}

// RunRepository abstracts persistence for workflow runs.
type RunRepository interface {
	Create(ctx context.Context, r *Run) error
	GetByID(ctx context.Context, id string) (*Run, error)
	List(ctx context.Context, opts ListOptions) ([]*Run, int, error)
	UpdateStatus(ctx context.Context, id string, status string) error
	GetByWorkflow(ctx context.Context, workflowID string, opts ListOptions) ([]*Run, int, error)
}

// ---------------------------------------------------------------------------
// Auth interface
// ---------------------------------------------------------------------------

// Authenticator validates an incoming HTTP request and returns the subject
// (e.g. user-id) on success, or an error to reject the request.
type Authenticator interface {
	Authenticate(r *http.Request) (subject string, err error)
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// Metrics bundles Prometheus collectors used by the REST server.
type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	ActiveRequests  prometheus.Gauge
}

// NewMetrics creates and registers Prometheus metrics for the REST API.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "flowforge",
			Subsystem: "rest",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests processed.",
		}, []string{"method", "path", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "flowforge",
			Subsystem: "rest",
			Name:      "request_duration_seconds",
			Help:      "Histogram of HTTP request durations in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "path"}),
		ActiveRequests: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "flowforge",
			Subsystem: "rest",
			Name:      "active_requests",
			Help:      "Number of in-flight HTTP requests.",
		}),
	}

	if reg != nil {
		reg.MustRegister(m.RequestsTotal, m.RequestDuration, m.ActiveRequests)
	}
	return m
}

// ---------------------------------------------------------------------------
// Server configuration
// ---------------------------------------------------------------------------

// Config carries parameters needed to construct a Server.
type Config struct {
	Addr              string
	WorkflowRepo      WorkflowRepository
	RunRepo           RunRepository
	Auth              Authenticator
	MetricsRegisterer prometheus.Registerer
	Logger            *zap.Logger
	Version           string
	AllowedOrigins    []string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
}

// ---------------------------------------------------------------------------
// context-key helpers
// ---------------------------------------------------------------------------

type contextKey int

const (
	ctxKeyRequestID contextKey = iota
	ctxKeySubject
)

// RequestIDFromContext extracts the request ID from the context.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// SubjectFromContext extracts the authenticated subject from the context.
func SubjectFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeySubject).(string); ok {
		return v
	}
	return ""
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Server is the top-level REST API server for FlowForge.
type Server struct {
	router       *mux.Router
	httpServer   *http.Server
	workflowRepo WorkflowRepository
	runRepo      RunRepository
	metrics      *Metrics
	auth         Authenticator
	logger       *zap.Logger
	version      string
	origins      []string
	healthy      atomic.Bool
}

// NewServer constructs a fully-wired Server. Call ListenAndServe to start it.
func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	version := cfg.Version
	if version == "" {
		version = "dev"
	}

	origins := cfg.AllowedOrigins
	if len(origins) == 0 {
		origins = []string{"*"}
	}

	readTimeout := cfg.ReadTimeout
	if readTimeout == 0 {
		readTimeout = 15 * time.Second
	}
	writeTimeout := cfg.WriteTimeout
	if writeTimeout == 0 {
		writeTimeout = 30 * time.Second
	}

	s := &Server{
		router:       mux.NewRouter(),
		workflowRepo: cfg.WorkflowRepo,
		runRepo:      cfg.RunRepo,
		auth:         cfg.Auth,
		metrics:      NewMetrics(cfg.MetricsRegisterer),
		logger:       logger,
		version:      version,
		origins:      origins,
	}
	s.healthy.Store(true)

	s.httpServer = &http.Server{
		Addr:         cfg.Addr,
		Handler:      s.router,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	s.SetupRoutes()
	return s
}

// ListenAndServe starts the HTTP server. It blocks until the server shuts down.
func (s *Server) ListenAndServe() error {
	s.logger.Info("starting REST server", zap.String("addr", s.httpServer.Addr))
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.healthy.Store(false)
	return s.httpServer.Shutdown(ctx)
}

// Router exposes the underlying mux.Router for testing purposes.
func (s *Server) Router() *mux.Router {
	return s.router
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// SetupRoutes registers all API routes and middleware on the router.
func (s *Server) SetupRoutes() {
	// Global middleware applied in order: request-ID first, then logging,
	// recovery, CORS, metrics. Auth is applied per-subrouter below.
	s.router.Use(s.requestIDMiddleware)
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.recoveryMiddleware)
	s.router.Use(s.corsMiddleware)
	s.router.Use(s.metricsMiddleware)

	// System routes (no auth required).
	s.router.HandleFunc("/api/v1/health", s.handleHealth).Methods(http.MethodGet, http.MethodOptions)
	s.router.HandleFunc("/api/v1/version", s.handleVersion).Methods(http.MethodGet, http.MethodOptions)
	s.router.Handle("/api/v1/metrics", promhttp.Handler()).Methods(http.MethodGet, http.MethodOptions)

	// Authenticated API sub-router.
	api := s.router.PathPrefix("/api/v1").Subrouter()
	api.Use(s.authMiddleware)

	// Workflow endpoints.
	api.HandleFunc("/workflows", s.handleCreateWorkflow).Methods(http.MethodPost)
	api.HandleFunc("/workflows", s.handleListWorkflows).Methods(http.MethodGet)
	api.HandleFunc("/workflows/{id}", s.handleGetWorkflow).Methods(http.MethodGet)
	api.HandleFunc("/workflows/{id}", s.handleUpdateWorkflow).Methods(http.MethodPut)
	api.HandleFunc("/workflows/{id}", s.handleDeleteWorkflow).Methods(http.MethodDelete)
	api.HandleFunc("/workflows/{id}/trigger", s.handleTriggerWorkflow).Methods(http.MethodPost)

	// Run endpoints.
	api.HandleFunc("/runs", s.handleListRuns).Methods(http.MethodGet)
	api.HandleFunc("/runs/{id}", s.handleGetRun).Methods(http.MethodGet)
	api.HandleFunc("/runs/{id}/cancel", s.handleCancelRun).Methods(http.MethodPost)
	api.HandleFunc("/runs/{id}/retry", s.handleRetryRun).Methods(http.MethodPost)
	api.HandleFunc("/runs/{id}/logs", s.handleGetRunLogs).Methods(http.MethodGet)
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// requestIDMiddleware injects a unique request ID into every request context
// and sets the X-Request-ID response header. If the client supplies
// X-Request-ID it is reused.
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", rid)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.statusCode = code
	sr.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware logs every completed request with method, path, status,
// duration, and request-id.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		s.logger.Info("request completed",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", rec.statusCode),
			zap.Duration("duration", time.Since(start)),
			zap.String("request_id", RequestIDFromContext(r.Context())),
			zap.String("remote_addr", r.RemoteAddr),
		)
	})
}

// recoveryMiddleware catches panics and returns a 500 error instead of
// crashing the server.
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				rid := RequestIDFromContext(r.Context())
				s.logger.Error("panic recovered",
					zap.Any("panic", rec),
					zap.String("request_id", rid),
					zap.String("path", r.URL.Path),
				)
				s.writeError(w, r, http.StatusInternalServerError, "internal server error", "INTERNAL_ERROR")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers and short-circuits OPTIONS pre-flight
// requests.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := false
		for _, o := range s.origins {
			if o == "*" || o == origin {
				allowed = true
				break
			}
		}
		if allowed && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if len(s.origins) > 0 && s.origins[0] == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware calls the configured Authenticator. If no authenticator is
// set, all requests pass through (useful for development).
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			next.ServeHTTP(w, r)
			return
		}
		subject, err := s.auth.Authenticate(r)
		if err != nil {
			s.writeError(w, r, http.StatusUnauthorized, "authentication failed: "+err.Error(), "AUTH_FAILED")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeySubject, subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// metricsMiddleware tracks request counts, durations, and in-flight gauge.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.metrics == nil {
			next.ServeHTTP(w, r)
			return
		}
		s.metrics.ActiveRequests.Inc()
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rec, r)

		duration := time.Since(start).Seconds()
		route := s.matchedRoutePath(r)
		s.metrics.RequestDuration.WithLabelValues(r.Method, route).Observe(duration)
		s.metrics.RequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(rec.statusCode)).Inc()
		s.metrics.ActiveRequests.Dec()
	})
}

// matchedRoutePath returns the route template (e.g. "/api/v1/workflows/{id}")
// so cardinality stays bounded, falling back to the raw path.
func (s *Server) matchedRoutePath(r *http.Request) string {
	route := mux.CurrentRoute(r)
	if route != nil {
		if tmpl, err := route.GetPathTemplate(); err == nil {
			return tmpl
		}
	}
	return r.URL.Path
}

// ---------------------------------------------------------------------------
// System handlers
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	code := http.StatusOK
	if !s.healthy.Load() {
		status = "unhealthy"
		code = http.StatusServiceUnavailable
	}
	s.writeJSON(w, code, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"status":    status,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]string{
			"version":    s.version,
			"go_version": "go1.22",
			"build_time": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// ---------------------------------------------------------------------------
// Workflow handlers
// ---------------------------------------------------------------------------

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var wf Workflow
	if err := s.readJSON(r, &wf); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid request body: "+err.Error(), "INVALID_BODY")
		return
	}
	if wf.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "workflow name is required", "VALIDATION_ERROR")
		return
	}

	now := time.Now().UTC()
	wf.ID = uuid.New().String()
	wf.Version = 1
	if wf.Status == "" {
		wf.Status = "active"
	}
	wf.CreatedAt = now
	wf.UpdatedAt = now

	if err := s.workflowRepo.Create(r.Context(), &wf); err != nil {
		s.logger.Error("failed to create workflow", zap.Error(err), zap.String("request_id", RequestIDFromContext(r.Context())))
		s.writeError(w, r, http.StatusInternalServerError, "failed to create workflow", "CREATE_FAILED")
		return
	}

	s.writeJSON(w, http.StatusCreated, APIResponse{Success: true, Data: wf})
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	opts := s.parseListOptions(r)
	workflows, total, err := s.workflowRepo.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list workflows", zap.Error(err))
		s.writeError(w, r, http.StatusInternalServerError, "failed to list workflows", "LIST_FAILED")
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    workflows,
		Meta:    &Meta{Total: total, Limit: opts.Limit, Offset: opts.Offset},
	})
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := s.workflowRepo.GetByID(r.Context(), id)
	if err != nil {
		s.handleRepoError(w, r, err, "workflow", id)
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: wf})
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	existing, err := s.workflowRepo.GetByID(r.Context(), id)
	if err != nil {
		s.handleRepoError(w, r, err, "workflow", id)
		return
	}

	var update Workflow
	if err := s.readJSON(r, &update); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid request body: "+err.Error(), "INVALID_BODY")
		return
	}

	// Merge fields: only overwrite non-zero values from the update.
	if update.Name != "" {
		existing.Name = update.Name
	}
	if update.Description != "" {
		existing.Description = update.Description
	}
	if update.Status != "" {
		existing.Status = update.Status
	}
	if update.Definition != nil {
		existing.Definition = update.Definition
	}
	if update.Tags != nil {
		existing.Tags = update.Tags
	}
	existing.Version++
	existing.UpdatedAt = time.Now().UTC()

	if err := s.workflowRepo.Update(r.Context(), existing); err != nil {
		s.logger.Error("failed to update workflow", zap.Error(err), zap.String("id", id))
		s.writeError(w, r, http.StatusInternalServerError, "failed to update workflow", "UPDATE_FAILED")
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: existing})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	// Ensure the workflow exists before deleting.
	if _, err := s.workflowRepo.GetByID(r.Context(), id); err != nil {
		s.handleRepoError(w, r, err, "workflow", id)
		return
	}

	if err := s.workflowRepo.Delete(r.Context(), id); err != nil {
		s.logger.Error("failed to delete workflow", zap.Error(err), zap.String("id", id))
		s.writeError(w, r, http.StatusInternalServerError, "failed to delete workflow", "DELETE_FAILED")
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    map[string]string{"id": id, "status": "deleted"},
	})
}

func (s *Server) handleTriggerWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	wf, err := s.workflowRepo.GetByID(r.Context(), id)
	if err != nil {
		s.handleRepoError(w, r, err, "workflow", id)
		return
	}
	if wf.Status != "active" {
		s.writeError(w, r, http.StatusConflict, "workflow is not active", "WORKFLOW_INACTIVE")
		return
	}

	var trigger TriggerRequest
	// Body is optional for trigger; ignore decode errors on empty body.
	_ = s.readJSON(r, &trigger)

	now := time.Now().UTC()
	run := &Run{
		ID:         uuid.New().String(),
		WorkflowID: id,
		Status:     "pending",
		Input:      trigger.Input,
		StartedAt:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.runRepo.Create(r.Context(), run); err != nil {
		s.logger.Error("failed to create run", zap.Error(err), zap.String("workflow_id", id))
		s.writeError(w, r, http.StatusInternalServerError, "failed to trigger workflow", "TRIGGER_FAILED")
		return
	}
	s.writeJSON(w, http.StatusAccepted, APIResponse{Success: true, Data: run})
}

// ---------------------------------------------------------------------------
// Run handlers
// ---------------------------------------------------------------------------

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	opts := s.parseListOptions(r)
	opts.WorkflowID = r.URL.Query().Get("workflow_id")
	opts.Status = r.URL.Query().Get("status")

	var (
		runs  []*Run
		total int
		err   error
	)

	if opts.WorkflowID != "" {
		runs, total, err = s.runRepo.GetByWorkflow(r.Context(), opts.WorkflowID, opts)
	} else {
		runs, total, err = s.runRepo.List(r.Context(), opts)
	}
	if err != nil {
		s.logger.Error("failed to list runs", zap.Error(err))
		s.writeError(w, r, http.StatusInternalServerError, "failed to list runs", "LIST_FAILED")
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    runs,
		Meta:    &Meta{Total: total, Limit: opts.Limit, Offset: opts.Offset},
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	run, err := s.runRepo.GetByID(r.Context(), id)
	if err != nil {
		s.handleRepoError(w, r, err, "run", id)
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: run})
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	run, err := s.runRepo.GetByID(r.Context(), id)
	if err != nil {
		s.handleRepoError(w, r, err, "run", id)
		return
	}

	switch run.Status {
	case "completed", "cancelled", "failed":
		s.writeError(w, r, http.StatusConflict,
			fmt.Sprintf("run is already %s", run.Status), "INVALID_STATE")
		return
	}

	if err := s.runRepo.UpdateStatus(r.Context(), id, "cancelled"); err != nil {
		s.logger.Error("failed to cancel run", zap.Error(err), zap.String("id", id))
		s.writeError(w, r, http.StatusInternalServerError, "failed to cancel run", "CANCEL_FAILED")
		return
	}
	s.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    map[string]string{"id": id, "status": "cancelled"},
	})
}

func (s *Server) handleRetryRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	run, err := s.runRepo.GetByID(r.Context(), id)
	if err != nil {
		s.handleRepoError(w, r, err, "run", id)
		return
	}

	if run.Status != "failed" {
		s.writeError(w, r, http.StatusConflict, "only failed runs can be retried", "INVALID_STATE")
		return
	}

	now := time.Now().UTC()
	newRun := &Run{
		ID:         uuid.New().String(),
		WorkflowID: run.WorkflowID,
		Status:     "pending",
		Input:      run.Input,
		StartedAt:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.runRepo.Create(r.Context(), newRun); err != nil {
		s.logger.Error("failed to retry run", zap.Error(err), zap.String("original_id", id))
		s.writeError(w, r, http.StatusInternalServerError, "failed to retry run", "RETRY_FAILED")
		return
	}
	s.writeJSON(w, http.StatusAccepted, APIResponse{Success: true, Data: newRun})
}

func (s *Server) handleGetRunLogs(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	run, err := s.runRepo.GetByID(r.Context(), id)
	if err != nil {
		s.handleRepoError(w, r, err, "run", id)
		return
	}

	// Build synthetic log entries from task states. In a full implementation
	// these would come from a dedicated log store (e.g. Loki, Elasticsearch).
	logs := make([]LogEntry, 0, len(run.TaskStates)*2)
	for _, ts := range run.TaskStates {
		if !ts.StartedAt.IsZero() {
			logs = append(logs, LogEntry{
				Timestamp: ts.StartedAt,
				Level:     "info",
				TaskID:    ts.TaskID,
				Message:   fmt.Sprintf("task %s started", ts.Name),
			})
		}
		if !ts.EndedAt.IsZero() {
			level := "info"
			msg := fmt.Sprintf("task %s completed with status %s", ts.Name, ts.Status)
			if ts.Error != "" {
				level = "error"
				msg = fmt.Sprintf("task %s failed: %s", ts.Name, ts.Error)
			}
			logs = append(logs, LogEntry{
				Timestamp: ts.EndedAt,
				Level:     level,
				TaskID:    ts.TaskID,
				Message:   msg,
			})
		}
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: logs})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	defaultLimit = 20
	maxLimit     = 100
	maxBodySize  = 1 << 20 // 1 MiB
)

// readJSON decodes a JSON request body into dst with a size limit.
func (s *Server) readJSON(r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodySize)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// Ensure there is no trailing content.
	if dec.More() {
		return fmt.Errorf("body must contain a single JSON object")
	}
	return nil
}

// writeJSON serialises data as JSON and writes it with the given status code.
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode JSON response", zap.Error(err))
	}
}

// writeError sends a structured JSON error response.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, msg, code string) {
	rid := ""
	if r != nil {
		rid = RequestIDFromContext(r.Context())
	}
	s.writeJSON(w, status, ErrorResponse{
		Success:   false,
		Error:     msg,
		Code:      code,
		RequestID: rid,
	})
}

// parseListOptions extracts pagination and sorting from query parameters.
func (s *Server) parseListOptions(r *http.Request) ListOptions {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	sort := q.Get("sort")
	if sort == "" {
		sort = "created_at"
	}
	return ListOptions{
		Limit:  limit,
		Offset: offset,
		Sort:   sort,
	}
}

// handleRepoError inspects the error returned from a repository call and
// writes an appropriate HTTP response. Errors whose message contains
// "not found" are mapped to 404; everything else is 500.
func (s *Server) handleRepoError(w http.ResponseWriter, r *http.Request, err error, entity, id string) {
	if isNotFound(err) {
		s.writeError(w, r, http.StatusNotFound,
			fmt.Sprintf("%s %q not found", entity, id), "NOT_FOUND")
		return
	}
	s.logger.Error(fmt.Sprintf("failed to get %s", entity), zap.Error(err), zap.String("id", id))
	s.writeError(w, r, http.StatusInternalServerError,
		fmt.Sprintf("failed to get %s", entity), "INTERNAL_ERROR")
}

// isNotFound performs a best-effort check for "not found" semantics. In a real
// application the repository layer would return a typed sentinel error.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no rows")
}

// Ensure io is used (for http.MaxBytesReader signature compatibility).
var _ = io.EOF
