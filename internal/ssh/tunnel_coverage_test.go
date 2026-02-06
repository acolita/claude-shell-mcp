package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realnet"
)

// --- Fake implementations for ports.NetworkDialer and ports.NetworkListener ---

// fakeDialer implements ports.NetworkDialer and records calls.
type fakeDialer struct {
	mu       sync.Mutex
	calls    []string
	dialFunc func(network, address string) (net.Conn, error)
}

func (d *fakeDialer) Dial(network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.calls = append(d.calls, fmt.Sprintf("%s:%s", network, address))
	fn := d.dialFunc
	d.mu.Unlock()
	if fn != nil {
		return fn(network, address)
	}
	return nil, fmt.Errorf("fakeDialer: no dialFunc set")
}

// fakeListener implements ports.NetworkListener.
type fakeListener struct {
	listenFunc func(network, address string) (net.Listener, error)
}

func (l *fakeListener) Listen(network, address string) (net.Listener, error) {
	if l.listenFunc != nil {
		return l.listenFunc(network, address)
	}
	return net.Listen(network, address)
}

// failingListener always returns an error from Listen.
type failingListener struct{}

func (l *failingListener) Listen(_, address string) (net.Listener, error) {
	return nil, fmt.Errorf("listen failed on %s", address)
}

// --- Tests ---

func TestNewTunnelManager_DefaultDialerAndListener(t *testing.T) {
	tm := NewTunnelManager(nil)
	if tm == nil {
		t.Fatal("NewTunnelManager returned nil")
	}
	if tm.tunnels == nil {
		t.Fatal("tunnels map should be initialized")
	}
	if tm.dialer == nil {
		t.Fatal("dialer should have a default value")
	}
	if tm.listener == nil {
		t.Fatal("listener should have a default value")
	}
	if tm.nextID != 0 {
		t.Errorf("nextID should start at 0, got %d", tm.nextID)
	}
}

func TestNewTunnelManager_WithOptions(t *testing.T) {
	d := &fakeDialer{}
	l := &fakeListener{}

	tm := NewTunnelManager(nil, WithTunnelDialer(d), WithTunnelListener(l))
	if tm.dialer != d {
		t.Error("WithTunnelDialer did not set the dialer")
	}
	if tm.listener != l {
		t.Error("WithTunnelListener did not set the listener")
	}
}

func TestCreateLocalTunnel_Success(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))

	tunnel, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel failed: %v", err)
	}
	defer tm.CloseAll()

	if tunnel.ID == "" {
		t.Error("tunnel ID should not be empty")
	}
	if tunnel.Type != TunnelTypeLocal {
		t.Errorf("expected type %s, got %s", TunnelTypeLocal, tunnel.Type)
	}
	if tunnel.LocalHost != "127.0.0.1" {
		t.Errorf("expected LocalHost 127.0.0.1, got %s", tunnel.LocalHost)
	}
	if tunnel.LocalPort == 0 {
		t.Error("LocalPort should be assigned when 0 is passed")
	}
	if tunnel.RemoteHost != "remote.host" {
		t.Errorf("expected RemoteHost remote.host, got %s", tunnel.RemoteHost)
	}
	if tunnel.RemotePort != 8080 {
		t.Errorf("expected RemotePort 8080, got %d", tunnel.RemotePort)
	}
}

func TestCreateLocalTunnel_ListenError(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(&failingListener{}))

	_, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err == nil {
		t.Fatal("expected error when listener fails")
	}
}

func TestCreateLocalTunnel_IDAutoIncrement(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))
	defer tm.CloseAll()

	t1, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel 1 failed: %v", err)
	}
	t2, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8081)
	if err != nil {
		t.Fatalf("CreateLocalTunnel 2 failed: %v", err)
	}

	if t1.ID == t2.ID {
		t.Error("tunnel IDs should be unique")
	}
	if t1.ID != "tunnel_1" {
		t.Errorf("first tunnel ID should be tunnel_1, got %s", t1.ID)
	}
	if t2.ID != "tunnel_2" {
		t.Errorf("second tunnel ID should be tunnel_2, got %s", t2.ID)
	}
}

