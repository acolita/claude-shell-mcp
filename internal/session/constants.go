package session

// Common constants used across session management.
const (
	// PTY device path prefix
	devPtsPrefix = "/dev/pts/"

	// Error messages
	errSessionNotInitialized = "session not initialized"
	errConnectionLostFmt     = "connection lost and reconnect failed: %w (original: %v)"

	// Peak-tty detection hint
	hintPeakTTYWaiting = "Process is waiting for input (detected by peak-tty)."
)
