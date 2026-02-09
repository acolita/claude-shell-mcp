package ssh

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesshdialer"
	gossh "golang.org/x/crypto/ssh"
)

// fakeSSHConn implements ssh.Conn using a net.Conn for safe Close() behavior.
type fakeSSHConn struct {
	net.Conn
}

func (f *fakeSSHConn) User() string          { return "test" }
func (f *fakeSSHConn) SessionID() []byte     { return []byte("fake") }
func (f *fakeSSHConn) ClientVersion() []byte { return []byte("SSH-2.0-test") }
func (f *fakeSSHConn) ServerVersion() []byte { return []byte("SSH-2.0-test") }
func (f *fakeSSHConn) RemoteAddr() net.Addr  { return f.Conn.RemoteAddr() }
func (f *fakeSSHConn) LocalAddr() net.Addr   { return f.Conn.LocalAddr() }
func (f *fakeSSHConn) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return false, nil, nil
}
func (f *fakeSSHConn) OpenChannel(name string, data []byte) (gossh.Channel, <-chan *gossh.Request, error) {
	return nil, nil, fmt.Errorf("not supported")
}
func (f *fakeSSHConn) Wait() error { return nil }

// newFakeSSHClient creates a *ssh.Client that can be safely closed.
// Returns the client and a cleanup function to close the underlying pipe.
func newFakeSSHClient() (*gossh.Client, func()) {
	c1, c2 := net.Pipe()
	chans := make(chan gossh.NewChannel)
	reqs := make(chan *gossh.Request)
	close(chans)
	close(reqs)
	conn := &fakeSSHConn{Conn: c1}
	client := gossh.NewClient(conn, chans, reqs)
	cleanup := func() {
		client.Close()
		c2.Close()
	}
	return client, cleanup
}

// newTestPool creates a Pool with injected fakes for deterministic testing.
func newTestPool(clk *fakeclock.Clock, dialer *fakesshdialer.Dialer, config PoolConfig) *Pool {
	p := &Pool{
		config: config,
		clientOpts: ClientOptions{
			Host:   "test.example.com",
			Port:   22,
			User:   "testuser",
			Clock:  clk,
			Dialer: dialer,
		},
		connections: make([]*pooledConn, 0, config.MaxConnections),
		done:        make(chan struct{}),
		clock:       clk,
		dialer:      dialer,
	}
	// Start health check goroutine like NewPool does.
	p.wg.Add(1)
	go p.healthCheck()
	return p
}

func defaultTestPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConnections:      5,
		MinConnections:      1,
		MaxIdleTime:         5 * time.Minute,
		ConnectionTimeout:   10 * time.Second,
		HealthCheckInterval: 1 * time.Hour, // large to avoid interference
	}
}

// --- Put tests ---

func TestPool_PutReturnsConnectionToPool(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	ctx := context.Background()
	client, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	stats := pool.Stats()
	if stats.InUse != 1 {
		t.Errorf("after Get: InUse = %d, want 1", stats.InUse)
	}
	if stats.Idle != 0 {
		t.Errorf("after Get: Idle = %d, want 0", stats.Idle)
	}

	pool.Put(client)

	stats = pool.Stats()
	if stats.Idle != 1 {
		t.Errorf("after Put: Idle = %d, want 1", stats.Idle)
	}
	if stats.InUse != 0 {
		t.Errorf("after Put: InUse = %d, want 0", stats.InUse)
	}
}

func TestPool_PutUnknownClientClosesIt(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	// Put a client that was never obtained from the pool.
	unknownClient, cleanup := newFakeSSHClient()
	defer cleanup()

	pool.Put(unknownClient)

	stats := pool.Stats()
	if stats.Total != 0 {
		t.Errorf("pool should have 0 connections after Put of unknown client, got %d", stats.Total)
	}
}

// --- Release tests ---

func TestPool_ReleaseRemovesConnection(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	ctx := context.Background()
	client, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if pool.Stats().Total != 1 {
		t.Fatalf("expected 1 total connection, got %d", pool.Stats().Total)
	}

	pool.Release(client)

	stats := pool.Stats()
	if stats.Total != 0 {
		t.Errorf("after Release: Total = %d, want 0", stats.Total)
	}
}

func TestPool_ReleaseUnknownClientIsNoOp(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	unknownClient, cleanup := newFakeSSHClient()
	defer cleanup()

	pool.Release(unknownClient)

	stats := pool.Stats()
	if stats.Total != 0 {
		t.Errorf("Total = %d, want 0 after releasing unknown client", stats.Total)
	}
}

// --- countIdle tests (exercised through Stats and Get reuse) ---