func TestGetTunnel_Found(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))
	defer tm.CloseAll()

	created, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel failed: %v", err)
	}

	found, ok := tm.GetTunnel(created.ID)
	if !ok {
		t.Fatal("GetTunnel should find the created tunnel")
	}
	if found.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, found.ID)
	}
}

func TestGetTunnel_NotFound(t *testing.T) {
	tm := NewTunnelManager(nil)

	_, ok := tm.GetTunnel("nonexistent")
	if ok {
		t.Error("GetTunnel should return false for non-existent tunnel")
	}
}

func TestListTunnels_Empty(t *testing.T) {
	tm := NewTunnelManager(nil)

	tunnels := tm.ListTunnels()
	if len(tunnels) != 0 {
		t.Errorf("expected 0 tunnels, got %d", len(tunnels))
	}
}

func TestListTunnels_Multiple(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))
	defer tm.CloseAll()

	_, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote1.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel 1 failed: %v", err)
	}
	_, err = tm.CreateLocalTunnel("127.0.0.1", 0, "remote2.host", 9090)
	if err != nil {
		t.Fatalf("CreateLocalTunnel 2 failed: %v", err)
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) != 2 {
		t.Errorf("expected 2 tunnels, got %d", len(tunnels))
	}
}

func TestCloseTunnel_Success(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))

	tunnel, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel failed: %v", err)
	}

	err = tm.CloseTunnel(tunnel.ID)
	if err != nil {
		t.Fatalf("CloseTunnel failed: %v", err)
	}

	// Should no longer be in the list
	_, ok := tm.GetTunnel(tunnel.ID)
	if ok {
		t.Error("tunnel should be removed after CloseTunnel")
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) != 0 {
		t.Errorf("expected 0 tunnels after close, got %d", len(tunnels))
	}
}

func TestCloseTunnel_NotFound(t *testing.T) {
	tm := NewTunnelManager(nil)

	err := tm.CloseTunnel("nonexistent")
	if err == nil {
		t.Error("CloseTunnel should return error for non-existent tunnel")
	}
	if err.Error() != "tunnel not found: nonexistent" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestCloseAll_Empty(t *testing.T) {
	tm := NewTunnelManager(nil)
	// Should not panic
	tm.CloseAll()

	tunnels := tm.ListTunnels()
	if len(tunnels) != 0 {
		t.Errorf("expected 0 tunnels after CloseAll, got %d", len(tunnels))
	}
}

func TestCloseAll_Multiple(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))

	_, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote1.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel 1 failed: %v", err)
	}
	_, err = tm.CreateLocalTunnel("127.0.0.1", 0, "remote2.host", 9090)
	if err != nil {
		t.Fatalf("CreateLocalTunnel 2 failed: %v", err)
	}

	tm.CloseAll()

	tunnels := tm.ListTunnels()
	if len(tunnels) != 0 {
		t.Errorf("expected 0 tunnels after CloseAll, got %d", len(tunnels))
	}
}

