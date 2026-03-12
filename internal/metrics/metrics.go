// Package metrics provides Prometheus metric collectors for monitoring
// workflow execution, task processing, system health, and HTTP traffic
// within FlowForge.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// Metrics aggregates all Prometheus collectors used by FlowForge.
type Metrics struct {
	// --- Workflow metrics ---

	// WorkflowsTotal counts the total number of workflow runs started,
	// labelled by workflow name.
	WorkflowsTotal *prometheus.CounterVec
	// WorkflowDurationSeconds observes the wall-clock duration of completed
	// workflow runs, labelled by workflow name and final status.
	WorkflowDurationSeconds *prometheus.HistogramVec
	// WorkflowStatus counts completed workflow runs by status.
	WorkflowStatus *prometheus.CounterVec

	// --- Task metrics ---

	// TasksTotal counts the total number of task executions, labelled by task
	// name.
	TasksTotal *prometheus.CounterVec
	// TaskDurationSeconds observes task execution duration, labelled by task
	// name and status.
	TaskDurationSeconds *prometheus.HistogramVec
	// TaskRetriesTotal counts the total number of task retries, labelled by
	// task name.
	TaskRetriesTotal *prometheus.CounterVec
	// TaskStatus counts completed task runs by status.
	TaskStatus *prometheus.CounterVec

	// --- System metrics ---

	// ActiveWorkers tracks the current number of active worker goroutines.
	ActiveWorkers prometheus.Gauge
	// QueueDepth tracks the current depth of the task queue.
	QueueDepth prometheus.Gauge
	// EventThroughput counts the total number of events processed.
	EventThroughput prometheus.Counter

	// --- HTTP metrics ---

	// HTTPRequestsTotal counts HTTP requests, labelled by method, path, and
	// status code.
	HTTPRequestsTotal *prometheus.CounterVec
	// HTTPRequestDurationSeconds observes HTTP request latency, labelled by
	// method and path.
	HTTPRequestDurationSeconds *prometheus.HistogramVec
}

// NewMetrics creates a fully initialised Metrics struct. The namespace
// parameter is prepended to every metric name (e.g. "flowforge").
func NewMetrics(namespace string) *Metrics {
	m := &Metrics{
		// Workflow metrics
		WorkflowsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "workflows_total",
				Help:      "Total number of workflow runs started.",
			},
			[]string{"workflow"},
		),
		WorkflowDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "workflow_duration_seconds",
				Help:      "Duration of completed workflow runs in seconds.",
				Buckets:   prometheus.ExponentialBuckets(0.1, 2, 15), // 0.1s to ~1638s
			},
			[]string{"workflow", "status"},
		),
		WorkflowStatus: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "workflow_status",
				Help:      "Count of completed workflow runs by status.",
			},
			[]string{"workflow", "status"},
		),

		// Task metrics
		TasksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "tasks_total",
				Help:      "Total number of task executions.",
			},
			[]string{"task"},
		),
		TaskDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "task_duration_seconds",
				Help:      "Duration of task executions in seconds.",
				Buckets:   prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms to ~81s
			},
			[]string{"task", "status"},
		),
		TaskRetriesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "task_retries_total",
				Help:      "Total number of task retries.",
			},
			[]string{"task"},
		),
		TaskStatus: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "task_status",
				Help:      "Count of completed task executions by status.",
			},
			[]string{"task", "status"},
		),

		// System metrics
		ActiveWorkers: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "active_workers",
				Help:      "Current number of active worker goroutines.",
			},
		),
		QueueDepth: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "queue_depth",
				Help:      "Current depth of the task queue.",
			},
		),
		EventThroughput: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "event_throughput",
				Help:      "Total number of events processed.",
			},
		),

		// HTTP metrics
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests.",
			},
			[]string{"method", "path", "status_code"},
		),
		HTTPRequestDurationSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request latency in seconds.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
	}
	return m
}

// RegisterMetrics registers all metric collectors with the given
// prometheus.Registerer (typically prometheus.DefaultRegisterer or a custom
// registry).
func (m *Metrics) RegisterMetrics(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.WorkflowsTotal,
		m.WorkflowDurationSeconds,
		m.WorkflowStatus,
		m.TasksTotal,
		m.TaskDurationSeconds,
		m.TaskRetriesTotal,
		m.TaskStatus,
		m.ActiveWorkers,
		m.QueueDepth,
		m.EventThroughput,
		m.HTTPRequestsTotal,
		m.HTTPRequestDurationSeconds,
	}

	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			// If the collector is already registered, skip it.
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				_ = are
				continue
			}
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Convenience recording methods
// ---------------------------------------------------------------------------

// RecordWorkflowStart increments the workflow counter when a new run begins.
func (m *Metrics) RecordWorkflowStart(workflowName string) {
	m.WorkflowsTotal.WithLabelValues(workflowName).Inc()
}

// RecordWorkflowComplete records the final status and duration of a workflow
// run.
func (m *Metrics) RecordWorkflowComplete(workflowName, status string, duration time.Duration) {
	m.WorkflowStatus.WithLabelValues(workflowName, status).Inc()
	m.WorkflowDurationSeconds.WithLabelValues(workflowName, status).Observe(duration.Seconds())
}

// RecordTaskStart increments the task counter when a new task execution begins.
func (m *Metrics) RecordTaskStart(taskName string) {
	m.TasksTotal.WithLabelValues(taskName).Inc()
}

// RecordTaskExecution records the final status and duration of a task run.
func (m *Metrics) RecordTaskExecution(taskName, status string, duration time.Duration) {
	m.TaskStatus.WithLabelValues(taskName, status).Inc()
	m.TaskDurationSeconds.WithLabelValues(taskName, status).Observe(duration.Seconds())
}

// RecordTaskRetry increments the retry counter for the named task.
func (m *Metrics) RecordTaskRetry(taskName string) {
	m.TaskRetriesTotal.WithLabelValues(taskName).Inc()
}

// SetActiveWorkers updates the active worker gauge.
func (m *Metrics) SetActiveWorkers(n float64) {
	m.ActiveWorkers.Set(n)
}

// SetQueueDepth updates the queue depth gauge.
func (m *Metrics) SetQueueDepth(n float64) {
	m.QueueDepth.Set(n)
}

// RecordEvent increments the event throughput counter.
func (m *Metrics) RecordEvent() {
	m.EventThroughput.Inc()
}

// RecordHTTPRequest records an HTTP request's method, path, status, and
// duration.
func (m *Metrics) RecordHTTPRequest(method, path, statusCode string, duration time.Duration) {
	m.HTTPRequestsTotal.WithLabelValues(method, path, statusCode).Inc()
	m.HTTPRequestDurationSeconds.WithLabelValues(method, path).Observe(duration.Seconds())
}

// IncActiveWorkers increments the active worker count by 1.
func (m *Metrics) IncActiveWorkers() {
	m.ActiveWorkers.Inc()
}

// DecActiveWorkers decrements the active worker count by 1.
func (m *Metrics) DecActiveWorkers() {
	m.ActiveWorkers.Dec()
}
