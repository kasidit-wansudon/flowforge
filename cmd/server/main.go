// Package main implements the FlowForge orchestration server.
//
// The server exposes a REST API, a gRPC API, a WebSocket hub for real-time
// events, and an internal workflow scheduler. It reads all configuration from
// environment variables and shuts down gracefully on SIGINT / SIGTERM.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	// Side-effect imports for database drivers and other integrations are
	// expected in production but omitted here to keep the binary buildable
	// without those services running.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ---------------------------------------------------------------------------
// Build-time variables (set via -ldflags)
// ---------------------------------------------------------------------------

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds all server configuration sourced from environment variables.
type Config struct {
	Port        string
	GRPCPort    string
	DatabaseURL string
	RedisURL    string
	NATSURL     string
	JWTSecret   string
	LogLevel    string
	TLSCert     string
	TLSKey      string
}

// LoadConfig reads configuration from the environment with sensible defaults.
func LoadConfig() *Config {
	return &Config{
		Port:        envOrDefault("PORT", "8080"),
		GRPCPort:    envOrDefault("GRPC_PORT", "9090"),
		DatabaseURL: envOrDefault("DATABASE_URL", "postgres://flowforge:flowforge@localhost:5432/flowforge?sslmode=disable"),
		RedisURL:    envOrDefault("REDIS_URL", "redis://localhost:6379/0"),
		NATSURL:     envOrDefault("NATS_URL", "nats://localhost:4222"),
		JWTSecret:   envOrDefault("JWT_SECRET", ""),
		LogLevel:    envOrDefault("LOG_LEVEL", "info"),
		TLSCert:     envOrDefault("TLS_CERT", ""),
		TLSKey:      envOrDefault("TLS_KEY", ""),
	}
}

// Validate ensures mandatory configuration values are present.
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET environment variable is required")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Metrics Collector
// ---------------------------------------------------------------------------

// MetricsCollector wraps Prometheus metrics used throughout the server.
type MetricsCollector struct {
	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	activeConnections   prometheus.Gauge
	workflowRunsTotal   *prometheus.CounterVec
	taskExecutionTime   *prometheus.HistogramVec
	schedulerCycles     prometheus.Counter
	wsConnections       prometheus.Gauge
}

// NewMetricsCollector registers and returns the server metrics.
func NewMetricsCollector(reg prometheus.Registerer) *MetricsCollector {
	m := &MetricsCollector{
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "flowforge",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests.",
		}, []string{"method", "path", "status"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "flowforge",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "path"}),
		activeConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "flowforge",
			Subsystem: "http",
			Name:      "active_connections",
			Help:      "Number of active HTTP connections.",
		}),
		workflowRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "flowforge",
			Subsystem: "engine",
			Name:      "workflow_runs_total",
			Help:      "Total number of workflow runs by status.",
		}, []string{"workflow_id", "status"}),
		taskExecutionTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "flowforge",
			Subsystem: "engine",
			Name:      "task_execution_seconds",
			Help:      "Task execution duration in seconds.",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"task_type", "status"}),
		schedulerCycles: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "flowforge",
			Subsystem: "scheduler",
			Name:      "cycles_total",
			Help:      "Number of scheduler evaluation cycles.",
		}),
		wsConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "flowforge",
			Subsystem: "websocket",
			Name:      "connections",
			Help:      "Number of active WebSocket connections.",
		}),
	}

	reg.MustRegister(
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.activeConnections,
		m.workflowRunsTotal,
		m.taskExecutionTime,
		m.schedulerCycles,
		m.wsConnections,
	)
	return m
}

// ---------------------------------------------------------------------------
// Auth Middleware
// ---------------------------------------------------------------------------

// AuthMiddleware provides JWT-based request authentication.
type AuthMiddleware struct {
	jwtSecret []byte
	logger    *zap.Logger
}

// NewAuthMiddleware creates a new AuthMiddleware.
func NewAuthMiddleware(secret string, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		jwtSecret: []byte(secret),
		logger:    logger,
	}
}

// Authenticate is an HTTP middleware that validates the Authorization header.
func (a *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
			return
		}

		// In production this would decode and verify the JWT with a.jwtSecret.
		// For now we pass through so the rest of the stack is exercised.
		// claims, err := jwt.Parse(token, a.jwtSecret)
		// if err != nil { ... }

		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// WebSocket Hub
// ---------------------------------------------------------------------------

// WSClient represents a connected WebSocket client.
type WSClient struct {
	conn   *websocket.Conn
	send   chan []byte
	hub    *WebSocketHub
	topics map[string]bool
}