func TestTunnel_Stats(t *testing.T) {
	tunnel := &Tunnel{
		ID:         "tunnel_test",
		Type:       TunnelTypeLocal,
		LocalHost:  "127.0.0.1",
		LocalPort:  12345,
		RemoteHost: "remote.host",
		RemotePort: 8080,
	}

	// Set some counters
	atomic.StoreInt64(&tunnel.ActiveConns, 3)
	atomic.StoreInt64(&tunnel.TotalConns, 10)
	atomic.StoreInt64(&tunnel.BytesSent, 1024)
	atomic.StoreInt64(&tunnel.BytesReceived, 2048)

	stats := tunnel.Stats()

	if stats["id"] != "tunnel_test" {
		t.Errorf("expected id tunnel_test, got %v", stats["id"])
	}
	if stats["type"] != TunnelTypeLocal {
		t.Errorf("expected type local, got %v", stats["type"])
	}
	if stats["local_host"] != "127.0.0.1" {
		t.Errorf("expected local_host 127.0.0.1, got %v", stats["local_host"])
	}
	if stats["local_port"] != 12345 {
		t.Errorf("expected local_port 12345, got %v", stats["local_port"])
	}
	if stats["remote_host"] != "remote.host" {
		t.Errorf("expected remote_host remote.host, got %v", stats["remote_host"])
	}
	if stats["remote_port"] != 8080 {
		t.Errorf("expected remote_port 8080, got %v", stats["remote_port"])
	}
	if stats["active_connections"].(int64) != 3 {
		t.Errorf("expected active_connections 3, got %v", stats["active_connections"])
	}
	if stats["total_connections"].(int64) != 10 {
		t.Errorf("expected total_connections 10, got %v", stats["total_connections"])
	}
	if stats["bytes_sent"].(int64) != 1024 {
		t.Errorf("expected bytes_sent 1024, got %v", stats["bytes_sent"])
	}
	if stats["bytes_received"].(int64) != 2048 {
		t.Errorf("expected bytes_received 2048, got %v", stats["bytes_received"])
	}
}

func TestTunnel_Stats_ZeroValues(t *testing.T) {
	tunnel := &Tunnel{
		ID:         "tunnel_zero",
		Type:       TunnelTypeReverse,
		LocalHost:  "0.0.0.0",
		LocalPort:  0,
		RemoteHost: "",
		RemotePort: 0,
	}

	stats := tunnel.Stats()

	if stats["active_connections"].(int64) != 0 {
		t.Errorf("expected 0 active_connections, got %v", stats["active_connections"])
	}
	if stats["total_connections"].(int64) != 0 {
		t.Errorf("expected 0 total_connections, got %v", stats["total_connections"])
	}
	if stats["bytes_sent"].(int64) != 0 {
		t.Errorf("expected 0 bytes_sent, got %v", stats["bytes_sent"])
	}
	if stats["bytes_received"].(int64) != 0 {
		t.Errorf("expected 0 bytes_received, got %v", stats["bytes_received"])
	}
}

