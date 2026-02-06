// Package ports defines interfaces for external dependencies (Ports and Adapters pattern).
package ports

import "net"

// NetworkDialer abstracts network dialing for testing.
type NetworkDialer interface {
	// Dial establishes a network connection.
	Dial(network, address string) (net.Conn, error)
}

// NetworkListener abstracts network listening for testing.
type NetworkListener interface {
	// Listen creates a network listener.
	Listen(network, address string) (net.Listener, error)
}
