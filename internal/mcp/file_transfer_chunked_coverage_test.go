package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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

// mcpTool is a type alias to avoid import conflicts with the mcp package namespace.
type mcpTool = mcpgo.Tool

// ==================== handleShellFileGetChunked ====================

func TestChunked_GetChunked_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing session_id")
	}
	text := resultText(result)
	if !strings.Contains(text, "session_id is required") {
		t.Errorf("error should mention session_id, got: %s", text)
	}
}

func TestChunked_GetChunked_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
		"local_path": "/local/file.bin",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing remote_path")
	}
	text := resultText(result)
	if !strings.Contains(text, "remote_path is required") {
		t.Errorf("error should mention remote_path, got: %s", text)
	}
}

func TestChunked_GetChunked_MissingLocalPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_1",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing local_path")
	}
	text := resultText(result)
	if !strings.Contains(text, "local_path is required") {
		t.Errorf("error should mention local_path, got: %s", text)
	}
}

func TestChunked_GetChunked_ChunkSizeClampedToMax(t *testing.T) {
	// chunk_size above MaxChunkSize should be clamped, then fail at IsSSH.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_clamp_max", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	_ = sess.Initialize()
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_clamp_max",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
		"chunk_size":  float64(999999999), // way over MaxChunkSize
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Will fail with "not SSH" but exercises the max clamping path
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
	text := resultText(result)
	if !strings.Contains(text, "SSH") {
		t.Errorf("error should mention SSH, got: %s", text)
	}
}

func TestChunked_GetChunked_LocalSessionRejected(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_local_get")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_local_get",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for local session")
	}
	text := resultText(result)
	if !strings.Contains(text, "only supported for SSH sessions") {
		t.Errorf("error should mention SSH only, got: %s", text)
	}
}

// ==================== handleShellFilePutChunked ====================

func TestChunked_PutChunked_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing session_id")
	}
	text := resultText(result)
	if !strings.Contains(text, "session_id is required") {
		t.Errorf("error should mention session_id, got: %s", text)
	}
}

func TestChunked_PutChunked_MissingLocalPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_1",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing local_path")
	}
	text := resultText(result)
	if !strings.Contains(text, "local_path is required") {
		t.Errorf("error should mention local_path, got: %s", text)
	}
}

func TestChunked_PutChunked_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
		"local_path": "/local/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing remote_path")
	}
	text := resultText(result)
	if !strings.Contains(text, "remote_path is required") {
		t.Errorf("error should mention remote_path, got: %s", text)
	}
}

func TestChunked_PutChunked_LocalSessionRejected(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_local_put")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_local_put",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for local session")
	}
	text := resultText(result)
	if !strings.Contains(text, "only supported for SSH sessions") {
		t.Errorf("error should mention SSH only, got: %s", text)
	}
}

func TestChunked_PutChunked_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_does_not_exist",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent session")
	}
	text := resultText(result)
	if !strings.Contains(text, "not found") {
		t.Errorf("error should mention not found, got: %s", text)
	}
}

// ==================== handleShellTransferStatus ====================

func TestChunked_TransferStatus_MissingManifestPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing manifest_path")
	}
	text := resultText(result)
	if !strings.Contains(text, "manifest_path is required") {
		t.Errorf("error should mention manifest_path, got: %s", text)
	}
}

func TestChunked_TransferStatus_ManifestNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"manifest_path": "/nonexistent/test.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent manifest")
	}
	text := resultText(result)
	if !strings.Contains(text, "manifest") {
		t.Errorf("error should mention manifest, got: %s", text)
	}
}

func TestChunked_TransferStatus_InProgress(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()

	manifest := TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   3000,
		TotalChunks: 3,
		BytesSent:   1000,
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1000, Completed: true, Checksum: "aaa"},
			{Index: 1, Offset: 1000, Size: 1000, Completed: false},
			{Index: 2, Offset: 2000, Size: 1000, Completed: false},
		},
		BytesPerSecond: 500,
	}
	data, _ := json.Marshal(manifest)
	ffs.AddFile("/tmp/inprogress.transfer", data, 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"manifest_path": "/tmp/inprogress.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "in_progress" {
		t.Errorf("status=%v, want in_progress", m["status"])
	}
	if m["chunks_completed"] != float64(1) {
		t.Errorf("chunks_completed=%v, want 1", m["chunks_completed"])
	}
	if m["total_chunks"] != float64(3) {
		t.Errorf("total_chunks=%v, want 3", m["total_chunks"])
	}
	if m["bytes_transferred"] != float64(1000) {
		t.Errorf("bytes_transferred=%v, want 1000", m["bytes_transferred"])
	}
	if m["total_bytes"] != float64(3000) {
		t.Errorf("total_bytes=%v, want 3000", m["total_bytes"])
	}
	// progress = 1000/3000 * 100 = 33.333...
	progress := m["progress_percent"].(float64)
	if progress < 33 || progress > 34 {
		t.Errorf("progress_percent=%v, want ~33.33", progress)
	}
	if m["bytes_per_second"] != float64(500) {
		t.Errorf("bytes_per_second=%v, want 500", m["bytes_per_second"])
	}
}

func TestChunked_TransferStatus_Completed(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()

	manifest := TransferManifest{
		Version:     1,
		Direction:   "put",
		TotalSize:   2048,
		TotalChunks: 2,
		BytesSent:   2048,
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "aaa"},
			{Index: 1, Offset: 1024, Size: 1024, Completed: true, Checksum: "bbb"},
		},
	}
	data, _ := json.Marshal(manifest)
	ffs.AddFile("/tmp/completed.transfer", data, 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"manifest_path": "/tmp/completed.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}
	if m["chunks_completed"] != float64(2) {
		t.Errorf("chunks_completed=%v, want 2", m["chunks_completed"])
	}
	if m["progress_percent"] != float64(100) {
		t.Errorf("progress_percent=%v, want 100", m["progress_percent"])
	}
}

func TestChunked_TransferStatus_InvalidJSON(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	ffs.AddFile("/tmp/bad.transfer", []byte("{not valid json!!}"), 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"manifest_path": "/tmp/bad.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid JSON manifest")
	}
	text := resultText(result)
	if !strings.Contains(text, "manifest") {
		t.Errorf("error should mention manifest, got: %s", text)
	}
}

// ==================== handleShellTransferResume ====================

func TestChunked_TransferResume_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"manifest_path": "/tmp/test.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing session_id")
	}
	text := resultText(result)
	if !strings.Contains(text, "session_id is required") {
		t.Errorf("error should mention session_id, got: %s", text)
	}
}

