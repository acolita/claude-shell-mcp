// Package fakenet provides fake network dialer and listener for testing.
package fakenet

import (
	"fmt"
	"net"
)

// Dialer is a fake network dialer that can be configured to return errors or specific connections.
type Dialer struct {
	DialFunc func(network, address string) (net.Conn, error)
	calls    []DialCall
}

// DialCall records a call to Dial.
type DialCall struct {
	Network string
	Address string
}

// NewDialer creates a new fake Dialer that returns an error by default.
func NewDialer() *Dialer {
	return &Dialer{
		DialFunc: func(network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("fakenet: not configured")
		},
	}
}

// Dial records the call and delegates to DialFunc.
func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	d.calls = append(d.calls, DialCall{Network: network, Address: address})
	return d.DialFunc(network, address)
}

// Calls returns all recorded Dial calls.
func (d *Dialer) Calls() []DialCall {
	return d.calls
}

// SetError configures the dialer to always return the given error.
func (d *Dialer) SetError(err error) {
	d.DialFunc = func(network, address string) (net.Conn, error) {
		return nil, err
	}
}

// Listener is a fake network listener that can be configured.
type Listener struct {
	ListenFunc func(network, address string) (net.Listener, error)
	calls      []ListenCall
}

// ListenCall records a call to Listen.
type ListenCall struct {
	Network string
	Address string
}

// NewListener creates a new fake Listener that returns an error by default.
func NewListener() *Listener {
	return &Listener{
		ListenFunc: func(network, address string) (net.Listener, error) {
			return nil, fmt.Errorf("fakenet: not configured")
		},
	}
}

// Listen records the call and delegates to ListenFunc.
func (l *Listener) Listen(network, address string) (net.Listener, error) {
	l.calls = append(l.calls, ListenCall{Network: network, Address: address})
	return l.ListenFunc(network, address)
}

// Calls returns all recorded Listen calls.
func (l *Listener) Calls() []ListenCall {
	return l.calls
}

// SetError configures the listener to always return the given error.
func (l *Listener) SetError(err error) {
	l.ListenFunc = func(network, address string) (net.Listener, error) {
		return nil, err
	}
}
