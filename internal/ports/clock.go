// Package ports defines interfaces for external dependencies (Ports and Adapters pattern).
package ports

import "time"

// Clock abstracts time operations for testing.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// Sleep pauses execution for the specified duration.
	Sleep(d time.Duration)

	// After returns a channel that receives the current time after duration d.
	After(d time.Duration) <-chan time.Time

	// NewTicker returns a new Ticker that sends the current time on its channel
	// after each tick.
	NewTicker(d time.Duration) Ticker
}

// Ticker wraps time.Ticker for testing.
type Ticker interface {
	// C returns the channel on which ticks are delivered.
	C() <-chan time.Time

	// Stop turns off the ticker.
	Stop()
}
