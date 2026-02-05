// Package fakessh provides a fake SSH client for testing.
package fakessh

import (
	"errors"
	"sync"

	"github.com/acolita/claude-shell-mcp/internal/ports"
	"github.com/acolita/claude-shell-mcp/internal/sftp"
)

// Client is a fake SSH client for testing.
type Client struct {
	mu            sync.Mutex
	connected     bool
	closed        bool
	connectErr    error
	closeErr      error
	sftpClient    *sftp.Client
	sftpErr       error
	tunnelManager ports.SSHTunnelManager
}

// New creates a new fake SSH client.
func New() *Client {
	return &Client{}
}

// SetConnectError sets an error to return from Connect.
func (c *Client) SetConnectError(err error) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectErr = err
	return c
}

// SetCloseError sets an error to return from Close.
func (c *Client) SetCloseError(err error) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeErr = err
	return c
}

// SetSFTPError sets an error to return from SFTPClient.
func (c *Client) SetSFTPError(err error) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sftpErr = err
	return c
}

// SetTunnelManager sets the tunnel manager to return.
func (c *Client) SetTunnelManager(tm ports.SSHTunnelManager) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tunnelManager = tm
	return c
}

// Connect simulates connecting to the SSH server.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connectErr != nil {
		return c.connectErr
	}

	c.connected = true
	return nil
}

// Close simulates closing the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closeErr != nil {
		return c.closeErr
	}

	c.connected = false
	c.closed = true
	return nil
}

// IsConnected returns whether the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// SFTPClient returns the fake SFTP client.
func (c *Client) SFTPClient() (*sftp.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sftpErr != nil {
		return nil, c.sftpErr
	}
	return c.sftpClient, nil
}

// TunnelManager returns the tunnel manager.
func (c *Client) TunnelManager() ports.SSHTunnelManager {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tunnelManager
}

// --- Test inspection methods ---

// WasConnected returns true if Connect was called successfully.
func (c *Client) WasConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected || c.closed
}

// WasClosed returns true if Close was called.
func (c *Client) WasClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// TunnelManager is a fake tunnel manager for testing.
type TunnelManager struct {
	mu       sync.Mutex
	tunnels  []ports.TunnelInfo
	createFn func(tunnelType string, localPort, remotePort int, remoteHost, localHost string) (string, error)
	closeFn  func(tunnelID string) error
}

// NewTunnelManager creates a new fake tunnel manager.
func NewTunnelManager() *TunnelManager {
	return &TunnelManager{
		tunnels: make([]ports.TunnelInfo, 0),
	}
}

// OnCreate sets a callback for tunnel creation.
func (tm *TunnelManager) OnCreate(fn func(tunnelType string, localPort, remotePort int, remoteHost, localHost string) (string, error)) *TunnelManager {
	tm.createFn = fn
	return tm
}

// OnClose sets a callback for tunnel closure.
func (tm *TunnelManager) OnClose(fn func(tunnelID string) error) *TunnelManager {
	tm.closeFn = fn
	return tm
}

// CreateLocalTunnel creates a local port forward.
func (tm *TunnelManager) CreateLocalTunnel(localPort, remotePort int, remoteHost, localHost string) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.createFn != nil {
		return tm.createFn("local", localPort, remotePort, remoteHost, localHost)
	}

	id := "tunnel_local_1"
	tm.tunnels = append(tm.tunnels, ports.TunnelInfo{
		ID:         id,
		Type:       "local",
		LocalHost:  localHost,
		LocalPort:  localPort,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
	})
	return id, nil
}

// CreateReverseTunnel creates a reverse port forward.
func (tm *TunnelManager) CreateReverseTunnel(localPort, remotePort int, remoteHost, localHost string) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.createFn != nil {
		return tm.createFn("reverse", localPort, remotePort, remoteHost, localHost)
	}

	id := "tunnel_reverse_1"
	tm.tunnels = append(tm.tunnels, ports.TunnelInfo{
		ID:         id,
		Type:       "reverse",
		LocalHost:  localHost,
		LocalPort:  localPort,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
	})
	return id, nil
}

// CloseTunnel closes a tunnel by ID.
func (tm *TunnelManager) CloseTunnel(tunnelID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.closeFn != nil {
		return tm.closeFn(tunnelID)
	}

	for i, t := range tm.tunnels {
		if t.ID == tunnelID {
			tm.tunnels = append(tm.tunnels[:i], tm.tunnels[i+1:]...)
			return nil
		}
	}
	return errors.New("tunnel not found")
}

// ListTunnels returns all active tunnels.
func (tm *TunnelManager) ListTunnels() []ports.TunnelInfo {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	result := make([]ports.TunnelInfo, len(tm.tunnels))
	copy(result, tm.tunnels)
	return result
}

// Close closes all tunnels.
func (tm *TunnelManager) Close() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tunnels = nil
	return nil
}