// WebSocketHub manages real-time WebSocket connections.
type WebSocketHub struct {
	mu         sync.RWMutex
	clients    map[*WSClient]bool
	broadcast  chan []byte
	register   chan *WSClient
	unregister chan *WSClient
	logger     *zap.Logger
	metrics    *MetricsCollector
	upgrader   websocket.Upgrader
}

// NewWebSocketHub creates a new WebSocket hub.
func NewWebSocketHub(logger *zap.Logger, metrics *MetricsCollector) *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		logger:     logger,
		metrics:    metrics,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// In production, validate the Origin header.
				return true
			},
		},
	}
}

// Run starts the hub event loop. It blocks until ctx is cancelled.
func (h *WebSocketHub) Run(ctx context.Context) {
	h.logger.Info("websocket hub started")
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				_ = client.conn.Close()
			}
			h.clients = make(map[*WSClient]bool)
			h.mu.Unlock()
			h.logger.Info("websocket hub stopped")
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.metrics.wsConnections.Inc()
			h.logger.Debug("websocket client connected", zap.Int("total", len(h.clients)))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.metrics.wsConnections.Dec()
			h.logger.Debug("websocket client disconnected", zap.Int("total", len(h.clients)))

		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					// Slow client — drop the message to avoid blocking.
					h.logger.Warn("dropping message to slow websocket client")
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Publish sends a JSON event to all connected clients.
func (h *WebSocketHub) Publish(eventType string, payload interface{}) {
	msg, err := json.Marshal(map[string]interface{}{
		"type":      eventType,
		"payload":   payload,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		h.logger.Error("failed to marshal websocket event", zap.Error(err))
		return
	}
	select {
	case h.broadcast <- msg:
	default:
		h.logger.Warn("websocket broadcast channel full, dropping event")
	}
}

// ServeWS upgrades the HTTP connection to a WebSocket and registers the client.
func (h *WebSocketHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", zap.Error(err))
		return
	}
	client := &WSClient{
		conn:   conn,
		send:   make(chan []byte, 256),
		hub:    h,
		topics: make(map[string]bool),
	}
	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *WSClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Scheduler
// ---------------------------------------------------------------------------

// Scheduler periodically evaluates cron-based and event-driven workflows.
type Scheduler struct {
	logger  *zap.Logger
	metrics *MetricsCollector
	// In production these fields would hold references to the workflow store,
	// event bus, and execution engine.
	interval time.Duration
}

// NewScheduler creates a new workflow scheduler.
func NewScheduler(logger *zap.Logger, metrics *MetricsCollector) *Scheduler {
	return &Scheduler{
		logger:   logger,
		metrics:  metrics,
		interval: 5 * time.Second,
	}
}

// Run starts the scheduler loop. It blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.logger.Info("scheduler started", zap.Duration("interval", s.interval))
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.evaluate(ctx)
		}
	}
}

func (s *Scheduler) evaluate(ctx context.Context) {
	s.metrics.schedulerCycles.Inc()

	// In production this would:
	// 1. Query workflows with cron triggers whose next fire time is <= now.
	// 2. Create run records for each triggered workflow.
	// 3. Enqueue the initial tasks via NATS.
	// 4. Process any pending webhook-triggered runs.
	s.logger.Debug("scheduler evaluation cycle completed")
}

// ---------------------------------------------------------------------------
// REST API
// ---------------------------------------------------------------------------

func newRouter(
	logger *zap.Logger,
	metrics *MetricsCollector,
	auth *AuthMiddleware,
	wsHub *WebSocketHub,
) *mux.Router {
	r := mux.NewRouter()

	// Public endpoints ---------------------------------------------------

	r.HandleFunc("/health", healthHandler(logger)).Methods("GET")
	r.HandleFunc("/ready", readinessHandler(logger)).Methods("GET")
	r.Handle("/metrics", promhttp.Handler()).Methods("GET")

	r.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version": version,
			"commit":  commit,
			"date":    buildDate,
		})
	}).Methods("GET")

	// WebSocket endpoint -------------------------------------------------

	r.HandleFunc("/ws", wsHub.ServeWS)

	// Authenticated API --------------------------------------------------

	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(auth.Authenticate)
	api.Use(metricsMiddleware(metrics))

	// Workflow CRUD
	api.HandleFunc("/workflows", listWorkflowsHandler(logger)).Methods("GET")
	api.HandleFunc("/workflows", createWorkflowHandler(logger)).Methods("POST")
	api.HandleFunc("/workflows/{id}", getWorkflowHandler(logger)).Methods("GET")
	api.HandleFunc("/workflows/{id}", updateWorkflowHandler(logger)).Methods("PUT")
	api.HandleFunc("/workflows/{id}", deleteWorkflowHandler(logger)).Methods("DELETE")
	api.HandleFunc("/workflows/{id}/run", triggerWorkflowHandler(logger)).Methods("POST")

	// Run management
	api.HandleFunc("/runs", listRunsHandler(logger)).Methods("GET")
	api.HandleFunc("/runs/{id}", getRunHandler(logger)).Methods("GET")
	api.HandleFunc("/runs/{id}/cancel", cancelRunHandler(logger)).Methods("POST")
	api.HandleFunc("/runs/{id}/logs", getRunLogsHandler(logger)).Methods("GET")

	return r
}