func TestChunked_TransferResume_MissingManifestPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing manifest_path")
	}
	text := resultText(result)
	if !strings.Contains(text, "manifest_path is required") {
		t.Errorf("error should mention manifest_path, got: %s", text)
	}
}

func TestChunked_TransferResume_ManifestNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_resume_nf")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":    "sess_resume_nf",
		"manifest_path": "/nonexistent.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent manifest")
	}
	text := resultText(result)
	if !strings.Contains(text, "manifest") {
		t.Errorf("error should mention manifest, got: %s", text)
	}
}

func TestChunked_TransferResume_GetDirection_SSHNoClient(t *testing.T) {
	// Resume a "get" transfer on SSH session without client - tests resumeChunkedGet path.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_resume_get", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	_ = sess.Initialize()
	sm.AddSession(sess)

	manifest := TransferManifest{
		Version:     1,
		Direction:   "get",
		RemotePath:  "/remote/file.bin",
		LocalPath:   "/local/file.bin",
		TotalSize:   2048,
		ChunkSize:   1024,
		TotalChunks: 2,
		SessionID:   "sess_resume_get",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "aaa"},
			{Index: 1, Offset: 1024, Size: 1024, Completed: false},
		},
		BytesSent: 1024,
	}
	data, _ := json.Marshal(manifest)
	ffs.AddFile("/tmp/resume_get.transfer", data, 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":    "sess_resume_get",
		"manifest_path": "/tmp/resume_get.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SFTP") || !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SFTP/SSH client, got: %s", text)
	}
}

func TestChunked_TransferResume_PutDirection_SSHNoClient(t *testing.T) {
	// Resume a "put" transfer on SSH session without client - tests resumeChunkedPut path.
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_resume_put", "ssh",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	_ = sess.Initialize()
	sm.AddSession(sess)

	manifest := TransferManifest{
		Version:     1,
		Direction:   "put",
		RemotePath:  "/remote/file.bin",
		LocalPath:   "/local/file.bin",
		TotalSize:   2048,
		ChunkSize:   1024,
		TotalChunks: 2,
		SessionID:   "sess_resume_put",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "aaa"},
			{Index: 1, Offset: 1024, Size: 1024, Completed: false},
		},
		BytesSent: 1024,
	}
	data, _ := json.Marshal(manifest)
	ffs.AddFile("/tmp/resume_put.transfer", data, 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":    "sess_resume_put",
		"manifest_path": "/tmp/resume_put.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for SSH session with no client")
	}
	text := resultText(result)
	if !strings.Contains(text, "SFTP") || !strings.Contains(text, "SSH client not initialized") {
		t.Errorf("error should mention SFTP/SSH client, got: %s", text)
	}
}

// ==================== loadManifest / saveManifest with real filesystem ====================

func TestChunked_SaveAndLoadManifest_RealFS(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "test.transfer")

	cfg := config.DefaultConfig()
	clk := fakeclock.New(time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC))
	srv := NewServer(cfg, WithClock(clk))

	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	manifest := &TransferManifest{
		Version:        1,
		Direction:      "get",
		RemotePath:     "/remote/data.bin",
		LocalPath:      "/local/data.bin",
		TotalSize:      5120,
		ChunkSize:      1024,
		TotalChunks:    5,
		StartedAt:      now,
		LastUpdatedAt:  now,
		SessionID:      "sess_manifest",
		BytesSent:      3072,
		BytesPerSecond: 1024,
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "hash0"},
			{Index: 1, Offset: 1024, Size: 1024, Completed: true, Checksum: "hash1"},
			{Index: 2, Offset: 2048, Size: 1024, Completed: true, Checksum: "hash2"},
			{Index: 3, Offset: 3072, Size: 1024, Completed: false},
			{Index: 4, Offset: 4096, Size: 1024, Completed: false},
		},
	}

	// Save
	if err := srv.saveManifest(manifest, manifestPath); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	// Verify file exists on disk
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest file should exist on disk: %v", err)
	}

	// Load
	loaded, err := srv.loadManifest(manifestPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	// Verify all fields
	if loaded.Version != 1 {
		t.Errorf("Version=%d, want 1", loaded.Version)
	}
	if loaded.Direction != "get" {
		t.Errorf("Direction=%q, want get", loaded.Direction)
	}
	if loaded.RemotePath != "/remote/data.bin" {
		t.Errorf("RemotePath=%q", loaded.RemotePath)
	}
	if loaded.LocalPath != "/local/data.bin" {
		t.Errorf("LocalPath=%q", loaded.LocalPath)
	}
	if loaded.TotalSize != 5120 {
		t.Errorf("TotalSize=%d, want 5120", loaded.TotalSize)
	}
	if loaded.ChunkSize != 1024 {
		t.Errorf("ChunkSize=%d, want 1024", loaded.ChunkSize)
	}
	if loaded.TotalChunks != 5 {
		t.Errorf("TotalChunks=%d, want 5", loaded.TotalChunks)
	}
	if loaded.SessionID != "sess_manifest" {
		t.Errorf("SessionID=%q", loaded.SessionID)
	}
	if loaded.BytesSent != 3072 {
		t.Errorf("BytesSent=%d, want 3072", loaded.BytesSent)
	}
	if loaded.BytesPerSecond != 1024 {
		t.Errorf("BytesPerSecond=%d, want 1024", loaded.BytesPerSecond)
	}
	if len(loaded.Chunks) != 5 {
		t.Fatalf("len(Chunks)=%d, want 5", len(loaded.Chunks))
	}
	for i := 0; i < 3; i++ {
		if !loaded.Chunks[i].Completed {
			t.Errorf("Chunk %d should be completed", i)
		}
	}
	for i := 3; i < 5; i++ {
		if loaded.Chunks[i].Completed {
			t.Errorf("Chunk %d should not be completed", i)
		}
	}
}

func TestChunked_SaveManifest_WithCompletedAt(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "completed.transfer")

	cfg := config.DefaultConfig()
	srv := NewServer(cfg)

	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	completedAt := time.Date(2025, 6, 1, 12, 5, 0, 0, time.UTC)
	manifest := &TransferManifest{
		Version:       1,
		Direction:     "put",
		TotalSize:     1024,
		TotalChunks:   1,
		BytesSent:     1024,
		StartedAt:     now,
		LastUpdatedAt: completedAt,
		CompletedAt:   &completedAt,
		SessionID:     "sess_done",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "done"},
		},
	}

	if err := srv.saveManifest(manifest, manifestPath); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	loaded, err := srv.loadManifest(manifestPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	if loaded.CompletedAt == nil {
		t.Fatal("CompletedAt should not be nil")
	}
	if !loaded.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt=%v, want %v", loaded.CompletedAt, completedAt)
	}
}

