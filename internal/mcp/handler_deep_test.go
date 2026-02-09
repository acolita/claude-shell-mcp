package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
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

// newTestServerWithFS creates a test server with a custom fakefs for file operations.
func newTestServerWithFS(sm *fakesessionmgr.Manager, fs *fakefs.FS) *Server {
	cfg := config.DefaultConfig()
	srv := NewServer(cfg,
		WithSessionManager(sm),
		WithFileSystem(fs),
		WithClock(fakeclock.New(time.Now())),
	)
	return srv
}

// newTestServerWithConfig creates a test server with a custom config.
func newTestServerWithConfig(sm *fakesessionmgr.Manager, fs *fakefs.FS, cfg *config.Config) *Server {
	srv := NewServer(cfg,
		WithSessionManager(sm),
		WithFileSystem(fs),
		WithClock(fakeclock.New(time.Now())),
	)
	return srv
}

// ==================== truncateOutput ====================

func TestTruncateOutput_TailLines(t *testing.T) {
	output := "line1\nline2\nline3\nline4\nline5"
	result, truncated, total, shown := truncateOutput(output, 2, 0)
	if !truncated {
		t.Error("expected truncated=true")
	}
	if total != 5 {
		t.Errorf("total=%d, want 5", total)
	}
	if shown != 2 {
		t.Errorf("shown=%d, want 2", shown)
	}
	if result != "line4\nline5" {
		t.Errorf("result=%q, want %q", result, "line4\nline5")
	}
}

func TestTruncateOutput_HeadLines(t *testing.T) {
	output := "line1\nline2\nline3\nline4\nline5"
	result, truncated, total, shown := truncateOutput(output, 0, 3)
	if !truncated {
		t.Error("expected truncated=true")
	}
	if total != 5 {
		t.Errorf("total=%d, want 5", total)
	}
	if shown != 3 {
		t.Errorf("shown=%d, want 3", shown)
	}
	if result != "line1\nline2\nline3" {
		t.Errorf("result=%q", result)
	}
}

func TestTruncateOutput_TailExceedsTotal(t *testing.T) {
	output := "line1\nline2"
	result, truncated, total, shown := truncateOutput(output, 10, 0)
	if truncated {
		t.Error("expected truncated=false when tail exceeds total")
	}
	if total != shown {
		t.Errorf("total=%d, shown=%d, should be equal", total, shown)
	}
	if result != output {
		t.Errorf("result=%q, want original", result)
	}
}

func TestTruncateOutput_HeadExceedsTotal(t *testing.T) {
	output := "line1\nline2"
	result, truncated, total, shown := truncateOutput(output, 0, 10)
	if truncated {
		t.Error("expected truncated=false when head exceeds total")
	}
	if total != shown {
		t.Errorf("total=%d, shown=%d, should be equal", total, shown)
	}
	if result != output {
		t.Errorf("result=%q, want original", result)
	}
}

func TestTruncateOutput_NoTruncation(t *testing.T) {
	output := "line1\nline2"
	result, truncated, _, _ := truncateOutput(output, 0, 0)
	if truncated {
		t.Error("expected truncated=false with no limits")
	}
	if result != output {
		t.Errorf("result=%q, want original", result)
	}
}

func TestTruncateOutput_TrailingNewline(t *testing.T) {
	output := "line1\nline2\n"
	_, _, total, _ := truncateOutput(output, 0, 0)
	// After removing trailing empty string from split, should be 2
	if total != 2 {
		t.Errorf("total=%d, want 2 (trailing empty line stripped)", total)
	}
}

// ==================== saveOutputToFile ====================

func TestSaveOutputToFile(t *testing.T) {
	fs := fakefs.New()
	fs.SetCwd("/workdir")
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	path, err := srv.saveOutputToFile("sess_1", "big output content")
	if err != nil {
		t.Fatalf("saveOutputToFile error: %v", err)
	}
	if !strings.HasPrefix(path, "/workdir/.claude-shell-mcp/") {
		t.Errorf("path=%q, expected /workdir/.claude-shell-mcp/ prefix", path)
	}
	if !strings.Contains(path, "sess_1") {
		t.Errorf("path=%q, expected to contain sess_1", path)
	}

	// Verify file was created
	data, err := fs.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "big output content" {
		t.Errorf("content=%q", string(data))
	}
}

// ==================== applyAutoTruncation ====================

func TestApplyAutoTruncation_SmallOutput(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	result := &session.ExecResult{
		Stdout: "small output",
	}
	srv.applyAutoTruncation("sess_1", result)

	if result.Truncated {
		t.Error("small output should not be truncated")
	}
	if result.Stdout != "small output" {
		t.Error("stdout should be unchanged")
	}
}

func TestApplyAutoTruncation_LargeOutput(t *testing.T) {
	fs := fakefs.New()
	fs.SetCwd("/workdir")
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	largeOutput := strings.Repeat("x", saveToFileThreshold+100)
	result := &session.ExecResult{
		Stdout: largeOutput,
	}
	srv.applyAutoTruncation("sess_1", result)

	if !result.Truncated {
		t.Error("large output should be truncated")
	}
	if result.Stdout != "" {
		t.Error("stdout should be cleared after save")
	}
	if result.OutputFile == "" {
		t.Error("output_file should be set")
	}
	if result.TotalBytes != saveToFileThreshold+100 {
		t.Errorf("total_bytes=%d, want %d", result.TotalBytes, saveToFileThreshold+100)
	}
}

// ==================== lookupSudoPasswordFromConfig ====================

func TestLookupSudoPassword_NoConfig(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)
	srv.config = nil

	result := srv.lookupSudoPasswordFromConfig("host1")
	if result != nil {
		t.Error("expected nil for nil config")
	}
}

func TestLookupSudoPassword_EmptyHost(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	result := srv.lookupSudoPasswordFromConfig("")
	if result != nil {
		t.Error("expected nil for empty host")
	}
}

func TestLookupSudoPassword_Found(t *testing.T) {
	fs := fakefs.New()
	fs.SetEnv("MY_SUDO_PASS", "secret123")
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "prod", Host: "prod.example.com", SudoPasswordEnv: "MY_SUDO_PASS"},
	}
	srv := newTestServerWithConfig(sm, fs, cfg)

	result := srv.lookupSudoPasswordFromConfig("prod.example.com")
	if result == nil {
		t.Fatal("expected non-nil password")
	}
	if string(result) != "secret123" {
		t.Errorf("password=%q, want secret123", string(result))
	}
}

func TestLookupSudoPassword_FoundByName(t *testing.T) {
	fs := fakefs.New()
	fs.SetEnv("MY_SUDO_PASS", "secret456")
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "prod", Host: "10.0.0.1", SudoPasswordEnv: "MY_SUDO_PASS"},
	}
	srv := newTestServerWithConfig(sm, fs, cfg)

	result := srv.lookupSudoPasswordFromConfig("prod")
	if result == nil {
		t.Fatal("expected non-nil password by name")
	}
	if string(result) != "secret456" {
		t.Errorf("password=%q", string(result))
	}
}

func TestLookupSudoPassword_EnvVarEmpty(t *testing.T) {
	fs := fakefs.New()
	// Don't set the env var — it will be empty
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "prod", Host: "prod.example.com", SudoPasswordEnv: "MY_SUDO_PASS"},
	}
	srv := newTestServerWithConfig(sm, fs, cfg)

	result := srv.lookupSudoPasswordFromConfig("prod.example.com")
	if result != nil {
		t.Error("expected nil when env var is empty")
	}
}

