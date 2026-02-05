// Package realclock provides a real implementation of the Clock port using the time package.
package realclock

import (
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// Clock implements ports.Clock using the standard time package.
type Clock struct{}

// New returns a new real Clock.
func New() *Clock {
	return &Clock{}
}

// Now returns the current time.
func (c *Clock) Now() time.Time {
	return time.Now()
}

// Sleep pauses execution for the specified duration.
func (c *Clock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// After returns a channel that receives the current time after duration d.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// NewTicker returns a new Ticker that sends the current time on its channel.
func (c *Clock) NewTicker(d time.Duration) ports.Ticker {
	return &realTicker{ticker: time.NewTicker(d)}
}

// realTicker wraps time.Ticker to implement ports.Ticker.
type realTicker struct {
	ticker *time.Ticker
}

// C returns the channel on which ticks are delivered.
func (t *realTicker) C() <-chan time.Time {
	return t.ticker.C
}

// Stop turns off the ticker.
func (t *realTicker) Stop() {
	t.ticker.Stop()
}

// Ensure Clock implements ports.Clock.
var _ ports.Clock = (*Clock)(nil)
