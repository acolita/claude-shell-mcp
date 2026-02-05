package ssh

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

// TunnelType represents the type of SSH tunnel.
type TunnelType string

const (
	// TunnelTypeLocal is a local port forward (-L): local listens, forwards through SSH to remote
	TunnelTypeLocal TunnelType = "local"
	// TunnelTypeReverse is a reverse port forward (-R): remote listens, forwards back to local
	TunnelTypeReverse TunnelType = "reverse"
)

// Tunnel represents an active SSH tunnel.
type Tunnel struct {
	ID            string     `json:"id"`
	Type          TunnelType `json:"type"`
	LocalHost     string     `json:"local_host"`
	LocalPort     int        `json:"local_port"`
	RemoteHost    string     `json:"remote_host"`
	RemotePort    int        `json:"remote_port"`
	ActiveConns   int64      `json:"active_connections"`
	TotalConns    int64      `json:"total_connections"`
	BytesSent     int64      `json:"bytes_sent"`
	BytesReceived int64      `json:"bytes_received"`

	listener  net.Listener
	sshClient *ssh.Client
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// TunnelManager manages SSH tunnels for a client.
type TunnelManager struct {
	sshClient *ssh.Client
	tunnels   map[string]*Tunnel
	mu        sync.RWMutex
	nextID    int
}

// NewTunnelManager creates a new tunnel manager.
func NewTunnelManager(sshClient *ssh.Client) *TunnelManager {
	return &TunnelManager{
		sshClient: sshClient,
		tunnels:   make(map[string]*Tunnel),
	}
}

// CreateLocalTunnel creates a local port forward (-L).
// Listens on localHost:localPort and forwards through SSH to remoteHost:remotePort.
func (tm *TunnelManager) CreateLocalTunnel(localHost string, localPort int, remoteHost string, remotePort int) (*Tunnel, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Listen locally
	localAddr := fmt.Sprintf("%s:%d", localHost, localPort)
	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", localAddr, err)
	}

	// Get actual port if 0 was specified
	actualPort := listener.Addr().(*net.TCPAddr).Port

	tm.nextID++
	tunnelID := fmt.Sprintf("tunnel_%d", tm.nextID)

	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &Tunnel{
		ID:         tunnelID,
		Type:       TunnelTypeLocal,
		LocalHost:  localHost,
		LocalPort:  actualPort,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
		listener:   listener,
		sshClient:  tm.sshClient,
		ctx:        ctx,
		cancel:     cancel,
	}

	tm.tunnels[tunnelID] = tunnel

	// Start accepting connections
	tunnel.wg.Add(1)
	go tunnel.acceptLocal()

	slog.Info("created local tunnel",
		slog.String("id", tunnelID),
		slog.String("local", fmt.Sprintf("%s:%d", localHost, actualPort)),
		slog.String("remote", fmt.Sprintf("%s:%d", remoteHost, remotePort)),
	)

	return tunnel, nil
}

// CreateReverseTunnel creates a reverse port forward (-R).
// Listens on the remote server at remoteHost:remotePort and forwards back to localHost:localPort.
func (tm *TunnelManager) CreateReverseTunnel(remoteHost string, remotePort int, localHost string, localPort int) (*Tunnel, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Listen on the remote server
	remoteAddr := fmt.Sprintf("%s:%d", remoteHost, remotePort)
	listener, err := tm.sshClient.Listen("tcp", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on remote %s: %w", remoteAddr, err)
	}

	// Get actual port if 0 was specified
	actualPort := listener.Addr().(*net.TCPAddr).Port

	tm.nextID++
	tunnelID := fmt.Sprintf("tunnel_%d", tm.nextID)

	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &Tunnel{
		ID:         tunnelID,
		Type:       TunnelTypeReverse,
		LocalHost:  localHost,
		LocalPort:  localPort,
		RemoteHost: remoteHost,
		RemotePort: actualPort,
		listener:   listener,
		sshClient:  tm.sshClient,
		ctx:        ctx,
		cancel:     cancel,
	}

	tm.tunnels[tunnelID] = tunnel

	// Start accepting connections
	tunnel.wg.Add(1)
	go tunnel.acceptReverse()

	slog.Info("created reverse tunnel",
		slog.String("id", tunnelID),
		slog.String("remote", fmt.Sprintf("%s:%d", remoteHost, actualPort)),
		slog.String("local", fmt.Sprintf("%s:%d", localHost, localPort)),
	)

	return tunnel, nil
}

// GetTunnel returns a tunnel by ID.
func (tm *TunnelManager) GetTunnel(id string) (*Tunnel, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	t, ok := tm.tunnels[id]
	return t, ok
}

// ListTunnels returns all active tunnels.
func (tm *TunnelManager) ListTunnels() []*Tunnel {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tunnels := make([]*Tunnel, 0, len(tm.tunnels))
	for _, t := range tm.tunnels {
		tunnels = append(tunnels, t)
	}
	return tunnels
}

