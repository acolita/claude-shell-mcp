package fakessh

import (
	"errors"
	"testing"
)

func TestClient_Connect(t *testing.T) {
	c := New()

	if c.IsConnected() {
		t.Error("should not be connected initially")
	}

	if err := c.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	if !c.IsConnected() {
		t.Error("should be connected after Connect")
	}
}

func TestClient_ConnectError(t *testing.T) {
	c := New()
	c.SetConnectError(errors.New("connection refused"))

	err := c.Connect()
	if err == nil {
		t.Error("expected error")
	}
	if err.Error() != "connection refused" {
		t.Errorf("error = %q, want %q", err.Error(), "connection refused")
	}
}

func TestClient_Close(t *testing.T) {
	c := New()
	c.Connect()

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if c.IsConnected() {
		t.Error("should not be connected after Close")
	}
	if !c.WasClosed() {
		t.Error("WasClosed should be true")
	}
}

func TestTunnelManager_CreateLocalTunnel(t *testing.T) {
	tm := NewTunnelManager()

	id, err := tm.CreateLocalTunnel(8080, 80, "remote", "localhost")
	if err != nil {
		t.Fatalf("CreateLocalTunnel error: %v", err)
	}

	if id == "" {
		t.Error("tunnel ID should not be empty")
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnels))
	}
	if tunnels[0].Type != "local" {
		t.Errorf("tunnel type = %q, want %q", tunnels[0].Type, "local")
	}
}

func TestTunnelManager_CloseTunnel(t *testing.T) {
	tm := NewTunnelManager()

	id, _ := tm.CreateLocalTunnel(8080, 80, "remote", "localhost")

	if err := tm.CloseTunnel(id); err != nil {
		t.Fatalf("CloseTunnel error: %v", err)
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) != 0 {
		t.Errorf("expected 0 tunnels after close, got %d", len(tunnels))
	}
}

func TestTunnelManager_CloseTunnelNotFound(t *testing.T) {
	tm := NewTunnelManager()

	err := tm.CloseTunnel("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent tunnel")
	}
}