func TestChunked_LoadManifest_NonexistentFile(t *testing.T) {
	cfg := config.DefaultConfig()
	srv := NewServer(cfg)

	_, err := srv.loadManifest("/nonexistent/path/manifest.transfer")
	if err == nil {
		t.Fatal("expected error for nonexistent manifest file")
	}
	if !strings.Contains(err.Error(), "read manifest") {
		t.Errorf("error should mention read manifest, got: %s", err.Error())
	}
}

func TestChunked_LoadManifest_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "bad.transfer")
	if err := os.WriteFile(invalidPath, []byte("this is not json"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	srv := NewServer(cfg)

	_, err := srv.loadManifest(invalidPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
		t.Errorf("error should mention parse manifest, got: %s", err.Error())
	}
}

func TestChunked_LoadManifest_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.transfer")
	if err := os.WriteFile(emptyPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	srv := NewServer(cfg)

	_, err := srv.loadManifest(emptyPath)
	if err == nil {
		t.Fatal("expected error for empty manifest file")
	}
	if !strings.Contains(err.Error(), "parse manifest") {
		t.Errorf("error should mention parse manifest, got: %s", err.Error())
	}
}

// ==================== transferChunksGet (direct test with fakeFileHandle) ====================

func TestChunked_TransferChunksGet_SingleChunk(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// Prepare source data
	sourceData := []byte("hello world, this is test data!")
	chunkSize := len(sourceData)

	// Create local file via fakefs
	ffs.AddFile("/local/output.bin", make([]byte, len(sourceData)), 0644)
	localFile, err := ffs.OpenFile("/local/output.bin", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open local file: %v", err)
	}
	defer localFile.Close()

	// Truncate to expected size
	if err := localFile.Truncate(int64(len(sourceData))); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Create remote reader (bytes.Reader satisfies io.ReadSeeker)
	remoteReader := bytes.NewReader(sourceData)

	manifest := &TransferManifest{
		Version:       1,
		Direction:     "get",
		TotalSize:     int64(len(sourceData)),
		ChunkSize:     chunkSize,
		TotalChunks:   1,
		StartedAt:     clk.Now(),
		LastUpdatedAt: clk.Now(),
		SessionID:     "sess_test",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: chunkSize, Completed: false},
		},
	}

	manifestPath := "/tmp/test_get.transfer"

	// Advance clock to simulate some transfer time
	clk.Advance(2 * time.Second)

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}
	if m["chunks_completed"] != float64(1) {
		t.Errorf("chunks_completed=%v, want 1", m["chunks_completed"])
	}
	if m["progress_percent"] != float64(100) {
		t.Errorf("progress_percent=%v, want 100", m["progress_percent"])
	}
	if m["bytes_transferred"] != float64(len(sourceData)) {
		t.Errorf("bytes_transferred=%v, want %d", m["bytes_transferred"], len(sourceData))
	}

	// Verify chunk checksum was computed
	if !manifest.Chunks[0].Completed {
		t.Error("chunk 0 should be marked completed")
	}
	if manifest.Chunks[0].Checksum == "" {
		t.Error("chunk 0 should have a checksum")
	}

	// Verify checksum is correct
	hash := sha256.Sum256(sourceData)
	expectedChecksum := hex.EncodeToString(hash[:])
	if manifest.Chunks[0].Checksum != expectedChecksum {
		t.Errorf("checksum=%q, want %q", manifest.Chunks[0].Checksum, expectedChecksum)
	}
}

func TestChunked_TransferChunksGet_MultipleChunks(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// Create data that spans 3 chunks
	sourceData := make([]byte, 3000)
	for i := range sourceData {
		sourceData[i] = byte(i % 256)
	}
	chunkSize := 1024

	ffs.AddFile("/local/multi.bin", make([]byte, len(sourceData)), 0644)
	localFile, err := ffs.OpenFile("/local/multi.bin", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open local file: %v", err)
	}
	defer localFile.Close()
	if err := localFile.Truncate(int64(len(sourceData))); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	remoteReader := bytes.NewReader(sourceData)

	totalChunks := 3
	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   int64(len(sourceData)),
		ChunkSize:   chunkSize,
		TotalChunks: totalChunks,
		StartedAt:   clk.Now(),
		SessionID:   "sess_multi",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: false},
			{Index: 1, Offset: 1024, Size: 1024, Completed: false},
			{Index: 2, Offset: 2048, Size: 952, Completed: false}, // last chunk is smaller
		},
	}

	manifestPath := "/tmp/multi_get.transfer"
	clk.Advance(3 * time.Second)

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}
	if m["chunks_completed"] != float64(3) {
		t.Errorf("chunks_completed=%v, want 3", m["chunks_completed"])
	}

	// All chunks should be completed with checksums
	for i, chunk := range manifest.Chunks {
		if !chunk.Completed {
			t.Errorf("chunk %d should be completed", i)
		}
		if chunk.Checksum == "" {
			t.Errorf("chunk %d should have checksum", i)
		}
	}
}

func TestChunked_TransferChunksGet_SkipsCompleted(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := make([]byte, 2048)
	for i := range sourceData {
		sourceData[i] = byte(i % 256)
	}

	ffs.AddFile("/local/skip.bin", make([]byte, 2048), 0644)
	localFile, err := ffs.OpenFile("/local/skip.bin", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()
	localFile.Truncate(2048)

	remoteReader := bytes.NewReader(sourceData)

	// Chunk 0 is already completed
	hash := sha256.Sum256(sourceData[:1024])
	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   2048,
		ChunkSize:   1024,
		TotalChunks: 2,
		StartedAt:   clk.Now(),
		SessionID:   "sess_skip",
		BytesSent:   1024, // already transferred first chunk
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: hex.EncodeToString(hash[:])},
			{Index: 1, Offset: 1024, Size: 1024, Completed: false},
		},
	}

	manifestPath := "/tmp/skip_get.transfer"
	clk.Advance(1 * time.Second)

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	// Both chunks should be completed now
	for i, chunk := range manifest.Chunks {
		if !chunk.Completed {
			t.Errorf("chunk %d should be completed", i)
		}
	}

	// BytesSent should include both chunks
	if manifest.BytesSent != 2048 {
		t.Errorf("BytesSent=%d, want 2048", manifest.BytesSent)
	}
}

