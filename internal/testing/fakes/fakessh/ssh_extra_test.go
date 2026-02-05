package fakessh

import (
	"errors"
	"testing"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

func TestClient_SetCloseError(t *testing.T) {
	c := New()
	c.Connect()

	closeErr := errors.New("close failed")
	c.SetCloseError(closeErr)

	err := c.Close()
	if err == nil {
		t.Fatal("Close should return error")
	}
	if err.Error() != "close failed" {
		t.Errorf("Close error = %q, want %q", err.Error(), "close failed")
	}
}

func TestClient_SetSFTPError(t *testing.T) {
	c := New()
	c.Connect()

	sftpErr := errors.New("sftp not available")
	c.SetSFTPError(sftpErr)

	_, err := c.SFTPClient()
	if err == nil {
		t.Fatal("SFTPClient should return error")
	}
	if err.Error() != "sftp not available" {
		t.Errorf("SFTPClient error = %q, want %q", err.Error(), "sftp not available")
	}
}

func TestClient_SFTPClientNoError(t *testing.T) {
	c := New()

	// Without setting error or client, should return nil client, no error
	client, err := c.SFTPClient()
	if err != nil {
		t.Fatalf("SFTPClient should not error, got %v", err)
	}
	if client != nil {
		t.Error("SFTPClient should return nil when not configured")
	}
}

func TestClient_SetTunnelManager(t *testing.T) {
	c := New()
	tm := NewTunnelManager()
	c.SetTunnelManager(tm)

	got := c.TunnelManager()
	if got != tm {
		t.Error("TunnelManager should return the configured tunnel manager")
	}
}

func TestClient_TunnelManagerNil(t *testing.T) {
	c := New()

	got := c.TunnelManager()
	if got != nil {
		t.Error("TunnelManager should return nil when not configured")
	}
}

func TestClient_WasConnected(t *testing.T) {
	c := New()

	// Before any connection
	if c.WasConnected() {
		t.Error("WasConnected should be false before Connect")
	}

	// After Connect
	c.Connect()
	if !c.WasConnected() {
		t.Error("WasConnected should be true after Connect")
	}

	// After Close (was still connected at some point)
	c.Close()
	if !c.WasConnected() {
		t.Error("WasConnected should be true after Close (because it was connected before)")
	}
}

func TestClient_ConnectThenCloseThenConnect(t *testing.T) {
	c := New()

	c.Connect()
	if !c.IsConnected() {
		t.Error("should be connected")
	}

	c.Close()
	if c.IsConnected() {
		t.Error("should not be connected after Close")
	}
}

func TestTunnelManager_CreateReverseTunnel(t *testing.T) {
	tm := NewTunnelManager()

	id, err := tm.CreateReverseTunnel(3000, 8080, "0.0.0.0", "localhost")
	if err != nil {
		t.Fatalf("CreateReverseTunnel error: %v", err)
	}
	if id == "" {
		t.Error("tunnel ID should not be empty")
	}
	if id != "tunnel_reverse_1" {
		t.Errorf("tunnel ID = %q, want %q", id, "tunnel_reverse_1")
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnels))
	}
	if tunnels[0].Type != "reverse" {
		t.Errorf("tunnel type = %q, want %q", tunnels[0].Type, "reverse")
	}
	if tunnels[0].LocalPort != 3000 {
		t.Errorf("LocalPort = %d, want %d", tunnels[0].LocalPort, 3000)
	}
	if tunnels[0].RemotePort != 8080 {
		t.Errorf("RemotePort = %d, want %d", tunnels[0].RemotePort, 8080)
	}
	if tunnels[0].RemoteHost != "0.0.0.0" {
		t.Errorf("RemoteHost = %q, want %q", tunnels[0].RemoteHost, "0.0.0.0")
	}
	if tunnels[0].LocalHost != "localhost" {
		t.Errorf("LocalHost = %q, want %q", tunnels[0].LocalHost, "localhost")
	}
}

func TestTunnelManager_OnCreate(t *testing.T) {
	tm := NewTunnelManager()

	var capturedType, capturedRemoteHost, capturedLocalHost string
	var capturedLocalPort, capturedRemotePort int

	tm.OnCreate(func(tunnelType string, localPort, remotePort int, remoteHost, localHost string) (string, error) {
		capturedType = tunnelType
		capturedLocalPort = localPort
		capturedRemotePort = remotePort
		capturedRemoteHost = remoteHost
		capturedLocalHost = localHost
		return "custom_id_1", nil
	})

	id, err := tm.CreateLocalTunnel(9090, 80, "backend", "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateLocalTunnel error: %v", err)
	}
	if id != "custom_id_1" {
		t.Errorf("id = %q, want %q", id, "custom_id_1")
	}
	if capturedType != "local" {
		t.Errorf("captured type = %q, want %q", capturedType, "local")
	}
	if capturedLocalPort != 9090 {
		t.Errorf("captured localPort = %d, want %d", capturedLocalPort, 9090)
	}
	if capturedRemotePort != 80 {
		t.Errorf("captured remotePort = %d, want %d", capturedRemotePort, 80)
	}
	if capturedRemoteHost != "backend" {
		t.Errorf("captured remoteHost = %q, want %q", capturedRemoteHost, "backend")
	}
	if capturedLocalHost != "127.0.0.1" {
		t.Errorf("captured localHost = %q, want %q", capturedLocalHost, "127.0.0.1")
	}
}

