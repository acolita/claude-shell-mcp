// Package fakeclock provides a controllable Clock implementation for testing.
package fakeclock

import (
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// Clock is a fake clock that can be controlled in tests.
type Clock struct {
	mu      sync.Mutex
	current time.Time
	waiters []waiter
}

type waiter struct {
	deadline time.Time
	ch       chan time.Time
}

// New creates a new fake clock initialized to the given time.
func New(initial time.Time) *Clock {
	return &Clock{current: initial}
}

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Sleep is a no-op in fake clock (returns immediately).
// Use Advance() to simulate time passing.
func (c *Clock) Sleep(d time.Duration) {
	// In tests, Sleep returns immediately.
	// The test controls time via Advance().
}

// After returns a channel that receives the time after duration d.
// The channel fires when Advance() is called past the deadline.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan time.Time, 1)
	deadline := c.current.Add(d)

	// If already past deadline, fire immediately
	if !c.current.Before(deadline) {
		ch <- c.current
		return ch
	}

	c.waiters = append(c.waiters, waiter{deadline: deadline, ch: ch})
	return ch
}

// NewTicker returns a fake ticker.
func (c *Clock) NewTicker(d time.Duration) ports.Ticker {
	return &fakeTicker{
		clock:    c,
		interval: d,
		ch:       make(chan time.Time, 1),
		stopped:  false,
	}
}

// Advance moves the clock forward by duration d, firing any waiters.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.current = c.current.Add(d)
	now := c.current

	// Find and fire expired waiters
	var remaining []waiter
	for _, w := range c.waiters {
		if !now.Before(w.deadline) {
			select {
			case w.ch <- now:
			default:
			}
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
	c.mu.Unlock()
}

// Set sets the clock to a specific time.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	c.current = t
	c.mu.Unlock()
}

// fakeTicker is a fake ticker for testing.
type fakeTicker struct {
	clock    *Clock
	interval time.Duration
	ch       chan time.Time
	stopped  bool
	mu       sync.Mutex
}

// C returns the channel on which ticks are delivered.
func (t *fakeTicker) C() <-chan time.Time {
	return t.ch
}

// Stop turns off the ticker.
func (t *fakeTicker) Stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
}

// Tick manually sends a tick (for test control).
func (t *fakeTicker) Tick() {
	t.mu.Lock()
	stopped := t.stopped
	t.mu.Unlock()

	if !stopped {
		select {
		case t.ch <- t.clock.Now():
		default:
		}
	}
}

// Ensure Clock implements ports.Clock.
var _ ports.Clock = (*Clock)(nil)