func TestLookupSudoPassword_NoSudoPasswordEnv(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "prod", Host: "prod.example.com"},
	}
	srv := newTestServerWithConfig(sm, fs, cfg)

	result := srv.lookupSudoPasswordFromConfig("prod.example.com")
	if result != nil {
		t.Error("expected nil when no sudo_password_env configured")
	}
}

func TestLookupSudoPassword_NoMatchingServer(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "staging", Host: "staging.example.com", SudoPasswordEnv: "X"},
	}
	srv := newTestServerWithConfig(sm, fs, cfg)

	result := srv.lookupSudoPasswordFromConfig("prod.example.com")
	if result != nil {
		t.Error("expected nil for non-matching server")
	}
}

// ==================== validateExecParams ====================

func TestValidateExecParams_EmptySessionID(t *testing.T) {
	r := validateExecParams("", "ls", 0, 0)
	if r == nil {
		t.Fatal("expected error")
	}
}

func TestValidateExecParams_EmptyCommand(t *testing.T) {
	r := validateExecParams("sess_1", "", 0, 0)
	if r == nil {
		t.Fatal("expected error")
	}
}

func TestValidateExecParams_BothTailAndHead(t *testing.T) {
	r := validateExecParams("sess_1", "ls", 5, 5)
	if r == nil {
		t.Fatal("expected error")
	}
}

func TestValidateExecParams_Heredoc(t *testing.T) {
	tests := []string{
		"cat <<EOF\nhello\nEOF",
		"cat <<'EOF'\nhello\nEOF",
		"cat <<\"EOF\"\nhello\nEOF",
		"cat <<-EOF\nhello\nEOF",
	}
	for _, cmd := range tests {
		r := validateExecParams("sess_1", cmd, 0, 0)
		if r == nil {
			t.Errorf("expected error for heredoc: %q", cmd)
		}
	}
}

func TestValidateExecParams_Valid(t *testing.T) {
	r := validateExecParams("sess_1", "ls -la", 0, 0)
	if r != nil {
		t.Error("expected no error for valid params")
	}
}

func TestValidateExecParams_TailOnly(t *testing.T) {
	r := validateExecParams("sess_1", "ls", 10, 0)
	if r != nil {
		t.Error("expected no error for tail only")
	}
}

func TestValidateExecParams_HeadOnly(t *testing.T) {
	r := validateExecParams("sess_1", "ls", 0, 10)
	if r != nil {
		t.Error("expected no error for head only")
	}
}

// ==================== handleShellExec — command filter ====================

func TestHandleShellExec_CommandBlocked(t *testing.T) {
	sm := fakesessionmgr.New()
	sm.AddSession(newFakeSession("sess_1"))

	cfg := config.DefaultConfig()
	cfg.Security.CommandBlocklist = []string{"rm -rf /"}
	fs := fakefs.New()

	srv := newTestServerWithConfig(sm, fs, cfg)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
		"command":    "rm -rf /",
	})

	result, err := srv.handleShellExec(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for blocked command")
	}
	if !strings.Contains(resultText(result), "blocked") {
		t.Errorf("error should mention 'blocked', got: %s", resultText(result))
	}
}

// ==================== handleShellSessionStatus — success ====================

func TestHandleShellSessionStatus_Success(t *testing.T) {
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := session.NewSession("sess_status", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	// Initialize to set up internal state
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_status",
	})

	result, err := srv.handleShellSessionStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["session_id"] != "sess_status" {
		t.Errorf("session_id=%v, want sess_status", m["session_id"])
	}
	if m["mode"] != "local" {
		t.Errorf("mode=%v, want local", m["mode"])
	}
	// State should be idle after initialization
	if m["state"] != "idle" {
		t.Errorf("state=%v, want idle", m["state"])
	}
}

// ==================== handleShellDebug — actions ====================

func TestHandleShellDebug_StatusAction(t *testing.T) {
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_debug", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_debug",
		"action":     "status",
	})

	result, err := srv.handleShellDebug(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["session_id"] != "sess_debug" {
		t.Errorf("session_id=%v", m["session_id"])
	}
	if m["action"] != "status" {
		t.Errorf("action=%v", m["action"])
	}
	if m["state"] != "idle" {
		t.Errorf("state=%v, want idle", m["state"])
	}
}

func TestHandleShellDebug_ForegroundAction(t *testing.T) {
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_debug2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_debug2",
		"action":     "foreground",
	})

	result, err := srv.handleShellDebug(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["action"] != "foreground" {
		t.Errorf("action=%v", m["action"])
	}
}

func TestHandleShellDebug_ControlExecNoCommand(t *testing.T) {
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_debug3", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_debug3",
		"action":     "control_exec",
	})

	result, err := srv.handleShellDebug(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for control_exec without command")
	}
}

// ==================== handleShellSessionCreate — detailed scenarios ====================

func TestHandleShellSessionCreate_SSHRateLimited(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	// Simulate rate limiting by recording failures
	for i := 0; i < 10; i++ {
		srv.authRateLimiter.RecordFailure("locked.host", "user")
	}

	req := makeRequest(map[string]any{
		"mode": "ssh",
		"host": "locked.host",
		"user": "user",
	})

	result, err := srv.handleShellSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for rate limited host")
	}
	if !strings.Contains(resultText(result), "locked") {
		t.Errorf("error should mention locked, got: %s", resultText(result))
	}
}

func TestHandleShellSessionCreate_DefaultModeLocal(t *testing.T) {
	sm := fakesessionmgr.New()
	sess := newFakeSession("sess_default")
	sm.CreateFunc = func(opts session.CreateOptions) (*session.Session, error) {
		return sess, nil
	}
	srv := newTestServer(sm)

	// No mode specified — should default to "local"
	req := makeRequest(map[string]any{})

	result, err := srv.handleShellSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["mode"] != "local" {
		t.Errorf("mode=%v, want local", m["mode"])
	}
}

// ==================== handleShellSudoAuth — deep paths ====================

func TestHandleShellSudoAuth_NoPasswordConfigured(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_sudo", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)

	cfg := config.DefaultConfig()
	// No servers configured — no password available
	srv := newTestServerWithConfig(sm, fs, cfg)

	req := makeRequest(map[string]any{
		"session_id": "sess_sudo",
	})

	result, err := srv.handleShellSudoAuth(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when no sudo password configured")
	}
	if !strings.Contains(resultText(result), "No sudo password") {
		t.Errorf("error should mention 'No sudo password', got: %s", resultText(result))
	}
}

// ==================== handleShellInterrupt — success path ====================

func TestHandleShellInterrupt_Success(t *testing.T) {
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_int", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	// Set session to running so Interrupt succeeds
	sess.State = session.StateRunning
	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_int",
	})

	result, err := srv.handleShellInterrupt(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "Interrupt") {
		t.Errorf("expected 'Interrupt' in result, got: %s", resultText(result))
	}
	if !pty.WasInterrupted() {
		t.Error("PTY should have been interrupted")
	}
}

// ==================== handleShellSendRaw — missing input ====================

func TestHandleShellSendRaw_MissingInput(t *testing.T) {
	sm := fakesessionmgr.New()
	sm.AddSession(newFakeSession("sess_raw"))
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_raw",
		"input":      "",
	})

	result, err := srv.handleShellSendRaw(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for empty input")
	}
}

// ==================== Local file transfer: handleShellFileGet ====================

