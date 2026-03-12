package delay

import (
	"context"
	"testing"
	"time"
)

func TestNewDelayHandler(t *testing.T) {
	h := NewDelayHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestExecuteShortDuration(t *testing.T) {
	h := NewDelayHandler()
	cfg := DelayConfig{Duration: 20 * time.Millisecond}

	start := time.Now()
	result, err := h.Execute(context.Background(), cfg)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Completed {
		t.Error("expected Completed=true")
	}
	if result.Cancelled {
		t.Error("expected Cancelled=false")
	}
	if result.ActualDelay < 20*time.Millisecond {
		t.Errorf("expected actual delay >= 20ms, got %s", result.ActualDelay)
	}
	// Sanity: elapsed should be close to requested (allow 200ms slack for slow CI).
	if elapsed > 200*time.Millisecond {
		t.Errorf("delay took unexpectedly long: %s", elapsed)
	}
}

func TestExecuteContextCancelDuringDelay(t *testing.T) {
	h := NewDelayHandler()
	cfg := DelayConfig{Duration: 10 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context after a short pause.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result, err := h.Execute(ctx, cfg)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Completed {
		t.Error("expected Completed=false when cancelled")
	}
	if !result.Cancelled {
		t.Error("expected Cancelled=true")
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason for cancelled delay")
	}
	// Should have been cancelled quickly.
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected early cancellation, elapsed: %s", elapsed)
	}
}

func TestExecuteContextDeadlineDuringDelay(t *testing.T) {
	h := NewDelayHandler()
	cfg := DelayConfig{Duration: 10 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	result, err := h.Execute(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Completed {
		t.Error("expected Completed=false when deadline exceeded")
	}
	if !result.Cancelled {
		t.Error("expected Cancelled=true when deadline exceeded")
	}
}

func TestExecuteAbsoluteTimePassed(t *testing.T) {
	h := NewDelayHandler()
	// Use a timestamp in the past.
	past := time.Now().Add(-1 * time.Hour)
	cfg := DelayConfig{Until: &past}

	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Completed {
		t.Error("expected Completed=true when target time already passed")
	}
	if result.Cancelled {
		t.Error("expected Cancelled=false when time already passed")
	}
	if result.Reason == "" {
		t.Error("expected Reason to explain why delay was skipped")
	}
}

func TestExecuteAbsoluteTimeFuture(t *testing.T) {
	h := NewDelayHandler()
	// Use a timestamp 30ms in the future.
	future := time.Now().Add(30 * time.Millisecond)
	cfg := DelayConfig{Until: &future}

	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Completed {
		t.Error("expected Completed=true after waiting for future time")
	}
	if result.Cancelled {
		t.Error("expected Cancelled=false")
	}
}

func TestExecuteMissingDurationAndUntil(t *testing.T) {
	h := NewDelayHandler()
	cfg := DelayConfig{}
	_, err := h.Execute(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when neither Duration nor Until is set")
	}
}

func TestExecuteResultFields(t *testing.T) {
	h := NewDelayHandler()
	cfg := DelayConfig{Duration: 10 * time.Millisecond}
	result, err := h.Execute(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequestedDelay != 10*time.Millisecond {
		t.Errorf("expected RequestedDelay=10ms, got %s", result.RequestedDelay)
	}
	if result.ActualDelay <= 0 {
		t.Error("expected ActualDelay > 0")
	}
}

func TestParseConfig(t *testing.T) {
	t.Run("duration string", func(t *testing.T) {
		raw := map[string]any{
			"duration": "5m",
		}
		cfg, err := ParseConfig(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Duration != 5*time.Minute {
			t.Errorf("expected 5m duration, got %s", cfg.Duration)
		}
	})

	t.Run("until timestamp", func(t *testing.T) {
		raw := map[string]any{
			"until": "2030-01-01T00:00:00Z",
		}
		cfg, err := ParseConfig(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Until == nil {
			t.Fatal("expected Until to be parsed")
		}
		if cfg.Until.Year() != 2030 {
			t.Errorf("expected year 2030, got %d", cfg.Until.Year())
		}
	})

	t.Run("invalid duration", func(t *testing.T) {
		raw := map[string]any{
			"duration": "not-a-duration",
		}
		_, err := ParseConfig(raw)
		if err == nil {
			t.Fatal("expected error for invalid duration")
		}
	})
}
