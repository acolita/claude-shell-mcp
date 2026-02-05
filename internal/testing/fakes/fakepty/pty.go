// Package fakepty provides a fake PTY implementation for testing.
package fakepty

import (
	"bytes"
	"io"
	"sync"
	"time"
)

// PTY is a fake PTY for testing session logic without real terminals.
type PTY struct {
	mu           sync.Mutex
	responses    [][]byte      // Queued responses to return on Read
	responseIdx  int           // Current response index
	written      bytes.Buffer  // Captures all written data
	closed       bool          // Whether Close() was called
	interrupted  bool          // Whether Interrupt() was called
	readDeadline time.Time     // Current read deadline
	blockReads   bool          // If true, Read blocks until deadline
	readDelay    time.Duration // Artificial delay before returning data
}

// New creates a new fake PTY.
func New() *PTY {
	return &PTY{
		responses: make([][]byte, 0),
	}
}

// AddResponse queues a response to be returned on subsequent Read calls.
// Responses are returned in order, one per Read call.
func (p *PTY) AddResponse(data string) *PTY {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.responses = append(p.responses, []byte(data))
	return p
}

// AddResponses queues multiple responses.
func (p *PTY) AddResponses(responses ...string) *PTY {
	for _, r := range responses {
		p.AddResponse(r)
	}
	return p
}

// SetBlockReads makes Read block until the deadline is reached.
// Useful for testing timeout behavior.
func (p *PTY) SetBlockReads(block bool) *PTY {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blockReads = block
	return p
}

// SetReadDelay adds an artificial delay before Read returns data.
func (p *PTY) SetReadDelay(d time.Duration) *PTY {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readDelay = d
	return p
}

// Read implements io.Reader. Returns queued responses in order.
// If blockReads is true, blocks until deadline.
// If no responses are queued, returns io.EOF.
func (p *PTY) Read(b []byte) (int, error) {
	p.mu.Lock()
	blockReads := p.blockReads
	deadline := p.readDeadline
	delay := p.readDelay
	p.mu.Unlock()

	// Apply read delay if set
	if delay > 0 {
		time.Sleep(delay)
	}

	// If blocking mode, wait until deadline
	if blockReads && !deadline.IsZero() {
		waitTime := time.Until(deadline)
		if waitTime > 0 {
			time.Sleep(waitTime)
		}
		// Return timeout-like behavior (0 bytes, but not EOF)
		return 0, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return 0, io.EOF
	}

	if p.responseIdx >= len(p.responses) {
		// No more responses - return 0 bytes (simulates no data available)
		return 0, nil
	}

	response := p.responses[p.responseIdx]
	p.responseIdx++

	n := copy(b, response)
	return n, nil
}

// Write implements io.Writer. Captures written data for later inspection.
func (p *PTY) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return 0, io.ErrClosedPipe
	}

	return p.written.Write(b)
}

// WriteString writes a string to the PTY.
func (p *PTY) WriteString(s string) (int, error) {
	return p.Write([]byte(s))
}

// Interrupt simulates sending Ctrl+C.
func (p *PTY) Interrupt() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interrupted = true
	return nil
}

// Close closes the fake PTY.
func (p *PTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// SetReadDeadline sets the read deadline.
func (p *PTY) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.readDeadline = t
	return nil
}

// --- Test inspection methods ---

// Written returns all data that was written to the PTY.
func (p *PTY) Written() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.String()
}

// WrittenBytes returns all data that was written to the PTY as bytes.
func (p *PTY) WrittenBytes() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.Bytes()
}

// WasInterrupted returns true if Interrupt() was called.
func (p *PTY) WasInterrupted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.interrupted
}

// IsClosed returns true if Close() was called.
func (p *PTY) IsClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// Reset clears all state for reuse.
func (p *PTY) Reset() *PTY {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.responses = make([][]byte, 0)
	p.responseIdx = 0
	p.written.Reset()
	p.closed = false
	p.interrupted = false
	p.readDeadline = time.Time{}
	p.blockReads = false
	p.readDelay = 0
	return p
}