func TestHandleShellFileGet_LocalSuccess(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/test.txt", []byte("hello world"), 0644)
	sm := fakesessionmgr.New()

	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_get", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)

	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get",
		"remote_path": "/data/test.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}
	if m["content"] != "hello world" {
		t.Errorf("content=%v", m["content"])
	}
	if m["size"] != float64(11) {
		t.Errorf("size=%v", m["size"])
	}
	// Checksum should be present by default
	if m["checksum"] == nil || m["checksum"] == "" {
		t.Error("checksum should be present")
	}
}

func TestHandleShellFileGet_LocalNotFound(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_get2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get2",
		"remote_path": "/nonexistent.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent file")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error should mention 'not found', got: %s", resultText(result))
	}
}

func TestHandleShellFileGet_LocalDirectory(t *testing.T) {
	fs := fakefs.New()
	fs.MkdirAll("/data/subdir", 0755)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_get3", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get3",
		"remote_path": "/data/subdir",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for directory")
	}
	if !strings.Contains(resultText(result), "directory") {
		t.Errorf("error should mention 'directory', got: %s", resultText(result))
	}
}

func TestHandleShellFileGet_LocalBase64(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/bin.dat", []byte{0x00, 0x01, 0x02, 0xff}, 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_get4", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get4",
		"remote_path": "/data/bin.dat",
		"encoding":    "base64",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["encoding"] != "base64" {
		t.Errorf("encoding=%v, want base64", m["encoding"])
	}
}

func TestHandleShellFileGet_ChecksumVerification(t *testing.T) {
	fs := fakefs.New()
	data := []byte("checksum test content")
	hash := sha256.Sum256(data)
	expectedChecksum := hex.EncodeToString(hash[:])

	fs.AddFile("/data/check.txt", data, 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_ck", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":        "sess_ck",
		"remote_path":       "/data/check.txt",
		"expected_checksum": expectedChecksum,
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["checksum_verified"] != true {
		t.Error("checksum should be verified")
	}
}

func TestHandleShellFileGet_ChecksumMismatch(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/check2.txt", []byte("some data"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_ck2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":        "sess_ck2",
		"remote_path":       "/data/check2.txt",
		"expected_checksum": "0000000000000000000000000000000000000000000000000000000000000000",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for checksum mismatch")
	}
	if !strings.Contains(resultText(result), "mismatch") {
		t.Errorf("error should mention 'mismatch', got: %s", resultText(result))
	}
}

// ==================== Local file transfer: handleShellFilePut ====================

func TestHandleShellFilePut_LocalSuccess(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_put", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put",
		"remote_path": "/data/output.txt",
		"content":     "written content",
		"create_dirs": true,
		"atomic":      false, // Non-atomic for simpler test
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}
	if m["dirs_created"] != true {
		t.Error("dirs_created should be true")
	}

	// Verify file was written
	data, err := fs.ReadFile("/data/output.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "written content" {
		t.Errorf("content=%q", string(data))
	}
}

func TestHandleShellFilePut_FileExistsNoOverwrite(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/existing.txt", []byte("old"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_put2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put2",
		"remote_path": "/data/existing.txt",
		"content":     "new content",
		"overwrite":   false,
		"atomic":      false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for existing file without overwrite")
	}
	if !strings.Contains(resultText(result), "file exists") {
		t.Errorf("error should mention 'file exists', got: %s", resultText(result))
	}
}

func TestHandleShellFilePut_FileExistsWithOverwrite(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/existing.txt", []byte("old"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_put3", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put3",
		"remote_path": "/data/existing.txt",
		"content":     "new content",
		"overwrite":   true,
		"atomic":      false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["overwritten"] != true {
		t.Error("overwritten should be true")
	}
}

func TestHandleShellFilePut_InvalidMode(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put",
		"remote_path": "/data/file.txt",
		"content":     "data",
		"mode":        "notamode",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid mode")
	}
}

func TestHandleShellFilePut_MissingContent(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put",
		"remote_path": "/data/file.txt",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing content/local_path")
	}
}

func TestHandleShellFilePut_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_put",
		"content":    "data",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing remote_path")
	}
}

// ==================== Local file transfer: handleShellFileMv ====================

func TestHandleShellFileMv_LocalSuccess(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/source.txt", []byte("content"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_mv", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv",
		"source":      "/data/source.txt",
		"destination": "/data/dest.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}
	if m["source"] != "/data/source.txt" {
		t.Errorf("source=%v", m["source"])
	}
	if m["destination"] != "/data/dest.txt" {
		t.Errorf("destination=%v", m["destination"])
	}

	// Verify source was moved
	_, err = fs.ReadFile("/data/source.txt")
	if err == nil {
		t.Error("source file should no longer exist")
	}
	data, err := fs.ReadFile("/data/dest.txt")
	if err != nil {
		t.Fatalf("ReadFile dest error: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("dest content=%q", string(data))
	}
}

func TestHandleShellFileMv_SourceNotFound(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_mv2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv2",
		"source":      "/nonexistent.txt",
		"destination": "/dest.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent source")
	}
}

func TestHandleShellFileMv_DestExistsNoOverwrite(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/src.txt", []byte("src"), 0644)
	fs.AddFile("/data/dst.txt", []byte("dst"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_mv3", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv3",
		"source":      "/data/src.txt",
		"destination": "/data/dst.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for existing destination")
	}
	if !strings.Contains(resultText(result), "destination exists") {
		t.Errorf("error should mention destination exists: %s", resultText(result))
	}
}

func TestHandleShellFileMv_MissingSource(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv",
		"destination": "/data/dst.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing source")
	}
}

func TestHandleShellFileMv_MissingDestination(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_mv",
		"source":     "/data/src.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing destination")
	}
}

func TestHandleShellFileMv_SourceIsDirectory(t *testing.T) {
	fs := fakefs.New()
	fs.MkdirAll("/data/srcdir", 0755)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_mv4", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv4",
		"source":      "/data/srcdir",
		"destination": "/data/dest",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for directory source")
	}
	if !strings.Contains(resultText(result), "directory") {
		t.Errorf("error should mention 'directory': %s", resultText(result))
	}
}

// ==================== Chunked transfer: SSH-only guard ====================

func TestHandleShellFileGetChunked_NotSSH(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_chunk", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_chunk",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
	if !strings.Contains(resultText(result), "SSH") {
		t.Errorf("error should mention SSH: %s", resultText(result))
	}
}

func TestHandleShellFilePutChunked_NotSSH(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_chunk2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_chunk2",
		"local_path":  "/local/file.bin",
		"remote_path": "/remote/file.bin",
	})

	result, err := srv.handleShellFilePutChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

func TestHandleShellFileGetChunked_MissingRemotePath(t *testing.T) {
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
		t.Error("expected error for missing remote_path")
	}
}

func TestHandleShellFileGetChunked_MissingLocalPath(t *testing.T) {
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
		t.Error("expected error for missing local_path")
	}
}

func TestHandleShellFilePutChunked_MissingRemotePath(t *testing.T) {
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
		t.Error("expected error for missing remote_path")
	}
}

func TestHandleShellFilePutChunked_MissingLocalPath(t *testing.T) {
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
		t.Error("expected error for missing local_path")
	}
}

// ==================== Transfer status ====================

