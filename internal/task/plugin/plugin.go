// Package plugin provides a plugin system interface for extending FlowForge
// with custom task types.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Result represents the outcome of a plugin task execution.
type Result struct {
	Output   interface{}   `json:"output"`
	Duration time.Duration `json:"duration"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Plugin defines the interface that all FlowForge plugins must implement.
type Plugin interface {
	// Name returns the unique name of the plugin.
	Name() string
	// Version returns the plugin version string.
	Version() string
	// TaskTypes returns the list of task types this plugin handles.
	TaskTypes() []string
	// Execute runs the specified task type with the given configuration.
	Execute(taskType string, ctx context.Context, config map[string]interface{}) (*Result, error)
}

// PluginInfo contains metadata about a registered plugin.
type PluginInfo struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	TaskTypes []string `json:"task_types"`
}

// PluginRegistry manages registered plugins and routes task executions.
type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin            // name -> plugin
	taskMap map[string]string            // taskType -> plugin name
}

// NewPluginRegistry creates a new PluginRegistry.
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]Plugin),
		taskMap: make(map[string]string),
	}
}

// Register adds a plugin to the registry. It maps all of the plugin's task types
// to this plugin for routing.
func (r *PluginRegistry) Register(p Plugin) error {
	if p == nil {
		return fmt.Errorf("plugin is nil")
	}
	if p.Name() == "" {
		return fmt.Errorf("plugin name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[p.Name()]; exists {
		return fmt.Errorf("plugin %q already registered", p.Name())
	}

	// Check for task type conflicts.
	for _, taskType := range p.TaskTypes() {
		if existing, exists := r.taskMap[taskType]; exists {
			return fmt.Errorf("task type %q is already registered by plugin %q", taskType, existing)
		}
	}

	r.plugins[p.Name()] = p
	for _, taskType := range p.TaskTypes() {
		r.taskMap[taskType] = p.Name()
	}

	return nil
}

// Get retrieves a plugin by name.
func (r *PluginRegistry) Get(name string) (Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found", name)
	}
	return p, nil
}

// GetByTaskType retrieves the plugin that handles the given task type.
func (r *PluginRegistry) GetByTaskType(taskType string) (Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name, ok := r.taskMap[taskType]
	if !ok {
		return nil, fmt.Errorf("no plugin registered for task type %q", taskType)
	}

	p, ok := r.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found (registered for task type %q)", name, taskType)
	}

	return p, nil
}

// List returns information about all registered plugins.
func (r *PluginRegistry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]PluginInfo, 0, len(r.plugins))
	for _, p := range r.plugins {
		infos = append(infos, PluginInfo{
			Name:      p.Name(),
			Version:   p.Version(),
			TaskTypes: p.TaskTypes(),
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})

	return infos
}

// Execute routes a task execution to the appropriate plugin based on task type.
func (r *PluginRegistry) Execute(taskType string, ctx context.Context, config map[string]interface{}) (*Result, error) {
	p, err := r.GetByTaskType(taskType)
	if err != nil {
		return nil, err
	}
	return p.Execute(taskType, ctx, config)
}

// Unregister removes a plugin from the registry.
func (r *PluginRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	// Remove task type mappings.
	for _, taskType := range p.TaskTypes() {
		delete(r.taskMap, taskType)
	}

	delete(r.plugins, name)
	return nil
}

// --- PluginLoader ---

// PluginLoader loads plugins by name from a configurable set of factories.
type PluginLoader struct {
	mu        sync.RWMutex
	factories map[string]PluginFactory
}

// PluginFactory is a function that creates a new plugin instance.
type PluginFactory func() (Plugin, error)

// NewPluginLoader creates a new PluginLoader with built-in plugins pre-registered.
func NewPluginLoader() *PluginLoader {
	loader := &PluginLoader{
		factories: make(map[string]PluginFactory),
	}

	// Register built-in plugins.
	loader.factories["logging"] = func() (Plugin, error) {
		return NewLoggingPlugin(), nil
	}
	loader.factories["debug"] = func() (Plugin, error) {
		return NewDebugPlugin(), nil
	}

	return loader
}

// RegisterFactory registers a plugin factory by name.
func (l *PluginLoader) RegisterFactory(name string, factory PluginFactory) error {
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if factory == nil {
		return fmt.Errorf("plugin factory is required")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, exists := l.factories[name]; exists {
		return fmt.Errorf("plugin factory %q already registered", name)
	}

	l.factories[name] = factory
	return nil
}

// Load creates a new instance of the named plugin.
func (l *PluginLoader) Load(name string) (Plugin, error) {
	l.mu.RLock()
	factory, ok := l.factories[name]
	l.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown plugin %q; available: %s", name, l.available())
	}

	return factory()
}

// Available returns the names of all available plugins.
func (l *PluginLoader) Available() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	names := make([]string, 0, len(l.factories))
	for name := range l.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// available returns a comma-separated list of available plugin names (for error messages).
func (l *PluginLoader) available() string {
	names := l.Available()
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// --- Built-in Logging Plugin ---

// LoggingPlugin is a built-in plugin that logs task inputs and outputs.
type LoggingPlugin struct {
	mu      sync.Mutex
	entries []LogEntry
}

// LogEntry represents a single log entry created by the logging plugin.
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	TaskType  string                 `json:"task_type"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// NewLoggingPlugin creates a new LoggingPlugin.
func NewLoggingPlugin() *LoggingPlugin {
	return &LoggingPlugin{
		entries: make([]LogEntry, 0),
	}
}

// Name returns "logging".
func (p *LoggingPlugin) Name() string { return "logging" }

// Version returns the plugin version.
func (p *LoggingPlugin) Version() string { return "1.0.0" }

// TaskTypes returns the task types handled by this plugin.
func (p *LoggingPlugin) TaskTypes() []string {
	return []string{"log", "logging"}
}

// Execute logs the provided configuration data and returns success.
func (p *LoggingPlugin) Execute(taskType string, ctx context.Context, config map[string]interface{}) (*Result, error) {
	start := time.Now()

	// Extract log parameters.
	level := "info"
	if l, ok := config["level"].(string); ok {
		level = l
	}

	message := ""
	if m, ok := config["message"].(string); ok {
		message = m
	}

	var data map[string]interface{}
	if d, ok := config["data"].(map[string]interface{}); ok {
		data = d
	}

	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		TaskType:  taskType,
		Level:     level,
		Message:   message,
		Data:      data,
	}

	p.mu.Lock()
	p.entries = append(p.entries, entry)
	p.mu.Unlock()

	entryJSON, _ := json.Marshal(entry)

	return &Result{
		Output:   string(entryJSON),
		Duration: time.Since(start),
		Success:  true,
		Metadata: map[string]interface{}{
			"entries_total": len(p.entries),
		},
	}, nil
}