func TestTunnel_Close_DirectCall(t *testing.T) {
	// Create a real listener for the tunnel
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &Tunnel{
		ID:       "tunnel_direct_close",
		Type:     TunnelTypeLocal,
		listener: lis,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start a goroutine that simulates acceptLocal (blocked on Accept)
	tunnel.wg.Add(1)
	go func() {
		defer tunnel.wg.Done()
		for {
			_, acceptErr := tunnel.listener.Accept()
			if acceptErr != nil {
				return
			}
		}
	}()

	// Close should cancel context, close listener, and wait for goroutine
	tunnel.Close()

	// Verify context is cancelled
	select {
	case <-ctx.Done():
		// expected
	default:
		t.Error("context should be cancelled after Close")
	}
}

func TestTunnel_Close_NilListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &Tunnel{
		ID:       "tunnel_nil_listener",
		Type:     TunnelTypeLocal,
		listener: nil,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Should not panic with nil listener
	tunnel.Close()

	select {
	case <-ctx.Done():
		// expected
	default:
		t.Error("context should be cancelled after Close")
	}
}

func TestTunnel_Proxy(t *testing.T) {
	// proxy(conn1, conn2) copies conn1->conn2 (BytesSent) and conn2->conn1 (BytesReceived).
	// Use net.Pipe() pairs. Each pipe is synchronous: write blocks until read.
	client1, server1 := net.Pipe()
	client2, server2 := net.Pipe()

	tunnel := &Tunnel{
		ID:   "tunnel_proxy_test",
		Type: TunnelTypeLocal,
	}

	testData := []byte("hello from conn1 to conn2")

	var proxyWg sync.WaitGroup
	proxyWg.Add(1)
	go func() {
		defer proxyWg.Done()
		tunnel.proxy(server1, server2)
	}()

	// Write testData into client1 (server1 reads it, proxy copies to server2, client2 can read).
	// Then close client1 to signal EOF to proxy's conn1->conn2 goroutine.
	go func() {
		client1.Write(testData)
		client1.Close()
	}()

	// Read the forwarded data that comes out on client2
	buf := make([]byte, 1024)
	n, err := io.ReadFull(client2, buf[:len(testData)])
	if err != nil {
		t.Fatalf("failed to read forwarded data from client2: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("expected %q, got %q", testData, buf[:n])
	}

	// Close client2 to let proxy's conn2->conn1 goroutine finish with EOF
	client2.Close()

	proxyWg.Wait()

	sent := atomic.LoadInt64(&tunnel.BytesSent)
	if sent != int64(len(testData)) {
		t.Errorf("expected BytesSent=%d, got %d", len(testData), sent)
	}
}

func TestTunnel_Proxy_BidirectionalData(t *testing.T) {
	// Test that data flows in both directions through proxy
	client1, server1 := net.Pipe()
	client2, server2 := net.Pipe()

	tunnel := &Tunnel{
		ID:   "tunnel_proxy_bidir",
		Type: TunnelTypeLocal,
	}

	dataFromConn1 := []byte("data-from-1")
	dataFromConn2 := []byte("data-from-2")

	var proxyWg sync.WaitGroup
	proxyWg.Add(1)
	go func() {
		defer proxyWg.Done()
		tunnel.proxy(server1, server2)
	}()

	// Writer goroutine: write to client1 then close
	go func() {
		client1.Write(dataFromConn1)
		// Read what arrives from the proxy (dataFromConn2 flows conn2->conn1)
		buf := make([]byte, 1024)
		io.ReadFull(client1, buf[:len(dataFromConn2)])
		client1.Close()
	}()

	// Writer goroutine: write to client2 then close
	go func() {
		client2.Write(dataFromConn2)
		// Read what arrives from the proxy (dataFromConn1 flows conn1->conn2)
		buf := make([]byte, 1024)
		io.ReadFull(client2, buf[:len(dataFromConn1)])
		client2.Close()
	}()

	proxyWg.Wait()

	sent := atomic.LoadInt64(&tunnel.BytesSent)
	received := atomic.LoadInt64(&tunnel.BytesReceived)
	if sent != int64(len(dataFromConn1)) {
		t.Errorf("expected BytesSent=%d, got %d", len(dataFromConn1), sent)
	}
	if received != int64(len(dataFromConn2)) {
		t.Errorf("expected BytesReceived=%d, got %d", len(dataFromConn2), received)
	}
}

func TestTunnel_Proxy_EmptyData(t *testing.T) {
	client1, server1 := net.Pipe()
	client2, server2 := net.Pipe()

	tunnel := &Tunnel{
		ID:   "tunnel_proxy_empty",
		Type: TunnelTypeLocal,
	}

	var proxyWg sync.WaitGroup
	proxyWg.Add(1)
	go func() {
		defer proxyWg.Done()
		tunnel.proxy(server1, server2)
	}()

	// Close both immediately without writing
	client1.Close()
	client2.Close()

	proxyWg.Wait()

	sent := atomic.LoadInt64(&tunnel.BytesSent)
	received := atomic.LoadInt64(&tunnel.BytesReceived)
	if sent != 0 {
		t.Errorf("expected BytesSent=0, got %d", sent)
	}
	if received != 0 {
		t.Errorf("expected BytesReceived=0, got %d", received)
	}
}

func TestTunnel_AcceptLocal_ContextCancel(t *testing.T) {
	// Create a real listener
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &Tunnel{
		ID:       "tunnel_accept_cancel",
		Type:     TunnelTypeLocal,
		listener: lis,
		ctx:      ctx,
		cancel:   cancel,
	}

	tunnel.wg.Add(1)
	go tunnel.acceptLocal()

	// Give the goroutine time to start blocking on Accept
	time.Sleep(50 * time.Millisecond)

	// Cancel context and close listener
	cancel()
	lis.Close()

	// Wait for acceptLocal to return
	done := make(chan struct{})
	go func() {
		tunnel.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("acceptLocal did not return after context cancel and listener close")
	}
}

func TestTunnel_AcceptReverse_ContextCancel(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &Tunnel{
		ID:       "tunnel_accept_reverse_cancel",
		Type:     TunnelTypeReverse,
		listener: lis,
		ctx:      ctx,
		cancel:   cancel,
		dialer:   &fakeDialer{},
	}

	tunnel.wg.Add(1)
	go tunnel.acceptReverse()

	time.Sleep(50 * time.Millisecond)

	cancel()
	lis.Close()

	done := make(chan struct{})
	go func() {
		tunnel.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("acceptReverse did not return after context cancel and listener close")
	}
}

func TestTunnel_HandleReverseConnection_DialError(t *testing.T) {
	// Create a listener and connect to it to get a net.Conn
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer lis.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fd := &fakeDialer{
		dialFunc: func(network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	tunnel := &Tunnel{
		ID:         "tunnel_reverse_dial_err",
		Type:       TunnelTypeReverse,
		LocalHost:  "127.0.0.1",
		LocalPort:  9999,
		RemoteHost: "remote.host",
		RemotePort: 8080,
		dialer:     fd,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Create a connection to pass to handleReverseConnection
	connCh := make(chan net.Conn, 1)
	go func() {
		c, _ := lis.Accept()
		connCh <- c
	}()

	clientConn, err := net.Dial("tcp", lis.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial listener: %v", err)
	}
	defer clientConn.Close()

	connToUse := <-connCh

	// Set ActiveConns to 1 to simulate the increment in accept loop
	atomic.StoreInt64(&tunnel.ActiveConns, 1)

	tunnel.wg.Add(1)
	tunnel.handleReverseConnection(connToUse)

	// After dial error, ActiveConns should be decremented
	if atomic.LoadInt64(&tunnel.ActiveConns) != 0 {
		t.Errorf("expected ActiveConns=0 after dial error, got %d", atomic.LoadInt64(&tunnel.ActiveConns))
	}
}

func TestTunnel_HandleReverseConnection_Success(t *testing.T) {
	// Use a fake dialer that returns a pipe, so we can control both ends.
	localClient, localServer := net.Pipe()

	fd := &fakeDialer{
		dialFunc: func(network, address string) (net.Conn, error) {
			return localServer, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:         "tunnel_reverse_success",
		Type:       TunnelTypeReverse,
		LocalHost:  "127.0.0.1",
		LocalPort:  3000,
		RemoteHost: "remote.host",
		RemotePort: 8080,
		dialer:     fd,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Create a pipe to simulate the remote connection
	remoteClient, remoteServer := net.Pipe()

	atomic.StoreInt64(&tunnel.ActiveConns, 1)

	tunnel.wg.Add(1)
	go tunnel.handleReverseConnection(remoteServer)

	// Data flow: remoteClient -> remoteServer -> proxy -> localServer -> localClient
	testData := []byte("reverse tunnel test data")

	// Write from remote side and close to signal EOF
	go func() {
		remoteClient.Write(testData)
		remoteClient.Close()
	}()

	// Read what arrives at the local target
	buf := make([]byte, 1024)
	n, err := io.ReadFull(localClient, buf[:len(testData)])
	if err != nil {
		t.Fatalf("failed to read from local target: %v", err)
	}
	if string(buf[:n]) != string(testData) {
		t.Errorf("local target received %q, expected %q", buf[:n], testData)
	}

	// Close local to let the reverse direction finish
	localClient.Close()

	// Wait for handleReverseConnection to complete
	done := make(chan struct{})
	go func() {
		tunnel.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handleReverseConnection timed out")
	}

	// ActiveConns should be back to 0
	if atomic.LoadInt64(&tunnel.ActiveConns) != 0 {
		t.Errorf("expected ActiveConns=0 after connection closed, got %d", atomic.LoadInt64(&tunnel.ActiveConns))
	}

	// Verify bytes were tracked
	if atomic.LoadInt64(&tunnel.BytesReceived) != int64(len(testData)) {
		t.Errorf("expected BytesReceived=%d, got %d", len(testData), atomic.LoadInt64(&tunnel.BytesReceived))
	}
}

func TestTunnelManager_ConcurrentAccess(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))
	defer tm.CloseAll()

	const numGoroutines = 20
	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*3)

	// Concurrent tunnel creation
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
			if err != nil {
				errCh <- fmt.Errorf("create: %w", err)
			}
		}()
	}
	wg.Wait()

	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) != numGoroutines {
		t.Errorf("expected %d tunnels, got %d", numGoroutines, len(tunnels))
	}

	// Concurrent GetTunnel and ListTunnels
	errCh2 := make(chan error, numGoroutines*2)
	wg.Add(numGoroutines * 2)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_ = tm.ListTunnels()
		}()
		go func(id string) {
			defer wg.Done()
			_, ok := tm.GetTunnel(id)
			if !ok {
				errCh2 <- fmt.Errorf("tunnel %s not found", id)
			}
		}(tunnels[i].ID)
	}
	wg.Wait()
	close(errCh2)
	for err := range errCh2 {
		t.Errorf("concurrent read error: %v", err)
	}
}