// ==================== transferChunksPut (direct test) ====================

func TestChunked_TransferChunksPut_SingleChunk(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := []byte("upload me please!")

	// Create local file in fakefs
	ffs.AddFile("/local/upload.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/upload.bin")
	if err != nil {
		t.Fatalf("open local: %v", err)
	}
	defer localFile.Close()

	// Create in-memory remote file (WriteSeeker)
	remoteBuffer := &seekableBuffer{data: make([]byte, len(sourceData))}

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "put",
		TotalSize:   int64(len(sourceData)),
		ChunkSize:   len(sourceData),
		TotalChunks: 1,
		StartedAt:   clk.Now(),
		SessionID:   "sess_put_test",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: len(sourceData), Completed: false},
		},
	}

	manifestPath := "/tmp/put_test.transfer"
	clk.Advance(1 * time.Second)

	result, err := srv.transferChunksPut(localFile, remoteBuffer, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}

	// Verify chunk completed
	if !manifest.Chunks[0].Completed {
		t.Error("chunk 0 should be completed")
	}
	if manifest.Chunks[0].Checksum == "" {
		t.Error("chunk 0 should have checksum")
	}

	// Verify correct data was written
	if !bytes.Equal(remoteBuffer.data[:len(sourceData)], sourceData) {
		t.Error("remote data mismatch")
	}
}

func TestChunked_TransferChunksPut_MultipleChunks(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := make([]byte, 2500)
	for i := range sourceData {
		sourceData[i] = byte(i % 256)
	}
	chunkSize := 1024

	ffs.AddFile("/local/multi_upload.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/multi_upload.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()

	remoteBuffer := &seekableBuffer{data: make([]byte, len(sourceData))}

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "put",
		TotalSize:   int64(len(sourceData)),
		ChunkSize:   chunkSize,
		TotalChunks: 3,
		StartedAt:   clk.Now(),
		SessionID:   "sess_multi_put",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: false},
			{Index: 1, Offset: 1024, Size: 1024, Completed: false},
			{Index: 2, Offset: 2048, Size: 452, Completed: false},
		},
	}

	manifestPath := "/tmp/multi_put.transfer"
	clk.Advance(2 * time.Second)

	result, err := srv.transferChunksPut(localFile, remoteBuffer, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}
	if m["chunks_completed"] != float64(3) {
		t.Errorf("chunks_completed=%v, want 3", m["chunks_completed"])
	}

	for i, chunk := range manifest.Chunks {
		if !chunk.Completed {
			t.Errorf("chunk %d should be completed", i)
		}
	}
}

// ==================== uploadChunk (direct test) ====================

func TestChunked_UploadChunk_Success(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := []byte("chunk data here")
	ffs.AddFile("/local/src.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/src.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()

	remoteBuffer := &seekableBuffer{data: make([]byte, len(sourceData))}

	chunk := &ChunkInfo{
		Index:  0,
		Offset: 0,
		Size:   len(sourceData),
	}

	manifest := &TransferManifest{
		Version:     1,
		ChunkSize:   len(sourceData),
		TotalChunks: 1,
	}
	manifestPath := "/tmp/upload_chunk.transfer"
	buf := make([]byte, len(sourceData))

	err = srv.uploadChunk(localFile, remoteBuffer, chunk, 0, buf, manifest, manifestPath)
	if err != nil {
		t.Fatalf("uploadChunk error: %v", err)
	}

	if !chunk.Completed {
		t.Error("chunk should be completed")
	}
	if chunk.Checksum == "" {
		t.Error("chunk should have checksum")
	}
	if manifest.BytesSent != int64(len(sourceData)) {
		t.Errorf("BytesSent=%d, want %d", manifest.BytesSent, len(sourceData))
	}

	// Verify data written to remote
	if !bytes.Equal(remoteBuffer.data[:len(sourceData)], sourceData) {
		t.Error("remote data mismatch")
	}
}

// ==================== finalizeChunkedTransfer (direct test) ====================

func TestChunked_FinalizeChunkedTransfer(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	startTime := clk.Now()
	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   4096,
		TotalChunks: 4,
		BytesSent:   4096,
		Chunks: []ChunkInfo{
			{Index: 0, Completed: true},
			{Index: 1, Completed: true},
			{Index: 2, Completed: true},
			{Index: 3, Completed: true},
		},
	}

	manifestPath := "/tmp/finalize.transfer"

	// Advance clock by 4 seconds => ~1024 bytes/sec
	clk.Advance(4 * time.Second)

	result := srv.finalizeChunkedTransfer(manifest, manifestPath, startTime)

	if result.Status != "completed" {
		t.Errorf("status=%q, want completed", result.Status)
	}
	if result.ChunksCompleted != 4 {
		t.Errorf("ChunksCompleted=%d, want 4", result.ChunksCompleted)
	}
	if result.TotalChunks != 4 {
		t.Errorf("TotalChunks=%d, want 4", result.TotalChunks)
	}
	if result.BytesTransferred != 4096 {
		t.Errorf("BytesTransferred=%d, want 4096", result.BytesTransferred)
	}
	if result.TotalBytes != 4096 {
		t.Errorf("TotalBytes=%d, want 4096", result.TotalBytes)
	}
	if result.Progress != 100 {
		t.Errorf("Progress=%f, want 100", result.Progress)
	}
	if result.DurationMs != 4000 {
		t.Errorf("DurationMs=%d, want 4000", result.DurationMs)
	}
	// BytesPerSecond should be ~1024
	if result.BytesPerSecond != 1024 {
		t.Errorf("BytesPerSecond=%d, want 1024", result.BytesPerSecond)
	}

	// manifest.CompletedAt should be set
	if manifest.CompletedAt == nil {
		t.Fatal("CompletedAt should be set")
	}
	if manifest.BytesPerSecond != 1024 {
		t.Errorf("manifest.BytesPerSecond=%d, want 1024", manifest.BytesPerSecond)
	}
}

func TestChunked_FinalizeChunkedTransfer_ZeroDuration(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	startTime := clk.Now()
	manifest := &TransferManifest{
		Version:     1,
		TotalSize:   100,
		TotalChunks: 1,
		BytesSent:   100,
		Chunks:      []ChunkInfo{{Index: 0, Completed: true}},
	}

	// Do NOT advance clock - duration is 0
	result := srv.finalizeChunkedTransfer(manifest, "/tmp/zero_dur.transfer", startTime)

	if result.Status != "completed" {
		t.Errorf("status=%q, want completed", result.Status)
	}
	// With 0 duration, BytesPerSecond should remain 0 (not divide by zero)
	if result.BytesPerSecond != 0 {
		t.Errorf("BytesPerSecond=%d, want 0 (zero duration)", result.BytesPerSecond)
	}
}