func TestTunnelManager_OnCreateReverse(t *testing.T) {
	tm := NewTunnelManager()

	tm.OnCreate(func(tunnelType string, localPort, remotePort int, remoteHost, localHost string) (string, error) {
		if tunnelType != "reverse" {
			t.Errorf("expected reverse tunnel type, got %q", tunnelType)
		}
		return "reverse_custom", nil
	})

	id, err := tm.CreateReverseTunnel(3000, 8080, "0.0.0.0", "localhost")
	if err != nil {
		t.Fatalf("CreateReverseTunnel error: %v", err)
	}
	if id != "reverse_custom" {
		t.Errorf("id = %q, want %q", id, "reverse_custom")
	}
}

func TestTunnelManager_OnCreateError(t *testing.T) {
	tm := NewTunnelManager()

	tm.OnCreate(func(tunnelType string, localPort, remotePort int, remoteHost, localHost string) (string, error) {
		return "", errors.New("port already in use")
	})

	_, err := tm.CreateLocalTunnel(80, 80, "remote", "localhost")
	if err == nil {
		t.Fatal("expected error from OnCreate callback")
	}
	if err.Error() != "port already in use" {
		t.Errorf("error = %q, want %q", err.Error(), "port already in use")
	}
}

func TestTunnelManager_OnClose(t *testing.T) {
	tm := NewTunnelManager()

	var closedID string
	tm.OnClose(func(tunnelID string) error {
		closedID = tunnelID
		return nil
	})

	err := tm.CloseTunnel("my_tunnel")
	if err != nil {
		t.Fatalf("CloseTunnel error: %v", err)
	}
	if closedID != "my_tunnel" {
		t.Errorf("closed ID = %q, want %q", closedID, "my_tunnel")
	}
}

func TestTunnelManager_OnCloseError(t *testing.T) {
	tm := NewTunnelManager()

	tm.OnClose(func(tunnelID string) error {
		return errors.New("tunnel busy")
	})

	err := tm.CloseTunnel("my_tunnel")
	if err == nil {
		t.Fatal("expected error from OnClose callback")
	}
	if err.Error() != "tunnel busy" {
		t.Errorf("error = %q, want %q", err.Error(), "tunnel busy")
	}
}

func TestTunnelManager_Close(t *testing.T) {
	tm := NewTunnelManager()

	// Add some tunnels
	tm.CreateLocalTunnel(8080, 80, "remote", "localhost")
	tm.CreateReverseTunnel(3000, 8080, "0.0.0.0", "localhost")

	if len(tm.ListTunnels()) != 2 {
		t.Fatalf("expected 2 tunnels, got %d", len(tm.ListTunnels()))
	}

	// Close all
	err := tm.Close()
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) != 0 {
		t.Errorf("expected 0 tunnels after Close, got %d", len(tunnels))
	}
}

func TestTunnelManager_ListTunnelsReturnsCopy(t *testing.T) {
	tm := NewTunnelManager()
	tm.CreateLocalTunnel(8080, 80, "remote", "localhost")

	tunnels1 := tm.ListTunnels()
	tunnels2 := tm.ListTunnels()

	// Modifying the returned slice should not affect the internal state
	if len(tunnels1) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnels1))
	}

	tunnels1[0].ID = "modified"

	// Second call should still return original data
	if tunnels2[0].ID != "tunnel_local_1" {
		t.Errorf("ListTunnels returned shared slice; got ID %q, want %q", tunnels2[0].ID, "tunnel_local_1")
	}
}

func TestTunnelManager_MultipleTunnels(t *testing.T) {
	tm := NewTunnelManager()

	id1, _ := tm.CreateLocalTunnel(8080, 80, "web", "localhost")
	id2, _ := tm.CreateReverseTunnel(3000, 3306, "db", "localhost")

	tunnels := tm.ListTunnels()
	if len(tunnels) != 2 {
		t.Fatalf("expected 2 tunnels, got %d", len(tunnels))
	}

	// Close the first tunnel
	err := tm.CloseTunnel(id1)
	if err != nil {
		t.Fatalf("CloseTunnel error: %v", err)
	}

	tunnels = tm.ListTunnels()
	if len(tunnels) != 1 {
		t.Fatalf("expected 1 tunnel after close, got %d", len(tunnels))
	}
	if tunnels[0].ID != id2 {
		t.Errorf("remaining tunnel ID = %q, want %q", tunnels[0].ID, id2)
	}
}

// Verify interface compliance
func TestClient_ImplementsSSHClient(t *testing.T) {
	var _ ports.SSHClient = (*Client)(nil)
}

func TestTunnelManager_ImplementsSSHTunnelManager(t *testing.T) {
	var _ ports.SSHTunnelManager = (*TunnelManager)(nil)
}
