// Package ssh provides SSH client functionality for remote shell sessions.
package ssh

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realclock"
	"github.com/acolita/claude-shell-mcp/internal/adapters/realsshdialer"
	"github.com/acolita/claude-shell-mcp/internal/ports"
	"github.com/acolita/claude-shell-mcp/internal/sftp"
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

	// SFTP client (lazy initialized)
	sftpClient *sftp.Client

	// Tunnel manager (lazy initialized)
	tunnelManager *TunnelManager

	// Injected dependencies
	clock  ports.Clock
	dialer ports.SSHDialer
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
	Clock             ports.Clock
	Dialer            ports.SSHDialer
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

	clk := opts.Clock
	if clk == nil {
		clk = realclock.New()
	}
	dial := opts.Dialer
	if dial == nil {
		dial = realsshdialer.New()
	}

	return &Client{
		config:            config,
		host:              opts.Host,
		port:              opts.Port,
		keepaliveInterval: opts.KeepaliveInterval,
		clock:             clk,
		dialer:            dial,
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
	conn, err := c.dialer.Dial("tcp", addr, c.config)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	c.conn = conn
	c.keepaliveStop = make(chan struct{})

	// Start keepalive goroutine.
	// Copy the channel reference so the goroutine never reads the struct field.
	stop := c.keepaliveStop
	go c.keepalive(stop)

	return nil
}

// keepalive sends periodic keepalive requests to prevent connection timeout.
// The stop channel is passed as a parameter to avoid a data race on the struct field.
func (c *Client) keepalive(stop <-chan struct{}) {
	ticker := c.clock.NewTicker(c.keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C():
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

// Close closes the SSH connection and any associated clients.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.keepaliveStop != nil {
		close(c.keepaliveStop)
		c.keepaliveStop = nil
	}

	// Close tunnels first
	if c.tunnelManager != nil {
		c.tunnelManager.CloseAll()
		c.tunnelManager = nil
	}

	// Close SFTP client
	if c.sftpClient != nil {
		c.sftpClient.Close()
		c.sftpClient = nil
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

// SFTPClient returns an SFTP client for file transfers.
// The SFTP client is lazily initialized and reuses the SSH connection.
func (c *Client) SFTPClient() (*sftp.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	if c.sftpClient == nil {
		c.sftpClient = sftp.NewClient(c.conn)
	}

	return c.sftpClient, nil
}

// CloseSFTP closes the SFTP client without closing the SSH connection.
func (c *Client) CloseSFTP() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sftpClient != nil {
		err := c.sftpClient.Close()
		c.sftpClient = nil
		return err
	}
	return nil
}

// TunnelManager returns the tunnel manager for this client.
// The tunnel manager is lazily initialized.
func (c *Client) TunnelManager() *TunnelManager {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	if c.tunnelManager == nil {
		c.tunnelManager = NewTunnelManager(c.conn)
	}

	return c.tunnelManager
}