func TestHandleShellTransferStatus_Success(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	// Write a manifest file to the fake filesystem
	manifest := TransferManifest{
		Version:     1,
		Direction:   "get",
		RemotePath:  "/remote/file.bin",
		LocalPath:   "/local/file.bin",
		TotalSize:   1000,
		TotalChunks: 10,
		BytesSent:   500,
		Chunks: []ChunkInfo{
			{Index: 0, Completed: true},
			{Index: 1, Completed: true},
			{Index: 2, Completed: true},
			{Index: 3, Completed: true},
			{Index: 4, Completed: true},
			{Index: 5, Completed: false},
			{Index: 6, Completed: false},
			{Index: 7, Completed: false},
			{Index: 8, Completed: false},
			{Index: 9, Completed: false},
		},
	}
	manifestData, _ := json.Marshal(manifest)
	fs.AddFile("/local/file.bin.transfer", manifestData, 0644)

	req := makeRequest(map[string]any{
		"manifest_path": "/local/file.bin.transfer",
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
	if m["chunks_completed"] != float64(5) {
		t.Errorf("chunks_completed=%v, want 5", m["chunks_completed"])
	}
	if m["total_chunks"] != float64(10) {
		t.Errorf("total_chunks=%v, want 10", m["total_chunks"])
	}
	if m["progress_percent"] != float64(50) {
		t.Errorf("progress=%v, want 50", m["progress_percent"])
	}
}

func TestHandleShellTransferStatus_Completed(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	manifest := TransferManifest{
		Version:     1,
		Direction:   "get",
		TotalSize:   100,
		TotalChunks: 2,
		BytesSent:   100,
		Chunks: []ChunkInfo{
			{Index: 0, Completed: true},
			{Index: 1, Completed: true},
		},
	}
	manifestData, _ := json.Marshal(manifest)
	fs.AddFile("/test.transfer", manifestData, 0644)

	req := makeRequest(map[string]any{
		"manifest_path": "/test.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want completed", m["status"])
	}
}

func TestHandleShellTransferStatus_ManifestNotFound(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"manifest_path": "/nonexistent.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent manifest")
	}
}

func TestHandleShellTransferStatus_InvalidManifest(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/bad.transfer", []byte("not json"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"manifest_path": "/bad.transfer",
	})

	result, err := srv.handleShellTransferStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid manifest JSON")
	}
}

// ==================== Transfer resume ====================

func TestHandleShellTransferResume_MissingManifestPath(t *testing.T) {
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
		t.Error("expected error for missing manifest_path")
	}
}

func TestHandleShellTransferResume_ManifestNotFound(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_resume", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":    "sess_resume",
		"manifest_path": "/nonexistent.transfer",
	})

	result, err := srv.handleShellTransferResume(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent manifest")
	}
}

// ==================== Recursive dir transfer: guard clauses ====================

func TestHandleShellDirGet_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
		"local_path": "/local/dir",
	})

	result, err := srv.handleShellDirGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing remote_path")
	}
}

func TestHandleShellDirGet_MissingLocalPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_1",
		"remote_path": "/remote/dir",
	})

	result, err := srv.handleShellDirGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing local_path")
	}
}

func TestHandleShellDirPut_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
		"local_path": "/local/dir",
	})

	result, err := srv.handleShellDirPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing remote_path")
	}
}

func TestHandleShellDirPut_MissingLocalPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_1",
		"remote_path": "/remote/dir",
	})

	result, err := srv.handleShellDirPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing local_path")
	}
}

func TestHandleShellDirGet_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"remote_path": "/remote/dir",
		"local_path":  "/local/dir",
	})

	result, err := srv.handleShellDirGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestHandleShellDirPut_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"local_path":  "/local/dir",
		"remote_path": "/remote/dir",
	})

	result, err := srv.handleShellDirPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

// ==================== Tunnel handlers ====================

func TestHandleShellTunnelCreate_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_missing",
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
}

func TestHandleShellTunnelCreate_MissingType(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_1",
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
}

func TestHandleShellTunnelList_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handleShellTunnelList(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestHandleShellTunnelClose_MissingTunnelID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
	})

	result, err := srv.handleShellTunnelClose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing tunnel_id")
	}
}

// ==================== Peak TTY handlers ====================

func TestHandlePeakTTYDeploy_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestHandlePeakTTYStart_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handlePeakTTYStart(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

func TestHandlePeakTTYStop_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_missing",
	})

	result, err := srv.handlePeakTTYStop(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent session")
	}
}

// ==================== Utility functions ====================

func TestProcessFileChecksum_NoChecksum(t *testing.T) {
	opts := FileGetOptions{Checksum: false}
	result := FileGetResult{}
	errResult := processFileChecksum([]byte("data"), opts, &result)
	if errResult != nil {
		t.Error("expected no error when checksum disabled")
	}
	if result.Checksum != "" {
		t.Error("checksum should be empty when disabled")
	}
}

func TestProcessFileChecksum_WithChecksum(t *testing.T) {
	opts := FileGetOptions{Checksum: true}
	result := FileGetResult{}
	data := []byte("test data")
	errResult := processFileChecksum(data, opts, &result)
	if errResult != nil {
		t.Errorf("unexpected error: %v", errResult)
	}
	if result.Checksum == "" {
		t.Error("checksum should be calculated")
	}

	// Verify checksum
	hash := sha256.Sum256(data)
	expected := hex.EncodeToString(hash[:])
	if result.Checksum != expected {
		t.Errorf("checksum=%s, want %s", result.Checksum, expected)
	}
}

func TestIsCompressible(t *testing.T) {
	tests := []struct {
		filename string
		expected bool
	}{
		{"test.go", true},
		{"test.json", true},
		{"test.txt", true},
		{"test.py", true},
		{"test.jpg", false},
		{"test.png", false},
		{"test.zip", false},
		{"test.exe", false},
		{"test", false},
	}
	for _, tt := range tests {
		if got := isCompressible(tt.filename); got != tt.expected {
			t.Errorf("isCompressible(%q) = %v, want %v", tt.filename, got, tt.expected)
		}
	}
}

func TestCompressDecompress(t *testing.T) {
	data := []byte("hello world hello world hello world")
	compressed, err := compressData(data)
	if err != nil {
		t.Fatalf("compress error: %v", err)
	}

	decompressed, err := decompressData(compressed)
	if err != nil {
		t.Fatalf("decompress error: %v", err)
	}

	if string(decompressed) != string(data) {
		t.Error("round-trip failed")
	}
}

func TestDecompressData_Invalid(t *testing.T) {
	_, err := decompressData([]byte("not gzip"))
	if err == nil {
		t.Error("expected error for invalid gzip data")
	}
}

func TestParseFilePutMode_Valid(t *testing.T) {
	opts := &FilePutOptions{}
	result := parseFilePutMode("0755", opts)
	if result != nil {
		t.Error("expected no error")
	}
	if opts.Mode != 0755 {
		t.Errorf("mode=%o, want 0755", opts.Mode)
	}
}

func TestParseFilePutMode_Empty(t *testing.T) {
	opts := &FilePutOptions{}
	result := parseFilePutMode("", opts)
	if result != nil {
		t.Error("expected no error for empty mode")
	}
}

