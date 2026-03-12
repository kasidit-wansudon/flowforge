package plugin

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// --- Helpers ---

// mockPlugin is a simple Plugin implementation for testing.
type mockPlugin struct {
	name      string
	version   string
	taskTypes []string
	execFn    func(taskType string, ctx context.Context, config map[string]interface{}) (*Result, error)
}

func (m *mockPlugin) Name() string      { return m.name }
func (m *mockPlugin) Version() string   { return m.version }
func (m *mockPlugin) TaskTypes() []string { return m.taskTypes }
func (m *mockPlugin) Execute(taskType string, ctx context.Context, config map[string]interface{}) (*Result, error) {
	if m.execFn != nil {
		return m.execFn(taskType, ctx, config)
	}
	return &Result{Success: true, Output: "mock"}, nil
}

// --- Tests: PluginRegistry ---

func TestPluginRegistryRegisterAndGet(t *testing.T) {
	r := NewPluginRegistry()
	p := &mockPlugin{name: "myplugin", version: "1.0", taskTypes: []string{"my-task"}}

	if err := r.Register(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := r.Get("myplugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name() != "myplugin" {
		t.Errorf("expected plugin name 'myplugin', got %q", got.Name())
	}
}

func TestPluginRegistryGetNotFound(t *testing.T) {
	r := NewPluginRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing plugin")
	}
}

