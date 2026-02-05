package fakeclock

import (
	"testing"
	"time"
)

func TestClock_Set(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	newTime := time.Date(2025, 6, 15, 12, 30, 0, 0, time.UTC)
	c.Set(newTime)

	if got := c.Now(); !got.Equal(newTime) {
		t.Errorf("Now() after Set = %v, want %v", got, newTime)
	}
}

func TestClock_NewTicker(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ticker := c.NewTicker(1 * time.Second)
	if ticker == nil {
		t.Fatal("NewTicker returned nil")
	}

	// Channel should be readable
	ch := ticker.C()
	if ch == nil {
		t.Fatal("C() returned nil channel")
	}

	// Channel should be empty initially
	select {
	case <-ch:
		t.Error("ticker channel should be empty initially")
	default:
		// expected
	}
}

func TestFakeTicker_Tick(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ticker := c.NewTicker(1 * time.Second)

	// Cast to get access to Tick method
	ft, ok := ticker.(*fakeTicker)
	if !ok {
		t.Fatal("expected *fakeTicker")
	}

	// Tick should send current time on the channel
	ft.Tick()

	select {
	case got := <-ticker.C():
		if !got.Equal(initial) {
			t.Errorf("Tick sent %v, want %v", got, initial)
		}
	default:
		t.Error("expected a tick on the channel")
	}
}

func TestFakeTicker_TickAfterStop(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ticker := c.NewTicker(1 * time.Second)

	ft, ok := ticker.(*fakeTicker)
	if !ok {
		t.Fatal("expected *fakeTicker")
	}

	// Stop the ticker
	ticker.Stop()

	// Tick should not send anything after stop
	ft.Tick()

	select {
	case <-ticker.C():
		t.Error("stopped ticker should not send ticks")
	default:
		// expected
	}
}

func TestFakeTicker_TickDropsIfChannelFull(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ticker := c.NewTicker(1 * time.Second)
	ft, ok := ticker.(*fakeTicker)
	if !ok {
		t.Fatal("expected *fakeTicker")
	}

	// Send two ticks without reading - second should be dropped (channel buffer = 1)
	ft.Tick()
	ft.Tick()

	// Should read one tick
	select {
	case <-ticker.C():
		// good
	default:
		t.Error("expected at least one tick")
	}

	// Channel should now be empty (second tick was dropped)
	select {
	case <-ticker.C():
		t.Error("expected second tick to be dropped due to full channel")
	default:
		// expected
	}
}

func TestClock_Sleep_ReturnsImmediately(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	// Sleep should be a no-op and return immediately
	start := time.Now()
	c.Sleep(10 * time.Second)
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("Sleep blocked for %v, expected immediate return", elapsed)
	}

	// Time should NOT advance from Sleep
	if got := c.Now(); !got.Equal(initial) {
		t.Errorf("Sleep should not advance clock, got %v, want %v", got, initial)
	}
}

func TestClock_AfterZeroDuration(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	// After(0) should fire immediately since current >= deadline
	ch := c.After(0)

	select {
	case got := <-ch:
		if !got.Equal(initial) {
			t.Errorf("After(0) sent %v, want %v", got, initial)
		}
	default:
		t.Error("After(0) should fire immediately")
	}
}

func TestClock_AfterNegativeDuration(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	// After with negative duration should fire immediately since current >= deadline
	ch := c.After(-1 * time.Second)

	select {
	case <-ch:
		// expected
	default:
		t.Error("After with negative duration should fire immediately")
	}
}

func TestClock_AdvanceFiresMultipleWaiters(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ch1 := c.After(1 * time.Minute)
	ch2 := c.After(2 * time.Minute)
	ch3 := c.After(5 * time.Minute)

	// Advance 3 minutes - should fire ch1 and ch2 but not ch3
	c.Advance(3 * time.Minute)

	select {
	case <-ch1:
		// expected
	default:
		t.Error("ch1 should have fired")
	}

	select {
	case <-ch2:
		// expected
	default:
		t.Error("ch2 should have fired")
	}

	select {
	case <-ch3:
		t.Error("ch3 should NOT have fired yet")
	default:
		// expected
	}

	// Advance 3 more minutes - should fire ch3
	c.Advance(3 * time.Minute)

	select {
	case <-ch3:
		// expected
	default:
		t.Error("ch3 should have fired after second advance")
	}
}

func TestClock_AdvanceExactDeadline(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ch := c.After(5 * time.Minute)

	// Advance exactly to the deadline
	c.Advance(5 * time.Minute)

	select {
	case <-ch:
		// expected: !now.Before(deadline) means now >= deadline fires
	default:
		t.Error("After channel should fire when advancing exactly to deadline")
	}
}

func TestClock_SetDoesNotFireWaiters(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ch := c.After(5 * time.Minute)

	// Set jumps the clock forward but does NOT fire waiters (unlike Advance)
	c.Set(initial.Add(10 * time.Minute))

	select {
	case <-ch:
		t.Error("Set should not fire waiters (only Advance does)")
	default:
		// expected
	}
}

func TestFakeTicker_StopMultipleTimes(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ticker := c.NewTicker(1 * time.Second)

	// Stop should be safe to call multiple times
	ticker.Stop()
	ticker.Stop()
	ticker.Stop()

	// Should not panic, and ticks should not be delivered
	ft := ticker.(*fakeTicker)
	ft.Tick()

	select {
	case <-ticker.C():
		t.Error("stopped ticker should not deliver ticks")
	default:
		// expected
	}
}

func TestClock_AdvanceMultipleTimes(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	c.Advance(1 * time.Hour)
	c.Advance(2 * time.Hour)
	c.Advance(30 * time.Minute)

	expected := initial.Add(3*time.Hour + 30*time.Minute)
	if got := c.Now(); !got.Equal(expected) {
		t.Errorf("Now() = %v, want %v", got, expected)
	}
}