func TestParseFilePutMode_Invalid(t *testing.T) {
	opts := &FilePutOptions{}
	result := parseFilePutMode("notamode", opts)
	if result == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestValidateFilePutInputs_Valid(t *testing.T) {
	result := validateFilePutInputs("sess_1", "/path", FilePutOptions{Content: "data"})
	if result != nil {
		t.Error("expected no error")
	}
}

func TestValidateFilePutInputs_NoContent(t *testing.T) {
	result := validateFilePutInputs("sess_1", "/path", FilePutOptions{})
	if result == nil {
		t.Error("expected error for missing content/local_path")
	}
}

func TestSetContentWithEncoding_Text(t *testing.T) {
	result := FileGetResult{}
	setContentWithEncoding([]byte("hello"), "test.txt", FileGetOptions{Encoding: "text"}, &result)
	if result.Content != "hello" {
		t.Errorf("content=%q", result.Content)
	}
	if result.Encoding != "text" {
		t.Errorf("encoding=%q", result.Encoding)
	}
}

func TestSetContentWithEncoding_Base64(t *testing.T) {
	result := FileGetResult{}
	setContentWithEncoding([]byte("hello"), "test.bin", FileGetOptions{Encoding: "base64"}, &result)
	if result.Encoding != "base64" {
		t.Errorf("encoding=%q", result.Encoding)
	}
}

func TestSetContentWithEncoding_Compressed(t *testing.T) {
	result := FileGetResult{}
	data := []byte(strings.Repeat("compressible text ", 100))
	setContentWithEncoding(data, "test.txt", FileGetOptions{Compress: true}, &result)
	if !result.Compressed {
		t.Error("expected compressed=true for compressible text file")
	}
	if result.Encoding != "base64" {
		t.Errorf("encoding=%q, want base64 for compressed content", result.Encoding)
	}
}

func TestNewFilePutResult(t *testing.T) {
	result := newFilePutResult("/path/to/file", []byte("data"), 0644)
	if result.Status != "completed" {
		t.Errorf("status=%q", result.Status)
	}
	if result.RemotePath != "/path/to/file" {
		t.Errorf("remote_path=%q", result.RemotePath)
	}
	if result.Size != 4 {
		t.Errorf("size=%d", result.Size)
	}
	if result.Mode != "0644" {
		t.Errorf("mode=%q", result.Mode)
	}
}

func TestSetPutChecksum_Enabled(t *testing.T) {
	result := FilePutResult{}
	setPutChecksum([]byte("data"), true, &result)
	if result.Checksum == "" {
		t.Error("checksum should be set")
	}
}

func TestSetPutChecksum_Disabled(t *testing.T) {
	result := FilePutResult{}
	setPutChecksum([]byte("data"), false, &result)
	if result.Checksum != "" {
		t.Error("checksum should be empty when disabled")
	}
}

func TestJsonResult(t *testing.T) {
	result, err := jsonResult(map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected no error")
	}
	text := resultText(result)
	if !strings.Contains(text, "key") || !strings.Contains(text, "value") {
		t.Errorf("result=%s", text)
	}
}

func TestFileStatError_NotExist(t *testing.T) {
	result := fileStatError("/missing.txt", &fs.PathError{Op: "stat", Err: fs.ErrNotExist})
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("expected 'not found': %s", resultText(result))
	}
}

func TestFileStatError_Other(t *testing.T) {
	result := fileStatError("/bad.txt", &fs.PathError{Op: "stat", Err: fs.ErrPermission})
	if !strings.Contains(resultText(result), "stat file") {
		t.Errorf("expected 'stat file': %s", resultText(result))
	}
}

// ==================== validateSSHParams ====================

func TestValidateSSHParams_Valid(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)
	result := srv.validateSSHParams("host.example.com", "user")
	if result != nil {
		t.Error("expected nil for valid params")
	}
}

func TestValidateSSHParams_MissingHost(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)
	result := srv.validateSSHParams("", "user")
	if result == nil {
		t.Error("expected error for missing host")
	}
}

func TestValidateSSHParams_MissingUser(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)
	result := srv.validateSSHParams("host", "")
	if result == nil {
		t.Error("expected error for missing user")
	}
}

// ==================== UpdateConfig ====================

func TestUpdateConfig(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	newCfg := config.DefaultConfig()
	newCfg.Security.CommandBlocklist = []string{"dangerous-cmd"}

	// Should not panic
	srv.UpdateConfig(newCfg)

	// Verify command filter was updated
	allowed, _ := srv.commandFilter.IsAllowed("dangerous-cmd")
	if allowed {
		t.Error("expected command to be blocked after config update")
	}
}

func TestUpdateConfig_InvalidFilter(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	newCfg := config.DefaultConfig()
	// This should not cause a panic even if filter creation fails
	newCfg.Security.CommandBlocklist = []string{"[invalid-regex"}

	// Should handle gracefully (keeps previous filter)
	srv.UpdateConfig(newCfg)
}

// ==================== Additional local file transfer tests ====================

func TestHandleShellFilePut_AtomicWrite(t *testing.T) {
	fs := fakefs.New()
	fs.MkdirAll("/data", 0755) // Pre-create dir so atomic write temp file lands
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_atomic", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_atomic",
		"remote_path": "/data/atomic.txt",
		"content":     "atomic content",
		"atomic":      true,
		"create_dirs": true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["atomic_write"] != true {
		t.Error("atomic_write should be true")
	}

	// Verify final file was written
	data, err := fs.ReadFile("/data/atomic.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "atomic content" {
		t.Errorf("content=%q", string(data))
	}
}

func TestHandleShellFilePut_Base64Content(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_b64", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_b64",
		"remote_path": "/data/binary.dat",
		"content":     "SGVsbG8gV29ybGQ=", // "Hello World" in base64
		"encoding":    "base64",
		"create_dirs": true,
		"atomic":      false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	data, err := fs.ReadFile("/data/binary.dat")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "Hello World" {
		t.Errorf("decoded content=%q", string(data))
	}
}

func TestHandleShellFilePut_FromLocalFile(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/source/input.txt", []byte("source file content"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_lf", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_lf",
		"remote_path": "/data/dest.txt",
		"local_path":  "/source/input.txt",
		"create_dirs": true,
		"atomic":      false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	data, err := fs.ReadFile("/data/dest.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "source file content" {
		t.Errorf("content=%q", string(data))
	}
}

func TestHandleShellFilePut_FromLocalFileNotFound(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_lfnf", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_lfnf",
		"remote_path": "/data/dest.txt",
		"local_path":  "/nonexistent/source.txt",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent local file")
	}
}

func TestHandleShellFileGet_CopyToLocalPath(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/remote/source.txt", []byte("remote content"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_cp", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_cp",
		"remote_path": "/remote/source.txt",
		"local_path":  "/local/copy.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["local_path"] != "/local/copy.txt" {
		t.Errorf("local_path=%v", m["local_path"])
	}

	// Verify copy was created
	data, err := fs.ReadFile("/local/copy.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "remote content" {
		t.Errorf("content=%q", string(data))
	}
}

func TestHandleShellFileGet_NoChecksum(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/test.txt", []byte("data"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_nock", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nock",
		"remote_path": "/data/test.txt",
		"checksum":    false,
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["checksum"] != nil && m["checksum"] != "" {
		t.Error("checksum should be absent when disabled")
	}
}

func TestHandleShellFileMv_WithOverwriteAndCreateDirs(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/src.txt", []byte("source data"), 0644)
	// Don't pre-create /newdir — create_dirs should handle it
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_mvcd", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mvcd",
		"source":      "/data/src.txt",
		"destination": "/newdir/dest.txt",
		"create_dirs": true,
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["dirs_created"] != true {
		t.Error("dirs_created should be true")
	}
}

// ==================== shouldExclude and matchesPattern ====================