// ==================== Manifest save with fakefs ====================

func TestChunked_SaveManifest_FakeFS(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "put",
		RemotePath:  "/remote/test.bin",
		LocalPath:   "/local/test.bin",
		TotalSize:   2048,
		ChunkSize:   1024,
		TotalChunks: 2,
		SessionID:   "sess_save",
		BytesSent:   1024,
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "abc"},
			{Index: 1, Offset: 1024, Size: 1024, Completed: false},
		},
	}

	err := srv.saveManifest(manifest, "/tmp/save_test.transfer")
	if err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	// Verify written data
	data, readErr := ffs.ReadFile("/tmp/save_test.transfer")
	if readErr != nil {
		t.Fatalf("read saved manifest: %v", readErr)
	}

	var loaded TransferManifest
	if jsonErr := json.Unmarshal(data, &loaded); jsonErr != nil {
		t.Fatalf("parse saved manifest: %v", jsonErr)
	}
	if loaded.Direction != "put" {
		t.Errorf("Direction=%q, want put", loaded.Direction)
	}
	if loaded.BytesSent != 1024 {
		t.Errorf("BytesSent=%d, want 1024", loaded.BytesSent)
	}
}

// ==================== TransferManifest struct tests ====================

func TestChunked_ManifestFileChecksumField(t *testing.T) {
	manifest := TransferManifest{
		Version:      1,
		Direction:    "get",
		TotalSize:    1024,
		FileChecksum: "abcdef0123456789",
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded TransferManifest
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.FileChecksum != "abcdef0123456789" {
		t.Errorf("FileChecksum=%q, want abcdef0123456789", loaded.FileChecksum)
	}
}

// ==================== ChunkedTransferResult struct tests ====================

func TestChunked_ChunkedTransferResult_ErrorField(t *testing.T) {
	result := ChunkedTransferResult{
		Status: "error",
		Error:  "something went wrong",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded ChunkedTransferResult
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.Error != "something went wrong" {
		t.Errorf("Error=%q, want 'something went wrong'", loaded.Error)
	}
	if loaded.Status != "error" {
		t.Errorf("Status=%q, want 'error'", loaded.Status)
	}
}

// ==================== Constants ====================

func TestChunked_Constants(t *testing.T) {
	if DefaultChunkSize != 1024*1024 {
		t.Errorf("DefaultChunkSize=%d, want %d", DefaultChunkSize, 1024*1024)
	}
	if MaxChunkSize != 10*1024*1024 {
		t.Errorf("MaxChunkSize=%d, want %d", MaxChunkSize, 10*1024*1024)
	}
	if ManifestSuffix != ".transfer" {
		t.Errorf("ManifestSuffix=%q, want '.transfer'", ManifestSuffix)
	}
}

// ==================== Tool definitions ====================

func TestChunked_ToolDefinitions(t *testing.T) {
	tools := []struct {
		name string
		fn   func() mcpTool
	}{
		{"shellFileGetChunkedTool", func() mcpTool { return shellFileGetChunkedTool() }},
		{"shellFilePutChunkedTool", func() mcpTool { return shellFilePutChunkedTool() }},
		{"shellTransferStatusTool", func() mcpTool { return shellTransferStatusTool() }},
		{"shellTransferResumeTool", func() mcpTool { return shellTransferResumeTool() }},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			tool := tt.fn()
			if tool.Name == "" {
				t.Errorf("%s: tool name is empty", tt.name)
			}
			if tool.Description == "" {
				t.Errorf("%s: tool description is empty", tt.name)
			}
		})
	}
}

// ==================== Edge cases ====================

func TestChunked_TransferStatus_PartiallyCompletedManifest(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()

	// 5 chunks, 3 completed
	manifest := TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   5000,
		TotalChunks: 5,
		BytesSent:   3000,
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1000, Completed: true},
			{Index: 1, Offset: 1000, Size: 1000, Completed: true},
			{Index: 2, Offset: 2000, Size: 1000, Completed: true},
			{Index: 3, Offset: 3000, Size: 1000, Completed: false},
			{Index: 4, Offset: 4000, Size: 1000, Completed: false},
		},
	}
	data, _ := json.Marshal(manifest)
	ffs.AddFile("/tmp/partial.transfer", data, 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"manifest_path": "/tmp/partial.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "in_progress" {
		t.Errorf("status=%v, want in_progress", m["status"])
	}
	if m["chunks_completed"] != float64(3) {
		t.Errorf("chunks_completed=%v, want 3", m["chunks_completed"])
	}
	// progress = 3000/5000 * 100 = 60
	if m["progress_percent"] != float64(60) {
		t.Errorf("progress_percent=%v, want 60", m["progress_percent"])
	}
}

