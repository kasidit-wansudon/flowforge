package metrics_test

import (
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// --- helpers ---

func newMetrics(t *testing.T) *metrics.Metrics {
	t.Helper()
	return metrics.NewMetrics("test")
}

// counterValue extracts the current float64 value from a CounterVec using the
// provided label values.
func counterValue(t *testing.T, cv *prometheus.CounterVec, labelValues ...string) float64 {
	t.Helper()
	c, err := cv.GetMetricWithLabelValues(labelValues...)
	if err != nil {
		t.Fatalf("GetMetricWithLabelValues: %v", err)
	}
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("Write metric: %v", err)
	}
	return m.Counter.GetValue()
}

func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("Write gauge: %v", err)
	}
	return m.Gauge.GetValue()
}

func histogramCount(t *testing.T, hv *prometheus.HistogramVec, labelValues ...string) uint64 {
	t.Helper()
	h, err := hv.GetMetricWithLabelValues(labelValues...)
	if err != nil {
		t.Fatalf("GetMetricWithLabelValues: %v", err)
	}
	var m dto.Metric
	if err := h.(prometheus.Metric).Write(&m); err != nil {
		t.Fatalf("Write histogram: %v", err)
	}
	return m.Histogram.GetSampleCount()
}

// --- NewMetrics ---

func TestNewMetrics_ReturnsNonNil(t *testing.T) {
	m := metrics.NewMetrics("flowforge")
	if m == nil {
		t.Fatal("NewMetrics returned nil")
	}
}

func TestNewMetrics_AllFieldsInitialized(t *testing.T) {
	m := metrics.NewMetrics("flowforge")
	if m.WorkflowsTotal == nil {
		t.Error("WorkflowsTotal is nil")
	}
	if m.WorkflowDurationSeconds == nil {
		t.Error("WorkflowDurationSeconds is nil")
	}
	if m.TasksTotal == nil {
		t.Error("TasksTotal is nil")
	}
	if m.TaskDurationSeconds == nil {
		t.Error("TaskDurationSeconds is nil")
	}
	if m.ActiveWorkers == nil {
		t.Error("ActiveWorkers is nil")
	}
	if m.QueueDepth == nil {
		t.Error("QueueDepth is nil")
	}
	if m.EventThroughput == nil {
		t.Error("EventThroughput is nil")
	}
	if m.HTTPRequestsTotal == nil {
		t.Error("HTTPRequestsTotal is nil")
	}
}

// --- RegisterMetrics ---

func TestRegisterMetrics_SucceedsWithFreshRegistry(t *testing.T) {
	m := metrics.NewMetrics("reg_test")
	reg := prometheus.NewRegistry()
	if err := m.RegisterMetrics(reg); err != nil {
		t.Fatalf("RegisterMetrics failed: %v", err)
	}
}

func TestRegisterMetrics_IdempotentOnAlreadyRegistered(t *testing.T) {
	m := metrics.NewMetrics("idempotent_test")
	reg := prometheus.NewRegistry()

	if err := m.RegisterMetrics(reg); err != nil {
		t.Fatalf("first RegisterMetrics failed: %v", err)
	}
	// Second call should not fail because AlreadyRegisteredError is handled gracefully.
	if err := m.RegisterMetrics(reg); err != nil {
		t.Fatalf("second RegisterMetrics should not fail: %v", err)
	}
}

// --- Counter incrementing ---

func TestRecordWorkflowStart_IncrementsCounter(t *testing.T) {
	m := newMetrics(t)

	m.RecordWorkflowStart("my-workflow")
	m.RecordWorkflowStart("my-workflow")

	v := counterValue(t, m.WorkflowsTotal, "my-workflow")
	if v != 2 {
		t.Errorf("expected counter=2, got %v", v)
	}
}

func TestRecordWorkflowComplete_IncrementsStatusCounter(t *testing.T) {
	m := newMetrics(t)

	m.RecordWorkflowComplete("wf-a", "success", 500*time.Millisecond)
	m.RecordWorkflowComplete("wf-a", "success", 200*time.Millisecond)
	m.RecordWorkflowComplete("wf-a", "failed", 100*time.Millisecond)

	successVal := counterValue(t, m.WorkflowStatus, "wf-a", "success")
	if successVal != 2 {
		t.Errorf("expected success count=2, got %v", successVal)
	}
	failedVal := counterValue(t, m.WorkflowStatus, "wf-a", "failed")
	if failedVal != 1 {
		t.Errorf("expected failed count=1, got %v", failedVal)
	}
}

