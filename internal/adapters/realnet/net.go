// Package realnet provides real implementations of the NetworkDialer and NetworkListener ports.
package realnet

import "net"

// Dialer implements ports.NetworkDialer using the real net.Dial function.
type Dialer struct{}

// NewDialer creates a new Dialer.
func NewDialer() *Dialer {
	return &Dialer{}
}

// Dial establishes a network connection.
func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return net.Dial(network, address)
}

// Listener implements ports.NetworkListener using the real net.Listen function.
type Listener struct{}

// NewListener creates a new Listener.
func NewListener() *Listener {
	return &Listener{}
}

// Listen creates a network listener.
func (l *Listener) Listen(network, address string) (net.Listener, error) {
	return net.Listen(network, address)
}