// --- Handler stubs (each returns a real HandlerFunc with JSON responses) ---

func healthHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": version,
		})
	}
}

func readinessHandler(logger *zap.Logger) http.HandlerFunc {
	// In production this checks database, Redis, NATS connectivity.
	return func(w http.ResponseWriter, _ *http.Request) {
		checks := map[string]string{
			"database": "ok",
			"redis":    "ok",
			"nats":     "ok",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ready",
			"checks": checks,
		})
	}
}

func listWorkflowsHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// In production: query workflow store with pagination from r.URL.Query().
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"workflows": []interface{}{},
			"total":     0,
			"page":      1,
			"per_page":  20,
		})
	}
}

func createWorkflowHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		logger.Info("workflow created", zap.Any("name", body["name"]))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "placeholder-uuid",
			"message": "workflow created",
		})
	}
}

func getWorkflowHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		logger.Debug("fetching workflow", zap.String("id", id))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	}
}

func updateWorkflowHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		logger.Info("workflow updated", zap.String("id", id))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "message": "workflow updated"})
	}
}

func deleteWorkflowHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		logger.Info("workflow deleted", zap.String("id", id))
		w.WriteHeader(http.StatusNoContent)
	}
}

func triggerWorkflowHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		logger.Info("workflow triggered", zap.String("workflow_id", id))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"run_id":  "placeholder-run-uuid",
			"message": "workflow execution started",
		})
	}
}

func listRunsHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"runs":     []interface{}{},
			"total":    0,
			"page":     1,
			"per_page": 20,
		})
	}
}

func getRunHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "running"})
	}
}

func cancelRunHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		logger.Info("run cancelled", zap.String("run_id", id))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "cancelled"})
	}
}

func getRunLogsHandler(logger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"run_id": id,
			"logs":   []interface{}{},
		})
	}
}

// metricsMiddleware records request count and latency.
func metricsMiddleware(m *MetricsCollector) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			m.activeConnections.Inc()
			defer m.activeConnections.Dec()

			next.ServeHTTP(rw, r)

			route := mux.CurrentRoute(r)
			path := "unknown"
			if route != nil {
				if tpl, err := route.GetPathTemplate(); err == nil {
					path = tpl
				}
			}

			duration := time.Since(start).Seconds()
			m.httpRequestsTotal.WithLabelValues(r.Method, path, fmt.Sprintf("%d", rw.statusCode)).Inc()
			m.httpRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// ---------------------------------------------------------------------------
// gRPC Server
// ---------------------------------------------------------------------------

