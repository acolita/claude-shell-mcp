package ssh

import (
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesshdialer"
	gossh "golang.org/x/crypto/ssh"
)

// TestClient_ConnectSuccess tests a successful connection using a fake dialer.
func TestClient_ConnectSuccess(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "test.example.com",
		port:              2222,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	err := client.Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if !client.IsConnected() {
		t.Error("expected IsConnected() to be true after Connect")
	}

	// Verify the dialer was called with correct address
	calls := dialer.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 dial call, got %d", len(calls))
	}
	if calls[0].Addr != "test.example.com:2222" {
		t.Errorf("expected addr test.example.com:2222, got %s", calls[0].Addr)
	}

	// Clean up
	client.Close()
}

// TestClient_ConnectAlreadyConnected tests that Connect is a no-op when already connected.
func TestClient_ConnectAlreadyConnected(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	// Connect first time
	if err := client.Connect(); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}

	// Connect again - should be a no-op
	if err := client.Connect(); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}

	// Only one dial call should have happened
	if len(dialer.Calls()) != 1 {
		t.Errorf("expected 1 dial call, got %d", len(dialer.Calls()))
	}

	client.Close()
}

// TestClient_CloseWithKeepalive tests that Close stops the keepalive goroutine.
func TestClient_CloseWithKeepalive(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if client.keepaliveStop == nil {
		t.Fatal("keepaliveStop channel should be initialized after Connect")
	}

	err := client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if client.keepaliveStop != nil {
		t.Error("keepaliveStop should be nil after Close")
	}
	if client.conn != nil {
		t.Error("conn should be nil after Close")
	}
}

// TestClient_CloseWithTunnelManager tests that Close closes the tunnel manager.
func TestClient_CloseWithTunnelManager(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Create tunnel manager (lazy init)
	tm := client.TunnelManager()
	if tm == nil {
		t.Fatal("TunnelManager should not be nil when connected")
	}

	// Close should clean up the tunnel manager
	err := client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if client.tunnelManager != nil {
		t.Error("tunnelManager should be nil after Close")
	}
}

// TestClient_TunnelManagerLazyInit tests that TunnelManager is lazily initialized.
func TestClient_TunnelManagerLazyInit(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// First call should create it
	tm1 := client.TunnelManager()
	if tm1 == nil {
		t.Fatal("TunnelManager should not be nil")
	}

	// Second call should return the same instance
	tm2 := client.TunnelManager()
	if tm1 != tm2 {
		t.Error("TunnelManager should return the same instance on repeated calls")
	}
}

// TestClient_TunnelManagerNotConnected tests that TunnelManager returns nil
// when the client is not connected.
func TestClient_TunnelManagerNotConnected(t *testing.T) {
	client := &Client{}
	tm := client.TunnelManager()
	if tm != nil {
		t.Error("expected nil TunnelManager when not connected")
	}
}

// TestClient_SFTPClientLazyInit tests SFTPClient lazy initialization.
func TestClient_SFTPClientLazyInit(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// SFTPClient should succeed and create a client
	sftp1, err := client.SFTPClient()
	if err != nil {
		t.Fatalf("SFTPClient() error = %v", err)
	}
	if sftp1 == nil {
		t.Fatal("SFTPClient should not be nil")
	}

	// Second call should return the same instance
	sftp2, err := client.SFTPClient()
	if err != nil {
		t.Fatalf("second SFTPClient() error = %v", err)
	}
	if sftp1 != sftp2 {
		t.Error("SFTPClient should return the same instance on repeated calls")
	}
}

// TestClient_CloseSFTPWithClient tests closing SFTP client after it was initialized.
func TestClient_CloseSFTPWithClient(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// Initialize SFTP client
	_, err := client.SFTPClient()
	if err != nil {
		t.Fatalf("SFTPClient() error = %v", err)
	}

	// Close SFTP client
	err = client.CloseSFTP()
	if err != nil {
		t.Errorf("CloseSFTP() error = %v", err)
	}

	if client.sftpClient != nil {
		t.Error("sftpClient should be nil after CloseSFTP")
	}
}

// TestClient_RemoteAddrWhenConnected tests RemoteAddr on a connected client.
func TestClient_RemoteAddrWhenConnected(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	addr := client.RemoteAddr()
	if addr == nil {
		t.Error("RemoteAddr should not be nil when connected")
	}
}

// TestClient_KeepaliveFiresAndStops tests the keepalive goroutine by
// advancing the fake clock past the keepalive interval.
func TestClient_KeepaliveFiresAndStops(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 1 * time.Minute,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Advance the clock past the keepalive interval.
	// The keepalive goroutine should fire and send a keepalive request.
	clk.Advance(2 * time.Minute)

	// Give the goroutine time to process
	time.Sleep(50 * time.Millisecond)

	// Close should stop the keepalive goroutine cleanly
	err := client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestClient_NewSessionWhenConnected tests creating a new session when connected.
// Since the fake client has no real SSH server, NewSession will fail, but it
// exercises the connected path.
func TestClient_NewSessionWhenConnected(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// NewSession will fail because the fake client has closed channels,
	// but this exercises the "connected" path in NewSession.
	_, err := client.NewSession()
	if err == nil {
		t.Fatal("expected error from NewSession on fake client")
	}
}

// TestClient_CloseWithSFTPClient tests that Close properly cleans up SFTP client.
func TestClient_CloseWithSFTPClient(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	dialer.SetDialFunc(func(network, addr string, config *gossh.ClientConfig) (*gossh.Client, error) {
		return fakeClient, nil
	})

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Initialize SFTP client
	_, err := client.SFTPClient()
	if err != nil {
		t.Fatalf("SFTPClient() error = %v", err)
	}

	// Close should clean up both SFTP and SSH
	err = client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if client.sftpClient != nil {
		t.Error("sftpClient should be nil after Close")
	}
	if client.conn != nil {
		t.Error("conn should be nil after Close")
	}
}

// TestClient_NewClientWithClockAndDialer verifies that injected Clock and Dialer
// are used by NewClient.
func TestClient_NewClientWithClockAndDialer(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	opts := ClientOptions{
		Host:        "myhost.com",
		Port:        2222,
		User:        "admin",
		AuthMethods: []gossh.AuthMethod{gossh.Password("secret")},
		Clock:       clk,
		Dialer:      dialer,
	}
	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.clock != clk {
		t.Error("expected client to use injected clock")
	}
	if client.dialer != dialer {
		t.Error("expected client to use injected dialer")
	}
}

// TestClient_NewClientDefaultClockAndDialer verifies that NewClient uses
// default Clock and Dialer when none are provided.
func TestClient_NewClientDefaultClockAndDialer(t *testing.T) {
	opts := ClientOptions{
		Host:        "myhost.com",
		User:        "admin",
		AuthMethods: []gossh.AuthMethod{gossh.Password("secret")},
	}
	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.clock == nil {
		t.Error("expected non-nil default clock")
	}
	if client.dialer == nil {
		t.Error("expected non-nil default dialer")
	}
}
