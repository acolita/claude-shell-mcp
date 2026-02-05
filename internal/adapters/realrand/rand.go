// Package realrand provides a real implementation of the Random port using crypto/rand.
package realrand

import (
	"crypto/rand"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// Random implements ports.Random using crypto/rand.
type Random struct{}

// New returns a new real Random.
func New() *Random {
	return &Random{}
}

// Read fills b with cryptographically secure random bytes.
func (r *Random) Read(b []byte) (n int, err error) {
	return rand.Read(b)
}

// Ensure Random implements ports.Random.
var _ ports.Random = (*Random)(nil)