func TestPool_CountIdleWithMixedConnections(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	// Return a different *ssh.Client for each dial call.
	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		client, cleanup := newFakeSSHClient()
		cleanups = append(cleanups, cleanup)
		return client, nil
	})

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	ctx := context.Background()

	c1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() #1 error = %v", err)
	}
	_, err = pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() #2 error = %v", err)
	}

	// Put back only the first one.
	pool.Put(c1)

	stats := pool.Stats()
	if stats.Total != 2 {
		t.Errorf("Total = %d, want 2", stats.Total)
	}
	if stats.Idle != 1 {
		t.Errorf("Idle = %d, want 1", stats.Idle)
	}
	if stats.InUse != 1 {
		t.Errorf("InUse = %d, want 1", stats.InUse)
	}
}

// --- Get with idle connection reuse ---

func TestPool_GetReusesIdleConnection(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	ctx := context.Background()

	client1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() #1 error = %v", err)
	}
	pool.Put(client1)

	client2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() #2 error = %v", err)
	}

	if client1 != client2 {
		t.Error("Get() should return the same idle connection, got a different one")
	}

	if len(dialer.Calls()) != 1 {
		t.Errorf("expected 1 dial call (reuse), got %d", len(dialer.Calls()))
	}

	stats := pool.Stats()
	if stats.Total != 1 {
		t.Errorf("Total = %d, want 1 (reused connection)", stats.Total)
	}
}

func TestPool_GetPoolExhausted(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		client, cleanup := newFakeSSHClient()
		cleanups = append(cleanups, cleanup)
		return client, nil
	})

	config := PoolConfig{
		MaxConnections:      2,
		MinConnections:      1,
		MaxIdleTime:         5 * time.Minute,
		ConnectionTimeout:   10 * time.Second,
		HealthCheckInterval: 1 * time.Hour,
	}

	pool := newTestPool(clk, dialer, config)
	defer pool.Close()

	ctx := context.Background()

	_, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() #1 error = %v", err)
	}
	_, err = pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() #2 error = %v", err)
	}

	_, err = pool.Get(ctx)
	if err == nil {
		t.Fatal("expected error when pool is exhausted")
	}
	if err.Error() != fmt.Sprintf("connection pool exhausted (max: %d)", 2) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPool_GetDialError(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()
	dialer.SetError(fmt.Errorf("connection refused"))

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	ctx := context.Background()
	_, err := pool.Get(ctx)
	if err == nil {
		t.Fatal("expected error from Get when dialer fails")
	}
	if pool.Stats().Total != 0 {
		t.Errorf("Total = %d, want 0 after failed Get", pool.Stats().Total)
	}
}

func TestPool_GetContextCancelled(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	dialStarted := make(chan struct{})
	dialDone := make(chan struct{})
	defer close(dialDone)
	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		close(dialStarted)
		// Block until test cleanup - context cancellation should unblock Get.
		<-dialDone
		return nil, fmt.Errorf("dial cancelled")
	})

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := pool.Get(ctx)
		errCh <- err
	}()

	<-dialStarted
	cancel()

	err := <-errCh
	if err == nil {
		t.Fatal("expected error from Get when context is cancelled")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- Stats with healthy tracking ---

func TestPool_StatsHealthyCount(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())
	defer pool.Close()

	ctx := context.Background()
	_, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	stats := pool.Stats()
	if stats.Healthy != 1 {
		t.Errorf("Healthy = %d, want 1", stats.Healthy)
	}
	if stats.Total != 1 {
		t.Errorf("Total = %d, want 1", stats.Total)
	}
}

// --- NewPool with injected clock/dialer ---

func TestPool_NewPoolUsesInjectedClockAndDialer(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	opts := ClientOptions{
		Host:   "test.example.com",
		Port:   22,
		User:   "testuser",
		Clock:  clk,
		Dialer: dialer,
	}
	config := DefaultPoolConfig()

	pool := NewPool(opts, config)
	defer pool.Close()

	if pool.clock != clk {
		t.Error("pool should use the injected clock")
	}
	if pool.dialer != dialer {
		t.Error("pool should use the injected dialer")
	}
}

// --- Pool.Close with connections ---

func TestPool_CloseWithActiveConnections(t *testing.T) {
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	dialer := fakesshdialer.New()

	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		client, cleanup := newFakeSSHClient()
		cleanups = append(cleanups, cleanup)
		return client, nil
	})

	pool := newTestPool(clk, dialer, defaultTestPoolConfig())

	ctx := context.Background()
	_, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	_, err = pool.Get(ctx)
	if err != nil {
		t.Fatalf("Get() #2 error = %v", err)
	}

	if pool.Stats().Total != 2 {
		t.Fatalf("expected 2 connections before Close, got %d", pool.Stats().Total)
	}

	err = pool.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !pool.closed {
		t.Error("pool should be marked as closed")
	}
}