func TestTunnelManager_ConcurrentCloseAll(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))

	for i := 0; i < 5; i++ {
		_, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
		if err != nil {
			t.Fatalf("CreateLocalTunnel %d failed: %v", i, err)
		}
	}

	// Multiple concurrent CloseAll calls should not panic
	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			tm.CloseAll()
		}()
	}
	wg.Wait()

	if len(tm.ListTunnels()) != 0 {
		t.Error("expected 0 tunnels after concurrent CloseAll")
	}
}

func TestTunnelType_Constants(t *testing.T) {
	if TunnelTypeLocal != "local" {
		t.Errorf("expected TunnelTypeLocal to be 'local', got %q", TunnelTypeLocal)
	}
	if TunnelTypeReverse != "reverse" {
		t.Errorf("expected TunnelTypeReverse to be 'reverse', got %q", TunnelTypeReverse)
	}
}

func TestCreateLocalTunnel_ListenerAndStorage(t *testing.T) {
	// Verify that CreateLocalTunnel stores the tunnel with a working listener.
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))
	defer tm.CloseAll()

	tunnel, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel failed: %v", err)
	}

	// Verify tunnel is stored in the manager
	found, ok := tm.GetTunnel(tunnel.ID)
	if !ok {
		t.Fatal("tunnel should be found in manager")
	}
	if found.listener == nil {
		t.Error("tunnel should have a non-nil listener")
	}
	if found.sshClient != nil {
		t.Error("sshClient should be nil for this test")
	}
	// Verify listener has a valid address
	lisAddr := found.listener.Addr()
	if lisAddr == nil {
		t.Error("listener address should not be nil")
	}
}

