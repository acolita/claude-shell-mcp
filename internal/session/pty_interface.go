package session

import (
	"io"
	"os"
	"time"
)

// PTY defines the interface for pseudo-terminal implementations.
// Both local PTY and SSH PTY implement this interface.
type PTY interface {
	io.Reader
	io.Writer

	// WriteString writes a string to the PTY input.
	WriteString(s string) (int, error)

	// Interrupt sends an interrupt signal (Ctrl+C).
	Interrupt() error

	// Close closes the PTY.
	Close() error

	// SetReadDeadline sets a deadline for read operations.
	// For SSH PTY this may be a no-op.
	SetReadDeadline(t time.Time) error
}

// localPTYAdapter wraps LocalPTY to implement the PTY interface.
type localPTYAdapter struct {
	pty interface {
		Read(b []byte) (int, error)
		Write(b []byte) (int, error)
		WriteString(s string) (int, error)
		Interrupt() error
		Close() error
		File() *os.File
	}
}

func (a *localPTYAdapter) Read(b []byte) (int, error) {
	return a.pty.Read(b)
}

func (a *localPTYAdapter) Write(b []byte) (int, error) {
	return a.pty.Write(b)
}

func (a *localPTYAdapter) WriteString(s string) (int, error) {
	return a.pty.WriteString(s)
}

func (a *localPTYAdapter) Interrupt() error {
	return a.pty.Interrupt()
}

func (a *localPTYAdapter) Close() error {
	return a.pty.Close()
}

func (a *localPTYAdapter) SetReadDeadline(t time.Time) error {
	if f := a.pty.File(); f != nil {
		// Ignore error — macOS PTY fds don't support OS-level deadlines.
		// Callers use goroutine-based timeouts for actual enforcement.
		_ = f.SetReadDeadline(t)
	}
	return nil
}

// sshPTYAdapter wraps SSHPTY to implement the PTY interface.
type sshPTYAdapter struct {
	pty interface {
		Read(b []byte) (int, error)
		Write(b []byte) (int, error)
		WriteString(s string) (int, error)
		Interrupt() error
		Close() error
		SetReadDeadline(t time.Time) error
	}
}

func (a *sshPTYAdapter) Read(b []byte) (int, error) {
	return a.pty.Read(b)
}

func (a *sshPTYAdapter) Write(b []byte) (int, error) {
	return a.pty.Write(b)
}

func (a *sshPTYAdapter) WriteString(s string) (int, error) {
	return a.pty.WriteString(s)
}

func (a *sshPTYAdapter) Interrupt() error {
	return a.pty.Interrupt()
}

func (a *sshPTYAdapter) Close() error {
	return a.pty.Close()
}

func (a *sshPTYAdapter) SetReadDeadline(t time.Time) error {
	return a.pty.SetReadDeadline(t)
}