func TestPluginRegistryDuplicateRegistration(t *testing.T) {
	r := NewPluginRegistry()
	p := &mockPlugin{name: "dup", version: "1.0", taskTypes: []string{"dup-task"}}

	if err := r.Register(p); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	err := r.Register(p)
	if err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestPluginRegistryGetByTaskType(t *testing.T) {
	r := NewPluginRegistry()
	p := &mockPlugin{name: "worker", version: "2.0", taskTypes: []string{"send-email", "send-sms"}}

	if err := r.Register(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, taskType := range []string{"send-email", "send-sms"} {
		got, err := r.GetByTaskType(taskType)
		if err != nil {
			t.Fatalf("GetByTaskType(%q) failed: %v", taskType, err)
		}
		if got.Name() != "worker" {
			t.Errorf("expected plugin 'worker' for task type %q, got %q", taskType, got.Name())
		}
	}
}

func TestPluginRegistryGetByTaskTypeNotFound(t *testing.T) {
	r := NewPluginRegistry()
	_, err := r.GetByTaskType("unknown-task")
	if err == nil {
		t.Fatal("expected error for unknown task type")
	}
}

func TestPluginRegistryList(t *testing.T) {
	r := NewPluginRegistry()
	plugins := []*mockPlugin{
		{name: "alpha", version: "1.0", taskTypes: []string{"task-a"}},
		{name: "beta", version: "1.0", taskTypes: []string{"task-b"}},
	}
	for _, p := range plugins {
		if err := r.Register(p); err != nil {
			t.Fatalf("failed to register %q: %v", p.name, err)
		}
	}

	infos := r.List()
	if len(infos) != 2 {
		t.Errorf("expected 2 plugins listed, got %d", len(infos))
	}
	// List should be sorted by name.
	if infos[0].Name != "alpha" {
		t.Errorf("expected first plugin to be 'alpha', got %q", infos[0].Name)
	}
	if infos[1].Name != "beta" {
		t.Errorf("expected second plugin to be 'beta', got %q", infos[1].Name)
	}
}

func TestPluginRegistryExecute(t *testing.T) {
	r := NewPluginRegistry()
	p := &mockPlugin{
		name:      "exec-test",
		version:   "1.0",
		taskTypes: []string{"run"},
		execFn: func(taskType string, ctx context.Context, config map[string]interface{}) (*Result, error) {
			return &Result{Success: true, Output: "executed"}, nil
		},
	}
	if err := r.Register(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := r.Execute("run", context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.Output != "executed" {
		t.Errorf("expected output 'executed', got %v", result.Output)
	}
}

func TestPluginRegistryUnregister(t *testing.T) {
	r := NewPluginRegistry()
	p := &mockPlugin{name: "removable", version: "1.0", taskTypes: []string{"rm-task"}}

	if err := r.Register(p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Unregister("removable"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := r.Get("removable")
	if err == nil {
		t.Error("expected error after unregistering plugin")
	}

	// Task type should also be removed.
	_, err = r.GetByTaskType("rm-task")
	if err == nil {
		t.Error("expected error for task type after plugin unregistered")
	}
}

func TestPluginRegistryNilPlugin(t *testing.T) {
	r := NewPluginRegistry()
	err := r.Register(nil)
	if err == nil {
		t.Fatal("expected error when registering nil plugin")
	}
}

// --- Tests: LoggingPlugin ---

func TestLoggingPluginName(t *testing.T) {
	p := NewLoggingPlugin()
	if p.Name() != "logging" {
		t.Errorf("expected name 'logging', got %q", p.Name())
	}
}

func TestLoggingPluginTaskTypes(t *testing.T) {
	p := NewLoggingPlugin()
	types := p.TaskTypes()
	if len(types) == 0 {
		t.Fatal("expected at least one task type")
	}
}

func TestLoggingPluginExecute(t *testing.T) {
	p := NewLoggingPlugin()
	cfg := map[string]interface{}{
		"level":   "info",
		"message": "test log entry",
		"data": map[string]interface{}{
			"key": "value",
		},
	}

	result, err := p.Execute("log", context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got error: %s", result.Error)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
	if result.Duration <= 0 {
		t.Error("expected Duration > 0")
	}

	// Verify entry was stored.
	entries := p.Entries()
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(entries))
	}
	if entries[0].Message != "test log entry" {
		t.Errorf("expected message 'test log entry', got %q", entries[0].Message)
	}
	if entries[0].Level != "info" {
		t.Errorf("expected level 'info', got %q", entries[0].Level)
	}
}

func TestLoggingPluginMultipleEntries(t *testing.T) {
	p := NewLoggingPlugin()
	for i := 0; i < 3; i++ {
		cfg := map[string]interface{}{
			"message": fmt.Sprintf("message-%d", i),
		}
		if _, err := p.Execute("log", context.Background(), cfg); err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
	}
	entries := p.Entries()
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

// --- Tests: DebugPlugin ---

func TestDebugPluginName(t *testing.T) {
	p := NewDebugPlugin()
	if p.Name() != "debug" {
		t.Errorf("expected name 'debug', got %q", p.Name())
	}
}

func TestDebugPluginEcho(t *testing.T) {
	p := NewDebugPlugin()
	cfg := map[string]interface{}{
		"echo": "hello-echo",
	}
	result, err := p.Execute("echo", context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got error: %s", result.Error)
	}
	out, ok := result.Output.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map output, got %T", result.Output)
	}
	if out["echo"] != "hello-echo" {
		t.Errorf("expected echo output 'hello-echo', got %v", out["echo"])
	}
}

func TestDebugPluginNoop(t *testing.T) {
	p := NewDebugPlugin()
	result, err := p.Execute("noop", context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected noop to succeed")
	}
	if result.Output != nil {
		t.Errorf("expected nil output for noop, got %v", result.Output)
	}
}

func TestDebugPluginDeliberateFail(t *testing.T) {
	p := NewDebugPlugin()
	cfg := map[string]interface{}{
		"fail":         true,
		"fail_message": "intentional error",
	}
	result, err := p.Execute("debug", context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error from deliberate failure")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on failure")
	}
	if result.Success {
		t.Error("expected Success=false for deliberate failure")
	}
	if result.Error != "intentional error" {
		t.Errorf("expected error 'intentional error', got %q", result.Error)
	}
}

func TestDebugPluginDelay(t *testing.T) {
	p := NewDebugPlugin()
	cfg := map[string]interface{}{
		"delay": "30ms",
	}
	start := time.Now()
	result, err := p.Execute("debug", context.Background(), cfg)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true after delay, got error: %s", result.Error)
	}
	if elapsed < 30*time.Millisecond {
		t.Errorf("expected delay of at least 30ms, elapsed: %s", elapsed)
	}
}

// --- Tests: PluginLoader ---

func TestPluginLoaderBuiltIns(t *testing.T) {
	loader := NewPluginLoader()

	for _, name := range []string{"logging", "debug"} {
		p, err := loader.Load(name)
		if err != nil {
			t.Errorf("expected built-in plugin %q to load, got error: %v", name, err)
			continue
		}
		if p.Name() != name {
			t.Errorf("expected plugin name %q, got %q", name, p.Name())
		}
	}
}

func TestPluginLoaderAvailable(t *testing.T) {
	loader := NewPluginLoader()
	available := loader.Available()
	if len(available) < 2 {
		t.Errorf("expected at least 2 built-in plugins, got %d", len(available))
	}
}

func TestPluginLoaderRegisterCustom(t *testing.T) {
	loader := NewPluginLoader()
	factory := func() (Plugin, error) {
		return &mockPlugin{name: "custom", version: "1.0", taskTypes: []string{"custom-task"}}, nil
	}
	if err := loader.RegisterFactory("custom", factory); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p, err := loader.Load("custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "custom" {
		t.Errorf("expected 'custom' plugin, got %q", p.Name())
	}
}

func TestPluginLoaderUnknownPlugin(t *testing.T) {
	loader := NewPluginLoader()
	_, err := loader.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown plugin")
	}
}