func TestTunnel_AcceptReverse_ProcessesConnections(t *testing.T) {
	// Create a listener that simulates the remote SSH listener
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	fd := &fakeDialer{
		dialFunc: func(network, address string) (net.Conn, error) {
			return nil, fmt.Errorf("cannot connect")
		},
	}

	tunnel := &Tunnel{
		ID:         "tunnel_reverse_accept_test",
		Type:       TunnelTypeReverse,
		LocalHost:  "127.0.0.1",
		LocalPort:  9999,
		RemoteHost: "remote.host",
		RemotePort: 8080,
		listener:   lis,
		dialer:     fd,
		ctx:        ctx,
		cancel:     cancel,
	}

	tunnel.wg.Add(1)
	go tunnel.acceptReverse()

	// Connect to the listener to trigger accept
	conn, err := net.DialTimeout("tcp", lis.Addr().String(), 1*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to reverse tunnel listener: %v", err)
	}
	conn.Close()

	// Give time for the accept loop to process
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&tunnel.TotalConns) < 1 {
		t.Error("expected TotalConns >= 1 after connection")
	}

	// Cleanup
	cancel()
	lis.Close()

	done := make(chan struct{})
	go func() {
		tunnel.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acceptReverse did not finish")
	}
}

func TestCloseTunnel_ThenGetTunnel(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))

	t1, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel failed: %v", err)
	}
	t2, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 9090)
	if err != nil {
		t.Fatalf("CreateLocalTunnel 2 failed: %v", err)
	}

	// Close the first tunnel
	err = tm.CloseTunnel(t1.ID)
	if err != nil {
		t.Fatalf("CloseTunnel failed: %v", err)
	}

	// First should be gone
	_, ok := tm.GetTunnel(t1.ID)
	if ok {
		t.Error("closed tunnel should not be found")
	}

	// Second should still be present
	_, ok = tm.GetTunnel(t2.ID)
	if !ok {
		t.Error("second tunnel should still be present")
	}

	tm.CloseAll()
}

