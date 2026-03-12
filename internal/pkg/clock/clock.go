// Package clock provides a testable clock interface that abstracts time operations.
// Use RealClock for production code and MockClock for deterministic testing.
package clock

import (
	"sync"
	"time"
)

// Clock abstracts time operations so that code depending on wall-clock time
// can be tested deterministically.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// After waits for the duration to elapse and then sends the current time
	// on the returned channel.
	After(d time.Duration) <-chan time.Time
	// Sleep pauses the current goroutine for at least the duration d.
	Sleep(d time.Duration)
}

// ---------------------------------------------------------------------------
// RealClock — delegates to the standard time package
// ---------------------------------------------------------------------------

// RealClock is a Clock backed by the standard library's time package.
type RealClock struct{}

// NewRealClock returns a RealClock instance.
func NewRealClock() *RealClock {
	return &RealClock{}
}

// Now returns the current wall-clock time.
func (c *RealClock) Now() time.Time {
	return time.Now()
}

// After returns a channel that receives the time after duration d elapses.
func (c *RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// Sleep pauses the current goroutine for duration d.
func (c *RealClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// ---------------------------------------------------------------------------
// MockClock — fully controllable clock for tests
// ---------------------------------------------------------------------------

// sleeper represents a goroutine blocked in Sleep or waiting on After.
type sleeper struct {
	deadline time.Time
	ch       chan time.Time
}

// MockClock is a Clock whose time only advances when explicitly told to.
// It is safe for concurrent use.
type MockClock struct {
	mu       sync.Mutex
	now      time.Time
	sleepers []sleeper
	// waitCh is used by AwaitSleepers to block until a new sleeper is
	// registered.  A new channel is created each time the caller drains it.
	waitCh chan struct{}
}

// NewMockClock creates a MockClock pinned to the given starting time.
func NewMockClock(start time.Time) *MockClock {
	return &MockClock{
		now:    start,
		waitCh: make(chan struct{}, 1),
	}
}

// Now returns the mock's current time.
func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// After returns a channel that will receive the mock's current time once the
// clock has been advanced past the deadline (now + d).
func (m *MockClock) After(d time.Duration) <-chan time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan time.Time, 1)
	deadline := m.now.Add(d)

	// If the duration is zero or negative, fire immediately.
	if d <= 0 {
		ch <- m.now
		return ch
	}

	m.sleepers = append(m.sleepers, sleeper{deadline: deadline, ch: ch})
	m.notifyWaiters()
	return ch
}

// Sleep blocks until the mock clock has been advanced past now + d.
func (m *MockClock) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	<-m.After(d)
}

// Advance moves the mock clock forward by d and wakes any sleepers whose
// deadline has been reached. It returns the number of sleepers that were
// woken.
func (m *MockClock) Advance(d time.Duration) int {
	m.mu.Lock()
	m.now = m.now.Add(d)
	now := m.now

	var remaining []sleeper
	var woken []sleeper

	for _, s := range m.sleepers {
		if !s.deadline.After(now) {
			woken = append(woken, s)
		} else {
			remaining = append(remaining, s)
		}
	}
	m.sleepers = remaining
	m.mu.Unlock()

	for _, s := range woken {
		s.ch <- now
	}
	return len(woken)
}

// Set moves the mock clock to an exact point in time and wakes expired
// sleepers. It returns the number of sleepers that were woken.
func (m *MockClock) Set(t time.Time) int {
	m.mu.Lock()
	m.now = t

	var remaining []sleeper
	var woken []sleeper

	for _, s := range m.sleepers {
		if !s.deadline.After(t) {
			woken = append(woken, s)
		} else {
			remaining = append(remaining, s)
		}
	}
	m.sleepers = remaining
	m.mu.Unlock()

	for _, s := range woken {
		s.ch <- t
	}
	return len(woken)
}

// PendingSleepers returns the number of goroutines currently blocked in
// Sleep or waiting on an After channel.
func (m *MockClock) PendingSleepers() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sleepers)
}

// AwaitSleepers blocks until at least n sleepers are registered. This is
// useful in tests to ensure a goroutine has called Sleep or After before
// advancing the clock.
func (m *MockClock) AwaitSleepers(n int) {
	for {
		m.mu.Lock()
		if len(m.sleepers) >= n {
			m.mu.Unlock()
			return
		}
		// Replace the wait channel so we can be notified of new sleepers.
		m.waitCh = make(chan struct{}, 1)
		ch := m.waitCh
		m.mu.Unlock()
		<-ch
	}
}

// notifyWaiters signals anyone blocked in AwaitSleepers. Must be called with
// m.mu held.
func (m *MockClock) notifyWaiters() {
	select {
	case m.waitCh <- struct{}{}:
	default:
	}
}