// CloseTunnel closes a specific tunnel.
func (tm *TunnelManager) CloseTunnel(id string) error {
	tm.mu.Lock()
	tunnel, ok := tm.tunnels[id]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("tunnel not found: %s", id)
	}
	delete(tm.tunnels, id)
	tm.mu.Unlock()

	tunnel.Close()
	return nil
}

// CloseAll closes all tunnels.
func (tm *TunnelManager) CloseAll() {
	tm.mu.Lock()
	tunnels := make([]*Tunnel, 0, len(tm.tunnels))
	for _, t := range tm.tunnels {
		tunnels = append(tunnels, t)
	}
	tm.tunnels = make(map[string]*Tunnel)
	tm.mu.Unlock()

	for _, t := range tunnels {
		t.Close()
	}
}

// Close closes a tunnel and all its connections.
func (t *Tunnel) Close() {
	t.cancel()
	if t.listener != nil {
		t.listener.Close()
	}
	t.wg.Wait()

	slog.Info("closed tunnel",
		slog.String("id", t.ID),
		slog.Int64("total_connections", t.TotalConns),
		slog.Int64("bytes_sent", t.BytesSent),
		slog.Int64("bytes_received", t.BytesReceived),
	)
}

// acceptLocal accepts connections for local tunnels.
func (t *Tunnel) acceptLocal() {
	defer t.wg.Done()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.ctx.Done():
				return
			default:
				slog.Warn("accept error on local tunnel",
					slog.String("id", t.ID),
					slog.String("error", err.Error()),
				)
				return
			}
		}

		atomic.AddInt64(&t.ActiveConns, 1)
		atomic.AddInt64(&t.TotalConns, 1)

		t.wg.Add(1)
		go t.handleLocalConnection(conn)
	}
}

// handleLocalConnection handles a single connection for local tunnels.
func (t *Tunnel) handleLocalConnection(localConn net.Conn) {
	defer t.wg.Done()
	defer localConn.Close()
	defer atomic.AddInt64(&t.ActiveConns, -1)

	// Connect to remote through SSH
	remoteAddr := fmt.Sprintf("%s:%d", t.RemoteHost, t.RemotePort)
	remoteConn, err := t.sshClient.Dial("tcp", remoteAddr)
	if err != nil {
		slog.Warn("failed to dial remote",
			slog.String("id", t.ID),
			slog.String("remote", remoteAddr),
			slog.String("error", err.Error()),
		)
		return
	}
	defer remoteConn.Close()

	// Proxy data bidirectionally
	t.proxy(localConn, remoteConn)
}

// acceptReverse accepts connections for reverse tunnels.
func (t *Tunnel) acceptReverse() {
	defer t.wg.Done()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.ctx.Done():
				return
			default:
				slog.Warn("accept error on reverse tunnel",
					slog.String("id", t.ID),
					slog.String("error", err.Error()),
				)
				return
			}
		}

		atomic.AddInt64(&t.ActiveConns, 1)
		atomic.AddInt64(&t.TotalConns, 1)

		t.wg.Add(1)
		go t.handleReverseConnection(conn)
	}
}

// handleReverseConnection handles a single connection for reverse tunnels.
func (t *Tunnel) handleReverseConnection(remoteConn net.Conn) {
	defer t.wg.Done()
	defer remoteConn.Close()
	defer atomic.AddInt64(&t.ActiveConns, -1)

	// Connect to local target
	localAddr := fmt.Sprintf("%s:%d", t.LocalHost, t.LocalPort)
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		slog.Warn("failed to dial local",
			slog.String("id", t.ID),
			slog.String("local", localAddr),
			slog.String("error", err.Error()),
		)
		return
	}
	defer localConn.Close()

	// Proxy data bidirectionally
	t.proxy(localConn, remoteConn)
}

// proxy copies data bidirectionally between two connections.
func (t *Tunnel) proxy(conn1, conn2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// conn1 -> conn2
	go func() {
		defer wg.Done()
		n, _ := io.Copy(conn2, conn1)
		atomic.AddInt64(&t.BytesSent, n)
	}()

	// conn2 -> conn1
	go func() {
		defer wg.Done()
		n, _ := io.Copy(conn1, conn2)
		atomic.AddInt64(&t.BytesReceived, n)
	}()

	wg.Wait()
}

// Stats returns current tunnel statistics.
func (t *Tunnel) Stats() map[string]interface{} {
	return map[string]interface{}{
		"id":                 t.ID,
		"type":               t.Type,
		"local_host":         t.LocalHost,
		"local_port":         t.LocalPort,
		"remote_host":        t.RemoteHost,
		"remote_port":        t.RemotePort,
		"active_connections": atomic.LoadInt64(&t.ActiveConns),
		"total_connections":  atomic.LoadInt64(&t.TotalConns),
		"bytes_sent":         atomic.LoadInt64(&t.BytesSent),
		"bytes_received":     atomic.LoadInt64(&t.BytesReceived),
	}
}
