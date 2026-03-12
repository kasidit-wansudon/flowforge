package retry

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"
)

func TestExponentialBackoffShouldRetry(t *testing.T) {
	eb := NewExponentialBackoff(3, time.Second, 30*time.Second, 2.0)
	if !eb.ShouldRetry(0, errors.New("err")) {
		t.Error("should retry on attempt 0")
	}
	if !eb.ShouldRetry(2, errors.New("err")) {
		t.Error("should retry on attempt 2")
	}
	if eb.ShouldRetry(3, errors.New("err")) {
		t.Error("should not retry on attempt 3 (max reached)")
	}
}

func TestExponentialBackoffDelay(t *testing.T) {
	eb := NewExponentialBackoff(5, 100*time.Millisecond, 10*time.Second, 2.0)
	d0 := eb.NextDelay(0)
	d1 := eb.NextDelay(1)
	d2 := eb.NextDelay(2)

	// Without jitter: 100ms, 200ms, 400ms
	if d0 != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", d0)
	}
	if d1 != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", d1)
	}
	if d2 != 400*time.Millisecond {
		t.Errorf("expected 400ms, got %v", d2)
	}
}

func TestExponentialBackoffMaxDelay(t *testing.T) {
	eb := NewExponentialBackoff(10, time.Second, 5*time.Second, 2.0)
	// attempt 5: 1*2^5 = 32s, should be capped at 5s
	d := eb.NextDelay(5)
	if d != 5*time.Second {
		t.Errorf("expected max delay 5s, got %v", d)
	}
}

func TestExponentialBackoffWithJitter(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	eb := NewExponentialBackoff(3, time.Second, 30*time.Second, 2.0, WithJitter(), WithRand(rng))
	d := eb.NextDelay(0)
	// With jitter, delay should be > 0 and <= initial delay
	if d <= 0 || d > time.Second {
		t.Errorf("jittered delay out of expected range: %v", d)
	}
}

func TestLinearBackoffShouldRetry(t *testing.T) {
	lb := NewLinearBackoff(3, 100*time.Millisecond, 1*time.Second, 100*time.Millisecond)
	if !lb.ShouldRetry(0, errors.New("err")) {
		t.Error("should retry attempt 0")
	}
	if lb.ShouldRetry(3, errors.New("err")) {
		t.Error("should not retry attempt 3")
	}
}

func TestLinearBackoffDelay(t *testing.T) {
	lb := NewLinearBackoff(5, 100*time.Millisecond, 1*time.Second, 100*time.Millisecond)
	d0 := lb.NextDelay(0)
	d1 := lb.NextDelay(1)
	d2 := lb.NextDelay(2)

	// 100ms + 0*100ms = 100ms, 100ms + 1*100ms = 200ms, 100ms + 2*100ms = 300ms
	if d0 != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", d0)
	}
	if d1 != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", d1)
	}
	if d2 != 300*time.Millisecond {
		t.Errorf("expected 300ms, got %v", d2)
	}
}

func TestLinearBackoffMaxDelay(t *testing.T) {
	lb := NewLinearBackoff(10, 100*time.Millisecond, 500*time.Millisecond, 200*time.Millisecond)
	// attempt 5: 100ms + 5*200ms = 1100ms, capped at 500ms
	d := lb.NextDelay(5)
	if d != 500*time.Millisecond {
		t.Errorf("expected max delay 500ms, got %v", d)
	}
}

func TestConstantBackoff(t *testing.T) {
	cb := NewConstantBackoff(2, 500*time.Millisecond)
	if !cb.ShouldRetry(0, errors.New("err")) {
		t.Error("should retry attempt 0")
	}
	if !cb.ShouldRetry(1, errors.New("err")) {
		t.Error("should retry attempt 1")
	}
	if cb.ShouldRetry(2, errors.New("err")) {
		t.Error("should not retry attempt 2")
	}

	d := cb.NextDelay(0)
	if d != 500*time.Millisecond {
		t.Errorf("expected 500ms, got %v", d)
	}
	d2 := cb.NextDelay(5)
	if d2 != 500*time.Millisecond {
		t.Errorf("expected constant 500ms, got %v", d2)
	}
}

