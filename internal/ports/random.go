package ports

// Random abstracts random byte generation for testing.
type Random interface {
	// Read fills b with random bytes and returns the number of bytes read.
	Read(b []byte) (n int, err error)
}
