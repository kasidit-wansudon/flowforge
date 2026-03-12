// Package retry implements configurable retry policies with multiple backoff
// strategies and an Execute helper for automatic retrying of fallible
// functions.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Strategy enum
// ---------------------------------------------------------------------------

// Strategy identifies the backoff algorithm used by a retry policy.
type Strategy string

const (
	StrategyExponential Strategy = "exponential"
	StrategyLinear      Strategy = "linear"
	StrategyConstant    Strategy = "constant"
	StrategyCustom      Strategy = "custom"
)

// ---------------------------------------------------------------------------
// RetryPolicy configuration
// ---------------------------------------------------------------------------

// RetryPolicy is a plain data object that describes how retries should behave.
type RetryPolicy struct {
	MaxRetries   int           `json:"max_retries" yaml:"max_retries"`
	InitialDelay time.Duration `json:"initial_delay" yaml:"initial_delay"`
	MaxDelay     time.Duration `json:"max_delay" yaml:"max_delay"`
	Multiplier   float64       `json:"multiplier" yaml:"multiplier"`
	Strategy     Strategy      `json:"strategy" yaml:"strategy"`
	Jitter       bool          `json:"jitter" yaml:"jitter"`
}

// DefaultRetryPolicy returns a sensible default policy: up to 3 retries with
// exponential backoff starting at 1 s and capped at 30 s with jitter.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Strategy:     StrategyExponential,
		Jitter:       true,
	}
}

// ---------------------------------------------------------------------------
// Policy interface
// ---------------------------------------------------------------------------

// Policy determines whether a retry should happen and how long to wait.
type Policy interface {
	// ShouldRetry returns true if the operation should be retried given the
	// current attempt number (0-indexed) and the error that occurred.
	ShouldRetry(attempt int, err error) bool

	// NextDelay returns the duration to wait before the given attempt
	// (0-indexed).
	NextDelay(attempt int) time.Duration
}

// ---------------------------------------------------------------------------
// Option functional options
// ---------------------------------------------------------------------------

// Option configures a backoff policy.
type Option func(*backoffCfg)

type backoffCfg struct {
	jitter bool
	rng    *rand.Rand
}

// WithJitter enables randomised jitter on top of the computed delay. The
// jitter is uniformly distributed in [0, delay).
func WithJitter() Option {
	return func(cfg *backoffCfg) {
		cfg.jitter = true
	}
}

// WithRand sets a custom random source for jitter (useful for deterministic
// tests).
func WithRand(r *rand.Rand) Option {
	return func(cfg *backoffCfg) {
		cfg.rng = r
	}
}

func applyOpts(opts []Option) backoffCfg {
	cfg := backoffCfg{}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.rng == nil {
		cfg.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return cfg
}

// ---------------------------------------------------------------------------
// Exponential backoff
// ---------------------------------------------------------------------------

// ExponentialBackoff implements Policy with exponential backoff.
type ExponentialBackoff struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	cfg          backoffCfg
	mu           sync.Mutex // protects rng
}

// NewExponentialBackoff creates an ExponentialBackoff policy.
func NewExponentialBackoff(maxRetries int, initial, maxDelay time.Duration, multiplier float64, opts ...Option) *ExponentialBackoff {
	if multiplier <= 0 {
		multiplier = 2.0
	}
	return &ExponentialBackoff{
		MaxRetries:   maxRetries,
		InitialDelay: initial,
		MaxDelay:     maxDelay,
		Multiplier:   multiplier,
		cfg:          applyOpts(opts),
	}
}

func (e *ExponentialBackoff) ShouldRetry(attempt int, _ error) bool {
	return attempt < e.MaxRetries
}

func (e *ExponentialBackoff) NextDelay(attempt int) time.Duration {
	delay := float64(e.InitialDelay) * math.Pow(e.Multiplier, float64(attempt))
	if delay > float64(e.MaxDelay) {
		delay = float64(e.MaxDelay)
	}
	d := time.Duration(delay)
	if e.cfg.jitter && d > 0 {
		e.mu.Lock()
		jitter := time.Duration(e.cfg.rng.Int63n(int64(d)))
		e.mu.Unlock()
		d = d/2 + jitter/2 // Centre the jitter around the computed delay.
	}
	return d
}

// ---------------------------------------------------------------------------
// Linear backoff
// ---------------------------------------------------------------------------

// LinearBackoff implements Policy with linearly increasing delays.
type LinearBackoff struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Increment    time.Duration
	cfg          backoffCfg
	mu           sync.Mutex
}

// NewLinearBackoff creates a LinearBackoff policy.
func NewLinearBackoff(maxRetries int, initial, maxDelay, increment time.Duration, opts ...Option) *LinearBackoff {
	return &LinearBackoff{
		MaxRetries:   maxRetries,
		InitialDelay: initial,
		MaxDelay:     maxDelay,
		Increment:    increment,
		cfg:          applyOpts(opts),
	}
}

func (l *LinearBackoff) ShouldRetry(attempt int, _ error) bool {
	return attempt < l.MaxRetries
}

func (l *LinearBackoff) NextDelay(attempt int) time.Duration {
	d := l.InitialDelay + time.Duration(attempt)*l.Increment
	if d > l.MaxDelay {
		d = l.MaxDelay
	}
	if l.cfg.jitter && d > 0 {
		l.mu.Lock()
		jitter := time.Duration(l.cfg.rng.Int63n(int64(d)))
		l.mu.Unlock()
		d = d/2 + jitter/2
	}
	return d
}