// Entries returns all logged entries.
func (p *LoggingPlugin) Entries() []LogEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	cp := make([]LogEntry, len(p.entries))
	copy(cp, p.entries)
	return cp
}

// --- Built-in Debug Plugin ---

// DebugPlugin is a built-in plugin for debugging and development.
// It echoes back the configuration, supports deliberate delays and failures.
type DebugPlugin struct{}

// NewDebugPlugin creates a new DebugPlugin.
func NewDebugPlugin() *DebugPlugin {
	return &DebugPlugin{}
}

// Name returns "debug".
func (p *DebugPlugin) Name() string { return "debug" }

// Version returns the plugin version.
func (p *DebugPlugin) Version() string { return "1.0.0" }

// TaskTypes returns the task types handled by this plugin.
func (p *DebugPlugin) TaskTypes() []string {
	return []string{"debug", "echo", "noop"}
}

// Execute runs the debug task. Supports the following config keys:
//   - "echo": echoes back the provided value
//   - "delay": adds a delay (e.g., "1s", "500ms")
//   - "fail": if true, returns an error
//   - "fail_message": custom error message
//   - "panic": if true, panics (for testing recovery)
func (p *DebugPlugin) Execute(taskType string, ctx context.Context, config map[string]interface{}) (*Result, error) {
	start := time.Now()

	// Handle noop.
	if taskType == "noop" {
		return &Result{
			Output:   nil,
			Duration: time.Since(start),
			Success:  true,
			Metadata: map[string]interface{}{"task_type": taskType},
		}, nil
	}

	// Handle delay.
	if delayStr, ok := config["delay"].(string); ok && delayStr != "" {
		d, err := time.ParseDuration(delayStr)
		if err != nil {
			return nil, fmt.Errorf("invalid delay duration %q: %w", delayStr, err)
		}

		timer := time.NewTimer(d)
		defer timer.Stop()

		select {
		case <-timer.C:
			// Delay complete.
		case <-ctx.Done():
			return &Result{
				Duration: time.Since(start),
				Success:  false,
				Error:    "cancelled during delay",
			}, nil
		}
	}

	// Handle deliberate failure.
	if shouldFail, ok := config["fail"].(bool); ok && shouldFail {
		msg := "deliberate failure for debugging"
		if customMsg, ok := config["fail_message"].(string); ok {
			msg = customMsg
		}
		return &Result{
			Duration: time.Since(start),
			Success:  false,
			Error:    msg,
		}, fmt.Errorf("%s", msg)
	}

	// Handle panic (for testing recovery).
	if shouldPanic, ok := config["panic"].(bool); ok && shouldPanic {
		panic("deliberate panic for debugging")
	}

	// Echo mode: return the config or echo value.
	output := config
	if echoVal, ok := config["echo"]; ok {
		output = map[string]interface{}{"echo": echoVal}
	}

	return &Result{
		Output:   output,
		Duration: time.Since(start),
		Success:  true,
		Metadata: map[string]interface{}{
			"task_type": taskType,
			"plugin":    "debug",
		},
	}, nil
}
