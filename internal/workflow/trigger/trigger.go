// Package trigger provides trigger types for starting workflow executions.
package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// TriggerEvent represents an event that triggered a workflow.
type TriggerEvent struct {
	Type      string                 `json:"type"`
	Source    string                 `json:"source"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp time.Time              `json:"timestamp"`
}

// TriggerHandler is a callback function invoked when a trigger fires.
type TriggerHandler func(ctx context.Context, event TriggerEvent)

// Trigger defines the interface for all trigger types.
type Trigger interface {
	// Type returns the trigger type identifier.
	Type() string
	// Start begins listening for trigger events and calls handler when fired.
	Start(ctx context.Context, handler TriggerHandler) error
	// Stop gracefully shuts down the trigger.
	Stop() error
}

// --- CronTrigger ---

// CronTrigger fires on a cron schedule.
type CronTrigger struct {
	Schedule string `json:"schedule"`
	Timezone string `json:"timezone,omitempty"`

	cron    *cron.Cron
	entryID cron.EntryID
	mu      sync.Mutex
	running bool
}

// NewCronTrigger creates a new CronTrigger with the given schedule.
func NewCronTrigger(schedule string, timezone string) *CronTrigger {
	return &CronTrigger{
		Schedule: schedule,
		Timezone: timezone,
	}
}

// Type returns "cron".
func (t *CronTrigger) Type() string { return "cron" }

// Start begins the cron scheduler and calls handler on each tick.
func (t *CronTrigger) Start(ctx context.Context, handler TriggerHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("cron trigger already running")
	}

	var opts []cron.Option
	opts = append(opts, cron.WithParser(cron.NewParser(
		cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor,
	)))

	if t.Timezone != "" {
		loc, err := time.LoadLocation(t.Timezone)
		if err != nil {
			return fmt.Errorf("invalid timezone %q: %w", t.Timezone, err)
		}
		opts = append(opts, cron.WithLocation(loc))
	}

	t.cron = cron.New(opts...)

	entryID, err := t.cron.AddFunc(t.Schedule, func() {
		event := TriggerEvent{
			Type:      "cron",
			Source:    fmt.Sprintf("cron:%s", t.Schedule),
			Payload:   map[string]interface{}{"schedule": t.Schedule},
			Timestamp: time.Now().UTC(),
		}
		handler(ctx, event)
	})
	if err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", t.Schedule, err)
	}

	t.entryID = entryID
	t.cron.Start()
	t.running = true

	// Stop cron when context is cancelled.
	go func() {
		<-ctx.Done()
		_ = t.Stop()
	}()

	return nil
}

// Stop stops the cron scheduler.
func (t *CronTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	if t.cron != nil {
		ctx := t.cron.Stop()
		<-ctx.Done()
	}
	t.running = false
	return nil
}

// --- WebhookTrigger ---

// WebhookTrigger fires when an HTTP POST is received on the configured path.
type WebhookTrigger struct {
	Path   string `json:"path"`
	Port   int    `json:"port"`
	Secret string `json:"secret,omitempty"`

	server  *http.Server
	mu      sync.Mutex
	running bool
	addr    string
}

// NewWebhookTrigger creates a new WebhookTrigger.
func NewWebhookTrigger(path string, port int, secret string) *WebhookTrigger {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return &WebhookTrigger{
		Path:   path,
		Port:   port,
		Secret: secret,
	}
}

// Type returns "webhook".
func (t *WebhookTrigger) Type() string { return "webhook" }

// Addr returns the actual address the webhook server is listening on.
// Only valid after Start returns successfully.
func (t *WebhookTrigger) Addr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.addr
}

// Start creates an HTTP server listening for webhook requests.
func (t *WebhookTrigger) Start(ctx context.Context, handler TriggerHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("webhook trigger already running")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(t.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Validate secret if configured.
		if t.Secret != "" {
			providedSecret := r.Header.Get("X-Webhook-Secret")
			if providedSecret != t.Secret {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var payload map[string]interface{}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				// If body isn't JSON, wrap it.
				payload = map[string]interface{}{
					"raw_body": string(body),
				}
			}
		}

		// Add request metadata to payload.
		if payload == nil {
			payload = make(map[string]interface{})
		}
		payload["_headers"] = flattenHeaders(r.Header)
		payload["_query"] = flattenQuery(r.URL.Query())

		event := TriggerEvent{
			Type:      "webhook",
			Source:    fmt.Sprintf("webhook:%s", t.Path),
			Payload:   payload,
			Timestamp: time.Now().UTC(),
		}

		handler(ctx, event)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", t.Port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", t.Port, err)
	}

	t.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	t.addr = ln.Addr().String()
	t.running = true

	go func() {
		if err := t.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash; the trigger can be stopped.
		}
	}()

	// Graceful shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		_ = t.Stop()
	}()

	return nil
}

// Stop shuts down the webhook HTTP server.
func (t *WebhookTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	if t.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := t.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("webhook server shutdown error: %w", err)
		}
	}
	t.running = false
	return nil
}

// flattenHeaders converts http.Header to a flat map.
func flattenHeaders(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		m[k] = strings.Join(v, ", ")
	}
	return m
}

// flattenQuery converts url.Values to a flat map.
func flattenQuery(q map[string][]string) map[string]string {
	m := make(map[string]string, len(q))
	for k, v := range q {
		m[k] = strings.Join(v, ", ")
	}
	return m
}

// --- EventTrigger ---

// EventTrigger fires when an event matching a pattern is received.
type EventTrigger struct {
	Pattern string `json:"pattern"`

	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	eventCh   chan TriggerEvent
}

// NewEventTrigger creates a new EventTrigger with the given pattern.
func NewEventTrigger(pattern string) *EventTrigger {
	return &EventTrigger{
		Pattern: pattern,
		eventCh: make(chan TriggerEvent, 100),
	}
}

// Type returns "event".
func (t *EventTrigger) Type() string { return "event" }

// Emit allows external code to push an event into this trigger.
// The event will be delivered if it matches the trigger's pattern.
func (t *EventTrigger) Emit(event TriggerEvent) {
	t.mu.Lock()
	running := t.running
	t.mu.Unlock()

	if !running {
		return
	}

	if matchPattern(t.Pattern, event.Type) {
		select {
		case t.eventCh <- event:
		default:
			// Channel full; drop event.
		}
	}
}

// Start begins listening for events matching the pattern.
func (t *EventTrigger) Start(ctx context.Context, handler TriggerHandler) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("event trigger already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.running = true
	t.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-t.eventCh:
				event.Source = fmt.Sprintf("event:%s", t.Pattern)
				if event.Timestamp.IsZero() {
					event.Timestamp = time.Now().UTC()
				}
				handler(ctx, event)
			}
		}
	}()

	return nil
}

// Stop stops listening for events.
func (t *EventTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	if t.cancel != nil {
		t.cancel()
	}
	t.running = false
	return nil
}

// matchPattern checks if a string matches a glob-like pattern.
// Supports * as a wildcard for any sequence of characters.
func matchPattern(pattern, s string) bool {
	if pattern == "*" {
		return true
	}

	// Simple glob matching with * wildcards.
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}

	// Check prefix match.
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]

	// Check each middle part.
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}

	// Check suffix match.
	return strings.HasSuffix(s, parts[len(parts)-1])
}

// --- ManualTrigger ---

// ManualTrigger fires when explicitly triggered by calling Fire().
type ManualTrigger struct {
	Description string `json:"description,omitempty"`

	mu      sync.Mutex
	running bool
	handler TriggerHandler
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewManualTrigger creates a new ManualTrigger.
func NewManualTrigger(description string) *ManualTrigger {
	return &ManualTrigger{
		Description: description,
	}
}

// Type returns "manual".
func (t *ManualTrigger) Type() string { return "manual" }

// Start registers the handler for manual trigger events.
func (t *ManualTrigger) Start(ctx context.Context, handler TriggerHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("manual trigger already running")
	}

	t.ctx, t.cancel = context.WithCancel(ctx)
	t.handler = handler
	t.running = true

	return nil
}

// Fire manually triggers the workflow with the given payload.
func (t *ManualTrigger) Fire(payload map[string]interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return fmt.Errorf("manual trigger is not running")
	}
	if t.handler == nil {
		return fmt.Errorf("no handler registered")
	}

	event := TriggerEvent{
		Type:      "manual",
		Source:    "manual",
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}

	// Fire asynchronously to avoid blocking the caller.
	go t.handler(t.ctx, event)

	return nil
}

// Stop deactivates the manual trigger.
func (t *ManualTrigger) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	if t.cancel != nil {
		t.cancel()
	}
	t.handler = nil
	t.running = false
	return nil
}

// --- TriggerManager ---

// TriggerManager manages multiple triggers for a workflow.
type TriggerManager struct {
	mu       sync.RWMutex
	triggers map[string]Trigger // name -> trigger
	running  bool
}

// NewTriggerManager creates a new TriggerManager.
func NewTriggerManager() *TriggerManager {
	return &TriggerManager{
		triggers: make(map[string]Trigger),
	}
}

// Register adds a trigger to the manager with a unique name.
func (m *TriggerManager) Register(name string, trigger Trigger) error {
	if name == "" {
		return fmt.Errorf("trigger name is required")
	}
	if trigger == nil {
		return fmt.Errorf("trigger is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.triggers[name]; exists {
		return fmt.Errorf("trigger %q already registered", name)
	}

	m.triggers[name] = trigger
	return nil
}

// Get retrieves a trigger by name.
func (m *TriggerManager) Get(name string) (Trigger, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.triggers[name]
	return t, ok
}

// Start starts all registered triggers.
func (m *TriggerManager) Start(ctx context.Context, handler TriggerHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("trigger manager already running")
	}

	var startErrors []string
	for name, t := range m.triggers {
		if err := t.Start(ctx, handler); err != nil {
			startErrors = append(startErrors, fmt.Sprintf("%s: %s", name, err.Error()))
		}
	}

	m.running = true

	if len(startErrors) > 0 {
		return fmt.Errorf("some triggers failed to start:\n  - %s", strings.Join(startErrors, "\n  - "))
	}

	return nil
}

// StopAll stops all registered triggers.
func (m *TriggerManager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	var stopErrors []string
	for name, t := range m.triggers {
		if err := t.Stop(); err != nil {
			stopErrors = append(stopErrors, fmt.Sprintf("%s: %s", name, err.Error()))
		}
	}

	m.running = false

	if len(stopErrors) > 0 {
		return fmt.Errorf("some triggers failed to stop:\n  - %s", strings.Join(stopErrors, "\n  - "))
	}

	return nil
}

// List returns the names of all registered triggers.
func (m *TriggerManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.triggers))
	for name := range m.triggers {
		names = append(names, name)
	}
	return names
}
