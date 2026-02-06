package ssh

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realclock"
	"github.com/acolita/claude-shell-mcp/internal/adapters/realsshdialer"
	"github.com/acolita/claude-shell-mcp/internal/ports"
	"golang.org/x/crypto/ssh"
)

// PoolConfig configures connection pool behavior.
type PoolConfig struct {
	// MaxConnections is the maximum number of connections per host.
	MaxConnections int
	// MinConnections is the minimum number of idle connections to maintain.
	MinConnections int
	// MaxIdleTime is how long a connection can be idle before being closed.
	MaxIdleTime time.Duration
	// ConnectionTimeout is the timeout for establishing new connections.
	ConnectionTimeout time.Duration
	// HealthCheckInterval is how often to check connection health.
	HealthCheckInterval time.Duration
}

// DefaultPoolConfig returns sensible defaults for connection pooling.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConnections:      10,
		MinConnections:      1,
		MaxIdleTime:         5 * time.Minute,
		ConnectionTimeout:   30 * time.Second,
		HealthCheckInterval: 30 * time.Second,
	}
}

// pooledConn wraps an SSH connection with pool metadata.
type pooledConn struct {
	client    *ssh.Client
	createdAt time.Time
	lastUsed  time.Time
	inUse     bool
	healthy   bool
}

// Pool manages a pool of SSH connections to a single host.
type Pool struct {
	config      PoolConfig
	clientOpts  ClientOptions
	connections []*pooledConn
	mu          sync.Mutex
	closed      bool
	done        chan struct{}
	wg          sync.WaitGroup
	clock       ports.Clock
	dialer      ports.SSHDialer
}

// NewPool creates a new connection pool for the given host.
func NewPool(clientOpts ClientOptions, config PoolConfig) *Pool {
	clk := clientOpts.Clock
	if clk == nil {
		clk = realclock.New()
	}
	dial := clientOpts.Dialer
	if dial == nil {
		dial = realsshdialer.New()
	}
	p := &Pool{
		config:      config,
		clientOpts:  clientOpts,
		connections: make([]*pooledConn, 0, config.MaxConnections),
		done:        make(chan struct{}),
		clock:       clk,
		dialer:      dial,
	}

	// Start health check goroutine
	p.wg.Add(1)
	go p.healthCheck()

	return p
}

// Get acquires a connection from the pool.
// If no idle connection is available, it creates a new one.
func (p *Pool) Get(ctx context.Context) (*ssh.Client, error) {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("pool is closed")
	}

	// Find an idle, healthy connection
	for _, conn := range p.connections {
		if !conn.inUse && conn.healthy {
			conn.inUse = true
			conn.lastUsed = p.clock.Now()
			p.mu.Unlock()

			slog.Debug("reusing pooled SSH connection",
				slog.String("host", p.clientOpts.Host),
				slog.Int("pool_size", len(p.connections)),
			)
			return conn.client, nil
		}
	}

	// Check if we can create a new connection
	if len(p.connections) >= p.config.MaxConnections {
		p.mu.Unlock()
		return nil, fmt.Errorf("connection pool exhausted (max: %d)", p.config.MaxConnections)
	}

	// Create new connection
	p.mu.Unlock()

	client, err := p.createConnection(ctx)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	now := p.clock.Now()
	conn := &pooledConn{
		client:    client,
		createdAt: now,
		lastUsed:  now,
		inUse:     true,
		healthy:   true,
	}
	p.connections = append(p.connections, conn)
	p.mu.Unlock()

	slog.Debug("created new pooled SSH connection",
		slog.String("host", p.clientOpts.Host),
		slog.Int("pool_size", len(p.connections)),
	)

	return client, nil
}

// Put returns a connection to the pool.
func (p *Pool) Put(client *ssh.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.connections {
		if conn.client == client {
			conn.inUse = false
			conn.lastUsed = p.clock.Now()
			return
		}
	}

	// Connection not in pool, close it
	client.Close()
}

// Release marks a connection as unhealthy and removes it from the pool.
func (p *Pool) Release(client *ssh.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, conn := range p.connections {
		if conn.client == client {
			conn.healthy = false
			conn.client.Close()
			// Remove from slice
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			slog.Debug("released unhealthy SSH connection",
				slog.String("host", p.clientOpts.Host),
				slog.Int("pool_size", len(p.connections)),
			)
			return
		}
	}
}