// ---------------------------------------------------------------------------
// Constant backoff
// ---------------------------------------------------------------------------

// ConstantBackoff implements Policy with a fixed delay between retries.
type ConstantBackoff struct {
	MaxRetries int
	Delay      time.Duration
	cfg        backoffCfg
	mu         sync.Mutex
}

// NewConstantBackoff creates a ConstantBackoff policy.
func NewConstantBackoff(maxRetries int, delay time.Duration, opts ...Option) *ConstantBackoff {
	return &ConstantBackoff{
		MaxRetries: maxRetries,
		Delay:      delay,
		cfg:        applyOpts(opts),
	}
}

func (c *ConstantBackoff) ShouldRetry(attempt int, _ error) bool {
	return attempt < c.MaxRetries
}

func (c *ConstantBackoff) NextDelay(_ int) time.Duration {
	d := c.Delay
	if c.cfg.jitter && d > 0 {
		c.mu.Lock()
		jitter := time.Duration(c.cfg.rng.Int63n(int64(d)))
		c.mu.Unlock()
		d = d/2 + jitter/2
	}
	return d
}

// ---------------------------------------------------------------------------
// CustomBackoff — wraps caller-supplied functions
// ---------------------------------------------------------------------------

// CustomBackoff delegates retry decisions to caller-supplied functions.
type CustomBackoff struct {
	ShouldRetryFn func(attempt int, err error) bool
	NextDelayFn   func(attempt int) time.Duration
}

func (c *CustomBackoff) ShouldRetry(attempt int, err error) bool {
	if c.ShouldRetryFn == nil {
		return false
	}
	return c.ShouldRetryFn(attempt, err)
}

func (c *CustomBackoff) NextDelay(attempt int) time.Duration {
	if c.NextDelayFn == nil {
		return 0
	}
	return c.NextDelayFn(attempt)
}

// ---------------------------------------------------------------------------
// PolicyFromConfig builds a Policy from a RetryPolicy config struct.
// ---------------------------------------------------------------------------

// PolicyFromConfig converts a declarative RetryPolicy into an executable
// Policy.
func PolicyFromConfig(rp RetryPolicy) Policy {
	var opts []Option
	if rp.Jitter {
		opts = append(opts, WithJitter())
	}

	switch rp.Strategy {
	case StrategyLinear:
		increment := rp.InitialDelay // linear step defaults to initial delay
		return NewLinearBackoff(rp.MaxRetries, rp.InitialDelay, rp.MaxDelay, increment, opts...)
	case StrategyConstant:
		return NewConstantBackoff(rp.MaxRetries, rp.InitialDelay, opts...)
	case StrategyExponential:
		return NewExponentialBackoff(rp.MaxRetries, rp.InitialDelay, rp.MaxDelay, rp.Multiplier, opts...)
	default:
		// Fall back to exponential.
		return NewExponentialBackoff(rp.MaxRetries, rp.InitialDelay, rp.MaxDelay, rp.Multiplier, opts...)
	}
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrMaxRetriesExceeded is returned by Execute when all retry attempts have
// been exhausted.
var ErrMaxRetriesExceeded = errors.New("retry: max retries exceeded")

// RetryableError wraps an underlying error with the attempt number.
type RetryableError struct {
	Attempt int
	Err     error
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("retry attempt %d: %v", e.Attempt, e.Err)
}

func (e *RetryableError) Unwrap() error { return e.Err }

// ---------------------------------------------------------------------------
// Execute helper
// ---------------------------------------------------------------------------

// Execute runs fn and retries it according to the given policy. It returns the
// first successful result or the last error wrapped in a RetryableError if all
// attempts fail. The context can be used to cancel retries early.
func Execute(ctx context.Context, fn func(ctx context.Context) error, policy Policy) error {
	var lastErr error

	for attempt := 0; ; attempt++ {
		// Check context before each attempt.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("retry: context cancelled after attempt %d: %w (last error: %v)", attempt, err, lastErr)
			}
			return fmt.Errorf("retry: context cancelled: %w", err)
		}

		if err := fn(ctx); err != nil {
			lastErr = err

			if !policy.ShouldRetry(attempt, err) {
				return &RetryableError{Attempt: attempt, Err: fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, err)}
			}

			delay := policy.NextDelay(attempt)
			if delay > 0 {
				select {
				case <-ctx.Done():
					return fmt.Errorf("retry: context cancelled during backoff: %w (last error: %v)", ctx.Err(), lastErr)
				case <-time.After(delay):
				}
			}
			continue
		}

		// Success.
		return nil
	}
}

// ExecuteWithResult is like Execute but returns a value on success.
func ExecuteWithResult[T any](ctx context.Context, fn func(ctx context.Context) (T, error), policy Policy) (T, error) {
	var result T
	var lastErr error

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return result, fmt.Errorf("retry: context cancelled after attempt %d: %w (last error: %v)", attempt, err, lastErr)
			}
			return result, fmt.Errorf("retry: context cancelled: %w", err)
		}

		val, err := fn(ctx)
		if err != nil {
			lastErr = err

			if !policy.ShouldRetry(attempt, err) {
				return result, &RetryableError{Attempt: attempt, Err: fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, err)}
			}

			delay := policy.NextDelay(attempt)
			if delay > 0 {
				select {
				case <-ctx.Done():
					return result, fmt.Errorf("retry: context cancelled during backoff: %w (last error: %v)", ctx.Err(), lastErr)
				case <-time.After(delay):
				}
			}
			continue
		}

		return val, nil
	}
}