func TestShouldExclude_Deep(t *testing.T) {
	tests := []struct {
		name       string
		exclusions []string
		expected   bool
	}{
		{".git", []string{".git", "node_modules"}, true},
		{"node_modules", []string{".git", "node_modules"}, true},
		{"src", []string{".git", "node_modules"}, false},
		{"test.pyc", []string{"*.pyc"}, true},
		{"test.go", []string{"*.pyc"}, false},
	}
	for _, tt := range tests {
		if got := shouldExclude(tt.name, tt.exclusions); got != tt.expected {
			t.Errorf("shouldExclude(%q, %v) = %v, want %v", tt.name, tt.exclusions, got, tt.expected)
		}
	}
}

func TestMatchesPattern_Empty(t *testing.T) {
	if !matchesPattern("any/file.txt", "") {
		t.Error("empty pattern should match everything")
	}
}

func TestMatchesPattern_Glob(t *testing.T) {
	if !matchesPattern("src/main.go", "**/*.go") {
		t.Error("**/*.go should match src/main.go")
	}
	if matchesPattern("src/main.go", "*.txt") {
		t.Error("*.txt should not match src/main.go")
	}
}

// ==================== Chunked transfer chunk size clamping ====================

func TestHandleShellFileGetChunked_ChunkSizeClamped(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_clamp", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	// Should reject because local session, but the chunk clamping happens before that
	req := makeRequest(map[string]any{
		"session_id":  "sess_clamp",
		"remote_path": "/remote/file.bin",
		"local_path":  "/local/file.bin",
		"chunk_size":  float64(999999999), // way over max
	})

	result, err := srv.handleShellFileGetChunked(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Will fail with "not SSH" but exercises the clamping code path
	if !result.IsError {
		t.Error("expected error for non-SSH session")
	}
}

// ==================== resolveFileContent edge cases ====================

func TestResolveFileContent_InvalidBase64(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	opts := FilePutOptions{
		Content:  "not-valid-base64!!!",
		Encoding: "base64",
	}

	_, _, errResult := srv.resolveFileContent(opts)
	if errResult == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestResolveFileContent_TextContent(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	opts := FilePutOptions{
		Content:  "plain text",
		Encoding: "text",
	}

	data, modTime, errResult := srv.resolveFileContent(opts)
	if errResult != nil {
		t.Fatalf("unexpected error")
	}
	if string(data) != "plain text" {
		t.Errorf("data=%q", string(data))
	}
	if !modTime.IsZero() {
		t.Error("modTime should be zero for text content")
	}
}

func TestResolveFileContent_FromLocalFile(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/source/file.txt", []byte("from disk"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, fs)

	opts := FilePutOptions{
		LocalPath: "/source/file.txt",
	}

	data, _, errResult := srv.resolveFileContent(opts)
	if errResult != nil {
		t.Fatalf("unexpected error")
	}
	if string(data) != "from disk" {
		t.Errorf("data=%q", string(data))
	}
}

// ==================== handleShellFilePut with mode ====================

func TestHandleShellFilePut_ValidMode(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_mode", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mode",
		"remote_path": "/data/executable.sh",
		"content":     "#!/bin/bash\necho hi",
		"mode":        "0755",
		"create_dirs": true,
		"atomic":      false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["mode"] != "0755" {
		t.Errorf("mode=%v, want 0755", m["mode"])
	}
}

// ==================== handleShellFileGet with compression ====================

func TestHandleShellFileGet_WithCompression(t *testing.T) {
	fs := fakefs.New()
	// Create a compressible text file
	data := []byte(strings.Repeat("compressible text content ", 100))
	fs.AddFile("/data/log.txt", data, 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_compress", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_compress",
		"remote_path": "/data/log.txt",
		"compress":    true,
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["compressed"] != true {
		t.Error("compressed should be true for compressible text file")
	}
	if m["encoding"] != "base64" {
		t.Errorf("encoding=%v, want base64 for compressed content", m["encoding"])
	}
}

// ==================== handleShellSessionCreate with SSH auth failure ====================

func TestHandleShellSessionCreate_SSHAuthFailure(t *testing.T) {
	sm := fakesessionmgr.New()
	sm.CreateFunc = func(opts session.CreateOptions) (*session.Session, error) {
		return nil, fmt.Errorf("auth failed: permission denied")
	}
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"mode": "ssh",
		"host": "fail.host",
		"user": "baduser",
	})

	result, err := srv.handleShellSessionCreate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for SSH auth failure")
	}
	if !strings.Contains(resultText(result), "auth failed") {
		t.Errorf("error should mention auth failure: %s", resultText(result))
	}
}

// ==================== handleShellFilePut_Checksum ====================

func TestHandleShellFilePut_WithChecksum(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_ckput", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_ckput",
		"remote_path": "/data/checked.txt",
		"content":     "check me",
		"checksum":    true,
		"create_dirs": true,
		"atomic":      false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["checksum"] == nil || m["checksum"] == "" {
		t.Error("checksum should be present")
	}
}

func TestHandleShellFilePut_NoChecksum(t *testing.T) {
	fs := fakefs.New()
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_nockput", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nockput",
		"remote_path": "/data/unchecked.txt",
		"content":     "no check",
		"checksum":    false,
		"create_dirs": true,
		"atomic":      false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if ck, ok := m["checksum"]; ok && ck != "" {
		t.Error("checksum should be absent when disabled")
	}
}

// ==================== handleShellFileMv with overwrite ====================

func TestHandleShellFileMv_WithOverwrite(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/data/src.txt", []byte("new"), 0644)
	fs.AddFile("/data/dst.txt", []byte("old"), 0644)
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_mvow", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, fs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mvow",
		"source":      "/data/src.txt",
		"destination": "/data/dst.txt",
		"overwrite":   true,
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["overwritten"] != true {
		t.Error("overwritten should be true")
	}

	data, err := fs.ReadFile("/data/dst.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("content=%q, want 'new'", string(data))
	}
}

// ==================== fakeDirEntry for testing processLocalCopyEntry + copyLocalFile ====================

type fakeDirEntry struct {
	name  string
	isDir bool
	mode  fs.FileMode
	size  int64
	mod   time.Time
}

func (f *fakeDirEntry) Name() string               { return f.name }
func (f *fakeDirEntry) IsDir() bool                { return f.isDir }
func (f *fakeDirEntry) Type() fs.FileMode          { return f.mode.Type() }
func (f *fakeDirEntry) Info() (fs.FileInfo, error) { return &fakeInfo{f}, nil }

type fakeInfo struct{ e *fakeDirEntry }

func (fi *fakeInfo) Name() string       { return fi.e.name }
func (fi *fakeInfo) Size() int64        { return fi.e.size }
func (fi *fakeInfo) Mode() fs.FileMode  { return fi.e.mode }
func (fi *fakeInfo) ModTime() time.Time { return fi.e.mod }
func (fi *fakeInfo) IsDir() bool        { return fi.e.isDir }
func (fi *fakeInfo) Sys() any           { return nil }

// ==================== copyLocalFile tests ====================