func TestChunked_GetChunked_DefaultChunkSize(t *testing.T) {
	// Test that default chunk size is used when not specified
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_default_chunk")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_default_chunk",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
		// chunk_size not specified - should use DefaultChunkSize
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Will fail at IsSSH check - that's expected, just verifying no panic
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

func TestChunked_PutChunked_DefaultChunkSize(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_default")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_default",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
		// no chunk_size
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

// ==================== SaveManifest periodic save + BytesPerSecond ====================

func TestChunked_TransferChunksGet_ManifestSavedPeriodically(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// Create data with 15 chunks to test periodic save (every 10 chunks)
	chunkSize := 100
	totalSize := chunkSize * 15
	sourceData := make([]byte, totalSize)
	for i := range sourceData {
		sourceData[i] = byte(i % 256)
	}

	ffs.AddFile("/local/periodic.bin", make([]byte, totalSize), 0644)
	localFile, err := ffs.OpenFile("/local/periodic.bin", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()
	localFile.Truncate(int64(totalSize))

	remoteReader := bytes.NewReader(sourceData)

	chunks := make([]ChunkInfo, 15)
	for i := range chunks {
		size := chunkSize
		if i == 14 {
			size = totalSize - (14 * chunkSize)
		}
		chunks[i] = ChunkInfo{
			Index:  i,
			Offset: int64(i * chunkSize),
			Size:   size,
		}
	}

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   int64(totalSize),
		ChunkSize:   chunkSize,
		TotalChunks: 15,
		StartedAt:   clk.Now(),
		SessionID:   "sess_periodic",
		Chunks:      chunks,
	}

	manifestPath := "/tmp/periodic_get.transfer"
	clk.Advance(1 * time.Second)

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	// Verify manifest was saved (should exist in fakefs)
	savedData, readErr := ffs.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatalf("manifest should have been saved: %v", readErr)
	}

	var savedManifest TransferManifest
	if jsonErr := json.Unmarshal(savedData, &savedManifest); jsonErr != nil {
		t.Fatalf("parse saved manifest: %v", jsonErr)
	}
	if savedManifest.CompletedAt == nil {
		t.Error("saved manifest should have CompletedAt set")
	}
}

func TestChunked_TransferChunksGet_BytesPerSecondCalculation(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := make([]byte, 10000)
	for i := range sourceData {
		sourceData[i] = byte(i % 256)
	}

	ffs.AddFile("/local/speed.bin", make([]byte, 10000), 0644)
	localFile, err := ffs.OpenFile("/local/speed.bin", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()
	localFile.Truncate(10000)

	remoteReader := bytes.NewReader(sourceData)

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   10000,
		ChunkSize:   10000,
		TotalChunks: 1,
		StartedAt:   clk.Now(),
		SessionID:   "sess_speed",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 10000, Completed: false},
		},
	}

	startTime := clk.Now()
	// 10000 bytes / 5 seconds = 2000 bytes/sec
	clk.Advance(5 * time.Second)

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, "/tmp/speed.transfer", startTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	bps := m["bytes_per_second"].(float64)
	if bps != 2000 {
		t.Errorf("bytes_per_second=%v, want 2000", bps)
	}
	durMs := m["duration_ms"].(float64)
	if durMs != 5000 {
		t.Errorf("duration_ms=%v, want 5000", durMs)
	}
}

// ==================== transferChunksPut skips completed chunks ====================

func TestChunked_TransferChunksPut_SkipsCompleted(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := make([]byte, 2048)
	for i := range sourceData {
		sourceData[i] = byte(i % 256)
	}

	ffs.AddFile("/local/skip_put.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/skip_put.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()

	remoteBuffer := &seekableBuffer{data: make([]byte, 2048)}

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "put",
		TotalSize:   2048,
		ChunkSize:   1024,
		TotalChunks: 2,
		StartedAt:   clk.Now(),
		SessionID:   "sess_skip_put",
		BytesSent:   1024, // first chunk already done
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "already_done"},
			{Index: 1, Offset: 1024, Size: 1024, Completed: false},
		},
	}

	manifestPath := "/tmp/skip_put.transfer"
	clk.Advance(1 * time.Second)

	result, err := srv.transferChunksPut(localFile, remoteBuffer, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error: %s", resultText(result))
	}

	for i, chunk := range manifest.Chunks {
		if !chunk.Completed {
			t.Errorf("chunk %d should be completed", i)
		}
	}
	// Chunk 0 checksum should still be "already_done" (not re-computed)
	if manifest.Chunks[0].Checksum != "already_done" {
		t.Errorf("chunk 0 checksum should be preserved: got %q", manifest.Chunks[0].Checksum)
	}
}

// ==================== uploadChunk error paths ====================

func TestChunked_UploadChunk_SeekRemoteError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := []byte("test data")
	ffs.AddFile("/local/src.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/src.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()

	// Use an errorWriter that fails on Seek
	remoteWriter := &errorWriteSeeker{
		seekErr: io.ErrClosedPipe,
	}

	chunk := &ChunkInfo{
		Index:  0,
		Offset: 0,
		Size:   len(sourceData),
	}

	manifest := &TransferManifest{Version: 1, ChunkSize: len(sourceData), TotalChunks: 1}
	buf := make([]byte, len(sourceData))

	uploadErr := srv.uploadChunk(localFile, remoteWriter, chunk, 0, buf, manifest, "/tmp/err.transfer")
	if uploadErr == nil {
		t.Fatal("expected error for remote seek failure")
	}
	if !strings.Contains(uploadErr.Error(), "seek remote") {
		t.Errorf("error should mention seek remote, got: %s", uploadErr.Error())
	}
}

func TestChunked_UploadChunk_WriteRemoteError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := []byte("test data")
	ffs.AddFile("/local/src.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/src.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()

	// Use a writer that succeeds on Seek but fails on Write
	remoteWriter := &errorWriteSeeker{
		writeErr: io.ErrShortWrite,
	}

	chunk := &ChunkInfo{
		Index:  0,
		Offset: 0,
		Size:   len(sourceData),
	}

	manifest := &TransferManifest{Version: 1, ChunkSize: len(sourceData), TotalChunks: 1}
	buf := make([]byte, len(sourceData))

	uploadErr := srv.uploadChunk(localFile, remoteWriter, chunk, 0, buf, manifest, "/tmp/write_err.transfer")
	if uploadErr == nil {
		t.Fatal("expected error for remote write failure")
	}
	if !strings.Contains(uploadErr.Error(), "write chunk") {
		t.Errorf("error should mention write chunk, got: %s", uploadErr.Error())
	}
}

// ==================== transferChunksGet error paths ====================

func TestChunked_TransferChunksGet_WriteAtError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := []byte("test data for error")

	// Use an errorFileHandle that fails on WriteAt
	localFile := &errorFileHandle{
		writeAtErr: io.ErrShortWrite,
	}

	remoteReader := bytes.NewReader(sourceData)

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   int64(len(sourceData)),
		ChunkSize:   len(sourceData),
		TotalChunks: 1,
		StartedAt:   clk.Now(),
		SessionID:   "sess_err",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: len(sourceData), Completed: false},
		},
	}

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, "/tmp/writeat_err.transfer", manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for WriteAt failure")
	}
	text := resultText(result)
	if !strings.Contains(text, "write chunk") {
		t.Errorf("error should mention write chunk, got: %s", text)
	}
}

// ==================== transferChunksPut with uploadChunk error ====================

func TestChunked_TransferChunksPut_UploadChunkError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	sourceData := []byte("test data")
	ffs.AddFile("/local/err_upload.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/err_upload.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()

	// Remote writer that fails on Write
	remoteWriter := &errorWriteSeeker{
		writeErr: io.ErrShortWrite,
	}

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "put",
		TotalSize:   int64(len(sourceData)),
		ChunkSize:   len(sourceData),
		TotalChunks: 1,
		StartedAt:   clk.Now(),
		SessionID:   "sess_upload_err",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: len(sourceData), Completed: false},
		},
	}

	result, err := srv.transferChunksPut(localFile, remoteWriter, manifest, "/tmp/upload_err.transfer", manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for upload chunk failure")
	}
	text := resultText(result)
	if !strings.Contains(text, "write chunk") {
		t.Errorf("error should mention write chunk, got: %s", text)
	}
}

