package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesessionmgr"
)

// ==================== handleShellTunnelRestore ====================

func TestValidation_TunnelRestore_SSHSessionNoClient(t *testing.T) {
	// An SSH-mode session with saved tunnels but no SSH client should fail
	// at the TunnelManager() call with "SSH client not initialized".
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := session.NewSession("sess_ssh_no_client", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	sess.SavedTunnels = []session.TunnelConfig{
		{Type: "local", LocalHost: "127.0.0.1", LocalPort: 8080, RemoteHost: "127.0.0.1", RemotePort: 80},
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_ssh_no_client",
	})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH session with no SSH client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SSH client, got: %s", text)
	}
}

func TestValidation_TunnelRestore_TunnelIndexOutOfBoundsWithLocalSession(t *testing.T) {
	// A local session with saved tunnels + tunnel_index should fail at TunnelManager
	// before reaching the bounds check. This tests the TunnelManager error path
	// when tunnel_index is specified.
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := session.NewSession("sess_local_idx", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	sess.SavedTunnels = []session.TunnelConfig{
		{Type: "local", LocalHost: "127.0.0.1", LocalPort: 8080, RemoteHost: "127.0.0.1", RemotePort: 80},
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":   "sess_local_idx",
		"tunnel_index": float64(5), // out of bounds, but won't reach that check
	})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
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

func TestValidation_TunnelRestore_MultipleSavedTunnelsSSHNoClient(t *testing.T) {
	// SSH session with multiple saved tunnels but no client - tests the path
	// where SavedTunnels has multiple entries and we pass the len check.
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := session.NewSession("sess_multi", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	sess.SavedTunnels = []session.TunnelConfig{
		{Type: "local", LocalHost: "127.0.0.1", LocalPort: 8080, RemoteHost: "db.internal", RemotePort: 5432},
		{Type: "reverse", LocalHost: "127.0.0.1", LocalPort: 3000, RemoteHost: "0.0.0.0", RemotePort: 8080},
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":   "sess_multi",
		"tunnel_index": float64(0), // valid index but TunnelManager will fail
	})

	result, err := srv.handleShellTunnelRestore(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SSH client, got: %s", text)
	}
}

// ==================== handleShellTunnelCreate ====================

func TestValidation_TunnelCreate_DefaultRemoteHostLocal(t *testing.T) {
	// When type is "local" and remote_host is empty, default should be 127.0.0.1.
	// The handler sets it before reaching TunnelManager, so exercise that path
	// even though TunnelManager will fail for a local session.
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	sess := session.NewSession("sess_defaults", "local", session.WithPTY(pty))
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_defaults",
		"type":        "local",
		"local_port":  float64(0),
		"remote_port": float64(80),
		// remote_host deliberately omitted to exercise default path
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// It will fail at TunnelManager since it's a local session
	if !result.IsError {
		t.Error("expected error for local session (no tunnel manager)")
	}
}

func TestValidation_TunnelCreate_DefaultRemoteHostReverse(t *testing.T) {
	// When type is "reverse" and remote_host is empty, default should be 0.0.0.0.
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	sess := session.NewSession("sess_reverse_default", "local", session.WithPTY(pty))
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_reverse_default",
		"type":        "reverse",
		"local_port":  float64(3000),
		"remote_port": float64(0),
		// remote_host deliberately omitted
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for local session (no tunnel manager)")
	}
}

func TestValidation_TunnelCreate_SSHSessionNoClient(t *testing.T) {
	// SSH mode session with no SSH client hits the TunnelManager error path.
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_ssh_tunnel", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_ssh_tunnel",
		"type":        "local",
		"local_port":  float64(8080),
		"remote_port": float64(80),
	})

	result, err := srv.handleShellTunnelCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SSH client, got: %s", text)
	}
}

// ==================== handleShellTunnelList ====================

func TestValidation_TunnelList_SSHSessionNoClient(t *testing.T) {
	// SSH session with no SSH client - should error at TunnelManager.
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_list_ssh", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_list_ssh",
	})

	result, err := srv.handleShellTunnelList(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SSH client, got: %s", text)
	}
}

// ==================== handleShellTunnelClose ====================

func TestValidation_TunnelClose_SSHSessionNoClient(t *testing.T) {
	// SSH session with no SSH client - exercises TunnelManager error path.
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_close_ssh", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_close_ssh",
		"tunnel_id":  "tunnel_nonexistent",
	})

	result, err := srv.handleShellTunnelClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SSH client, got: %s", text)
	}
}

// ==================== handleShellFileGetChunked ====================

func TestValidation_FileGetChunked_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
	text := resultText(result)
	if !strings.Contains(text, "not found") {
		t.Errorf("error should mention 'not found', got: %s", text)
	}
}

func TestValidation_FileGetChunked_ChunkSizeClampedSmall(t *testing.T) {
	// chunk_size below 1024 should be clamped to 1024.
	// Session is local so it will fail at IsSSH check, but clamping runs first.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_small_chunk", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_small_chunk",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
		"chunk_size":  float64(100), // below minimum 1024
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Will fail with "not SSH" but exercises the small clamping code path
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
	text := resultText(result)
	if !strings.Contains(text, "SSH") {
		t.Errorf("error should mention SSH, got: %s", text)
	}
}

func TestValidation_FileGetChunked_SSHSessionNoClient(t *testing.T) {
	// An SSH session that passes IsSSH but has no SFTPClient.
	// This covers the path: IsSSH() returns true, then performChunkedGet
	// calls SFTPClient() which fails with "SSH client not initialized".
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_ssh_get", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_ssh_get",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SFTP") || !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SFTP/SSH client, got: %s", text)
	}
}

