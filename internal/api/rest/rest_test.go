package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Mock repositories
// ---------------------------------------------------------------------------

type mockWorkflowRepo struct {
	workflows map[string]*Workflow
}

func newMockWorkflowRepo() *mockWorkflowRepo {
	return &mockWorkflowRepo{workflows: make(map[string]*Workflow)}
}

func (m *mockWorkflowRepo) Create(_ context.Context, w *Workflow) error {
	m.workflows[w.ID] = w
	return nil
}

func (m *mockWorkflowRepo) GetByID(_ context.Context, id string) (*Workflow, error) {
	w, ok := m.workflows[id]
	if !ok {
		return nil, fmt.Errorf("workflow %q not found", id)
	}
	return w, nil
}

func (m *mockWorkflowRepo) List(_ context.Context, opts ListOptions) ([]*Workflow, int, error) {
	result := make([]*Workflow, 0, len(m.workflows))
	for _, w := range m.workflows {
		result = append(result, w)
	}
	total := len(result)
	// Apply basic pagination.
	if opts.Offset >= len(result) {
		return nil, total, nil
	}
	end := opts.Offset + opts.Limit
	if end > len(result) {
		end = len(result)
	}
	return result[opts.Offset:end], total, nil
}

func (m *mockWorkflowRepo) Update(_ context.Context, w *Workflow) error {
	if _, ok := m.workflows[w.ID]; !ok {
		return fmt.Errorf("workflow %q not found", w.ID)
	}
	m.workflows[w.ID] = w
	return nil
}

func (m *mockWorkflowRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.workflows[id]; !ok {
		return fmt.Errorf("workflow %q not found", id)
	}
	delete(m.workflows, id)
	return nil
}

type mockRunRepo struct {
	runs map[string]*Run
}

func newMockRunRepo() *mockRunRepo {
	return &mockRunRepo{runs: make(map[string]*Run)}
}

func (m *mockRunRepo) Create(_ context.Context, r *Run) error {
	m.runs[r.ID] = r
	return nil
}

func (m *mockRunRepo) GetByID(_ context.Context, id string) (*Run, error) {
	r, ok := m.runs[id]
	if !ok {
		return nil, fmt.Errorf("run %q not found", id)
	}
	return r, nil
}

func (m *mockRunRepo) List(_ context.Context, opts ListOptions) ([]*Run, int, error) {
	result := make([]*Run, 0, len(m.runs))
	for _, r := range m.runs {
		result = append(result, r)
	}
	total := len(result)
	if opts.Offset >= len(result) {
		return nil, total, nil
	}
	end := opts.Offset + opts.Limit
	if end > len(result) {
		end = len(result)
	}
	return result[opts.Offset:end], total, nil
}

func (m *mockRunRepo) UpdateStatus(_ context.Context, id string, status string) error {
	r, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("run %q not found", id)
	}
	r.Status = status
	return nil
}

func (m *mockRunRepo) GetByWorkflow(_ context.Context, workflowID string, opts ListOptions) ([]*Run, int, error) {
	var result []*Run
	for _, r := range m.runs {
		if r.WorkflowID == workflowID {
			result = append(result, r)
		}
	}
	total := len(result)
	if opts.Offset >= len(result) {
		return nil, total, nil
	}
	end := opts.Offset + opts.Limit
	if end > len(result) {
		end = len(result)
	}
	return result[opts.Offset:end], total, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServer() (*Server, *mockWorkflowRepo, *mockRunRepo) {
	logger, _ := zap.NewDevelopment()
	wfRepo := newMockWorkflowRepo()
	runRepo := newMockRunRepo()

	srv := NewServer(Config{
		Addr:         ":0",
		WorkflowRepo: wfRepo,
		RunRepo:      runRepo,
		Logger:       logger,
		Version:      "test-1.0",
	})
	return srv, wfRepo, runRepo
}

func doRequest(srv *Server, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(data)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func parseAPIResponse(t *testing.T, rec *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response body: %v\nbody: %s", err, rec.Body.String())
	}
	return resp
}

func parseErrorResponse(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var resp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response body: %v\nbody: %s", err, rec.Body.String())
	}
	return resp
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHealthEndpoint(t *testing.T) {
	srv, _, _ := newTestServer()
	rec := doRequest(srv, http.MethodGet, "/api/v1/health", nil)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	resp := parseAPIResponse(t, rec)
	if !resp.Success {
		t.Error("expected success = true")
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("data is not a map")
	}
	if data["status"] != "healthy" {
		t.Errorf("status = %q, want %q", data["status"], "healthy")
	}
}

func TestVersionEndpoint(t *testing.T) {
	srv, _, _ := newTestServer()
	rec := doRequest(srv, http.MethodGet, "/api/v1/version", nil)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	resp := parseAPIResponse(t, rec)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("data is not a map")
	}
	if data["version"] != "test-1.0" {
		t.Errorf("version = %q, want %q", data["version"], "test-1.0")
	}
}