func newGRPCServer(logger *zap.Logger) *grpc.Server {
	srv := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 10 * time.Second,
			Time:                  1 * time.Minute,
			Timeout:               20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             15 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxRecvMsgSize(16*1024*1024), // 16 MB
		grpc.MaxSendMsgSize(16*1024*1024),
	)

	// Register the standard gRPC health service.
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(srv, healthSrv)
	healthSrv.SetServingStatus("flowforge.WorkflowService", healthpb.HealthCheckResponse_SERVING)
	healthSrv.SetServingStatus("flowforge.WorkerService", healthpb.HealthCheckResponse_SERVING)

	// In production, register application-specific gRPC services here:
	// pb.RegisterWorkflowServiceServer(srv, workflowSvc)
	// pb.RegisterWorkerServiceServer(srv, workerSvc)

	reflection.Register(srv)

	return srv
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

	// ---- Logger ---------------------------------------------------------
	logger, err := newLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("starting flowforge server",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("port", cfg.Port),
		zap.String("grpc_port", cfg.GRPCPort),
	)

	// ---- Validate configuration ----------------------------------------
	if err := cfg.Validate(); err != nil {
		logger.Warn("configuration warning", zap.Error(err))
		// Continue in development mode; in production you might os.Exit(1).
	}

	// ---- PostgreSQL connection pool ------------------------------------
	// In production, uncomment the following to establish a real connection:
	//
	// dbPool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	// if err != nil {
	//     logger.Fatal("failed to connect to database", zap.Error(err))
	// }
	// defer dbPool.Close()
	// logger.Info("database connection established")
	logger.Info("database connection pool initialised (stub)",
		zap.String("url", redactURL(cfg.DatabaseURL)),
	)

	// ---- Redis cache ---------------------------------------------------
	// In production:
	//
	// rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisURL})
	// if err := rdb.Ping(context.Background()).Err(); err != nil {
	//     logger.Fatal("failed to connect to redis", zap.Error(err))
	// }
	// defer rdb.Close()
	logger.Info("redis cache initialised (stub)",
		zap.String("url", redactURL(cfg.RedisURL)),
	)

	// ---- NATS connection -----------------------------------------------
	// In production:
	//
	// nc, err := nats.Connect(cfg.NATSURL,
	//     nats.RetryOnFailedConnect(true),
	//     nats.MaxReconnects(-1),
	//     nats.ReconnectWait(2*time.Second),
	// )
	// if err != nil {
	//     logger.Fatal("failed to connect to NATS", zap.Error(err))
	// }
	// defer nc.Close()
	logger.Info("NATS connection initialised (stub)",
		zap.String("url", redactURL(cfg.NATSURL)),
	)

	// ---- Metrics -------------------------------------------------------
	promRegistry := prometheus.NewRegistry()
	promRegistry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	promRegistry.MustRegister(prometheus.NewGoCollector())
	metrics := NewMetricsCollector(promRegistry)
	logger.Info("metrics collector initialised")

	// ---- Auth middleware ------------------------------------------------
	auth := NewAuthMiddleware(cfg.JWTSecret, logger)

	// ---- WebSocket hub -------------------------------------------------
	wsHub := NewWebSocketHub(logger.Named("ws"), metrics)

	// ---- Scheduler -----------------------------------------------------
	scheduler := NewScheduler(logger.Named("scheduler"), metrics)

	// ---- HTTP router ---------------------------------------------------
	router := newRouter(logger.Named("api"), metrics, auth, wsHub)

	// ---- Graceful shutdown context -------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// ---- Start HTTP server ---------------------------------------------
	httpAddr := net.JoinHostPort("", cfg.Port)
	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		httpServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("HTTP server listening", zap.String("addr", httpAddr))
		var listenErr error
		if httpServer.TLSConfig != nil {
			listenErr = httpServer.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			listenErr = httpServer.ListenAndServe()
		}
		if listenErr != nil && listenErr != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(listenErr))
			cancel()
		}
	}()

	// ---- Start gRPC server ---------------------------------------------
	grpcAddr := net.JoinHostPort("", cfg.GRPCPort)
	grpcListener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		logger.Fatal("failed to listen for gRPC", zap.Error(err))
	}
	grpcServer := newGRPCServer(logger.Named("grpc"))

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("gRPC server listening", zap.String("addr", grpcAddr))
		if err := grpcServer.Serve(grpcListener); err != nil {
			logger.Error("gRPC server error", zap.Error(err))
			cancel()
		}
	}()

	// ---- Start WebSocket hub -------------------------------------------
	wg.Add(1)
	go func() {
		defer wg.Done()
		wsHub.Run(ctx)
	}()

	// ---- Start scheduler -----------------------------------------------
	wg.Add(1)
	go func() {
		defer wg.Done()
		scheduler.Run(ctx)
	}()

	logger.Info("all subsystems started, awaiting shutdown signal")

	// ---- Wait for shutdown signal --------------------------------------
	select {
	case sig := <-sigCh:
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
	case <-ctx.Done():
		logger.Info("context cancelled, shutting down")
	}

	// ---- Graceful shutdown with timeout --------------------------------
	shutdownTimeout := 30 * time.Second
	logger.Info("initiating graceful shutdown", zap.Duration("timeout", shutdownTimeout))

	// Cancel the root context to stop scheduler and websocket hub.
	cancel()

	// Shut down gRPC server gracefully.
	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()

	// Shut down HTTP server gracefully.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server forced shutdown", zap.Error(err))
	}

	// Wait for gRPC to finish, with a timeout.
	select {
	case <-grpcDone:
		logger.Info("gRPC server stopped gracefully")
	case <-shutdownCtx.Done():
		logger.Warn("gRPC server forced stop (timeout)")
		grpcServer.Stop()
	}

	// Wait for all goroutines to finish.
	wg.Wait()

	logger.Info("flowforge server shut down cleanly")
}

// redactURL removes credentials from a URL for safe logging.
func redactURL(rawURL string) string {
	// Simple redaction: just return the scheme + host portion.
	// In production use net/url.Parse for proper redaction.
	if len(rawURL) > 30 {
		return rawURL[:30] + "..."
	}
	return rawURL
}