func TestCreateLocalTunnel_SpecificPort(t *testing.T) {
	// Find a free port first
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	lis.Close()

	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))
	defer tm.CloseAll()

	tunnel, err := tm.CreateLocalTunnel("127.0.0.1", port, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel with specific port failed: %v", err)
	}

	if tunnel.LocalPort != port {
		t.Errorf("expected LocalPort %d, got %d", port, tunnel.LocalPort)
	}
}

func TestTunnel_Stats_AllFields(t *testing.T) {
	tunnel := &Tunnel{
		ID:         "stats_test",
		Type:       TunnelTypeReverse,
		LocalHost:  "10.0.0.1",
		LocalPort:  3000,
		RemoteHost: "0.0.0.0",
		RemotePort: 8080,
	}

	stats := tunnel.Stats()

	// Verify all expected keys are present
	expectedKeys := []string{
		"id", "type", "local_host", "local_port",
		"remote_host", "remote_port", "active_connections",
		"total_connections", "bytes_sent", "bytes_received",
	}
	for _, key := range expectedKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("Stats() missing key %q", key)
		}
	}

	// Verify there are exactly the expected number of keys
	if len(stats) != len(expectedKeys) {
		t.Errorf("expected %d keys in Stats(), got %d", len(expectedKeys), len(stats))
	}
}

func TestCloseAll_AfterSomeClosed(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))

	t1, _ := tm.CreateLocalTunnel("127.0.0.1", 0, "r1.host", 8080)
	tm.CreateLocalTunnel("127.0.0.1", 0, "r2.host", 8081)
	tm.CreateLocalTunnel("127.0.0.1", 0, "r3.host", 8082)

	// Close one manually
	tm.CloseTunnel(t1.ID)

	if len(tm.ListTunnels()) != 2 {
		t.Fatalf("expected 2 tunnels before CloseAll, got %d", len(tm.ListTunnels()))
	}

	// CloseAll should handle the remaining
	tm.CloseAll()

	if len(tm.ListTunnels()) != 0 {
		t.Errorf("expected 0 tunnels after CloseAll, got %d", len(tm.ListTunnels()))
	}
}

