// Package circuit implements the circuit breaker pattern for protecting
// downstream services from cascading failures.
package circuit

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// State represents the current state of a circuit breaker.
type State int

const (
	StateClosed   State = iota // Normal operation — requests pass through.
	StateOpen                  // Tripped — requests are rejected immediately.
	StateHalfOpen              // Probing — a limited number of requests pass through.
)

// String returns a human-readable label for the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

// Sentinel errors.
var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

// Options configures a CircuitBreaker.
type Options struct {
	// FailureThreshold is the number of consecutive failures required to trip
	// the breaker from closed to open. Default: 5.
	FailureThreshold int
	// ResetTimeout is the duration the breaker stays open before transitioning
	// to half-open. Default: 30s.
	ResetTimeout time.Duration
	// HalfOpenMaxRequests is the number of requests allowed through in the
	// half-open state to probe whether the downstream has recovered. Default: 1.
	HalfOpenMaxRequests int
}

// DefaultOptions returns sensible defaults for a CircuitBreaker.
func DefaultOptions() Options {
	return Options{
		FailureThreshold:    5,
		ResetTimeout:        30 * time.Second,
		HalfOpenMaxRequests: 1,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	mu   sync.Mutex
	opts Options

	state            State
	consecutiveFails int
	lastFailureTime  time.Time
	halfOpenCount    int

	// nowFunc is overridable for testing.
	nowFunc func() time.Time
}

// New creates a CircuitBreaker with the given options.
func New(opts Options) *CircuitBreaker {
	if opts.FailureThreshold <= 0 {
		opts.FailureThreshold = 5
	}
	if opts.ResetTimeout <= 0 {
		opts.ResetTimeout = 30 * time.Second
	}
	if opts.HalfOpenMaxRequests <= 0 {
		opts.HalfOpenMaxRequests = 1
	}
	return &CircuitBreaker{
		opts:    opts,
		state:   StateClosed,
		nowFunc: time.Now,
	}
}

// NewWithClock creates a CircuitBreaker with a custom clock function (for testing).
func NewWithClock(opts Options, nowFunc func() time.Time) *CircuitBreaker {
	cb := New(opts)
	cb.nowFunc = nowFunc
	return cb
}

// Execute runs fn through the circuit breaker. If the breaker is open, fn is
// not called and ErrCircuitOpen is returned.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	err := fn()
	cb.afterRequest(err)
	return err
}

// State returns the current breaker state.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if the open state has timed out and should transition to half-open.
	if cb.state == StateOpen {
		if cb.nowFunc().Sub(cb.lastFailureTime) >= cb.opts.ResetTimeout {
			cb.state = StateHalfOpen
			cb.halfOpenCount = 0
		}
	}
	return cb.state
}

// Reset forces the breaker back to the closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.consecutiveFails = 0
	cb.halfOpenCount = 0
}

// ConsecutiveFailures returns the current consecutive failure count.
func (cb *CircuitBreaker) ConsecutiveFailures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.consecutiveFails
}

// beforeRequest checks whether the request should be allowed through.
func (cb *CircuitBreaker) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := cb.nowFunc()

	switch cb.state {
	case StateClosed:
		return nil
	case StateOpen:
		// Check if the reset timeout has elapsed.
		if now.Sub(cb.lastFailureTime) >= cb.opts.ResetTimeout {
			cb.state = StateHalfOpen
			cb.halfOpenCount = 0
			return nil
		}
		return ErrCircuitOpen
	case StateHalfOpen:
		if cb.halfOpenCount >= cb.opts.HalfOpenMaxRequests {
			return ErrCircuitOpen
		}
		cb.halfOpenCount++
		return nil
	}
	return nil
}

// afterRequest records the result of the request.
func (cb *CircuitBreaker) afterRequest(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.consecutiveFails++
		cb.lastFailureTime = cb.nowFunc()

		switch cb.state {
		case StateClosed:
			if cb.consecutiveFails >= cb.opts.FailureThreshold {
				cb.state = StateOpen
			}
		case StateHalfOpen:
			// Probe failed — reopen.
			cb.state = StateOpen
		}
	} else {
		switch cb.state {
		case StateHalfOpen:
			// Probe succeeded — close the breaker.
			cb.state = StateClosed
			cb.consecutiveFails = 0
			cb.halfOpenCount = 0
		case StateClosed:
			cb.consecutiveFails = 0
		}
	}
}