// Close closes all connections and shuts down the pool.
func (p *Pool) Close() error {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()
		return nil
	}

	p.closed = true
	close(p.done)

	// Close all connections
	for _, conn := range p.connections {
		conn.client.Close()
	}
	p.connections = nil

	p.mu.Unlock()

	// Wait for health check to finish
	p.wg.Wait()

	slog.Debug("closed SSH connection pool",
		slog.String("host", p.clientOpts.Host),
	)

	return nil
}

// Stats returns pool statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats := PoolStats{
		Total: len(p.connections),
	}

	for _, conn := range p.connections {
		if conn.inUse {
			stats.InUse++
		} else {
			stats.Idle++
		}
		if conn.healthy {
			stats.Healthy++
		}
	}

	return stats
}

// PoolStats contains pool statistics.
type PoolStats struct {
	Total   int
	InUse   int
	Idle    int
	Healthy int
}

func (p *Pool) createConnection(ctx context.Context) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            p.clientOpts.User,
		Auth:            p.clientOpts.AuthMethods,
		HostKeyCallback: p.clientOpts.HostKeyCallback,
		Timeout:         p.clientOpts.Timeout,
	}

	addr := fmt.Sprintf("%s:%d", p.clientOpts.Host, p.clientOpts.Port)

	// Create connection with context timeout
	var client *ssh.Client
	var err error

	done := make(chan struct{})
	go func() {
		client, err = p.dialer.Dial("tcp", addr, config)
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
		if err != nil {
			return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
		}
		return client, nil
	}
}

func (p *Pool) healthCheck() {
	defer p.wg.Done()

	ticker := p.clock.NewTicker(p.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C():
			p.performHealthCheck()
		}
	}
}

func (p *Pool) performHealthCheck() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.clock.Now()
	toRemove := make([]int, 0)

	for i, conn := range p.connections {
		// Skip connections in use
		if conn.inUse {
			continue
		}

		// Check for idle timeout
		if now.Sub(conn.lastUsed) > p.config.MaxIdleTime {
			// Keep minimum connections
			idleCount := p.countIdle()
			if idleCount > p.config.MinConnections {
				conn.client.Close()
				toRemove = append(toRemove, i)
				slog.Debug("closed idle SSH connection",
					slog.String("host", p.clientOpts.Host),
					slog.Duration("idle_time", now.Sub(conn.lastUsed)),
				)
				continue
			}
		}

		// Health check via keepalive
		_, _, err := conn.client.SendRequest("keepalive@openssh.com", true, nil)
		if err != nil {
			conn.healthy = false
			conn.client.Close()
			toRemove = append(toRemove, i)
			slog.Debug("removed unhealthy SSH connection",
				slog.String("host", p.clientOpts.Host),
				slog.String("error", err.Error()),
			)
		}
	}

	// Remove dead connections (in reverse order to preserve indices)
	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		p.connections = append(p.connections[:idx], p.connections[idx+1:]...)
	}
}

func (p *Pool) countIdle() int {
	count := 0
	for _, conn := range p.connections {
		if !conn.inUse {
			count++
		}
	}
	return count
}

// PoolManager manages multiple connection pools.
type PoolManager struct {
	pools  map[string]*Pool
	config PoolConfig
	mu     sync.RWMutex
}

// NewPoolManager creates a new pool manager.
func NewPoolManager(config PoolConfig) *PoolManager {
	return &PoolManager{
		pools:  make(map[string]*Pool),
		config: config,
	}
}

// GetPool returns the pool for a given host, creating it if necessary.
func (pm *PoolManager) GetPool(clientOpts ClientOptions) *Pool {
	key := fmt.Sprintf("%s@%s:%d", clientOpts.User, clientOpts.Host, clientOpts.Port)

	pm.mu.RLock()
	pool, ok := pm.pools[key]
	pm.mu.RUnlock()

	if ok {
		return pool
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, ok = pm.pools[key]; ok {
		return pool
	}

	pool = NewPool(clientOpts, pm.config)
	pm.pools[key] = pool

	slog.Debug("created new connection pool",
		slog.String("key", key),
	)

	return pool
}

// CloseAll closes all pools.
func (pm *PoolManager) CloseAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for key, pool := range pm.pools {
		pool.Close()
		delete(pm.pools, key)
	}
}

// Stats returns statistics for all pools.
func (pm *PoolManager) Stats() map[string]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[string]PoolStats)
	for key, pool := range pm.pools {
		stats[key] = pool.Stats()
	}
	return stats
}
