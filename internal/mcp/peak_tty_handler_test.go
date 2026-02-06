package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakerand"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesessionmgr"
)

// fixedCmdID is the hex-encoded command ID produced by fakerand with bytes 0xAA,0xBB,0xCC,0xDD.
const fixedCmdID = "aabbccdd"

// makeExecResponse builds a fake PTY response that simulates a completed command
// with the given stdout and exit code, using marker-based output isolation.
// The cmdID must match what generateCommandID will produce.
func makeExecResponse(cmdID, stdout string, exitCode int) string {
	startMarker := "___CMD_START_" + cmdID + "___"
	endMarker := "___CMD_END_" + cmdID + "___"
	return fmt.Sprintf("%s\n%s\n%s%d\n", startMarker, stdout, endMarker, exitCode)
}

// makePwdResponse builds a fake PTY response for the pwd command that updateCwd issues
// after each successful Exec.
func makePwdResponse(cwd string) string {
	return cwd + "\n"
}

// newInitializedFakeSession creates a fake session with predictable random,
// initialized and ready for Exec calls. The returned fakepty can have responses
// queued on it.
func newInitializedFakeSession(id string) (*session.Session, *fakepty.PTY) {
	pty := fakepty.New()
	rng := fakerand.NewFixed([]byte{0xAA, 0xBB, 0xCC, 0xDD})
	clk := fakeclock.New(time.Now())

	sess := session.NewSession(id, "local",
		session.WithPTY(pty),
		session.WithSessionRandom(rng),
		session.WithSessionClock(clk),
	)
	if err := sess.Initialize(); err != nil {
		panic(fmt.Sprintf("failed to initialize session: %v", err))
	}
	return sess, pty
}

// --- handlePeakTTYStatus ---

