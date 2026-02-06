// Package fakesshdialer provides a fake SSH dialer for testing.
package fakesshdialer

import (
	"fmt"

	"golang.org/x/crypto/ssh"
)

// Dialer is a fake SSH dialer that can be configured to return errors or specific clients.
type Dialer struct {
	DialFunc func(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error)
	calls    []DialCall
}

// DialCall records a call to Dial.
type DialCall struct {
	Network string
	Addr    string
	Config  *ssh.ClientConfig
}

// New creates a new fake Dialer that returns an error by default.
func New() *Dialer {
	return &Dialer{
		DialFunc: func(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
			return nil, fmt.Errorf("fakesshdialer: not configured")
		},
	}
}

// Dial records the call and delegates to DialFunc.
func (d *Dialer) Dial(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	d.calls = append(d.calls, DialCall{Network: network, Addr: addr, Config: config})
	return d.DialFunc(network, addr, config)
}

// Calls returns all recorded Dial calls.
func (d *Dialer) Calls() []DialCall {
	return d.calls
}

// SetDialFunc sets the function called by Dial.
func (d *Dialer) SetDialFunc(fn func(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error)) {
	d.DialFunc = fn
}

// SetError configures the dialer to always return the given error.
func (d *Dialer) SetError(err error) {
	d.DialFunc = func(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
		return nil, err
	}
}
