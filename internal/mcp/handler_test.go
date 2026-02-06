package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesessionmgr"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// --- Test helpers ---

func newTestServer(sm *fakesessionmgr.Manager) *Server {
	cfg := config.DefaultConfig()
	srv := NewServer(cfg,
		WithSessionManager(sm),
		WithFileSystem(fakefs.New()),
		WithClock(fakeclock.New(time.Now())),
	)
	return srv
}

func makeRequest(args map[string]any) mcpgo.CallToolRequest {
	return mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: args,
		},
	}
}

func resultText(result *mcpgo.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	tc, ok := mcpgo.AsTextContent(result.Content[0])
	if !ok {
		return ""
	}
	return tc.Text
}

func resultJSON(t *testing.T, result *mcpgo.CallToolResult) map[string]any {
	t.Helper()
	text := resultText(result)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("failed to parse result JSON: %v (text: %s)", err, text)
	}
	return m
}

func newFakeSession(id string) *session.Session {
	pty := fakepty.New()
	return session.NewSession(id, "local", session.WithPTY(pty))
}

// --- handleShellSessionCreate ---

func TestHandleShellSessionCreate_SSHMissingHost(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"mode": "ssh",
		"user": "testuser",
	})

	result, err := srv.handleShellSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH without host")
	}
	if !strings.Contains(resultText(result), "host") {
		t.Errorf("error should mention host, got: %s", resultText(result))
	}
}

func TestHandleShellSessionCreate_SSHMissingUser(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"mode": "ssh",
		"host": "example.com",
	})

	result, err := srv.handleShellSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH without user")
	}
}

func TestHandleShellSessionCreate_CreateError(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	// Default CreateFunc returns error
	req := makeRequest(map[string]any{
		"mode": "local",
	})

	result, err := srv.handleShellSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when Create fails")
	}
}

func TestHandleShellSessionCreate_Success(t *testing.T) {
	sm := fakesessionmgr.New()
	sess := newFakeSession("sess_test123")
	sm.CreateFunc = func(opts session.CreateOptions) (*session.Session, error) {
		return sess, nil
	}
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"mode": "local",
	})

	result, err := srv.handleShellSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["session_id"] != "sess_test123" {
		t.Errorf("session_id = %v, want sess_test123", m["session_id"])
	}
	if m["status"] != "connected" {
		t.Errorf("status = %v, want connected", m["status"])
	}
}

// --- handleShellSessionList ---

func TestHandleShellSessionList_Empty(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	result, err := srv.handleShellSessionList(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["count"] != float64(0) {
		t.Errorf("count = %v, want 0", m["count"])
	}
}

func TestHandleShellSessionList_WithSessions(t *testing.T) {
	sm := fakesessionmgr.New()
	sm.AddSession(newFakeSession("sess_a"))
	sm.AddSession(newFakeSession("sess_b"))
	srv := newTestServer(sm)

	result, err := srv.handleShellSessionList(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultJSON(t, result)
	if m["count"] != float64(2) {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

// --- handleShellExec ---

func TestHandleShellExec_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"command": "ls",
	})

	result, err := srv.handleShellExec(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellExec_MissingCommand(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_123",
	})

	result, err := srv.handleShellExec(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing command")
	}
}

func TestHandleShellExec_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_nonexistent",
		"command":    "ls",
	})

	result, err := srv.handleShellExec(context.Background(), req)
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

func TestHandleShellExec_HeredocRejected(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_123",
		"command":    "cat <<EOF\nhello\nEOF",
	})

	result, err := srv.handleShellExec(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for heredoc command")
	}
	if !strings.Contains(resultText(result), "Heredoc") {
		t.Errorf("error should mention heredoc, got: %s", resultText(result))
	}
}

func TestHandleShellExec_BothTailAndHead(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_123",
		"command":    "cat file",
		"tail_lines": float64(10),
		"head_lines": float64(5),
	})

	result, err := srv.handleShellExec(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for both tail and head")
	}
}

// --- handleShellProvideInput ---

func TestHandleShellProvideInput_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"input": "yes",
	})

	result, err := srv.handleShellProvideInput(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellProvideInput_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
		"input":      "yes",
	})

	result, err := srv.handleShellProvideInput(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

// --- handleShellSendRaw ---

func TestHandleShellSendRaw_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"input": "\\x04",
	})

	result, err := srv.handleShellSendRaw(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellSendRaw_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
		"input":      "\\x04",
	})

	result, err := srv.handleShellSendRaw(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

// --- handleShellInterrupt ---

func TestHandleShellInterrupt_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellInterrupt(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellInterrupt_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handleShellInterrupt(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

// --- handleShellSessionStatus ---

func TestHandleShellSessionStatus_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellSessionStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellSessionStatus_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handleShellSessionStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

// --- handleShellSessionClose ---

func TestHandleShellSessionClose_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellSessionClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellSessionClose_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handleShellSessionClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestHandleShellSessionClose_Success(t *testing.T) {
	sm := fakesessionmgr.New()
	sm.AddSession(newFakeSession("sess_to_close"))
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_to_close",
	})

	result, err := srv.handleShellSessionClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "closed" {
		t.Errorf("status = %v, want closed", m["status"])
	}
}

// --- handleShellSudoAuth ---

func TestHandleShellSudoAuth_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellSudoAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellSudoAuth_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handleShellSudoAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

// --- Peak TTY handlers ---

func TestHandlePeakTTYStatus_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handlePeakTTYStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandlePeakTTYStatus_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handlePeakTTYStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestHandlePeakTTYStart_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handlePeakTTYStart(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandlePeakTTYStop_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handlePeakTTYStop(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandlePeakTTYDeploy_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

// --- Tunnel handlers ---

func TestHandleShellTunnelCreate_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"type":        "local",
		"local_port":  float64(8080),
		"remote_port": float64(80),
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellTunnelList_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellTunnelList(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellTunnelClose_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"tunnel_id": "tunnel_1",
	})

	result, err := srv.handleShellTunnelClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

// --- File transfer handlers ---

func TestHandleShellFileGet_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"remote_path": "/test/file.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellFileGet_MissingPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_123",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing remote_path")
	}
}

func TestHandleShellFilePut_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"remote_path": "/test/file.txt",
		"content":     "hello",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellFileMv_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"source":      "/test/old.txt",
		"destination": "/test/new.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellDirGet_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"remote_path": "/test/dir",
		"local_path":  "/local/dir",
	})

	result, err := srv.handleShellDirGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellDirPut_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"local_path":  "/local/dir",
		"remote_path": "/remote/dir",
	})

	result, err := srv.handleShellDirPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

// --- Chunked transfer handlers ---

func TestHandleShellFileGetChunked_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"remote_path": "/test/big.file",
		"local_path":  "/local/big.file",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellFilePutChunked_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"local_path":  "/local/big.file",
		"remote_path": "/remote/big.file",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellTransferStatus_MissingManifestPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing manifest_path")
	}
}

func TestHandleShellTransferResume_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"manifest_path": "/tmp/transfer.json",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

// --- handleShellDebug ---

func TestHandleShellDebug_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellDebug(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing session_id")
	}
}

func TestHandleShellDebug_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handleShellDebug(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}