func TestRecordTaskStart_IncrementsCounter(t *testing.T) {
	m := newMetrics(t)

	m.RecordTaskStart("task-build")
	m.RecordTaskStart("task-build")
	m.RecordTaskStart("task-test")

	buildVal := counterValue(t, m.TasksTotal, "task-build")
	if buildVal != 2 {
		t.Errorf("expected task-build count=2, got %v", buildVal)
	}
	testVal := counterValue(t, m.TasksTotal, "task-test")
	if testVal != 1 {
		t.Errorf("expected task-test count=1, got %v", testVal)
	}
}

func TestRecordTaskRetry_IncrementsCounter(t *testing.T) {
	m := newMetrics(t)

	m.RecordTaskRetry("flaky-task")
	m.RecordTaskRetry("flaky-task")
	m.RecordTaskRetry("flaky-task")

	v := counterValue(t, m.TaskRetriesTotal, "flaky-task")
	if v != 3 {
		t.Errorf("expected retry count=3, got %v", v)
	}
}

// --- Histogram observations ---

func TestRecordWorkflowComplete_ObservesHistogram(t *testing.T) {
	m := newMetrics(t)

	m.RecordWorkflowComplete("wf-hist", "success", 1*time.Second)
	m.RecordWorkflowComplete("wf-hist", "success", 2*time.Second)

	count := histogramCount(t, m.WorkflowDurationSeconds, "wf-hist", "success")
	if count != 2 {
		t.Errorf("expected histogram sample count=2, got %d", count)
	}
}

func TestRecordTaskExecution_ObservesHistogram(t *testing.T) {
	m := newMetrics(t)

	m.RecordTaskExecution("task-x", "success", 100*time.Millisecond)
	m.RecordTaskExecution("task-x", "failed", 50*time.Millisecond)

	successCount := histogramCount(t, m.TaskDurationSeconds, "task-x", "success")
	if successCount != 1 {
		t.Errorf("expected success histogram count=1, got %d", successCount)
	}
}

func TestRecordHTTPRequest_ObservesHistogram(t *testing.T) {
	m := newMetrics(t)

	m.RecordHTTPRequest("GET", "/api/workflows", "200", 10*time.Millisecond)
	m.RecordHTTPRequest("GET", "/api/workflows", "200", 20*time.Millisecond)
	m.RecordHTTPRequest("POST", "/api/workflows", "201", 30*time.Millisecond)

	count := histogramCount(t, m.HTTPRequestDurationSeconds, "GET", "/api/workflows")
	if count != 2 {
		t.Errorf("expected HTTP histogram count=2 for GET /api/workflows, got %d", count)
	}
}

// --- Gauge operations ---

func TestSetActiveWorkers_UpdatesGauge(t *testing.T) {
	m := newMetrics(t)

	m.SetActiveWorkers(5)
	if v := gaugeValue(t, m.ActiveWorkers); v != 5 {
		t.Errorf("expected 5, got %v", v)
	}

	m.SetActiveWorkers(0)
	if v := gaugeValue(t, m.ActiveWorkers); v != 0 {
		t.Errorf("expected 0, got %v", v)
	}
}

func TestIncDecActiveWorkers_UpdatesGauge(t *testing.T) {
	m := newMetrics(t)

	m.IncActiveWorkers()
	m.IncActiveWorkers()
	m.IncActiveWorkers()
	m.DecActiveWorkers()

	if v := gaugeValue(t, m.ActiveWorkers); v != 2 {
		t.Errorf("expected 2, got %v", v)
	}
}

func TestSetQueueDepth_UpdatesGauge(t *testing.T) {
	m := newMetrics(t)

	m.SetQueueDepth(10)
	if v := gaugeValue(t, m.QueueDepth); v != 10 {
		t.Errorf("expected 10, got %v", v)
	}
}

// --- Event throughput ---

func TestRecordEvent_IncrementsCounter(t *testing.T) {
	m := newMetrics(t)

	m.RecordEvent()
	m.RecordEvent()
	m.RecordEvent()

	var metric dto.Metric
	if err := m.EventThroughput.Write(&metric); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if metric.Counter.GetValue() != 3 {
		t.Errorf("expected event throughput=3, got %v", metric.Counter.GetValue())
	}
}