// ==================== handleShellFilePutChunked ====================

func TestValidation_FilePutChunked_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
	text := resultText(result)
	if !strings.Contains(text, "not found") {
		t.Errorf("error should mention 'not found', got: %s", text)
	}
}

func TestValidation_FilePutChunked_ChunkSizeClampedSmall(t *testing.T) {
	// chunk_size below 1024 should be clamped to 1024.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_put_small", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_small",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
		"chunk_size":  float64(512), // below 1024
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Will fail with "not SSH" but exercises the small clamping code path
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

func TestValidation_FilePutChunked_ChunkSizeClampedLarge(t *testing.T) {
	// chunk_size above MaxChunkSize should be clamped.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_put_large", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_large",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
		"chunk_size":  float64(999999999), // way over MaxChunkSize
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

func TestValidation_FilePutChunked_SSHSessionNoClient(t *testing.T) {
	// SSH session that passes IsSSH but has no SFTPClient.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_ssh_put", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_ssh_put",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SFTP") || !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SFTP/SSH client, got: %s", text)
	}
}

// ==================== handleShellTransferResume ====================

func TestValidation_TransferResume_SessionNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":    "sess_nonexistent",
		"manifest_path": "/tmp/test.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
	text := resultText(result)
	if !strings.Contains(text, "not found") {
		t.Errorf("error should mention 'not found', got: %s", text)
	}
}

func TestValidation_TransferResume_InvalidManifestJSON(t *testing.T) {
	ffs := fakefs.New()
	// Write invalid JSON to the manifest file
	ffs.AddFile("/tmp/bad.transfer", []byte("{invalid json!!!}"), 0644)

	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_bad_json", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":    "sess_bad_json",
		"manifest_path": "/tmp/bad.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid manifest JSON")
	}
	text := resultText(result)
	if !strings.Contains(text, "manifest") {
		t.Errorf("error should mention manifest, got: %s", text)
	}
}

// ==================== handleShellTransferStatus ====================

func TestValidation_TransferStatus_ZeroTotalSize(t *testing.T) {
	// Tests the zero total size edge case where progress calculation
	// avoids division by zero.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()

	manifest := TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   0, // edge case: empty file
		TotalChunks: 0,
		BytesSent:   0,
		Chunks:      []ChunkInfo{},
	}
	data, _ := json.Marshal(manifest)
	ffs.AddFile("/tmp/zero.transfer", data, 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"manifest_path": "/tmp/zero.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	// With 0 total chunks and 0 completed, status should be "completed"
	if m["status"] != "completed" {
		t.Errorf("status = %v, want completed", m["status"])
	}
	// Progress should be 0 (not NaN or division error)
	if m["progress_percent"] != float64(0) {
		t.Errorf("progress_percent = %v, want 0", m["progress_percent"])
	}
}

// ==================== saveManifest ====================

func TestValidation_SaveManifest_Success(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	manifest := &TransferManifest{
		Version:    1,
		Direction:  "get",
		RemotePath: "/remote/file.bin",
		LocalPath:  "/local/file.bin",
		TotalSize:  1024,
		ChunkSize:  512,
		TotalChunks: 2,
		SessionID:  "sess_test",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 512, Completed: true, Checksum: "abc123"},
			{Index: 1, Offset: 512, Size: 512, Completed: false},
		},
	}

	err := srv.saveManifest(manifest, "/tmp/test.transfer")
	if err != nil {
		t.Fatalf("saveManifest returned error: %v", err)
	}

	// Verify the manifest was written
	data, err := ffs.ReadFile("/tmp/test.transfer")
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}

	var loaded TransferManifest
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to parse saved manifest: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}
	if loaded.TotalSize != 1024 {
		t.Errorf("TotalSize = %d, want 1024", loaded.TotalSize)
	}
	if len(loaded.Chunks) != 2 {
		t.Errorf("Chunks len = %d, want 2", len(loaded.Chunks))
	}
	if !loaded.Chunks[0].Completed {
		t.Error("expected chunk 0 to be completed")
	}
	if loaded.Chunks[1].Completed {
		t.Error("expected chunk 1 to not be completed")
	}
}

// ==================== loadManifest ====================

func TestValidation_LoadManifest_NotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	_, err := srv.loadManifest("/nonexistent/path.transfer")
	if err == nil {
		t.Fatal("expected error for nonexistent manifest")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Errorf("error should mention read manifest, got: %s", err.Error())
	}
}

func TestValidation_LoadManifest_InvalidJSON(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/tmp/invalid.transfer", []byte("not json at all"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	_, err := srv.loadManifest("/tmp/invalid.transfer")
	if err == nil {
		t.Fatal("expected error for invalid JSON manifest")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
		t.Errorf("error should mention parse manifest, got: %s", err.Error())
	}
}

func TestValidation_LoadManifest_Success(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	manifest := TransferManifest{
		Version:    1,
		Direction:  "put",
		RemotePath: "/remote/upload.bin",
		LocalPath:  "/local/upload.bin",
		TotalSize:  2048,
		ChunkSize:  1024,
		TotalChunks: 2,
		SessionID:  "sess_load",
	}
	data, _ := json.Marshal(manifest)
	ffs.AddFile("/tmp/load.transfer", data, 0644)

	loaded, err := srv.loadManifest("/tmp/load.transfer")
	if err != nil {
		t.Fatalf("loadManifest returned error: %v", err)
	}
	if loaded.Direction != "put" {
		t.Errorf("Direction = %q, want 'put'", loaded.Direction)
	}
	if loaded.TotalSize != 2048 {
		t.Errorf("TotalSize = %d, want 2048", loaded.TotalSize)
	}
}
