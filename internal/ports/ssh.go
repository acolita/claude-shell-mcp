// Package ports defines interfaces for external dependencies.
package ports

import (
	"github.com/acolita/claude-shell-mcp/internal/sftp"
)

// SSHClient defines the interface for SSH client operations used by Session.
type SSHClient interface {
	// Connect establishes the SSH connection.
	Connect() error

	// Close closes the SSH connection.
	Close() error

	// IsConnected returns true if the connection is active.
	IsConnected() bool

	// SFTPClient returns the SFTP client for file operations.
	SFTPClient() (*sftp.Client, error)

	// TunnelManager returns the tunnel manager for port forwarding.
	// Returns nil if not supported by the implementation.
	TunnelManager() SSHTunnelManager
}

// SSHTunnelManager defines the interface for SSH tunnel management.
type SSHTunnelManager interface {
	// CreateLocalTunnel creates a local port forward (-L).
	CreateLocalTunnel(localPort, remotePort int, remoteHost, localHost string) (string, error)

	// CreateReverseTunnel creates a reverse port forward (-R).
	CreateReverseTunnel(localPort, remotePort int, remoteHost, localHost string) (string, error)

	// CloseTunnel closes a tunnel by ID.
	CloseTunnel(tunnelID string) error

	// ListTunnels returns all active tunnels.
	ListTunnels() []TunnelInfo

	// Close closes all tunnels.
	Close() error
}

// TunnelInfo contains information about an active tunnel.
type TunnelInfo struct {
	ID         string
	Type       string // "local" or "reverse"
	LocalHost  string
	LocalPort  int
	RemoteHost string
	RemotePort int
}

// SSHPTY defines the interface for SSH PTY operations.
type SSHPTY interface {
	// Read reads from the PTY.
	Read(b []byte) (int, error)

	// Write writes to the PTY.
	Write(b []byte) (int, error)

	// WriteString writes a string to the PTY.
	WriteString(s string) (int, error)

	// Interrupt sends an interrupt signal (Ctrl+C).
	Interrupt() error

	// Close closes the PTY.
	Close() error

	// SetReadDeadline sets a deadline for read operations.
	SetReadDeadline(t interface{}) error
}

// SSHClientFactory creates SSH clients.
type SSHClientFactory func(host string, port int, user string, authMethods interface{}) (SSHClient, error)

// SSHPTYFactory creates SSH PTYs from a client.
type SSHPTYFactory func(client SSHClient) (SSHPTY, error)
