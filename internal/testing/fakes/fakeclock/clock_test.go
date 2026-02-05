package fakeclock

import (
	"testing"
	"time"
)

func TestClock_Now(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	if got := c.Now(); !got.Equal(initial) {
		t.Errorf("Now() = %v, want %v", got, initial)
	}
}

func TestClock_Advance(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	c.Advance(5 * time.Minute)

	expected := initial.Add(5 * time.Minute)
	if got := c.Now(); !got.Equal(expected) {
		t.Errorf("Now() after Advance = %v, want %v", got, expected)
	}
}

func TestClock_After(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	ch := c.After(5 * time.Minute)

	// Should not fire yet
	select {
	case <-ch:
		t.Error("After() channel fired too early")
	default:
		// good
	}

	// Advance past deadline
	c.Advance(6 * time.Minute)

	// Should fire now
	select {
	case <-ch:
		// good
	default:
		t.Error("After() channel did not fire after Advance")
	}
}

func TestClock_Sleep(t *testing.T) {
	initial := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(initial)

	// Sleep should return immediately (no-op)
	start := time.Now()
	c.Sleep(1 * time.Hour)
	if time.Since(start) > 100*time.Millisecond {
		t.Error("Sleep() blocked instead of returning immediately")
	}
}
