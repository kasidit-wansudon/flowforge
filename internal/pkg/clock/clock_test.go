package clock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/kasidit-wansudon/flowforge/internal/pkg/clock"
)

// --- RealClock ---

func TestRealClock_Now_ReturnsCurrentTime(t *testing.T) {
	c := clock.NewRealClock()
	before := time.Now()
	got := c.Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("RealClock.Now() = %v, want between %v and %v", got, before, after)
	}
}

func TestRealClock_After_ChannelReceivesAfterDuration(t *testing.T) {
	c := clock.NewRealClock()
	d := 50 * time.Millisecond
	start := time.Now()
	ch := c.After(d)
	received := <-ch
	elapsed := time.Since(start)

	if elapsed < d {
		t.Errorf("channel fired too early: elapsed=%v, want >= %v", elapsed, d)
	}
	if received.IsZero() {
		t.Error("received zero time from After channel")
	}
}

func TestRealClock_ImplementsClockInterface(t *testing.T) {
	// Compile-time check: *RealClock satisfies the Clock interface.
	var _ clock.Clock = clock.NewRealClock()
}

// --- MockClock ---

func TestMockClock_Now_ReturnsStartTime(t *testing.T) {
	start := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	if got := mc.Now(); !got.Equal(start) {
		t.Errorf("Now() = %v, want %v", got, start)
	}
}

func TestMockClock_Advance_MovesTime(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	mc.Advance(1 * time.Hour)
	expected := start.Add(1 * time.Hour)
	if got := mc.Now(); !got.Equal(expected) {
		t.Errorf("After Advance(1h): Now()=%v, want %v", got, expected)
	}

	mc.Advance(30 * time.Minute)
	expected = expected.Add(30 * time.Minute)
	if got := mc.Now(); !got.Equal(expected) {
		t.Errorf("After second Advance(30m): Now()=%v, want %v", got, expected)
	}
}

func TestMockClock_Set_SetsExactTime(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	target := time.Date(2025, 6, 15, 9, 30, 0, 0, time.UTC)
	mc.Set(target)

	if got := mc.Now(); !got.Equal(target) {
		t.Errorf("Set(): Now()=%v, want %v", got, target)
	}
}

func TestMockClock_After_WokenByAdvance(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	ch := mc.After(10 * time.Minute)

	// No advance yet — should not receive.
	select {
	case <-ch:
		t.Fatal("channel should not have fired before Advance")
	default:
	}

	woken := mc.Advance(10 * time.Minute)
	if woken != 1 {
		t.Errorf("expected 1 sleeper woken, got %d", woken)
	}

	select {
	case received := <-ch:
		expected := start.Add(10 * time.Minute)
		if !received.Equal(expected) {
			t.Errorf("received time %v, want %v", received, expected)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for After channel")
	}
}

func TestMockClock_After_ZeroOrNegativeFiresImmediately(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	ch := mc.After(0)
	select {
	case <-ch:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatal("After(0) should fire immediately")
	}

	ch2 := mc.After(-1 * time.Second)
	select {
	case <-ch2:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatal("After(-1s) should fire immediately")
	}
}

func TestMockClock_Sleep_BlocksUntilAdvance(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	done := make(chan struct{})
	go func() {
		mc.Sleep(5 * time.Minute)
		close(done)
	}()

	// Wait until the goroutine is blocked.
	mc.AwaitSleepers(1)

	select {
	case <-done:
		t.Fatal("Sleep should still be blocking")
	default:
	}

	mc.Advance(5 * time.Minute)

	select {
	case <-done:
		// ok — Sleep returned
	case <-time.After(1 * time.Second):
		t.Fatal("timeout: Sleep did not return after Advance")
	}
}

func TestMockClock_PendingSleepers_Count(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	if mc.PendingSleepers() != 0 {
		t.Errorf("expected 0 pending sleepers initially")
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mc.Sleep(1 * time.Hour)
		}()
	}

	mc.AwaitSleepers(3)
	if mc.PendingSleepers() != 3 {
		t.Errorf("expected 3 pending sleepers, got %d", mc.PendingSleepers())
	}

	mc.Advance(1 * time.Hour)
	wg.Wait()

	if mc.PendingSleepers() != 0 {
		t.Errorf("expected 0 pending sleepers after all woken, got %d", mc.PendingSleepers())
	}
}

func TestMockClock_ImplementsClockInterface(t *testing.T) {
	start := time.Now()
	var _ clock.Clock = clock.NewMockClock(start)
}

func TestMockClock_Advance_ReturnsWokenCount(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mc := clock.NewMockClock(start)

	// Queue up 3 sleepers at different deadlines.
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		d := time.Duration(i) * time.Minute
		go func(dur time.Duration) {
			defer wg.Done()
			mc.Sleep(dur)
		}(d)
	}

	mc.AwaitSleepers(3)

	// Advance past the first two only.
	woken := mc.Advance(2 * time.Minute)
	if woken != 2 {
		t.Errorf("expected 2 woken, got %d", woken)
	}

	// Advance past the last one.
	woken2 := mc.Advance(1 * time.Minute)
	if woken2 != 1 {
		t.Errorf("expected 1 woken, got %d", woken2)
	}

	wg.Wait()
}
