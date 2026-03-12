package script

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewScriptTaskHandler(t *testing.T) {
	h := NewScriptTaskHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.MaxOutputSize <= 0 {
		t.Error("expected MaxOutputSize > 0")
	}
}

func TestExecuteBashEcho(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   `echo "hello world"`,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got error: %s", result.Error)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello world") {
		t.Errorf("expected stdout to contain 'hello world', got %q", result.Stdout)
	}
}

func TestExecuteBashStderr(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   `echo "error message" >&2`,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Script itself exited with 0 even though something was written to stderr.
	if !result.Success {
		t.Errorf("expected success=true, got error: %s", result.Error)
	}
	if !strings.Contains(result.Stderr, "error message") {
		t.Errorf("expected stderr to contain 'error message', got %q", result.Stderr)
	}
}

func TestExecuteBashNonZeroExitCode(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   `exit 42`,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected success=false for non-zero exit code")
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
	if result.Error == "" {
		t.Error("expected non-empty error message for non-zero exit")
	}
}

func TestExecuteBashEnvVariables(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   `echo "$MY_VAR"`,
		Env: map[string]string{
			"MY_VAR": "injected-value",
		},
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Stdout, "injected-value") {
		t.Errorf("expected stdout to contain 'injected-value', got %q", result.Stdout)
	}
}

func TestExecuteBashTimeout(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   `sleep 30`,
		Timeout:  50 * time.Millisecond,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected success=false for timed-out script")
	}
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for timed-out script")
	}
}

func TestExecuteMissingScript(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   "",
	}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for missing script")
	}
	if !strings.Contains(err.Error(), "script") {
		t.Errorf("expected error about script, got: %v", err)
	}
}

func TestExecuteMissingLanguage(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "",
		Script:   `echo hi`,
	}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for missing language")
	}
}

func TestExecuteUnsupportedLanguage(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "ruby",
		Script:   `puts "hello"`,
	}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
}

func TestExecuteAllowedLanguages(t *testing.T) {
	h := NewScriptTaskHandler()
	h.AllowedLanguages = []string{"bash"}

	t.Run("allowed language succeeds", func(t *testing.T) {
		cfg := ScriptTaskConfig{
			Language: "bash",
			Script:   `echo ok`,
		}
		result, err := h.Execute(context.Background(), cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Errorf("expected success for allowed language, got: %s", result.Error)
		}
	})

	t.Run("disallowed language fails validation", func(t *testing.T) {
		cfg := ScriptTaskConfig{
			Language: "sh",
			Script:   `echo ok`,
		}
		_, err := h.Execute(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected error for disallowed language")
		}
		if !strings.Contains(err.Error(), "allowed") {
			t.Errorf("expected error about allowed list, got: %v", err)
		}
	})
}

func TestExecuteContextCancellation(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   `sleep 30`,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	result, err := h.Execute(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected success=false for cancelled script")
	}
}

func TestExecuteDuration(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script:   `echo done`,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Duration <= 0 {
		t.Error("expected Duration > 0")
	}
}

func TestExecuteOutputCaptured(t *testing.T) {
	h := NewScriptTaskHandler()
	cfg := ScriptTaskConfig{
		Language: "bash",
		Script: `
echo "line1"
echo "line2"
echo "err" >&2
`,
	}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stdout, "line1") {
		t.Errorf("expected stdout to contain 'line1', got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "line2") {
		t.Errorf("expected stdout to contain 'line2', got %q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "err") {
		t.Errorf("expected stderr to contain 'err', got %q", result.Stderr)
	}
}

func TestParseConfig(t *testing.T) {
	raw := map[string]any{
		"language": "bash",
		"script":   `echo hello`,
	}
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Language != "bash" {
		t.Errorf("expected language 'bash', got %q", cfg.Language)
	}
	if cfg.Script != `echo hello` {
		t.Errorf("expected script 'echo hello', got %q", cfg.Script)
	}
}
