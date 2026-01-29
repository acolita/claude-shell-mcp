package security

import (
	"crypto/rand"
)

// WipeBytes securely wipes a byte slice by overwriting with random data,
// then zeros, then random data again to prevent recovery.
func WipeBytes(data []byte) {
	if len(data) == 0 {
		return
	}

	// First pass: random data
	rand.Read(data)

	// Second pass: zeros
	for i := range data {
		data[i] = 0
	}

	// Third pass: random data again
	rand.Read(data)

	// Final pass: zeros
	for i := range data {
		data[i] = 0
	}
}

// WipeString creates a zeroed copy of a string's underlying bytes.
// Note: Go strings are immutable, so we can't wipe them directly.
// This function is provided for documentation, but you should use []byte
// for sensitive data instead of strings.
func WipeString(s *string) {
	if s == nil || *s == "" {
		return
	}

	// Convert to bytes for wiping
	// Note: This doesn't actually wipe the original string memory
	// because strings are immutable in Go. Use []byte for sensitive data.
	b := []byte(*s)
	WipeBytes(b)
	*s = ""
}

// SecureBytes wraps a byte slice and ensures it gets wiped when done.
type SecureBytes struct {
	data []byte
}

// NewSecureBytes creates a new SecureBytes with a copy of the data.
func NewSecureBytes(data []byte) *SecureBytes {
	d := make([]byte, len(data))
	copy(d, data)
	return &SecureBytes{data: d}
}

// Data returns the underlying byte slice.
func (sb *SecureBytes) Data() []byte {
	return sb.data
}

// String returns the data as a string (for convenience).
func (sb *SecureBytes) String() string {
	return string(sb.data)
}

// Wipe securely wipes the data.
func (sb *SecureBytes) Wipe() {
	WipeBytes(sb.data)
	sb.data = nil
}

// Len returns the length of the data.
func (sb *SecureBytes) Len() int {
	return len(sb.data)
}
