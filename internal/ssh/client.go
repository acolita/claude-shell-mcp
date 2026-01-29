// Package ssh provides SSH client functionality for remote shell sessions.
package ssh

import (
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client manages SSH connections to remote hosts.
type Client struct {
	conn   *ssh.Client
	config *ssh.ClientConfig
	host   string
	port   int
	mu     sync.Mutex

	// Keepalive settings
	keepaliveInterval time.Duration
	keepaliveStop     chan struct{}
}

// ClientOptions configures SSH client behavior.
type ClientOptions struct {
	Host              string
	Port              int
	User              string
	AuthMethods       []ssh.AuthMethod
	HostKeyCallback   ssh.HostKeyCallback
	Timeout           time.Duration
	KeepaliveInterval time.Duration
}

// DefaultClientOptions returns default client options.
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		Port:              22,
		Timeout:           30 * time.Second,
		KeepaliveInterval: 30 * time.Second,
		HostKeyCallback:   ssh.InsecureIgnoreHostKey(), // Will be overridden
	}
}

// NewClient creates a new SSH client with the given options.
func NewClient(opts ClientOptions) (*Client, error) {
	if opts.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if opts.User == "" {
		return nil, fmt.Errorf("user is required")
	}
	if len(opts.AuthMethods) == 0 {
		return nil, fmt.Errorf("at least one auth method is required")
	}
	if opts.Port == 0 {
		opts.Port = 22
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.KeepaliveInterval == 0 {
		opts.KeepaliveInterval = 30 * time.Second
	}
	if opts.HostKeyCallback == nil {
		opts.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	config := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            opts.AuthMethods,
		HostKeyCallback: opts.HostKeyCallback,
		Timeout:         opts.Timeout,
	}

	return &Client{
		config:            config,
		host:              opts.Host,
		port:              opts.Port,
		keepaliveInterval: opts.KeepaliveInterval,
	}, nil
}

// Connect establishes the SSH connection.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	conn, err := ssh.Dial("tcp", addr, c.config)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	c.conn = conn
	c.keepaliveStop = make(chan struct{})

	// Start keepalive goroutine
	go c.keepalive()

	return nil
}

// keepalive sends periodic keepalive requests to prevent connection timeout.
func (c *Client) keepalive() {
	ticker := time.NewTicker(c.keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.keepaliveStop:
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.conn != nil {
				// Send a keepalive request
				_, _, err := c.conn.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					// Connection may be dead, but don't close here
					// Let the next operation detect the failure
				}
			}
			c.mu.Unlock()
		}
	}
}

// NewSession creates a new SSH session on the connection.
func (c *Client) NewSession() (*ssh.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}

	return session, nil
}

// Close closes the SSH connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.keepaliveStop != nil {
		close(c.keepaliveStop)
		c.keepaliveStop = nil
	}

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}

	return nil
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Host returns the target host.
func (c *Client) Host() string {
	return c.host
}

// Port returns the target port.
func (c *Client) Port() int {
	return c.port
}

// RemoteAddr returns the remote address if connected.
func (c *Client) RemoteAddr() net.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.RemoteAddr()
	}
	return nil
}