func TestCreateWorkflow(t *testing.T) {
	srv, wfRepo, _ := newTestServer()
	body := map[string]interface{}{
		"name":        "My Workflow",
		"description": "A test workflow",
	}
	rec := doRequest(srv, http.MethodPost, "/api/v1/workflows", body)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	resp := parseAPIResponse(t, rec)
	if !resp.Success {
		t.Error("expected success = true")
	}

	// Verify it was stored.
	if len(wfRepo.workflows) != 1 {
		t.Errorf("workflows count = %d, want 1", len(wfRepo.workflows))
	}
}

func TestCreateWorkflowMissingName(t *testing.T) {
	srv, _, _ := newTestServer()
	body := map[string]interface{}{
		"description": "No name",
	}
	rec := doRequest(srv, http.MethodPost, "/api/v1/workflows", body)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	errResp := parseErrorResponse(t, rec)
	if errResp.Success {
		t.Error("expected success = false")
	}
	if errResp.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want %q", errResp.Code, "VALIDATION_ERROR")
	}
}

func TestGetWorkflow(t *testing.T) {
	srv, wfRepo, _ := newTestServer()

	// Seed a workflow.
	wf := &Workflow{
		ID:        "wf-123",
		Name:      "Test",
		Version:   1,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	wfRepo.workflows[wf.ID] = wf

	rec := doRequest(srv, http.MethodGet, "/api/v1/workflows/wf-123", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseAPIResponse(t, rec)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("data is not a map")
	}
	if data["id"] != "wf-123" {
		t.Errorf("id = %q, want %q", data["id"], "wf-123")
	}
}

func TestGetWorkflowNotFound(t *testing.T) {
	srv, _, _ := newTestServer()
	rec := doRequest(srv, http.MethodGet, "/api/v1/workflows/non-existent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	errResp := parseErrorResponse(t, rec)
	if errResp.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want %q", errResp.Code, "NOT_FOUND")
	}
}

func TestListWorkflows(t *testing.T) {
	srv, wfRepo, _ := newTestServer()

	for i := 0; i < 3; i++ {
		wf := &Workflow{
			ID:        fmt.Sprintf("wf-%d", i),
			Name:      fmt.Sprintf("Workflow %d", i),
			Version:   1,
			Status:    "active",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		wfRepo.workflows[wf.ID] = wf
	}

	rec := doRequest(srv, http.MethodGet, "/api/v1/workflows", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	resp := parseAPIResponse(t, rec)
	if resp.Meta == nil {
		t.Fatal("meta is nil")
	}
	if resp.Meta.Total != 3 {
		t.Errorf("meta.total = %d, want 3", resp.Meta.Total)
	}
}

func TestUpdateWorkflow(t *testing.T) {
	srv, wfRepo, _ := newTestServer()

	wf := &Workflow{
		ID:        "wf-upd",
		Name:      "Original",
		Version:   1,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	wfRepo.workflows[wf.ID] = wf

	update := map[string]interface{}{
		"name":        "Updated Name",
		"description": "New description",
	}
	rec := doRequest(srv, http.MethodPut, "/api/v1/workflows/wf-upd", update)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify the update was applied.
	updated := wfRepo.workflows["wf-upd"]
	if updated.Name != "Updated Name" {
		t.Errorf("name = %q, want %q", updated.Name, "Updated Name")
	}
	if updated.Version != 2 {
		t.Errorf("version = %d, want 2", updated.Version)
	}
}

func TestUpdateWorkflowNotFound(t *testing.T) {
	srv, _, _ := newTestServer()
	update := map[string]interface{}{"name": "New Name"}
	rec := doRequest(srv, http.MethodPut, "/api/v1/workflows/non-existent", update)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	srv, wfRepo, _ := newTestServer()

	wf := &Workflow{
		ID:      "wf-del",
		Name:    "To Delete",
		Version: 1,
		Status:  "active",
	}
	wfRepo.workflows[wf.ID] = wf

	rec := doRequest(srv, http.MethodDelete, "/api/v1/workflows/wf-del", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if _, exists := wfRepo.workflows["wf-del"]; exists {
		t.Error("workflow should have been deleted")
	}
}

func TestDeleteWorkflowNotFound(t *testing.T) {
	srv, _, _ := newTestServer()
	rec := doRequest(srv, http.MethodDelete, "/api/v1/workflows/non-existent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestTriggerWorkflow(t *testing.T) {
	srv, wfRepo, runRepo := newTestServer()

	wf := &Workflow{
		ID:      "wf-trig",
		Name:    "Triggerable",
		Version: 1,
		Status:  "active",
	}
	wfRepo.workflows[wf.ID] = wf

	body := map[string]interface{}{
		"input": map[string]interface{}{"key": "value"},
	}
	rec := doRequest(srv, http.MethodPost, "/api/v1/workflows/wf-trig/trigger", body)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	if len(runRepo.runs) != 1 {
		t.Errorf("runs count = %d, want 1", len(runRepo.runs))
	}

	// Check the created run.
	for _, r := range runRepo.runs {
		if r.WorkflowID != "wf-trig" {
			t.Errorf("run.WorkflowID = %q, want %q", r.WorkflowID, "wf-trig")
		}
		if r.Status != "pending" {
			t.Errorf("run.Status = %q, want %q", r.Status, "pending")
		}
	}
}

func TestTriggerInactiveWorkflow(t *testing.T) {
	srv, wfRepo, _ := newTestServer()

	wf := &Workflow{
		ID:     "wf-inactive",
		Name:   "Inactive",
		Status: "paused",
	}
	wfRepo.workflows[wf.ID] = wf

	rec := doRequest(srv, http.MethodPost, "/api/v1/workflows/wf-inactive/trigger", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestTriggerWorkflowNotFound(t *testing.T) {
	srv, _, _ := newTestServer()
	rec := doRequest(srv, http.MethodPost, "/api/v1/workflows/non-existent/trigger", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListRuns(t *testing.T) {
	srv, _, runRepo := newTestServer()

	for i := 0; i < 3; i++ {
		r := &Run{
			ID:         fmt.Sprintf("run-%d", i),
			WorkflowID: "wf-1",
			Status:     "completed",
			CreatedAt:  time.Now().UTC(),
		}
		runRepo.runs[r.ID] = r
	}

	rec := doRequest(srv, http.MethodGet, "/api/v1/runs", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	resp := parseAPIResponse(t, rec)
	if resp.Meta == nil {
		t.Fatal("meta is nil")
	}
	if resp.Meta.Total != 3 {
		t.Errorf("meta.total = %d, want 3", resp.Meta.Total)
	}
}

func TestGetRun(t *testing.T) {
	srv, _, runRepo := newTestServer()

	r := &Run{
		ID:         "run-get",
		WorkflowID: "wf-1",
		Status:     "running",
		CreatedAt:  time.Now().UTC(),
	}
	runRepo.runs[r.ID] = r

	rec := doRequest(srv, http.MethodGet, "/api/v1/runs/run-get", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestGetRunNotFound(t *testing.T) {
	srv, _, _ := newTestServer()
	rec := doRequest(srv, http.MethodGet, "/api/v1/runs/non-existent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCancelRun(t *testing.T) {
	srv, _, runRepo := newTestServer()

	r := &Run{
		ID:         "run-cancel",
		WorkflowID: "wf-1",
		Status:     "running",
	}
	runRepo.runs[r.ID] = r

	rec := doRequest(srv, http.MethodPost, "/api/v1/runs/run-cancel/cancel", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if runRepo.runs["run-cancel"].Status != "cancelled" {
		t.Errorf("status = %q, want %q", runRepo.runs["run-cancel"].Status, "cancelled")
	}
}

func TestCancelAlreadyCompletedRun(t *testing.T) {
	srv, _, runRepo := newTestServer()

	r := &Run{
		ID:     "run-done",
		Status: "completed",
	}
	runRepo.runs[r.ID] = r

	rec := doRequest(srv, http.MethodPost, "/api/v1/runs/run-done/cancel", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestRetryRun(t *testing.T) {
	srv, _, runRepo := newTestServer()

	r := &Run{
		ID:         "run-fail",
		WorkflowID: "wf-1",
		Status:     "failed",
		Input:      map[string]interface{}{"key": "value"},
	}
	runRepo.runs[r.ID] = r

	rec := doRequest(srv, http.MethodPost, "/api/v1/runs/run-fail/retry", nil)
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	// Should have 2 runs now: the original failed one and the new retry.
	if len(runRepo.runs) != 2 {
		t.Errorf("runs count = %d, want 2", len(runRepo.runs))
	}
}

func TestRetryNonFailedRun(t *testing.T) {
	srv, _, runRepo := newTestServer()

	r := &Run{
		ID:     "run-ok",
		Status: "completed",
	}
	runRepo.runs[r.ID] = r

	rec := doRequest(srv, http.MethodPost, "/api/v1/runs/run-ok/retry", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestGetRunLogs(t *testing.T) {
	srv, _, runRepo := newTestServer()

	now := time.Now().UTC()
	r := &Run{
		ID:         "run-logs",
		WorkflowID: "wf-1",
		Status:     "completed",
		TaskStates: []TaskState{
			{
				TaskID:    "task-1",
				Name:      "First",
				Status:    "success",
				StartedAt: now.Add(-10 * time.Second),
				EndedAt:   now,
			},
		},
	}
	runRepo.runs[r.ID] = r

	rec := doRequest(srv, http.MethodGet, "/api/v1/runs/run-logs/logs", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := parseAPIResponse(t, rec)
	logs, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatal("data is not an array")
	}
	if len(logs) != 2 {
		t.Errorf("len(logs) = %d, want 2 (start + end)", len(logs))
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	srv, _, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-ID")
	if rid == "" {
		t.Error("X-Request-ID header is missing")
	}
}

func TestRequestIDMiddlewarePreservesClientID(t *testing.T) {
	srv, _, _ := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("X-Request-ID", "my-custom-id")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-ID")
	if rid != "my-custom-id" {
		t.Errorf("X-Request-ID = %q, want %q", rid, "my-custom-id")
	}
}

func TestCORSMiddleware(t *testing.T) {
	srv, _, _ := newTestServer()

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	acam := rec.Header().Get("Access-Control-Allow-Methods")
	if acam == "" {
		t.Error("Access-Control-Allow-Methods header is missing")
	}
}

func TestCreateWorkflowInvalidJSON(t *testing.T) {
	srv, _, _ := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows", bytes.NewBufferString("{invalid}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListRunsByWorkflowID(t *testing.T) {
	srv, _, runRepo := newTestServer()

	// Create runs for different workflows.
	for i := 0; i < 2; i++ {
		r := &Run{
			ID:         fmt.Sprintf("run-wf1-%d", i),
			WorkflowID: "wf-1",
			Status:     "completed",
			CreatedAt:  time.Now().UTC(),
		}
		runRepo.runs[r.ID] = r
	}
	r := &Run{
		ID:         "run-wf2-0",
		WorkflowID: "wf-2",
		Status:     "running",
		CreatedAt:  time.Now().UTC(),
	}
	runRepo.runs[r.ID] = r

	rec := doRequest(srv, http.MethodGet, "/api/v1/runs?workflow_id=wf-1", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	resp := parseAPIResponse(t, rec)
	if resp.Meta.Total != 2 {
		t.Errorf("meta.total = %d, want 2 (filtered by workflow_id)", resp.Meta.Total)
	}
}

func TestContentTypeHeader(t *testing.T) {
	srv, _, _ := newTestServer()
	rec := doRequest(srv, http.MethodGet, "/api/v1/health", nil)

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json; charset=utf-8")
	}
}
