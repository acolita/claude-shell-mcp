// Package realsshdialer provides a real implementation of the SSHDialer port.
package realsshdialer

import "golang.org/x/crypto/ssh"

// Dialer implements ports.SSHDialer using the real ssh.Dial function.
type Dialer struct{}

// New creates a new Dialer.
func New() *Dialer {
	return &Dialer{}
}

// Dial establishes an SSH connection to the given address.
func (d *Dialer) Dial(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	return ssh.Dial(network, addr, config)
}
