// Package flowforge provides a Go SDK for interacting with the FlowForge
// distributed workflow orchestration engine.
package flowforge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrNotFound is returned when the requested resource does not exist.
	ErrNotFound = errors.New("flowforge: resource not found")
	// ErrUnauthorized is returned when the API key is invalid or missing.
	ErrUnauthorized = errors.New("flowforge: unauthorized")
	// ErrTimeout is returned when an operation exceeds its deadline.
	ErrTimeout = errors.New("flowforge: operation timed out")
	// ErrBadRequest is returned for invalid input.
	ErrBadRequest = errors.New("flowforge: bad request")
	// ErrConflict is returned when a resource conflict occurs.
	ErrConflict = errors.New("flowforge: conflict")
	// ErrServerError is returned for unexpected server-side errors.
	ErrServerError = errors.New("flowforge: server error")
)

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

// WorkflowStatus represents the status of a workflow.
type WorkflowStatus string

const (
	WorkflowStatusActive   WorkflowStatus = "active"
	WorkflowStatusInactive WorkflowStatus = "inactive"
	WorkflowStatusArchived WorkflowStatus = "archived"
	WorkflowStatusDraft    WorkflowStatus = "draft"
)

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
	TaskStatusSkipped   TaskStatus = "skipped"
	TaskStatusRetrying  TaskStatus = "retrying"
	TaskStatusTimedOut  TaskStatus = "timed_out"
)

// RunStatus represents the status of a workflow run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
	RunStatusTimedOut  RunStatus = "timed_out"
)

// Priority represents the priority level.
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

// Workflow represents a workflow definition.
type Workflow struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Version     int               `json:"version"`
	Status      WorkflowStatus    `json:"status"`
	Triggers    []TriggerConfig   `json:"triggers,omitempty"`
	Tasks       []TaskDefinition  `json:"tasks"`
	Timeout     string            `json:"timeout,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// TriggerConfig defines a workflow trigger.
type TriggerConfig struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// TaskDefinition defines a single task within a workflow.
type TaskDefinition struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Config    map[string]any    `json:"config,omitempty"`
	DependsOn []string          `json:"depends_on,omitempty"`
	Timeout   string            `json:"timeout,omitempty"`
	Retry     *RetryPolicy      `json:"retry,omitempty"`
	Condition string            `json:"condition,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// RetryPolicy configures retry behaviour for a task.
type RetryPolicy struct {
	MaxRetries   int     `json:"max_retries"`
	InitialDelay string  `json:"initial_delay,omitempty"`
	MaxDelay     string  `json:"max_delay,omitempty"`
	Multiplier   float64 `json:"multiplier,omitempty"`
	Strategy     string  `json:"strategy,omitempty"` // "fixed", "exponential", "linear"
}

