package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/engine/retry"
)

func TestNewDefaultExecutor(t *testing.T) {
	e := NewDefaultExecutor()
	if e == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestRegisterAndExecute(t *testing.T) {
	e := NewDefaultExecutor()
	err := e.Register("http", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return "response", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := e.Execute(context.Background(), Task{
		ID:   "t-1",
		Type: "http",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "response" {
		t.Errorf("expected 'response', got %v", result.Output)
	}
	if result.Attempt != 1 {
		t.Errorf("expected attempt 1, got %d", result.Attempt)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("http", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return nil, nil
	})
	err := e.Register("http", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected ErrHandlerExists")
	}
	if !errors.Is(err, ErrHandlerExists) {
		t.Errorf("expected ErrHandlerExists, got %v", err)
	}
}

func TestRegisterEmptyType(t *testing.T) {
	e := NewDefaultExecutor()
	err := e.Register("", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for empty type")
	}
}

func TestRegisterNilHandler(t *testing.T) {
	e := NewDefaultExecutor()
	err := e.Register("http", nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestExecuteHandlerNotFound(t *testing.T) {
	e := NewDefaultExecutor()
	_, err := e.Execute(context.Background(), Task{ID: "t-1", Type: "missing"})
	if err == nil {
		t.Fatal("expected ErrHandlerNotFound")
	}
	if !errors.Is(err, ErrHandlerNotFound) {
		t.Errorf("expected ErrHandlerNotFound, got %v", err)
	}
}

func TestExecuteHandlerError(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("fail", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return nil, errors.New("handler failed")
	})

	result, err := e.Execute(context.Background(), Task{ID: "t-1", Type: "fail"})
	if err == nil {
		t.Fatal("expected error")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on error")
	}
	if result.Error == nil {
		t.Error("result.Error should be set")
	}
}

func TestExecuteTimeout(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("slow", func(ctx context.Context, config map[string]any) (interface{}, error) {
		select {
		case <-time.After(5 * time.Second):
			return "done", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	result, err := e.Execute(context.Background(), Task{
		ID:      "t-1",
		Type:    "slow",
		Timeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, ErrTaskTimeout) {
		t.Errorf("expected ErrTaskTimeout, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExecutePanicRecovery(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("panic", func(ctx context.Context, config map[string]any) (interface{}, error) {
		panic("something terrible")
	})

	result, err := e.Execute(context.Background(), Task{ID: "t-1", Type: "panic"})
	if err == nil {
		t.Fatal("expected error from panic")
	}
	if !errors.Is(err, ErrTaskPanicked) {
		t.Errorf("expected ErrTaskPanicked, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExecuteContextCancellation(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("wait", func(ctx context.Context, config map[string]any) (interface{}, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := e.Execute(ctx, Task{ID: "t-1", Type: "wait"})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestReplaceHandler(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("http", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return "v1", nil
	})

	e.ReplaceHandler("http", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return "v2", nil
	})

	result, err := e.Execute(context.Background(), Task{ID: "t-1", Type: "http"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "v2" {
		t.Errorf("expected v2, got %v", result.Output)
	}
}

func TestExecuteWithRetrySuccess(t *testing.T) {
	e := NewDefaultExecutor()
	attempts := 0
	_ = e.Register("flaky", func(ctx context.Context, config map[string]any) (interface{}, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("temporary")
		}
		return "success", nil
	})

	policy := &retry.RetryPolicy{
		MaxRetries:   5,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   1.0,
		Strategy:     retry.StrategyConstant,
	}

	result, err := e.ExecuteWithRetry(context.Background(), Task{ID: "t-1", Type: "flaky"}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "success" {
		t.Errorf("expected 'success', got %v", result.Output)
	}
	if result.Attempt != 3 {
		t.Errorf("expected attempt 3, got %d", result.Attempt)
	}
}

func TestExecuteWithRetryExhausted(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("always-fail", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return nil, errors.New("fail")
	})

	policy := &retry.RetryPolicy{
		MaxRetries:   2,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   1.0,
		Strategy:     retry.StrategyConstant,
	}

	_, err := e.ExecuteWithRetry(context.Background(), Task{ID: "t-1", Type: "always-fail"}, policy)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
}

func TestExecuteWithRetryNoPolicy(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("simple", func(ctx context.Context, config map[string]any) (interface{}, error) {
		return "ok", nil
	})

	result, err := e.ExecuteWithRetry(context.Background(), Task{ID: "t-1", Type: "simple"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "ok" {
		t.Errorf("expected 'ok', got %v", result.Output)
	}
}

func TestExecuteConfigPassed(t *testing.T) {
	e := NewDefaultExecutor()
	_ = e.Register("check-config", func(ctx context.Context, config map[string]any) (interface{}, error) {
		url, ok := config["url"]
		if !ok {
			return nil, errors.New("url not found")
		}
		return url, nil
	})

	result, err := e.Execute(context.Background(), Task{
		ID:     "t-1",
		Type:   "check-config",
		Config: map[string]any{"url": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "https://example.com" {
		t.Errorf("expected URL in output, got %v", result.Output)
	}
}