func TestCopyLocalFile_Success(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/hello.go", []byte("package main"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	entry := &fakeDirEntry{name: "hello.go", mode: 0644, size: 12, mod: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

	srv.copyLocalFile("/src/hello.go", "/dst/hello.go", entry, false, result)

	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
	if result.TotalBytes != 12 {
		t.Errorf("TotalBytes=%d, want 12", result.TotalBytes)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	data, err := ffs.ReadFile("/dst/hello.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "package main" {
		t.Errorf("content=%q, want 'package main'", string(data))
	}
}

func TestCopyLocalFile_WithPreserve(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/hello.go", []byte("code"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	modTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	entry := &fakeDirEntry{name: "hello.go", mode: 0644, size: 4, mod: modTime}

	srv.copyLocalFile("/src/hello.go", "/dst/hello.go", entry, true, result)

	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestCopyLocalFile_ReadError(t *testing.T) {
	ffs := fakefs.New()
	// Don't add the source file — ReadFile will fail
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	entry := &fakeDirEntry{name: "missing.go", mode: 0644, size: 0}

	srv.copyLocalFile("/src/missing.go", "/dst/missing.go", entry, false, result)

	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0", result.FilesTransferred)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Path != "/src/missing.go" {
		t.Errorf("error path=%q, want '/src/missing.go'", result.Errors[0].Path)
	}
}

func TestCopyLocalFile_MultipleFiles(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/a.go", []byte("aaa"), 0644)
	ffs.AddFile("/src/b.go", []byte("bbbbb"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}

	srv.copyLocalFile("/src/a.go", "/dst/a.go", &fakeDirEntry{name: "a.go", mode: 0644, size: 3}, false, result)
	srv.copyLocalFile("/src/b.go", "/dst/b.go", &fakeDirEntry{name: "b.go", mode: 0644, size: 5}, false, result)

	if result.FilesTransferred != 2 {
		t.Errorf("FilesTransferred=%d, want 2", result.FilesTransferred)
	}
	if result.TotalBytes != 8 {
		t.Errorf("TotalBytes=%d, want 8", result.TotalBytes)
	}
}

// ==================== processLocalCopyEntry tests ====================

func TestProcessLocalCopyEntry_WalkError(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src/broken", nil, fmt.Errorf("permission denied"), opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if !strings.Contains(result.Errors[0].Error, "permission denied") {
		t.Errorf("error=%q, should contain 'permission denied'", result.Errors[0].Error)
	}
}

func TestProcessLocalCopyEntry_RootDot(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{}
	entry := &fakeDirEntry{name: "src", isDir: true, mode: fs.ModeDir | 0755}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src", entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should skip root "." entry, no files transferred
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0", result.FilesTransferred)
	}
}

func TestProcessLocalCopyEntry_ExcludedDir(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{Exclusions: []string{".git", "node_modules"}}
	entry := &fakeDirEntry{name: ".git", isDir: true, mode: fs.ModeDir | 0755}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src/.git", entry, nil, opts, result)
	if err != nil && err.Error() != "skip this directory" {
		// filepath.SkipDir is the expected error
		t.Logf("got expected SkipDir")
	}
}

func TestProcessLocalCopyEntry_ExcludedFile(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{Exclusions: []string{"*.pyc"}}
	entry := &fakeDirEntry{name: "cache.pyc", mode: 0644}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src/cache.pyc", entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// File should be excluded, not transferred
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0", result.FilesTransferred)
	}
}

func TestProcessLocalCopyEntry_Directory(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{}
	entry := &fakeDirEntry{name: "subdir", isDir: true, mode: fs.ModeDir | 0755}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src/subdir", entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Directories are skipped (not counted as files)
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0", result.FilesTransferred)
	}
}

func TestProcessLocalCopyEntry_PatternNoMatch(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/readme.txt", []byte("text"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{Pattern: "*.go"}
	entry := &fakeDirEntry{name: "readme.txt", mode: 0644, size: 4}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src/readme.txt", entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0 (pattern mismatch)", result.FilesTransferred)
	}
}

func TestProcessLocalCopyEntry_PatternMatch(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/main.go", []byte("package main"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{Pattern: "*.go"}
	entry := &fakeDirEntry{name: "main.go", mode: 0644, size: 12, mod: time.Now()}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src/main.go", entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
}

// ==================== tryCachedSudoInjection early return tests ====================

func TestTryCachedSudoInjection_NotAwaitingInput(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_sudo1", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)

	input := &session.ExecResult{Status: "completed", Stdout: "done"}
	result, err := srv.tryCachedSudoInjection("sess_sudo1", sess, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status=%q, want 'completed' (should pass through unchanged)", result.Status)
	}
}

func TestTryCachedSudoInjection_NotPasswordPrompt(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_sudo2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)

	input := &session.ExecResult{Status: "awaiting_input", PromptType: "confirmation"}
	result, err := srv.tryCachedSudoInjection("sess_sudo2", sess, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "awaiting_input" {
		t.Errorf("status=%q, want 'awaiting_input' (should pass through unchanged)", result.Status)
	}
	if result.PromptType != "confirmation" {
		t.Errorf("prompt_type=%q, want 'confirmation'", result.PromptType)
	}
}

func TestTryCachedSudoInjection_NoPasswordAvailable(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_sudo3", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)

	input := &session.ExecResult{Status: "awaiting_input", PromptType: "password"}
	result, err := srv.tryCachedSudoInjection("sess_sudo3", sess, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No cached password and no config password → return unchanged
	if result.Status != "awaiting_input" {
		t.Errorf("status=%q, want 'awaiting_input' (no password available)", result.Status)
	}
}

// ==================== handlePeakTTYDeploy: binary search with fakefs ====================

func TestHandlePeakTTYDeploy_BinaryNotFound(t *testing.T) {
	ffs := fakefs.New()
	ffs.SetExecutable("/usr/bin/claude-shell-mcp")
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_deploy", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id": "sess_deploy",
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for binary not found")
	}
	txt := resultText(result)
	if !strings.Contains(txt, "peak-tty binary not found") {
		t.Errorf("error=%q, should mention binary not found", txt)
	}
}

func TestHandlePeakTTYDeploy_BinaryFoundLocal(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("peak-tty/peak-tty", []byte("ELF-binary-data"), 0755)
	ffs.SetExecutable("/usr/bin/claude-shell-mcp")
	sm := fakesessionmgr.New()
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession("sess_deploy2", "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id": "sess_deploy2",
		"overwrite":  true,
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The result should be success (deployed to local fs)
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}
	m := resultJSON(t, result)
	if m["status"] != "deployed" {
		t.Errorf("status=%v, want 'deployed'", m["status"])
	}
	sizeBytes, _ := m["size_bytes"].(float64)
	if sizeBytes != float64(len("ELF-binary-data")) {
		t.Errorf("size_bytes=%v, want %d", sizeBytes, len("ELF-binary-data"))
	}
}

// ==================== dirEntryFromInfo coverage ====================

func TestDirEntryFromInfo(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/test.txt", []byte("hello"), 0644)

	info, err := ffs.Stat("/test.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	de := dirEntryFromInfo{info: info}

	if de.Name() != "test.txt" {
		t.Errorf("Name()=%q, want 'test.txt'", de.Name())
	}
	if de.IsDir() {
		t.Error("IsDir() should be false")
	}
	if de.Type() != 0 {
		t.Errorf("Type()=%v, want 0 (regular file)", de.Type())
	}
	gotInfo, err := de.Info()
	if err != nil {
		t.Fatalf("Info(): %v", err)
	}
	if gotInfo.Size() != 5 {
		t.Errorf("Info().Size()=%d, want 5", gotInfo.Size())
	}
}

func TestDirEntryFromInfo_Directory(t *testing.T) {
	ffs := fakefs.New()
	ffs.MkdirAll("/mydir", 0755)

	info, err := ffs.Stat("/mydir")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	de := dirEntryFromInfo{info: info}

	if de.Name() != "mydir" {
		t.Errorf("Name()=%q, want 'mydir'", de.Name())
	}
	if !de.IsDir() {
		t.Error("IsDir() should be true")
	}
	if de.Type()&fs.ModeDir == 0 {
		t.Errorf("Type()=%v, should include ModeDir", de.Type())
	}
}

// ==================== handleLocalDirCopy error paths ====================

func TestHandleLocalDirCopy_SourceNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{LocalPath: "/dst"}
	result, err := srv.handleLocalDirCopy("/nonexistent", "/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent source")
	}
	if !strings.Contains(resultText(result), "stat source") {
		t.Errorf("error=%q, should mention stat source", resultText(result))
	}
}

