package circuit

import (
	"errors"
	"testing"
	"time"
)

func TestNewCircuitBreaker(t *testing.T) {
	cb := New(DefaultOptions())
	if cb.State() != StateClosed {
		t.Errorf("initial state = %v, want StateClosed", cb.State())
	}
	if cb.ConsecutiveFailures() != 0 {
		t.Errorf("initial ConsecutiveFailures() = %d, want 0", cb.ConsecutiveFailures())
	}
}

func TestNewCircuitBreakerDefaultsApplied(t *testing.T) {
	cb := New(Options{})
	// Defaults should be applied for zero-value options.
	if cb.opts.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", cb.opts.FailureThreshold)
	}
	if cb.opts.ResetTimeout != 30*time.Second {
		t.Errorf("ResetTimeout = %v, want 30s", cb.opts.ResetTimeout)
	}
	if cb.opts.HalfOpenMaxRequests != 1 {
		t.Errorf("HalfOpenMaxRequests = %d, want 1", cb.opts.HalfOpenMaxRequests)
	}
}

func TestExecuteSuccessKeepsClosed(t *testing.T) {
	cb := New(DefaultOptions())

	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("state = %v, want StateClosed", cb.State())
	}
	if cb.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want 0", cb.ConsecutiveFailures())
	}
}

func TestExecuteFailureIncrementsCount(t *testing.T) {
	cb := New(Options{FailureThreshold: 5, ResetTimeout: 30 * time.Second})
	testErr := errors.New("test error")

	err := cb.Execute(func() error { return testErr })
	if err != testErr {
		t.Fatalf("Execute() returned %v, want %v", err, testErr)
	}
	if cb.ConsecutiveFailures() != 1 {
		t.Errorf("ConsecutiveFailures() = %d, want 1", cb.ConsecutiveFailures())
	}
	if cb.State() != StateClosed {
		t.Errorf("state = %v, want StateClosed (not yet at threshold)", cb.State())
	}
}

func TestTransitionClosedToOpen(t *testing.T) {
	threshold := 3
	cb := New(Options{
		FailureThreshold:    threshold,
		ResetTimeout:        30 * time.Second,
		HalfOpenMaxRequests: 1,
	})
	testErr := errors.New("fail")

	for i := 0; i < threshold; i++ {
		cb.Execute(func() error { return testErr })
	}

	if cb.State() != StateOpen {
		t.Errorf("state = %v, want StateOpen after %d failures", cb.State(), threshold)
	}
	if cb.ConsecutiveFailures() != threshold {
		t.Errorf("ConsecutiveFailures() = %d, want %d", cb.ConsecutiveFailures(), threshold)
	}
}

func TestOpenRejectsRequests(t *testing.T) {
	threshold := 2
	cb := New(Options{
		FailureThreshold:    threshold,
		ResetTimeout:        1 * time.Hour, // long timeout so it stays open
		HalfOpenMaxRequests: 1,
	})
	testErr := errors.New("fail")

	for i := 0; i < threshold; i++ {
		cb.Execute(func() error { return testErr })
	}

	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Execute() err = %v, want ErrCircuitOpen", err)
	}
	if called {
		t.Error("function was called despite circuit being open")
	}
}

func TestTransitionOpenToHalfOpen(t *testing.T) {
	now := time.Now()
	currentTime := now

	cb := NewWithClock(Options{
		FailureThreshold:    2,
		ResetTimeout:        1 * time.Second,
		HalfOpenMaxRequests: 1,
	}, func() time.Time { return currentTime })

	testErr := errors.New("fail")

	// Trip the breaker.
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })

	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want StateOpen", cb.State())
	}

	// Advance time past the reset timeout.
	currentTime = now.Add(2 * time.Second)

	if cb.State() != StateHalfOpen {
		t.Errorf("state = %v, want StateHalfOpen after reset timeout", cb.State())
	}
}

