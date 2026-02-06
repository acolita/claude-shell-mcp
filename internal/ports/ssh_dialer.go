// Package ports defines interfaces for external dependencies (Ports and Adapters pattern).
package ports

import (
	"golang.org/x/crypto/ssh"
)

// SSHDialer abstracts SSH connection establishment for testing.
type SSHDialer interface {
	// Dial establishes an SSH connection to the given address.
	Dial(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error)
}