func TestTunnel_Proxy_LargeData(t *testing.T) {
	client1, server1 := net.Pipe()
	client2, server2 := net.Pipe()

	tunnel := &Tunnel{
		ID:   "tunnel_proxy_large",
		Type: TunnelTypeLocal,
	}

	// Generate a larger payload (~100KB)
	largeData := make([]byte, 100*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	var proxyWg sync.WaitGroup
	proxyWg.Add(1)
	go func() {
		defer proxyWg.Done()
		tunnel.proxy(server1, server2)
	}()

	// Send data from client1 side and close to signal EOF
	go func() {
		client1.Write(largeData)
		client1.Close()
	}()

	// Read the exact expected number of bytes from client2 (using ReadFull, not ReadAll,
	// because ReadAll would deadlock waiting for EOF which won't come until proxy finishes).
	received := make([]byte, len(largeData))
	n, err := io.ReadFull(client2, received)
	if err != nil {
		t.Fatalf("error reading from client2: %v", err)
	}
	// Close client2 to let the reverse direction goroutine finish
	client2.Close()

	proxyWg.Wait()

	if n != len(largeData) {
		t.Errorf("expected %d bytes, received %d bytes", len(largeData), n)
	}

	sent := atomic.LoadInt64(&tunnel.BytesSent)
	if sent != int64(len(largeData)) {
		t.Errorf("expected BytesSent=%d, got %d", len(largeData), sent)
	}
}

func TestWithTunnelDialer_OverridesDefault(t *testing.T) {
	d := &fakeDialer{}
	tm := NewTunnelManager(nil, WithTunnelDialer(d))

	if tm.dialer != d {
		t.Error("WithTunnelDialer should override the default dialer")
	}
}

func TestWithTunnelListener_OverridesDefault(t *testing.T) {
	l := &fakeListener{}
	tm := NewTunnelManager(nil, WithTunnelListener(l))

	if tm.listener != l {
		t.Error("WithTunnelListener should override the default listener")
	}
}

func TestCloseTunnel_DoubleClosed(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))

	tunnel, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel failed: %v", err)
	}

	err = tm.CloseTunnel(tunnel.ID)
	if err != nil {
		t.Fatalf("first CloseTunnel failed: %v", err)
	}

	// Second close should fail with not found
	err = tm.CloseTunnel(tunnel.ID)
	if err == nil {
		t.Error("second CloseTunnel should return error")
	}
}

func TestListTunnels_ReturnsNewSlice(t *testing.T) {
	tm := NewTunnelManager(nil, WithTunnelListener(realnet.NewListener()))
	defer tm.CloseAll()

	_, err := tm.CreateLocalTunnel("127.0.0.1", 0, "remote.host", 8080)
	if err != nil {
		t.Fatalf("CreateLocalTunnel failed: %v", err)
	}

	list1 := tm.ListTunnels()
	list2 := tm.ListTunnels()

	// They should be separate slices (modifying one doesn't affect the other)
	if len(list1) != 1 || len(list2) != 1 {
		t.Fatalf("expected 1 tunnel in each list")
	}

	// Modify list1 and check list2 is unaffected
	list1[0] = nil
	if list2[0] == nil {
		t.Error("ListTunnels should return independent slices")
	}
}

func TestTunnel_AcceptLocal_AcceptError_WithoutContextCancel(t *testing.T) {
	// Test the "default" branch in acceptLocal's select when Accept fails
	// without context being cancelled.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:       "tunnel_accept_err",
		Type:     TunnelTypeLocal,
		listener: lis,
		ctx:      ctx,
		cancel:   cancel,
	}

	tunnel.wg.Add(1)

	// Close the listener to make Accept fail, but don't cancel context
	lis.Close()

	// acceptLocal should return via the default branch (not ctx.Done)
	done := make(chan struct{})
	go func() {
		tunnel.acceptLocal()
		close(done)
	}()

	select {
	case <-done:
		// success - acceptLocal returned due to accept error
	case <-time.After(2 * time.Second):
		t.Fatal("acceptLocal did not return after listener close")
	}
}

func TestTunnel_AcceptReverse_AcceptError_WithoutContextCancel(t *testing.T) {
	// Test the "default" branch in acceptReverse's select when Accept fails
	// without context being cancelled.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := &Tunnel{
		ID:       "tunnel_accept_reverse_err",
		Type:     TunnelTypeReverse,
		listener: lis,
		ctx:      ctx,
		cancel:   cancel,
		dialer:   &fakeDialer{},
	}

	tunnel.wg.Add(1)

	// Close the listener to make Accept fail, but don't cancel context
	lis.Close()

	done := make(chan struct{})
	go func() {
		tunnel.acceptReverse()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("acceptReverse did not return after listener close")
	}
}
