// Package fakerand provides a predictable Random implementation for testing.
package fakerand

import (
	"sync"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// Random is a fake random generator that produces predictable output.
type Random struct {
	mu       sync.Mutex
	sequence []byte
	offset   int
}

// New creates a new fake random with the given sequence.
// If the sequence is nil, it defaults to sequential bytes 0-255.
func New(sequence []byte) *Random {
	if sequence == nil {
		sequence = make([]byte, 256)
		for i := range sequence {
			sequence[i] = byte(i)
		}
	}
	return &Random{sequence: sequence}
}

// NewSequential creates a fake random that returns 0, 1, 2, ..., 255, 0, 1, ...
func NewSequential() *Random {
	return New(nil)
}

// NewFixed creates a fake random that always returns the same bytes.
func NewFixed(b []byte) *Random {
	return New(b)
}

// Read fills b with predictable bytes from the sequence.
func (r *Random) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range b {
		b[i] = r.sequence[r.offset%len(r.sequence)]
		r.offset++
	}
	return len(b), nil
}

// Reset resets the random generator to the beginning of its sequence.
func (r *Random) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offset = 0
}

// Ensure Random implements ports.Random.
var _ ports.Random = (*Random)(nil)