// ==================== transferChunksGet read error (saves manifest) ====================

func TestChunked_TransferChunksGet_ReadError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// Use a reader that fails after being seeked
	remoteReader := &errorReadSeeker{
		readErr: io.ErrClosedPipe,
	}

	ffs.AddFile("/local/read_err.bin", make([]byte, 100), 0644)
	localFile, err := ffs.OpenFile("/local/read_err.bin", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()
	localFile.Truncate(100)

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   100,
		ChunkSize:   100,
		TotalChunks: 1,
		StartedAt:   clk.Now(),
		SessionID:   "sess_read_err",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 100, Completed: false},
		},
	}

	manifestPath := "/tmp/read_err_get.transfer"

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for read failure")
	}
	text := resultText(result)
	if !strings.Contains(text, "read chunk") {
		t.Errorf("error should mention read chunk, got: %s", text)
	}

	// Manifest should have been saved before returning error
	_, readErr := ffs.ReadFile(manifestPath)
	if readErr != nil {
		t.Error("manifest should have been saved before error return")
	}
}

func TestChunked_TransferChunksGet_SeekError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// Use a reader that fails on Seek
	remoteReader := &errorReadSeeker{
		seekErr: io.ErrClosedPipe,
	}

	ffs.AddFile("/local/seek_err.bin", make([]byte, 100), 0644)
	localFile, err := ffs.OpenFile("/local/seek_err.bin", os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()
	localFile.Truncate(100)

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   100,
		ChunkSize:   100,
		TotalChunks: 1,
		StartedAt:   clk.Now(),
		SessionID:   "sess_seek_err",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 100, Completed: false},
		},
	}

	result, err := srv.transferChunksGet(localFile, remoteReader, manifest, "/tmp/seek_err.transfer", manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for seek failure")
	}
	text := resultText(result)
	if !strings.Contains(text, "seek remote") {
		t.Errorf("error should mention seek remote, got: %s", text)
	}
}

// ==================== uploadChunk local seek/read error ====================

func TestChunked_UploadChunk_SeekLocalError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// Local file that fails on Seek
	localFile := &errorFileHandle{
		seekErr: io.ErrClosedPipe,
	}

	remoteBuffer := &seekableBuffer{data: make([]byte, 100)}

	chunk := &ChunkInfo{Index: 0, Offset: 0, Size: 10}
	manifest := &TransferManifest{Version: 1, ChunkSize: 10, TotalChunks: 1}
	buf := make([]byte, 10)

	uploadErr := srv.uploadChunk(localFile, remoteBuffer, chunk, 0, buf, manifest, "/tmp/seek_local.transfer")
	if uploadErr == nil {
		t.Fatal("expected error for local seek failure")
	}
	if !strings.Contains(uploadErr.Error(), "seek local") {
		t.Errorf("error should mention seek local, got: %s", uploadErr.Error())
	}
}

func TestChunked_UploadChunk_ReadLocalError(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// Local file that succeeds on Seek but fails on Read
	localFile := &errorFileHandle{
		readErr: io.ErrClosedPipe,
	}

	remoteBuffer := &seekableBuffer{data: make([]byte, 100)}

	chunk := &ChunkInfo{Index: 0, Offset: 0, Size: 10}
	manifest := &TransferManifest{Version: 1, ChunkSize: 10, TotalChunks: 1}
	buf := make([]byte, 10)

	uploadErr := srv.uploadChunk(localFile, remoteBuffer, chunk, 0, buf, manifest, "/tmp/read_local.transfer")
	if uploadErr == nil {
		t.Fatal("expected error for local read failure")
	}
	if !strings.Contains(uploadErr.Error(), "read chunk") {
		t.Errorf("error should mention read chunk, got: %s", uploadErr.Error())
	}
}

// ==================== saveManifest error path ====================

func TestChunked_SaveManifest_WriteFileError(t *testing.T) {
	efs := &errorFS{
		FS:           fakefs.New(),
		writeFileErr: io.ErrClosedPipe,
	}
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(efs))

	manifest := &TransferManifest{
		Version:   1,
		Direction: "get",
		TotalSize: 100,
	}

	err := srv.saveManifest(manifest, "/tmp/should_fail.transfer")
	if err == nil {
		t.Fatal("expected error for write failure")
	}
	if !strings.Contains(err.Error(), "write manifest") {
		t.Errorf("error should mention write manifest, got: %s", err.Error())
	}
}

// ==================== chunk size clamped to minimum ====================