func TestCustomBackoff(t *testing.T) {
	cb := &CustomBackoff{
		ShouldRetryFn: func(attempt int, err error) bool {
			return attempt < 1
		},
		NextDelayFn: func(attempt int) time.Duration {
			return time.Duration(attempt+1) * 10 * time.Millisecond
		},
	}

	if !cb.ShouldRetry(0, errors.New("err")) {
		t.Error("should retry attempt 0")
	}
	if cb.ShouldRetry(1, errors.New("err")) {
		t.Error("should not retry attempt 1")
	}
	d := cb.NextDelay(0)
	if d != 10*time.Millisecond {
		t.Errorf("expected 10ms, got %v", d)
	}
}

func TestCustomBackoffNilFunctions(t *testing.T) {
	cb := &CustomBackoff{}
	if cb.ShouldRetry(0, errors.New("err")) {
		t.Error("nil ShouldRetryFn should return false")
	}
	if cb.NextDelay(0) != 0 {
		t.Error("nil NextDelayFn should return 0")
	}
}

func TestExecuteSuccess(t *testing.T) {
	policy := NewConstantBackoff(3, time.Millisecond)
	called := 0
	err := Execute(context.Background(), func(ctx context.Context) error {
		called++
		return nil
	}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}
}

func TestExecuteRetryThenSuccess(t *testing.T) {
	policy := NewConstantBackoff(3, time.Millisecond)
	called := 0
	err := Execute(context.Background(), func(ctx context.Context) error {
		called++
		if called < 3 {
			return errors.New("temporary")
		}
		return nil
	}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 3 {
		t.Errorf("expected 3 calls, got %d", called)
	}
}

func TestExecuteMaxRetriesExceeded(t *testing.T) {
	policy := NewConstantBackoff(2, time.Millisecond)
	err := Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("always fails")
	}, policy)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrMaxRetriesExceeded) {
		t.Errorf("expected ErrMaxRetriesExceeded, got %v", err)
	}
}

func TestExecuteContextCancelled(t *testing.T) {
	policy := NewConstantBackoff(100, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := Execute(ctx, func(ctx context.Context) error {
		return errors.New("fail")
	}, policy)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestExecuteWithResultSuccess(t *testing.T) {
	policy := NewConstantBackoff(3, time.Millisecond)
	result, err := ExecuteWithResult(context.Background(), func(ctx context.Context) (string, error) {
		return "ok", nil
	}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %s", result)
	}
}

func TestExecuteWithResultRetryThenSuccess(t *testing.T) {
	policy := NewConstantBackoff(3, time.Millisecond)
	called := 0
	result, err := ExecuteWithResult(context.Background(), func(ctx context.Context) (int, error) {
		called++
		if called < 2 {
			return 0, errors.New("temp")
		}
		return 42, nil
	}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestPolicyFromConfigExponential(t *testing.T) {
	rp := RetryPolicy{
		MaxRetries:   3,
		InitialDelay: time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Strategy:     StrategyExponential,
	}
	p := PolicyFromConfig(rp)
	if !p.ShouldRetry(0, errors.New("err")) {
		t.Error("should retry attempt 0")
	}
	if p.ShouldRetry(3, errors.New("err")) {
		t.Error("should not retry after max")
	}
}

func TestPolicyFromConfigLinear(t *testing.T) {
	rp := RetryPolicy{
		MaxRetries:   2,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Strategy:     StrategyLinear,
	}
	p := PolicyFromConfig(rp)
	d := p.NextDelay(0)
	if d != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", d)
	}
}

func TestPolicyFromConfigConstant(t *testing.T) {
	rp := RetryPolicy{
		MaxRetries:   2,
		InitialDelay: 50 * time.Millisecond,
		Strategy:     StrategyConstant,
	}
	p := PolicyFromConfig(rp)
	if p.NextDelay(0) != 50*time.Millisecond {
		t.Errorf("expected 50ms constant delay")
	}
	if p.NextDelay(3) != 50*time.Millisecond {
		t.Errorf("expected 50ms constant delay at attempt 3")
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	rp := DefaultRetryPolicy()
	if rp.MaxRetries != 3 {
		t.Errorf("expected 3, got %d", rp.MaxRetries)
	}
	if rp.Strategy != StrategyExponential {
		t.Errorf("expected exponential, got %s", rp.Strategy)
	}
	if !rp.Jitter {
		t.Error("expected jitter to be true")
	}
}

func TestRetryableErrorUnwrap(t *testing.T) {
	inner := errors.New("inner err")
	re := &RetryableError{Attempt: 2, Err: inner}
	if !errors.Is(re, inner) {
		t.Error("should unwrap to inner error")
	}
	if re.Error() != "retry attempt 2: inner err" {
		t.Errorf("unexpected error string: %s", re.Error())
	}
}