func TestHalfOpenSuccessCloses(t *testing.T) {
	now := time.Now()
	currentTime := now

	cb := NewWithClock(Options{
		FailureThreshold:    2,
		ResetTimeout:        1 * time.Second,
		HalfOpenMaxRequests: 1,
	}, func() time.Time { return currentTime })

	testErr := errors.New("fail")

	// Trip the breaker.
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })

	// Advance to half-open.
	currentTime = now.Add(2 * time.Second)

	// Probe success.
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("Execute() returned error in half-open: %v", err)
	}

	if cb.State() != StateClosed {
		t.Errorf("state = %v, want StateClosed after successful probe", cb.State())
	}
	if cb.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want 0 after successful probe", cb.ConsecutiveFailures())
	}
}

func TestHalfOpenFailureReopens(t *testing.T) {
	now := time.Now()
	currentTime := now

	cb := NewWithClock(Options{
		FailureThreshold:    2,
		ResetTimeout:        1 * time.Second,
		HalfOpenMaxRequests: 1,
	}, func() time.Time { return currentTime })

	testErr := errors.New("fail")

	// Trip the breaker.
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })

	// Advance to half-open.
	currentTime = now.Add(2 * time.Second)

	// Probe failure.
	cb.Execute(func() error { return testErr })

	if cb.State() != StateOpen {
		t.Errorf("state = %v, want StateOpen after failed probe", cb.State())
	}
}

func TestHalfOpenExceedsMaxRequests(t *testing.T) {
	now := time.Now()
	currentTime := now

	cb := NewWithClock(Options{
		FailureThreshold:    2,
		ResetTimeout:        1 * time.Second,
		HalfOpenMaxRequests: 1,
	}, func() time.Time { return currentTime })

	testErr := errors.New("fail")

	// Trip the breaker.
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })

	// Advance to half-open.
	currentTime = now.Add(2 * time.Second)

	// First request goes through (probe).
	cb.beforeRequest()

	// Second request should be rejected (half-open max requests = 1).
	err := cb.Execute(func() error { return nil })
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("Execute() err = %v, want ErrCircuitOpen (max half-open requests exceeded)", err)
	}
}

func TestReset(t *testing.T) {
	cb := New(Options{
		FailureThreshold:    2,
		ResetTimeout:        1 * time.Hour,
		HalfOpenMaxRequests: 1,
	})
	testErr := errors.New("fail")

	// Trip the breaker.
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })

	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want StateOpen", cb.State())
	}

	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("state = %v after Reset(), want StateClosed", cb.State())
	}
	if cb.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d after Reset(), want 0", cb.ConsecutiveFailures())
	}

	// Should accept requests again.
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("Execute() after Reset() returned error: %v", err)
	}
}

func TestSuccessResetsConsecutiveFailures(t *testing.T) {
	cb := New(Options{
		FailureThreshold:    5,
		ResetTimeout:        30 * time.Second,
		HalfOpenMaxRequests: 1,
	})
	testErr := errors.New("fail")

	// Accumulate some failures (not enough to trip).
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })

	if cb.ConsecutiveFailures() != 2 {
		t.Fatalf("ConsecutiveFailures() = %d, want 2", cb.ConsecutiveFailures())
	}

	// A success resets the counter.
	cb.Execute(func() error { return nil })

	if cb.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d after success, want 0", cb.ConsecutiveFailures())
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "state(99)"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestFullLifecycle(t *testing.T) {
	now := time.Now()
	currentTime := now

	cb := NewWithClock(Options{
		FailureThreshold:    3,
		ResetTimeout:        1 * time.Second,
		HalfOpenMaxRequests: 1,
	}, func() time.Time { return currentTime })

	testErr := errors.New("downstream error")

	// Phase 1: Closed, accumulate failures.
	if cb.State() != StateClosed {
		t.Fatalf("phase 1: state = %v, want StateClosed", cb.State())
	}

	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })
	cb.Execute(func() error { return testErr })

	// Phase 2: Should be Open now.
	if cb.State() != StateOpen {
		t.Fatalf("phase 2: state = %v, want StateOpen", cb.State())
	}

	// Phase 3: Wait for reset timeout, should be HalfOpen.
	currentTime = now.Add(2 * time.Second)
	if cb.State() != StateHalfOpen {
		t.Fatalf("phase 3: state = %v, want StateHalfOpen", cb.State())
	}

	// Phase 4: Successful probe, should close.
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("phase 4: Execute() = %v, want nil", err)
	}
	if cb.State() != StateClosed {
		t.Fatalf("phase 4: state = %v, want StateClosed", cb.State())
	}
}