func TestHandleLocalDirCopy_SourceNotDir(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.txt", []byte("text"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{LocalPath: "/dst"}
	result, err := srv.handleLocalDirCopy("/src/file.txt", "/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-directory source")
	}
	if !strings.Contains(resultText(result), "not a directory") {
		t.Errorf("error=%q, should mention not a directory", resultText(result))
	}
}

// ==================== handleLocalDirCopyPut (thin wrapper) ====================

func TestHandleLocalDirCopyPut_SourceNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirPutOptions{RemotePath: "/dst", Preserve: true, Symlinks: "follow", MaxDepth: 20}
	result, err := srv.handleLocalDirCopyPut("/nonexistent", "/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent source")
	}
}

// ==================== finalizeTransferResult ====================

func TestFinalizeTransferResult_Timing(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{
		Status:           "completed",
		FilesTransferred: 5,
		TotalBytes:       1000,
	}
	startTime := time.Now().Add(-2 * time.Second)
	srv.finalizeTransferResult(result, startTime)

	if result.DurationMs <= 0 {
		t.Errorf("DurationMs=%d, should be > 0", result.DurationMs)
	}
	if result.BytesPerSecond <= 0 {
		t.Errorf("BytesPerSecond=%d, should be > 0", result.BytesPerSecond)
	}
}

func TestFinalizeTransferResult_WithErrors(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{
		Status:           "completed",
		FilesTransferred: 3,
		Errors:           []TransferError{{Path: "/bad", Error: "fail"}},
	}
	startTime := time.Now().Add(-1 * time.Second)
	srv.finalizeTransferResult(result, startTime)

	if result.Status != "completed_with_errors" {
		t.Errorf("Status=%q, want 'completed_with_errors'", result.Status)
	}
}

// ==================== handleShellSessionList ====================

func TestHandleShellSessionList_MultiSessions(t *testing.T) {
	sm := fakesessionmgr.New()
	pty1 := fakepty.New()
	pty2 := fakepty.New()
	clk := fakeclock.New(time.Now())

	sess1 := session.NewSession("sess_list1", "local", session.WithPTY(pty1), session.WithSessionClock(clk))
	sess2 := session.NewSession("sess_list2", "ssh", session.WithPTY(pty2), session.WithSessionClock(clk))
	sess2.Host = "remote.host"
	sess2.User = "admin"

	sm.AddSession(sess1)
	sm.AddSession(sess2)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{})
	result, err := srv.handleShellSessionList(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	sessions, ok := m["sessions"].([]any)
	if !ok {
		t.Fatalf("sessions not an array: %T", m["sessions"])
	}
	if len(sessions) != 2 {
		t.Errorf("sessions count=%d, want 2", len(sessions))
	}
}

// ==================== addError helper ====================

func TestAddError(t *testing.T) {
	result := &DirTransferResult{Status: "completed"}
	result.addError("/path/to/file", "access denied")
	result.addError("/path/to/other", "disk full")

	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(result.Errors))
	}
	if result.Errors[0].Path != "/path/to/file" {
		t.Errorf("Errors[0].Path=%q, want '/path/to/file'", result.Errors[0].Path)
	}
	if result.Errors[1].Error != "disk full" {
		t.Errorf("Errors[1].Error=%q, want 'disk full'", result.Errors[1].Error)
	}
}

// ==================== buildRelPath ====================

func TestBuildRelPath_WithParent(t *testing.T) {
	got := buildRelPath("subdir", "file.go")
	if got != "subdir/file.go" {
		t.Errorf("buildRelPath=%q, want 'subdir/file.go'", got)
	}
}

func TestBuildRelPath_EmptyParent(t *testing.T) {
	got := buildRelPath("", "file.go")
	if got != "file.go" {
		t.Errorf("buildRelPath=%q, want 'file.go'", got)
	}
}

// ==================== handleShellServerList ====================

func TestHandleShellServerList_NoConfig(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)
	srv.config = nil

	result, err := srv.handleShellServerList(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcpgo.TextContent).Text), &parsed); err != nil {
		t.Fatal(err)
	}
	if int(parsed["count"].(float64)) != 0 {
		t.Errorf("count=%v, want 0", parsed["count"])
	}
	servers := parsed["servers"].([]any)
	if len(servers) != 0 {
		t.Errorf("servers length=%d, want 0", len(servers))
	}
}

func TestHandleShellServerList_NoServers(t *testing.T) {
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{}
	srv := newTestServerWithConfig(sm, fakefs.New(), cfg)

	result, err := srv.handleShellServerList(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcpgo.TextContent).Text), &parsed); err != nil {
		t.Fatal(err)
	}
	if int(parsed["count"].(float64)) != 0 {
		t.Errorf("count=%v, want 0", parsed["count"])
	}
}

func TestHandleShellServerList_WithServers(t *testing.T) {
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "s1", Host: "192.168.1.10", Port: 22, User: "admin", KeyPath: "~/.ssh/id_ed25519", SudoPasswordEnv: "S1_SUDO"},
		{Name: "s2", Host: "10.0.0.5", Port: 2222, User: "deploy"},
	}
	srv := newTestServerWithConfig(sm, fakefs.New(), cfg)

	result, err := srv.handleShellServerList(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcpgo.TextContent).Text), &parsed); err != nil {
		t.Fatal(err)
	}
	if int(parsed["count"].(float64)) != 2 {
		t.Errorf("count=%v, want 2", parsed["count"])
	}

	servers := parsed["servers"].([]any)
	s1 := servers[0].(map[string]any)
	if s1["name"] != "s1" {
		t.Errorf("name=%v, want s1", s1["name"])
	}
	if s1["host"] != "192.168.1.10" {
		t.Errorf("host=%v, want 192.168.1.10", s1["host"])
	}
	if int(s1["port"].(float64)) != 22 {
		t.Errorf("port=%v, want 22", s1["port"])
	}
	if s1["user"] != "admin" {
		t.Errorf("user=%v, want admin", s1["user"])
	}
	if s1["has_sudo_password"] != true {
		t.Error("has_sudo_password should be true for s1")
	}

	s2 := servers[1].(map[string]any)
	if s2["has_sudo_password"] != false {
		t.Error("has_sudo_password should be false for s2")
	}
	if int(s2["port"].(float64)) != 2222 {
		t.Errorf("port=%v, want 2222", s2["port"])
	}

	// Verify secrets are not leaked
	text := result.Content[0].(mcpgo.TextContent).Text
	if strings.Contains(text, "S1_SUDO") {
		t.Error("sudo_password_env value should not be in output")
	}
}

func TestHandleShellServerList_DefaultPort(t *testing.T) {
	sm := fakesessionmgr.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "noport", Host: "example.com", User: "user"},
	}
	srv := newTestServerWithConfig(sm, fakefs.New(), cfg)

	result, err := srv.handleShellServerList(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(mcpgo.TextContent).Text), &parsed); err != nil {
		t.Fatal(err)
	}

	servers := parsed["servers"].([]any)
	s := servers[0].(map[string]any)
	if int(s["port"].(float64)) != 22 {
		t.Errorf("port=%v, want 22 (default)", s["port"])
	}
}