func TestChunked_GetChunked_ChunkSizeClampedToMin(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_clamp_min_get")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_clamp_min_get",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
		"chunk_size":  float64(100), // below minimum of 1024
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Will fail at IsSSH check, but exercises the min clamping path
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

func TestChunked_PutChunked_ChunkSizeClampedToMin(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_clamp_min_put")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_clamp_min_put",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
		"chunk_size":  float64(50), // below minimum of 1024
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

func TestChunked_PutChunked_ChunkSizeClampedToMax(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_clamp_max_put")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_clamp_max_put",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
		"chunk_size":  float64(999999999), // above MaxChunkSize
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

// ==================== transferChunksPut periodic save ====================

func TestChunked_TransferChunksPut_ManifestSavedPeriodically(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	// 15 chunks to trigger periodic save every 10 chunks
	chunkSize := 100
	totalSize := chunkSize * 15
	sourceData := make([]byte, totalSize)
	for i := range sourceData {
		sourceData[i] = byte(i % 256)
	}

	ffs.AddFile("/local/periodic_put.bin", sourceData, 0644)
	localFile, err := ffs.Open("/local/periodic_put.bin")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer localFile.Close()

	remoteBuffer := &seekableBuffer{data: make([]byte, totalSize)}

	chunks := make([]ChunkInfo, 15)
	for i := range chunks {
		size := chunkSize
		if i == 14 {
			size = totalSize - (14 * chunkSize)
		}
		chunks[i] = ChunkInfo{
			Index:  i,
			Offset: int64(i * chunkSize),
			Size:   size,
		}
	}

	manifest := &TransferManifest{
		Version:     1,
		Direction:   "put",
		TotalSize:   int64(totalSize),
		ChunkSize:   chunkSize,
		TotalChunks: 15,
		StartedAt:   clk.Now(),
		SessionID:   "sess_periodic_put",
		Chunks:      chunks,
	}

	manifestPath := "/tmp/periodic_put.transfer"
	clk.Advance(1 * time.Second)

	result, err := srv.transferChunksPut(localFile, remoteBuffer, manifest, manifestPath, manifest.StartedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}
	if m["chunks_completed"] != float64(15) {
		t.Errorf("chunks_completed=%v, want 15", m["chunks_completed"])
	}

	// Manifest should have been saved
	savedData, readErr := ffs.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatalf("manifest should have been saved: %v", readErr)
	}

	var savedManifest TransferManifest
	if jsonErr := json.Unmarshal(savedData, &savedManifest); jsonErr != nil {
		t.Fatalf("parse: %v", jsonErr)
	}
	if savedManifest.CompletedAt == nil {
		t.Error("saved manifest should have CompletedAt set")
	}
}

// ==================== uploadChunk saves manifest on read error ====================

func TestChunked_UploadChunk_ReadLocalError_SavesManifest(t *testing.T) {
	ffs := fakefs.New()
	clk := fakeclock.New(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs), WithClock(clk))

	localFile := &errorFileHandle{
		readErr: io.ErrClosedPipe,
	}

	remoteBuffer := &seekableBuffer{data: make([]byte, 100)}

	chunk := &ChunkInfo{Index: 0, Offset: 0, Size: 10}
	manifest := &TransferManifest{Version: 1, ChunkSize: 10, TotalChunks: 1}
	manifestPath := "/tmp/read_save.transfer"
	buf := make([]byte, 10)

	uploadErr := srv.uploadChunk(localFile, remoteBuffer, chunk, 0, buf, manifest, manifestPath)
	if uploadErr == nil {
		t.Fatal("expected error for local read failure")
	}

	// Verify manifest was saved before error return
	_, readErr := ffs.ReadFile(manifestPath)
	if readErr != nil {
		t.Error("manifest should have been saved before error return")
	}
}

// ==================== handleShellTransferResume session not found ====================

func TestChunked_TransferResume_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":    "sess_nonexistent",
		"manifest_path": "/tmp/test.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent session")
	}
	text := resultText(result)
	if !strings.Contains(text, "not found") {
		t.Errorf("error should mention not found, got: %s", text)
	}
}

// ==================== handleShellTransferResume invalid manifest JSON ====================

func TestChunked_TransferResume_InvalidManifest(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_resume_bad")
	sm.AddSession(sess)
	ffs.AddFile("/tmp/bad_resume.transfer", []byte("{bad json}"), 0644)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":    "sess_resume_bad",
		"manifest_path": "/tmp/bad_resume.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid manifest JSON")
	}
	text := resultText(result)
	if !strings.Contains(text, "manifest") {
		t.Errorf("error should mention manifest, got: %s", text)
	}
}

// ==================== handleShellFileGetChunked session not found ====================

func TestChunked_GetChunked_SessionNotFound(t *testing.T) {
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
		t.Fatal("expected error for nonexistent session")
	}
	text := resultText(result)
	if !strings.Contains(text, "not found") {
		t.Errorf("error should mention not found, got: %s", text)
	}
}

// ==================== TransferStatus with zero total size ====================

func TestChunked_TransferStatus_ZeroTotalSize(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()

	manifest := TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   0,
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
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	// All chunks completed (0 of 0)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}
}

// ==================== Error helper types ====================

// errorWriteSeeker is an io.WriteSeeker that returns configurable errors.
type errorWriteSeeker struct {
	seekErr  error
	writeErr error
}

func (e *errorWriteSeeker) Write(p []byte) (int, error) {
	if e.writeErr != nil {
		return 0, e.writeErr
	}
	return len(p), nil
}

func (e *errorWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	if e.seekErr != nil {
		return 0, e.seekErr
	}
	return offset, nil
}

// errorFileHandle is a ports.FileHandle that returns errors on specific operations.
type errorFileHandle struct {
	writeAtErr error
	seekErr    error
	readErr    error
}

func (e *errorFileHandle) Read(p []byte) (int, error) {
	if e.readErr != nil {
		return 0, e.readErr
	}
	return 0, io.EOF
}
func (e *errorFileHandle) Write(p []byte) (int, error) { return len(p), nil }
func (e *errorFileHandle) Seek(offset int64, whence int) (int64, error) {
	if e.seekErr != nil {
		return 0, e.seekErr
	}
	return offset, nil
}
func (e *errorFileHandle) Close() error                            { return nil }
func (e *errorFileHandle) ReadAt(p []byte, off int64) (int, error) { return 0, nil }
func (e *errorFileHandle) WriteAt(p []byte, off int64) (int, error) {
	if e.writeAtErr != nil {
		return 0, e.writeAtErr
	}
	return len(p), nil
}
func (e *errorFileHandle) Truncate(size int64) error { return nil }
func (e *errorFileHandle) Name() string              { return "error_file" }

// errorReadSeeker is an io.ReadSeeker that returns configurable errors.
type errorReadSeeker struct {
	seekErr error
	readErr error
}

func (e *errorReadSeeker) Read(p []byte) (int, error) {
	if e.readErr != nil {
		return 0, e.readErr
	}
	return 0, io.EOF
}

func (e *errorReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if e.seekErr != nil {
		return 0, e.seekErr
	}
	return offset, nil
}

// ==================== seekableBuffer helper for testing io.WriteSeeker ====================

// seekableBuffer implements io.WriteSeeker for testing transferChunksPut.
type seekableBuffer struct {
	data []byte
	pos  int64
}

func (sb *seekableBuffer) Write(p []byte) (int, error) {
	endPos := sb.pos + int64(len(p))
	if endPos > int64(len(sb.data)) {
		newData := make([]byte, endPos)
		copy(newData, sb.data)
		sb.data = newData
	}
	copy(sb.data[sb.pos:], p)
	sb.pos = endPos
	return len(p), nil
}

func (sb *seekableBuffer) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = sb.pos + offset
	case io.SeekEnd:
		newPos = int64(len(sb.data)) + offset
	default:
		return 0, io.ErrNoProgress
	}
	if newPos < 0 {
		return 0, io.ErrNoProgress
	}
	sb.pos = newPos
	return newPos, nil
}

// errorFS wraps a FileSystem and injects errors on WriteFile calls.
type errorFS struct {
	*fakefs.FS
	writeFileErr error
}

func (e *errorFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if e.writeFileErr != nil {
		return e.writeFileErr
	}
	return e.FS.WriteFile(name, data, perm)
}