func TestHandlePeakTTYStatus_Running(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_status1")

	// First Exec: pgrep -x peak-tty -> returns PID "12345"
	pty.AddResponse(makeExecResponse(fixedCmdID, "12345", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// Second Exec: test -x /tmp/peak-tty -> returns "exists"
	pty.AddResponse(makeExecResponse(fixedCmdID, "exists", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_status1",
	})

	result, err := srv.handlePeakTTYStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["running"] != true {
		t.Errorf("running = %v, want true", m["running"])
	}
	if m["pid"] != "12345" {
		t.Errorf("pid = %v, want 12345", m["pid"])
	}
	if m["binary_exists"] != true {
		t.Errorf("binary_exists = %v, want true", m["binary_exists"])
	}
	if m["session_id"] != "sess_status1" {
		t.Errorf("session_id = %v, want sess_status1", m["session_id"])
	}
}

func TestHandlePeakTTYStatus_NotRunning(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_status2")

	// First Exec: pgrep returns empty (not running)
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// Second Exec: test -x returns "missing"
	pty.AddResponse(makeExecResponse(fixedCmdID, "missing", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_status2",
	})

	result, err := srv.handlePeakTTYStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["running"] != false {
		t.Errorf("running = %v, want false", m["running"])
	}
	if m["pid"] != nil {
		t.Errorf("pid = %v, want nil (not present)", m["pid"])
	}
	if m["binary_exists"] != false {
		t.Errorf("binary_exists = %v, want false", m["binary_exists"])
	}
}

func TestHandlePeakTTYStatus_CustomBinaryPath(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_status3")

	// First Exec: pgrep returns empty
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// Second Exec: test -x /opt/bin/peak-tty -> "exists"
	pty.AddResponse(makeExecResponse(fixedCmdID, "exists", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_status3",
		"binary_path": "/opt/bin/peak-tty",
	})

	result, err := srv.handlePeakTTYStatus(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["binary_path"] != "/opt/bin/peak-tty" {
		t.Errorf("binary_path = %v, want /opt/bin/peak-tty", m["binary_path"])
	}
}

// --- handlePeakTTYStart ---

func TestHandlePeakTTYStart_AlreadyRunning(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_start1")

	// pgrep returns a PID (already running)
	pty.AddResponse(makeExecResponse(fixedCmdID, "9999", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_start1",
	})

	result, err := srv.handlePeakTTYStart(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when already running")
	}
	if !strings.Contains(resultText(result), "already running") {
		t.Errorf("error should mention 'already running', got: %s", resultText(result))
	}
}

func TestHandlePeakTTYStart_BinaryNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_start2")

	// pgrep returns empty (not running)
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// test -x returns "missing"
	pty.AddResponse(makeExecResponse(fixedCmdID, "missing", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_start2",
	})

	result, err := srv.handlePeakTTYStart(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when binary not found")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error should mention 'not found', got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "peak_tty_deploy") {
		t.Errorf("error should suggest peak_tty_deploy, got: %s", resultText(result))
	}
}

func TestHandlePeakTTYStart_Success(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_start3")

	// 1. pgrep returns empty (not running)
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 2. test -x returns "exists"
	pty.AddResponse(makeExecResponse(fixedCmdID, "exists", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 3. sudo bash -c start command completes
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 4. sleep 0.5 && pgrep verification returns PID
	pty.AddResponse(makeExecResponse(fixedCmdID, "5678", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_start3",
	})

	result, err := srv.handlePeakTTYStart(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "started" {
		t.Errorf("status = %v, want started", m["status"])
	}
	if m["pid"] != "5678" {
		t.Errorf("pid = %v, want 5678", m["pid"])
	}
	if m["binary_path"] != "/tmp/peak-tty" {
		t.Errorf("binary_path = %v, want /tmp/peak-tty", m["binary_path"])
	}
}

func TestHandlePeakTTYStart_FailedToStart(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_start4")

	// 1. pgrep returns empty (not running)
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 2. test -x returns "exists"
	pty.AddResponse(makeExecResponse(fixedCmdID, "exists", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 3. sudo start command completes
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 4. pgrep verification returns empty (failed to start)
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 1))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 5. cat log file
	pty.AddResponse(makeExecResponse(fixedCmdID, "error: permission denied", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_start4",
	})

	result, err := srv.handlePeakTTYStart(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when peak-tty fails to start")
	}
	if !strings.Contains(resultText(result), "failed to start") {
		t.Errorf("error should mention 'failed to start', got: %s", resultText(result))
	}
}

// --- handlePeakTTYStop ---

func TestHandlePeakTTYStop_NotRunning(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_stop1")

	// pgrep returns empty (not running)
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_stop1",
	})

	result, err := srv.handlePeakTTYStop(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when not running")
	}
	if !strings.Contains(resultText(result), "not running") {
		t.Errorf("error should mention 'not running', got: %s", resultText(result))
	}
}

func TestHandlePeakTTYStop_Success(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_stop2")

	// 1. pgrep returns PID "4321"
	pty.AddResponse(makeExecResponse(fixedCmdID, "4321", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	// 2. sudo pkill succeeds
	pty.AddResponse(makeExecResponse(fixedCmdID, "", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_stop2",
	})

	result, err := srv.handlePeakTTYStop(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "stopped" {
		t.Errorf("status = %v, want stopped", m["status"])
	}
	if m["killed_pid"] != "4321" {
		t.Errorf("killed_pid = %v, want 4321", m["killed_pid"])
	}
}

// --- handlePeakTTYDeploy ---

func TestHandlePeakTTYDeploy_BinaryAlreadyExists_NoOverwrite(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_deploy1")

	// test -e returns "exists"
	pty.AddResponse(makeExecResponse(fixedCmdID, "exists", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_deploy1",
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when binary exists and overwrite is false")
	}
	if !strings.Contains(resultText(result), "already exists") {
		t.Errorf("error should mention 'already exists', got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "overwrite=true") {
		t.Errorf("error should suggest overwrite=true, got: %s", resultText(result))
	}
}

func TestHandlePeakTTYDeploy_BinaryNotFoundLocally(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_deploy2")

	// test -e returns "missing" (no existing binary on remote)
	pty.AddResponse(makeExecResponse(fixedCmdID, "missing", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)

	// Use default fakefs which has no peak-tty binary files
	fakeFS := fakefs.New()
	srv := newTestServerWithFS(sm, fakeFS)

	req := makeRequest(map[string]any{
		"session_id": "sess_deploy2",
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when local binary not found")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error should mention 'not found', got: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "Build it first") {
		t.Errorf("error should mention build instructions, got: %s", resultText(result))
	}
}

func TestHandlePeakTTYDeploy_SuccessWithExecCheck(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_deploy3")

	// test -e returns "missing" (no existing binary on remote)
	pty.AddResponse(makeExecResponse(fixedCmdID, "missing", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)

	// Set up fakefs with the peak-tty binary in one of the search paths
	fakeFS := fakefs.New()
	binaryData := []byte("fake-peak-tty-binary-data")
	fakeFS.AddFile("peak-tty/peak-tty", binaryData, 0755)

	srv := newTestServerWithFS(sm, fakeFS)

	req := makeRequest(map[string]any{
		"session_id": "sess_deploy3",
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "deployed" {
		t.Errorf("status = %v, want deployed", m["status"])
	}
	if m["binary_path"] != "/tmp/peak-tty" {
		t.Errorf("binary_path = %v, want /tmp/peak-tty", m["binary_path"])
	}
	sizeBytes, ok := m["size_bytes"].(float64)
	if !ok || int(sizeBytes) != len(binaryData) {
		t.Errorf("size_bytes = %v, want %d", m["size_bytes"], len(binaryData))
	}
}

func TestHandlePeakTTYDeploy_CustomBinaryPath(t *testing.T) {
	sm := fakesessionmgr.New()
	sess, pty := newInitializedFakeSession("sess_deploy4")

	// test -e /opt/bin/peak-tty returns "missing"
	pty.AddResponse(makeExecResponse(fixedCmdID, "missing", 0))
	pty.AddResponse(makePwdResponse("/home/test"))

	sm.AddSession(sess)

	fakeFS := fakefs.New()
	binaryData := []byte("peak-tty-binary")
	fakeFS.AddFile("peak-tty/peak-tty", binaryData, 0755)

	srv := newTestServerWithFS(sm, fakeFS)

	req := makeRequest(map[string]any{
		"session_id":  "sess_deploy4",
		"binary_path": "/opt/bin/peak-tty",
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["binary_path"] != "/opt/bin/peak-tty" {
		t.Errorf("binary_path = %v, want /opt/bin/peak-tty", m["binary_path"])
	}
}

func TestHandlePeakTTYDeploy_OverwriteExisting(t *testing.T) {
	sm := fakesessionmgr.New()
	// With overwrite=true, the test -e check is skipped, so no Exec is needed for it.
	sess, _ := newInitializedFakeSession("sess_deploy5")

	sm.AddSession(sess)

	fakeFS := fakefs.New()
	binaryData := []byte("peak-tty-binary-v2")
	fakeFS.AddFile("peak-tty/peak-tty", binaryData, 0755)
	// Pre-existing file at the target path
	fakeFS.AddFile("/tmp/peak-tty", []byte("old-binary"), 0755)

	srv := newTestServerWithFS(sm, fakeFS)

	req := makeRequest(map[string]any{
		"session_id": "sess_deploy5",
		"overwrite":  true,
	})

	result, err := srv.handlePeakTTYDeploy(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "deployed" {
		t.Errorf("status = %v, want deployed", m["status"])
	}
	sizeBytes, ok := m["size_bytes"].(float64)
	if !ok || int(sizeBytes) != len(binaryData) {
		t.Errorf("size_bytes = %v, want %d", m["size_bytes"], len(binaryData))
	}
}