// Run represents a workflow execution instance.
type Run struct {
	ID          string            `json:"id"`
	WorkflowID  string           `json:"workflow_id"`
	Version     int              `json:"version"`
	Status      RunStatus        `json:"status"`
	Trigger     string           `json:"trigger"`
	Params      map[string]any   `json:"params,omitempty"`
	Results     []TaskResult     `json:"results,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	StartedAt   *time.Time       `json:"started_at,omitempty"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	Error       string           `json:"error,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TaskResult represents the outcome of a single task execution.
type TaskResult struct {
	TaskID     string            `json:"task_id"`
	Status     TaskStatus        `json:"status"`
	Output     json.RawMessage   `json:"output,omitempty"`
	Error      string            `json:"error,omitempty"`
	Duration   string            `json:"duration,omitempty"`
	RetryCount int               `json:"retry_count"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// LogEntry represents a single log line from a workflow run.
type LogEntry struct {
	ID        string            `json:"id"`
	RunID     string            `json:"run_id"`
	TaskID    string            `json:"task_id,omitempty"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Timestamp time.Time         `json:"timestamp"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// ---------------------------------------------------------------------------
// API error type
// ---------------------------------------------------------------------------

// APIError is returned when the server responds with an error payload.
type APIError struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("flowforge: HTTP %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// WithTimeout sets the default request timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// Client connects to a FlowForge server and exposes workflow operations.
type Client struct {
	serverURL  string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new FlowForge client.
//
//	client := flowforge.NewClient("https://flowforge.example.com", "my-api-key")
func NewClient(serverURL, apiKey string, opts ...ClientOption) *Client {
	serverURL = strings.TrimRight(serverURL, "/")
	c := &Client{
		serverURL: serverURL,
		apiKey:    apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ---------------------------------------------------------------------------
// Workflow CRUD
// ---------------------------------------------------------------------------

// CreateWorkflowInput holds the parameters for creating a workflow.
type CreateWorkflowInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Triggers    []TriggerConfig   `json:"triggers,omitempty"`
	Tasks       []TaskDefinition  `json:"tasks"`
	Timeout     string            `json:"timeout,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// CreateWorkflow creates a new workflow on the server.
func (c *Client) CreateWorkflow(ctx context.Context, input *CreateWorkflowInput) (*Workflow, error) {
	var wf Workflow
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/workflows", input, &wf); err != nil {
		return nil, err
	}
	return &wf, nil
}

// GetWorkflow retrieves a workflow by ID.
func (c *Client) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	var wf Workflow
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/workflows/"+url.PathEscape(id), nil, &wf); err != nil {
		return nil, err
	}
	return &wf, nil
}

// ListWorkflowsOptions holds optional filters for listing workflows.
type ListWorkflowsOptions struct {
	Status    WorkflowStatus `json:"status,omitempty"`
	PageSize  int            `json:"page_size,omitempty"`
	PageToken string         `json:"page_token,omitempty"`
}

// ListWorkflowsResponse is the paginated response for listing workflows.
type ListWorkflowsResponse struct {
	Workflows     []Workflow `json:"workflows"`
	NextPageToken string     `json:"next_page_token,omitempty"`
	TotalCount    int        `json:"total_count"`
}

// ListWorkflows lists workflows with optional filtering.
func (c *Client) ListWorkflows(ctx context.Context, opts *ListWorkflowsOptions) (*ListWorkflowsResponse, error) {
	path := "/api/v1/workflows"
	if opts != nil {
		q := url.Values{}
		if opts.Status != "" {
			q.Set("status", string(opts.Status))
		}
		if opts.PageSize > 0 {
			q.Set("page_size", fmt.Sprintf("%d", opts.PageSize))
		}
		if opts.PageToken != "" {
			q.Set("page_token", opts.PageToken)
		}
		if qs := q.Encode(); qs != "" {
			path += "?" + qs
		}
	}
	var resp ListWorkflowsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateWorkflowInput holds fields to update on a workflow.
type UpdateWorkflowInput struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Status      *WorkflowStatus   `json:"status,omitempty"`
	Triggers    []TriggerConfig   `json:"triggers,omitempty"`
	Tasks       []TaskDefinition  `json:"tasks,omitempty"`
	Timeout     *string           `json:"timeout,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// UpdateWorkflow updates an existing workflow.
func (c *Client) UpdateWorkflow(ctx context.Context, id string, input *UpdateWorkflowInput) (*Workflow, error) {
	var wf Workflow
	if err := c.doJSON(ctx, http.MethodPut, "/api/v1/workflows/"+url.PathEscape(id), input, &wf); err != nil {
		return nil, err
	}
	return &wf, nil
}

// DeleteWorkflow removes a workflow by ID.
func (c *Client) DeleteWorkflow(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/workflows/"+url.PathEscape(id), nil, nil)
}

// ---------------------------------------------------------------------------
// Trigger & Run
// ---------------------------------------------------------------------------

// TriggerWorkflowInput holds the parameters for triggering a workflow.
type TriggerWorkflowInput struct {
	Params   map[string]any    `json:"params,omitempty"`
	Priority Priority          `json:"priority,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// TriggerWorkflow starts a new run of the given workflow.
func (c *Client) TriggerWorkflow(ctx context.Context, workflowID string, input *TriggerWorkflowInput) (*Run, error) {
	var run Run
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/workflows/"+url.PathEscape(workflowID)+"/trigger", input, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// GetRun retrieves a workflow run by ID.
func (c *Client) GetRun(ctx context.Context, runID string) (*Run, error) {
	var run Run
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/runs/"+url.PathEscape(runID), nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// CancelRun cancels a running workflow execution.
func (c *Client) CancelRun(ctx context.Context, runID string) (*Run, error) {
	var run Run
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/runs/"+url.PathEscape(runID)+"/cancel", nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// WaitForCompletion polls the run until it reaches a terminal state or the
// context is cancelled.
func (c *Client) WaitForCompletion(ctx context.Context, runID string, pollInterval time.Duration) (*Run, error) {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		run, err := c.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}

		switch run.Status {
		case RunStatusCompleted, RunStatusFailed, RunStatusCancelled, RunStatusTimedOut:
			return run, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %v", ErrTimeout, ctx.Err())
		case <-ticker.C:
			// continue polling
		}
	}
}

// ---------------------------------------------------------------------------
// Log streaming
// ---------------------------------------------------------------------------

// LogHandler is the callback invoked for each streamed log entry.
type LogHandler func(entry LogEntry)

// StreamLogs opens a streaming connection and delivers log entries for a run
// via the provided handler. It blocks until the run completes, the context is
// cancelled, or an error occurs.
func (c *Client) StreamLogs(ctx context.Context, runID string, handler LogHandler) error {
	path := "/api/v1/runs/" + url.PathEscape(runID) + "/logs/stream"
	reqURL := c.serverURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("flowforge: create request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("flowforge: stream logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseErrorResponse(resp)
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("flowforge: decode log entry: %w", err)
		}
		handler(entry)
	}
}

// ---------------------------------------------------------------------------
// Workflow builder
// ---------------------------------------------------------------------------

// WorkflowBuilder provides a fluent interface for constructing workflows.
type WorkflowBuilder struct {
	name        string
	description string
	triggers    []TriggerConfig
	tasks       []TaskDefinition
	timeout     string
	metadata    map[string]string
	taskIndex   map[string]int // task ID -> index in tasks slice
	err         error
}

// NewWorkflow starts building a new workflow with the given name.
func NewWorkflow(name string) *WorkflowBuilder {
	return &WorkflowBuilder{
		name:      name,
		taskIndex: make(map[string]int),
		metadata:  make(map[string]string),
	}
}

// Description sets the workflow description.
func (b *WorkflowBuilder) Description(desc string) *WorkflowBuilder {
	b.description = desc
	return b
}

// Timeout sets the overall workflow timeout (e.g. "30m", "1h").
func (b *WorkflowBuilder) Timeout(t string) *WorkflowBuilder {
	b.timeout = t
	return b
}

// WithMetadata adds a metadata key-value pair.
func (b *WorkflowBuilder) WithMetadata(key, value string) *WorkflowBuilder {
	b.metadata[key] = value
	return b
}

// AddTrigger adds a trigger configuration.
func (b *WorkflowBuilder) AddTrigger(triggerType string, config map[string]any) *WorkflowBuilder {
	b.triggers = append(b.triggers, TriggerConfig{
		Type:   triggerType,
		Config: config,
	})
	return b
}

// AddTask adds a task definition to the workflow.
func (b *WorkflowBuilder) AddTask(task TaskDefinition) *WorkflowBuilder {
	if task.ID == "" {
		b.err = errors.New("flowforge: task ID is required")
		return b
	}
	if _, exists := b.taskIndex[task.ID]; exists {
		b.err = fmt.Errorf("flowforge: duplicate task ID %q", task.ID)
		return b
	}
	b.taskIndex[task.ID] = len(b.tasks)
	b.tasks = append(b.tasks, task)
	return b
}

// AddDependency declares that taskID depends on dependsOnID.
func (b *WorkflowBuilder) AddDependency(taskID, dependsOnID string) *WorkflowBuilder {
	idx, ok := b.taskIndex[taskID]
	if !ok {
		b.err = fmt.Errorf("flowforge: task %q not found", taskID)
		return b
	}
	if _, ok := b.taskIndex[dependsOnID]; !ok {
		b.err = fmt.Errorf("flowforge: dependency %q not found", dependsOnID)
		return b
	}
	// Avoid duplicate dependencies.
	for _, dep := range b.tasks[idx].DependsOn {
		if dep == dependsOnID {
			return b
		}
	}
	b.tasks[idx].DependsOn = append(b.tasks[idx].DependsOn, dependsOnID)
	return b
}

// Build validates and returns the CreateWorkflowInput.
func (b *WorkflowBuilder) Build() (*CreateWorkflowInput, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.name == "" {
		return nil, errors.New("flowforge: workflow name is required")
	}
	if len(b.tasks) == 0 {
		return nil, errors.New("flowforge: workflow must have at least one task")
	}

	// Validate dependency references.
	for _, task := range b.tasks {
		for _, dep := range task.DependsOn {
			if _, ok := b.taskIndex[dep]; !ok {
				return nil, fmt.Errorf("flowforge: task %q depends on unknown task %q", task.ID, dep)
			}
		}
	}

	return &CreateWorkflowInput{
		Name:        b.name,
		Description: b.description,
		Triggers:    b.triggers,
		Tasks:       b.tasks,
		Timeout:     b.timeout,
		Metadata:    b.metadata,
	}, nil
}

// ---------------------------------------------------------------------------
// Task builder helpers
// ---------------------------------------------------------------------------

// NewHTTPTask creates a task definition for an HTTP request.
func NewHTTPTask(id, name, method, taskURL string) TaskDefinition {
	return TaskDefinition{
		ID:   id,
		Name: name,
		Type: "http",
		Config: map[string]any{
			"method": method,
			"url":    taskURL,
		},
	}
}

// NewHTTPTaskWithBody creates an HTTP task with a request body.
func NewHTTPTaskWithBody(id, name, method, taskURL string, body any, headers map[string]string) TaskDefinition {
	config := map[string]any{
		"method": method,
		"url":    taskURL,
	}
	if body != nil {
		config["body"] = body
	}
	if len(headers) > 0 {
		config["headers"] = headers
	}
	return TaskDefinition{
		ID:     id,
		Name:   name,
		Type:   "http",
		Config: config,
	}
}

// NewScriptTask creates a task definition that runs a script or command.
func NewScriptTask(id, name, command string, args []string) TaskDefinition {
	config := map[string]any{
		"command": command,
	}
	if len(args) > 0 {
		config["args"] = args
	}
	return TaskDefinition{
		ID:     id,
		Name:   name,
		Type:   "script",
		Config: config,
	}
}

// NewConditionTask creates a conditional branching task.
func NewConditionTask(id, name, expression string) TaskDefinition {
	return TaskDefinition{
		ID:   id,
		Name: name,
		Type: "condition",
		Config: map[string]any{
			"expression": expression,
		},
	}
}

// NewDelayTask creates a task that pauses execution for a specified duration.
func NewDelayTask(id, name, duration string) TaskDefinition {
	return TaskDefinition{
		ID:   id,
		Name: name,
		Type: "delay",
		Config: map[string]any{
			"duration": duration,
		},
	}
}

// NewNotifyTask creates a notification task.
func NewNotifyTask(id, name, channel string, config map[string]any) TaskDefinition {
	if config == nil {
		config = make(map[string]any)
	}
	config["channel"] = channel
	return TaskDefinition{
		ID:     id,
		Name:   name,
		Type:   "notify",
		Config: config,
	}
}

// NewApprovalTask creates a human approval task with a timeout.
func NewApprovalTask(id, name, timeout string, approvers []string) TaskDefinition {
	return TaskDefinition{
		ID:   id,
		Name: name,
		Type: "approval",
		Config: map[string]any{
			"approvers": approvers,
		},
		Timeout: timeout,
	}
}

// WithRetry adds a retry policy to a task definition and returns it.
func WithRetry(task TaskDefinition, maxRetries int, strategy string, initialDelay string) TaskDefinition {
	task.Retry = &RetryPolicy{
		MaxRetries:   maxRetries,
		Strategy:     strategy,
		InitialDelay: initialDelay,
	}
	return task
}

// WithCondition adds a condition expression to a task definition and returns it.
func WithCondition(task TaskDefinition, condition string) TaskDefinition {
	task.Condition = condition
	return task
}

// WithTimeout adds a timeout to a task definition and returns it.
func WithTimeout(task TaskDefinition, timeout string) TaskDefinition {
	task.Timeout = timeout
	return task
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (c *Client) doJSON(ctx context.Context, method, path string, body any, result any) error {
	reqURL := c.serverURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("flowforge: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return fmt.Errorf("flowforge: create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return fmt.Errorf("flowforge: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseErrorResponse(resp)
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("flowforge: read response: %w", err)
		}
		if len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("flowforge: unmarshal response: %w", err)
			}
		}
	}

	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "flowforge-go-sdk/1.0")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func (c *Client) parseErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
	}

	if len(body) > 0 {
		_ = json.Unmarshal(body, apiErr)
	}

	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	if apiErr.Code == "" {
		apiErr.Code = http.StatusText(resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, apiErr.Message)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrUnauthorized, apiErr.Message)
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrBadRequest, apiErr.Message)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", ErrConflict, apiErr.Message)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: %s", ErrTimeout, apiErr.Message)
	default:
		return apiErr
	}
}
