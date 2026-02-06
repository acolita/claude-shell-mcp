package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesessionmgr"
)

// --- handleShellTunnelCreate ---

func TestHandleTunnelCreate_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"type":        "local",
		"local_port":  float64(8080),
		"remote_port": float64(80),
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error should mention 'not found', got: %s", resultText(result))
	}
}

func TestHandleTunnelCreate_MissingType(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_test",
		"local_port":  float64(8080),
		"remote_port": float64(80),
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing type")
	}
	text := resultText(result)
	if !strings.Contains(text, "type") {
		t.Errorf("error should mention type, got: %s", text)
	}
}

func TestHandleTunnelCreate_InvalidType(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_test",
		"type":        "invalid",
		"local_port":  float64(8080),
		"remote_port": float64(80),
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid type")
	}
	text := resultText(result)
	if !strings.Contains(text, "local") || !strings.Contains(text, "reverse") {
		t.Errorf("error should mention valid types 'local' and 'reverse', got: %s", text)
	}
}

func TestHandleTunnelCreate_LocalSessionNoTunnelManager(t *testing.T) {
	sm := fakesessionmgr.New()
	sess := newFakeSession("sess_local")
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_local",
		"type":        "local",
		"local_port":  float64(8080),
		"remote_port": float64(80),
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for local session (no tunnel manager)")
	}
	text := resultText(result)
	if !strings.Contains(text, "local sessions") {
		t.Errorf("error should mention local sessions limitation, got: %s", text)
	}
}

func TestHandleTunnelCreate_ReverseTypeLocalSession(t *testing.T) {
	sm := fakesessionmgr.New()
	sess := newFakeSession("sess_local")
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_local",
		"type":        "reverse",
		"local_port":  float64(3000),
		"remote_port": float64(8080),
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for local session (no tunnel manager)")
	}
}

// --- handleShellTunnelList ---

func TestHandleTunnelList_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_nonexistent",
	})

	result, err := srv.handleShellTunnelList(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error should mention 'not found', got: %s", resultText(result))
	}
}

func TestHandleTunnelList_LocalSessionNoTunnelManager(t *testing.T) {
	sm := fakesessionmgr.New()
	sess := newFakeSession("sess_local")
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_local",
	})

	result, err := srv.handleShellTunnelList(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for local session (no tunnel manager)")
	}
	text := resultText(result)
	if !strings.Contains(text, "local sessions") {
		t.Errorf("error should mention local sessions limitation, got: %s", text)
	}
}

// --- handleShellTunnelClose ---

func TestHandleTunnelClose_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_nonexistent",
		"tunnel_id":  "tunnel_1",
	})

	result, err := srv.handleShellTunnelClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error should mention 'not found', got: %s", resultText(result))
	}
}

func TestHandleTunnelClose_MissingTunnelID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_test",
	})

	result, err := srv.handleShellTunnelClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing tunnel_id")
	}
	text := resultText(result)
	if !strings.Contains(text, "tunnel_id") {
		t.Errorf("error should mention tunnel_id, got: %s", text)
	}
}

func TestHandleTunnelClose_LocalSessionNoTunnelManager(t *testing.T) {
	sm := fakesessionmgr.New()
	sess := newFakeSession("sess_local")
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_local",
		"tunnel_id":  "tunnel_1",
	})

	result, err := srv.handleShellTunnelClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for local session (no tunnel manager)")
	}
	text := resultText(result)
	if !strings.Contains(text, "local sessions") {
		t.Errorf("error should mention local sessions limitation, got: %s", text)
	}
}

// --- handleShellTunnelRestore ---

func TestHandleTunnelRestore_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
	text := resultText(result)
	if !strings.Contains(text, "session_id") {
		t.Errorf("error should mention session_id, got: %s", text)
	}
}

func TestHandleTunnelRestore_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_nonexistent",
	})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error should mention 'not found', got: %s", resultText(result))
	}
}

func TestHandleTunnelRestore_NoSavedTunnels(t *testing.T) {
	sm := fakesessionmgr.New()
	// Create a session with a clock (needed for Status() which calls clock.Now())
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := session.NewSession("sess_no_tunnels", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clock),
	)
	// SavedTunnels is nil by default
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_no_tunnels",
	})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for session with no saved tunnels")
	}
	text := resultText(result)
	if !strings.Contains(text, "no saved tunnels") {
		t.Errorf("error should mention 'no saved tunnels', got: %s", text)
	}
}

func TestHandleTunnelRestore_EmptySavedTunnels(t *testing.T) {
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := session.NewSession("sess_empty_tunnels", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clock),
	)
	sess.SavedTunnels = []session.TunnelConfig{} // Empty slice, not nil
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_empty_tunnels",
	})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for session with empty saved tunnels")
	}
	text := resultText(result)
	if !strings.Contains(text, "no saved tunnels") {
		t.Errorf("error should mention 'no saved tunnels', got: %s", text)
	}
}

func TestHandleTunnelRestore_LocalSessionWithSavedTunnels(t *testing.T) {
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := session.NewSession("sess_local_saved", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clock),
	)
	sess.SavedTunnels = []session.TunnelConfig{
		{Type: "local", LocalHost: "127.0.0.1", LocalPort: 8080, RemoteHost: "127.0.0.1", RemotePort: 80},
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_local_saved",
	})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for local session trying to restore tunnels (no tunnel manager)")
	}
	text := resultText(result)
	if !strings.Contains(text, "local sessions") {
		t.Errorf("error should mention local sessions limitation, got: %s", text)
	}
}
