package session

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/prompt"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
)

// ============================================================================
// configurablePTY is a test PTY that returns configurable errors.
// ============================================================================

type configurablePTY struct {
	writeErr    error // error to return on Write/WriteString
	readErr     error // error to return on Read
	readData    []byte
	written     strings.Builder
	interrupted bool
	closed      bool
}

func (e *configurablePTY) Read(b []byte) (int, error) {
	if e.readErr != nil {
		return 0, e.readErr
	}
	if len(e.readData) > 0 {
		n := copy(b, e.readData)
		e.readData = nil
		return n, nil
	}
	return 0, nil
}

func (e *configurablePTY) Write(b []byte) (int, error) {
	if e.writeErr != nil {
		return 0, e.writeErr
	}
	e.written.Write(b)
	return len(b), nil
}

func (e *configurablePTY) WriteString(s string) (int, error) {
	return e.Write([]byte(s))
}

func (e *configurablePTY) Written() string {
	return e.written.String()
}

func (e *configurablePTY) Interrupt() error {
	e.interrupted = true
	return nil
}

func (e *configurablePTY) Close() error {
	e.closed = true
	return nil
}

func (e *configurablePTY) SetReadDeadline(_ time.Time) error {
	return nil
}

// ============================================================================
// writeCommandWithReconnect tests
// ============================================================================

func TestPartial_WriteCommandWithReconnect_WriteSuccess(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		pty:   pty,
		clock: clock,
		Mode:  "local",
		State: StateRunning,
	}

	err := sess.writeCommandWithReconnect("echo hello\n")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(pty.Written(), "echo hello") {
		t.Error("expected command to be written to PTY")
	}
}

func TestPartial_WriteCommandWithReconnect_WriteFailsLocalMode(t *testing.T) {
	// When write fails on a local session, it should return an error
	// without attempting reconnect.
	ep := &configurablePTY{writeErr: fmt.Errorf("broken pipe")}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		pty:   ep,
		clock: clock,
		Mode:  "local",
		State: StateRunning,
	}

	err := sess.writeCommandWithReconnect("echo hello\n")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "write command") {
		t.Errorf("error = %q, want containing 'write command'", err.Error())
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q", sess.State, StateIdle)
	}
}

func TestPartial_WriteCommandWithReconnect_WriteFailsNonConnectionError(t *testing.T) {
	// A non-connection error (e.g., generic "timeout") should not trigger reconnect.
	ep := &configurablePTY{writeErr: fmt.Errorf("timeout")}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		pty:   ep,
		clock: clock,
		Mode:  "ssh",
		State: StateRunning,
	}

	err := sess.writeCommandWithReconnect("echo hello\n")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "write command") {
		t.Errorf("error = %q, want containing 'write command'", err.Error())
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q", sess.State, StateIdle)
	}
}

func TestPartial_WriteCommandWithReconnect_ConnectionBrokenSSHReconnectFails(t *testing.T) {
	// SSH mode, write returns EOF (connection broken), reconnect will fail
	// because there's no real SSH server.
	ep := &configurablePTY{writeErr: io.EOF}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		pty:   ep,
		clock: clock,
		Mode:  "ssh",
		Host:  "nonexistent.invalid",
		User:  "testuser",
		Port:  22,
		State: StateRunning,
	}

	err := sess.writeCommandWithReconnect("echo hello\n")
	if err == nil {
		t.Fatal("expected error when reconnect fails")
	}
	// Should contain "connection lost" message since reconnect fails
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error = %q, want containing 'connection lost'", err.Error())
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q", sess.State, StateIdle)
	}
}

// ============================================================================
// checkPTYAlive tests
// ============================================================================

func TestPartial_CheckPTYAlive_NoControlSessionNoPTYName(t *testing.T) {
	sess := &Session{
		controlSession: nil,
		PTYName:        "",
	}
	err := sess.checkPTYAlive()
	if err != nil {
		t.Errorf("expected nil when both controlSession and PTYName are empty, got %v", err)
	}
}

func TestPartial_CheckPTYAlive_HasControlSessionButNoPTYName(t *testing.T) {
	// ControlSession is set but PTYName is empty - should return nil (early return).
	sess := &Session{
		controlSession: &ControlSession{},
		PTYName:        "",
	}
	err := sess.checkPTYAlive()
	if err != nil {
		t.Errorf("expected nil when PTYName is empty, got %v", err)
	}
}

// NOTE: Testing checkPTYAlive with a dead PTY requires a working ControlSession.Exec,
// which uses readWithTimeout internally. Since readWithTimeout loops forever with
// fakeclock (Now() never advances), we cannot directly test the dead-PTY path without
// using real clocks (which makes tests slow and flaky). The early-return paths are
// covered above.

// ============================================================================
// ProvideInput tests
// ============================================================================

func TestPartial_ProvideInput_NotAwaitingInput(t *testing.T) {
	pty := fakepty.New()
	sess := &Session{
		pty:   pty,
		State: StateIdle,
	}

	_, err := sess.ProvideInput("yes")
	if err == nil {
		t.Fatal("expected error when not awaiting input")
	}
	if !strings.Contains(err.Error(), "not awaiting input") {
		t.Errorf("error = %q, want containing 'not awaiting input'", err.Error())
	}
}

func TestPartial_ProvideInput_SuccessWithCompletion(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_provide", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.State = StateAwaitingInput
	sess.pendingPrompt = nil

	// Queue response with end marker so readOutput completes
	pty.AddResponse("___CMD_END_MARKER___0\n")

	result, err := sess.ProvideInput("yes")
	if err != nil {
		t.Fatalf("ProvideInput error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}

	// Verify "yes\n" was written to PTY
	written := pty.Written()
	if !strings.Contains(written, "yes\n") {
		t.Errorf("expected 'yes\\n' to be written, got %q", written)
	}
}

func TestPartial_ProvideInput_WithPasswordPrompt(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_provide_pwd", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.State = StateAwaitingInput
	sess.pendingPrompt = &prompt.Detection{
		Pattern: prompt.Pattern{
			Type:      "password",
			MaskInput: true,
		},
	}

	// Queue response with end marker
	pty.AddResponse("___CMD_END_MARKER___0\n")

	result, err := sess.ProvideInput("mypassword")
	if err != nil {
		t.Fatalf("ProvideInput error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
}

// ============================================================================
// forceKillCommand tests
// ============================================================================

// NOTE: Testing forceKillCommand with a successful ControlSession.KillPTY is not feasible
// with fakepty + fakeclock because ControlSession.Exec uses drainOutputLocked() which
// consumes queued responses before the marker-reading loop can see them, and the
// 5-second context.WithTimeout causes real wall-clock delays. The fallback path
// (no control session) is tested below in TestPartial_ForceKillCommand_FallsBackToManual.

func TestPartial_ForceKillCommand_FallbackWithQuitAndFinalInterrupt(t *testing.T) {
	// Tests the forceKillCommandFallback path, exercising the "q" quit
	// and final interrupt branches. drainOutput reads up to 10 times,
	// so we queue 10 responses for it, then 1 more for the direct Read
	// that checks if output is still being produced (triggering "q" send),
	// then more for drainOutput after "q", then 1 more for the final
	// check (triggering the final 3 interrupts).
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	sess := &Session{
		pty:            pty,
		clock:          clock,
		controlSession: nil, // No control session -> fallback
		PTYName:        "3",
	}

	// 10 responses for first drainOutput (fills its 10-iteration loop)
	for i := 0; i < 10; i++ {
		pty.AddResponse("x")
	}
	// 1 response for the "still producing?" check at line 1291 -> triggers "q"
	pty.AddResponse("still running\n")
	// 10 responses for drainOutput after "q" (fills its 10-iteration loop)
	for i := 0; i < 10; i++ {
		pty.AddResponse("x")
	}
	// 1 response for the second "still producing?" check at line 1301 -> triggers final interrupts
	pty.AddResponse("more output\n")
	// 10 responses for the final drainOutput in the "n > 0" block
	for i := 0; i < 10; i++ {
		pty.AddResponse("x")
	}
	// Remaining drainOutputs will get (0, nil) and break

	sess.forceKillCommand()

	if !pty.WasInterrupted() {
		t.Error("expected PTY to be interrupted via fallback")
	}
	written := pty.Written()
	// Should have sent "q" since output was still being produced
	if !strings.Contains(written, "q") {
		t.Errorf("expected 'q' to be written for quit, got %q", written)
	}
	// Should have a trailing newline for fresh prompt
	if !strings.Contains(written, "\n") {
		t.Error("expected newline for fresh prompt")
	}
}

func TestPartial_ForceKillCommand_FallsBackToManual(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	sess := &Session{
		pty:            pty,
		clock:          clock,
		controlSession: nil, // No control session -> fallback
		PTYName:        "3",
	}

	sess.forceKillCommand()

	// Should have used interrupt as fallback
	if !pty.WasInterrupted() {
		t.Error("expected PTY to be interrupted via fallback")
	}
}

// ============================================================================
// restoreState tests
// ============================================================================

func TestPartial_RestoreState_NilPTY(t *testing.T) {
	sess := &Session{pty: nil}
	// Should return immediately without panic
	sess.restoreState("/some/dir", map[string]string{"FOO": "bar"})
}

func TestPartial_RestoreState_WithCwdAndEnvVars(t *testing.T) {
	// Use configurablePTY with a timeout error on Read so that readWithTimeout
	// breaks immediately (fakeclock + fakepty would cause infinite loop since
	// Now() never advances and Read returns (0, nil) forever).
	ep := &configurablePTY{readErr: &timeoutError{}}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	sess := &Session{
		pty:   ep,
		clock: clock,
	}

	envVars := map[string]string{
		"MY_VAR":         "my_value",
		"CUSTOM":         "custom_val",
		"PATH":           "/usr/bin",  // Should be skipped (system var)
		"HOME":           "/home/usr", // Should be skipped (system var)
		"PS1":            "$ ",        // Should be skipped
		"PWD":            "/tmp",      // Should be skipped
		"OLDPWD":         "/old",      // Should be skipped
		"SHLVL":          "2",         // Should be skipped
		"_":              "ignore",    // Should be skipped
		"TERM":           "xterm",     // Should be skipped
		"SHELL":          "/bin/bash", // Should be skipped
		"USER":           "test",      // Should be skipped
		"LOGNAME":        "test",      // Should be skipped
		"PROMPT_COMMAND": "",          // Should be skipped
	}

	sess.restoreState("/restored/dir", envVars)

	written := ep.Written()

	// Should have written cd command
	if !strings.Contains(written, "cd \"/restored/dir\"") {
		t.Errorf("expected cd command in written output, got %q", written)
	}

	// Should have exported custom variables
	if !strings.Contains(written, "export MY_VAR=") {
		t.Errorf("expected MY_VAR export in written output, got %q", written)
	}
	if !strings.Contains(written, "export CUSTOM=") {
		t.Errorf("expected CUSTOM export in written output, got %q", written)
	}

	// Should NOT have exported system variables
	if strings.Contains(written, "export PATH=") {
		t.Error("PATH should not be exported (system var)")
	}
	if strings.Contains(written, "export HOME=") {
		t.Error("HOME should not be exported (system var)")
	}

	// Cwd should be restored
	if sess.Cwd != "/restored/dir" {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, "/restored/dir")
	}

	// EnvVars should be updated
	if sess.EnvVars["MY_VAR"] != "my_value" {
		t.Errorf("EnvVars[MY_VAR] = %q, want %q", sess.EnvVars["MY_VAR"], "my_value")
	}
	if sess.EnvVars["CUSTOM"] != "custom_val" {
		t.Errorf("EnvVars[CUSTOM] = %q, want %q", sess.EnvVars["CUSTOM"], "custom_val")
	}
}

func TestPartial_RestoreState_EmptyCwd(t *testing.T) {
	// Use configurablePTY with timeout error so readWithTimeout breaks immediately.
	ep := &configurablePTY{readErr: &timeoutError{}}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	sess := &Session{
		pty:   ep,
		clock: clock,
	}

	sess.restoreState("", map[string]string{})

	written := ep.Written()
	// Should NOT have written a cd command for empty cwd
	if strings.Contains(written, "cd ") {
		t.Error("should not write cd for empty cwd")
	}
}

func TestPartial_RestoreState_HomeCwd(t *testing.T) {
	// Use configurablePTY with timeout error so readWithTimeout breaks immediately.
	ep := &configurablePTY{readErr: &timeoutError{}}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	sess := &Session{
		pty:   ep,
		clock: clock,
	}

	sess.restoreState("~", map[string]string{})

	written := ep.Written()
	// Should NOT have written a cd command for "~" cwd
	if strings.Contains(written, "cd ") {
		t.Error("should not write cd for ~ cwd")
	}
}

// ============================================================================
// processLegacyRead tests
// ============================================================================

func TestPartial_ProcessLegacyRead_ContextTimeout(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_timeout", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("partial output")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	buf := make([]byte, 4096)
	result, _, err := sess.processLegacyRead(ctx, buf, "cmd", 0, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result on context timeout")
	}
	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
}

func TestPartial_ProcessLegacyRead_CompletionDetected(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_complete", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	// Queue response with legacy end marker
	pty.AddResponse("output\n___CMD_END_MARKER___0\n")
	// Queue response for updateCwd's "pwd" call
	pty.AddResponse("/home/user\n")

	ctx := context.Background()
	buf := make([]byte, 4096)
	result, _, err := sess.processLegacyRead(ctx, buf, "cmd", 0, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result when completion marker is found")
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
}

func TestPartial_ProcessLegacyRead_StallCountResets(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_stall", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("some output so far")

	// No response queued - Read returns 0,nil which is a "no data" scenario
	ctx := context.Background()
	buf := make([]byte, 4096)

	// Not at stall threshold - should return nil result, same stall count
	result, newStall, err := sess.processLegacyRead(ctx, buf, "cmd", 5, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when no data and not at stall threshold")
	}
	if newStall != 5 {
		t.Errorf("stallCount = %d, want 5 (unchanged)", newStall)
	}
}

// ============================================================================
// handleLegacyContextTimeout tests
// ============================================================================

func TestPartial_HandleLegacyContextTimeout_NotCancelled(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, State: StateRunning}

	ctx := context.Background()
	result := sess.handleLegacyContextTimeout(ctx, "cmd")
	if result != nil {
		t.Error("expected nil result when context is not cancelled")
	}
}

func TestPartial_HandleLegacyContextTimeout_Cancelled_WithOutput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, State: StateRunning}
	sess.outputBuffer.WriteString("partial output before timeout")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := sess.handleLegacyContextTimeout(ctx, "long_running_cmd")
	if result == nil {
		t.Fatal("expected non-nil result when context is cancelled")
	}
	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q", sess.State, StateIdle)
	}
}

func TestPartial_HandleLegacyContextTimeout_Cancelled_NoOutput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, State: StateRunning}
	// No output buffer content

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := sess.handleLegacyContextTimeout(ctx, "cmd")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
	if result.Stdout != "" {
		t.Errorf("Stdout = %q, want empty", result.Stdout)
	}
}

// ============================================================================
// checkLegacyOutputForResult tests
// ============================================================================

func TestPartial_CheckLegacyOutputForResult_CompletionFound(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_check", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("result\n___CMD_END_MARKER___0\n")

	// Queue response for updateCwd
	pty.AddResponse("/home/user\n")

	result := sess.checkLegacyOutputForResult("cmd")
	if result == nil {
		t.Fatal("expected non-nil result when completion marker is present")
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
}

func TestPartial_CheckLegacyOutputForResult_PromptDetected(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_prompt", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("[sudo] password for user: ")

	result := sess.checkLegacyOutputForResult("sudo cmd")
	if result == nil {
		t.Fatal("expected non-nil result when prompt is detected")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "password" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "password")
	}
	if sess.State != StateAwaitingInput {
		t.Errorf("State = %q, want %q", sess.State, StateAwaitingInput)
	}
}

func TestPartial_CheckLegacyOutputForResult_NoMatch(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_no_match", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("just some normal output")

	result := sess.checkLegacyOutputForResult("cmd")
	if result != nil {
		t.Errorf("expected nil result for normal output, got %+v", result)
	}
}

// ============================================================================
// processMarkedRead tests
// ============================================================================

func TestPartial_ProcessMarkedRead_ContextTimeout(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_marked_timeout", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmdID := "abc12345"
	execCtx := newExecContext(cmdID,
		startMarkerPrefix+cmdID+markerSuffix,
		endMarkerPrefix+cmdID+markerSuffix,
		"sleep 100")

	buf := make([]byte, 4096)
	result, _, err := sess.processMarkedRead(ctx, buf, execCtx, 0, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result on context timeout")
	}
	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
}

func TestPartial_ProcessMarkedRead_CompletionFound(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_marked_complete", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	cmdID := "deadbeef"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	execCtx := newExecContext(cmdID, startM, endM, "ls")

	// Queue response with markers and exit code
	pty.AddResponse(startM + "\nfile.txt\n" + endM + "0\n")
	// Queue response for updateCwd
	pty.AddResponse("/home/user\n")

	ctx := context.Background()
	buf := make([]byte, 4096)
	result, _, err := sess.processMarkedRead(ctx, buf, execCtx, 0, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result when completion marker is found")
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
}

func TestPartial_ProcessMarkedRead_NoData(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_marked_nodata", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	cmdID := "11223344"
	execCtx := newExecContext(cmdID,
		startMarkerPrefix+cmdID+markerSuffix,
		endMarkerPrefix+cmdID+markerSuffix,
		"cmd")

	ctx := context.Background()
	buf := make([]byte, 4096)

	// No response queued - returns 0 bytes
	result, stall, err := sess.processMarkedRead(ctx, buf, execCtx, 3, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result with no data")
	}
	if stall != 3 {
		t.Errorf("stallCount = %d, want 3 (unchanged)", stall)
	}
}

// ============================================================================
// checkSSHConnection tests
// ============================================================================

func TestPartial_CheckSSHConnection_LocalMode(t *testing.T) {
	sess := &Session{Mode: "local"}
	err := sess.checkSSHConnection()
	if err != nil {
		t.Errorf("expected nil for local mode, got %v", err)
	}
}

func TestPartial_CheckSSHConnection_NilClient(t *testing.T) {
	sess := &Session{Mode: "ssh", sshClient: nil}
	err := sess.checkSSHConnection()
	if err != nil {
		t.Errorf("expected nil for nil sshClient, got %v", err)
	}
}

// ============================================================================
// handleLegacyReadError tests
// ============================================================================

func TestPartial_HandleLegacyReadError_NonTimeoutError(t *testing.T) {
	sess := &Session{}
	// A non-timeout, non-EOF error should return (nil, stallCount, false)
	// indicating it should NOT continue (cont = false).
	result, stall, cont := sess.handleLegacyReadError(
		fmt.Errorf("some fatal error"), "cmd", 5, 15)
	if result != nil {
		t.Error("expected nil result for non-timeout error")
	}
	if cont {
		t.Error("expected cont=false for non-timeout error")
	}
	if stall != 5 {
		t.Errorf("stallCount = %d, want 5", stall)
	}
}

func TestPartial_HandleLegacyReadError_TimeoutWithCompletion(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_err_complete", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("output\n___CMD_END_MARKER___0\n")

	// Queue response for updateCwd
	pty.AddResponse("/home\n")

	// Create a timeout error
	timeoutErr := &timeoutError{}
	result, _, cont := sess.handleLegacyReadError(timeoutErr, "cmd", 3, 15)
	if result == nil {
		t.Fatal("expected result when completion marker found after timeout")
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if cont {
		t.Error("expected cont=false when result is returned")
	}
}

func TestPartial_HandleLegacyReadError_TimeoutContinues(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_err_continue", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("partial output, no marker")

	timeoutErr := &timeoutError{}
	result, stall, cont := sess.handleLegacyReadError(timeoutErr, "cmd", 3, 15)
	if result != nil {
		t.Error("expected nil result when no completion or prompt")
	}
	if !cont {
		t.Error("expected cont=true to continue reading")
	}
	if stall != 4 {
		t.Errorf("stallCount = %d, want 4 (incremented)", stall)
	}
}

func TestPartial_HandleLegacyReadError_EOF(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_eof", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("no marker")

	// io.EOF should be treated like timeout
	result, stall, cont := sess.handleLegacyReadError(io.EOF, "cmd", 5, 15)
	if result != nil {
		t.Error("expected nil result")
	}
	if !cont {
		t.Error("expected cont=true for EOF (treated like timeout)")
	}
	if stall != 6 {
		t.Errorf("stallCount = %d, want 6", stall)
	}
}

// ============================================================================
// checkLegacyStallSignals tests
// ============================================================================

func TestPartial_CheckLegacyStallSignals_PeakTTYDetected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_stall_peak", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	output := "some output" + peakTTYSignal
	result := sess.checkLegacyStallSignals(output, "cmd")
	if result == nil {
		t.Fatal("expected result when peak-tty signal is detected")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "interactive" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "interactive")
	}
	if sess.State != StateAwaitingInput {
		t.Errorf("State = %q, want %q", sess.State, StateAwaitingInput)
	}
	// NUL bytes should be stripped
	if strings.Contains(result.Stdout, "\x00") {
		t.Error("Stdout should not contain NUL bytes")
	}
}

func TestPartial_CheckLegacyStallSignals_PasswordPromptDetected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_stall_pwd", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	output := "[sudo] password for user: "
	result := sess.checkLegacyStallSignals(output, "sudo cmd")
	if result == nil {
		t.Fatal("expected result when password prompt is detected")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "password" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "password")
	}
	if !result.MaskInput {
		t.Error("MaskInput should be true for password")
	}
}

func TestPartial_CheckLegacyStallSignals_NothingDetected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_stall_none", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	result := sess.checkLegacyStallSignals("normal output", "cmd")
	if result != nil {
		t.Error("expected nil result when nothing is detected")
	}
}

// ============================================================================
// handleReadError tests (marked mode)
// ============================================================================

func TestPartial_HandleReadError_NonTimeoutError(t *testing.T) {
	sess := &Session{}
	execCtx := newExecContext("abc", "start", "end", "cmd")

	result, stall, cont := sess.handleReadError(
		fmt.Errorf("fatal read error"), execCtx, 5, 15)
	if result != nil {
		t.Error("expected nil result for non-timeout error")
	}
	if cont {
		t.Error("expected cont=false for non-timeout error")
	}
	if stall != 5 {
		t.Errorf("stallCount = %d, want 5", stall)
	}
}

func TestPartial_HandleReadError_TimeoutWithCompletion(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_read_err_complete", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning

	cmdID := "aabb1122"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	execCtx := newExecContext(cmdID, startM, endM, "ls")

	sess.outputBuffer.WriteString(startM + "\nfile.txt\n" + endM + "0\n")
	// Queue response for updateCwd
	pty.AddResponse("/home\n")

	timeoutErr := &timeoutError{}
	result, _, cont := sess.handleReadError(timeoutErr, execCtx, 3, 15)
	if result == nil {
		t.Fatal("expected result when completion found")
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if cont {
		t.Error("expected cont=false when result is returned")
	}
}

func TestPartial_HandleReadError_TimeoutContinues(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_read_err_continue", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("partial output")

	cmdID := "ccdd5566"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	execCtx := newExecContext(cmdID, startM, endM, "cmd")

	timeoutErr := &timeoutError{}
	result, stall, cont := sess.handleReadError(timeoutErr, execCtx, 5, 15)
	if result != nil {
		t.Error("expected nil result when no completion")
	}
	if !cont {
		t.Error("expected cont=true to continue reading")
	}
	if stall != 6 {
		t.Errorf("stallCount = %d, want 6 (incremented)", stall)
	}
}

func TestPartial_HandleReadError_AtStallThreshold_PasswordPrompt(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_read_err_stall_pwd", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("[sudo] password for user: ")

	cmdID := "eeff0011"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	execCtx := newExecContext(cmdID, startM, endM, "sudo cmd")

	timeoutErr := &timeoutError{}
	// stallCount = stallThreshold to trigger prompt checking
	result, _, cont := sess.handleReadError(timeoutErr, execCtx, 15, 15)
	if result == nil {
		t.Fatal("expected result when password prompt detected at stall threshold")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if cont {
		t.Error("expected cont=false when result is returned")
	}
}

func TestPartial_HandleReadError_AtStallThreshold_NothingDetected(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_read_err_stall_none", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("normal output no prompts")

	cmdID := "aabb3344"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	execCtx := newExecContext(cmdID, startM, endM, "cmd")

	timeoutErr := &timeoutError{}
	// stallCount >= stallThreshold, nothing detected -> stall partially reset
	result, stall, cont := sess.handleReadError(timeoutErr, execCtx, 15, 15)
	if result != nil {
		t.Error("expected nil result when nothing detected")
	}
	if !cont {
		t.Error("expected cont=true to continue")
	}
	// stallCount should be partially reset to stallThreshold/2
	if stall != 7 {
		t.Errorf("stallCount = %d, want 7 (stallThreshold/2)", stall)
	}
}

func TestPartial_HandleReadError_PeakTTYSignal(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_read_err_peak", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("waiting" + peakTTYSignal)

	cmdID := "55667788"
	startM := startMarkerPrefix + cmdID + markerSuffix
	endM := endMarkerPrefix + cmdID + markerSuffix
	execCtx := newExecContext(cmdID, startM, endM, "interactive")

	timeoutErr := &timeoutError{}
	result, _, cont := sess.handleReadError(timeoutErr, execCtx, 15, 15)
	if result == nil {
		t.Fatal("expected result when peak-tty signal detected")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "interactive" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "interactive")
	}
	if cont {
		t.Error("expected cont=false when result is returned")
	}
}

// ============================================================================
// writeInputToPTY tests
// ============================================================================

func TestPartial_WriteInputToPTY_Success(t *testing.T) {
	pty := fakepty.New()
	sess := &Session{
		pty:   pty,
		Mode:  "local",
		State: StateRunning,
	}

	err := sess.writeInputToPTY("hello\n")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(pty.Written(), "hello\n") {
		t.Errorf("expected input to be written, got %q", pty.Written())
	}
}

func TestPartial_WriteInputToPTY_WriteFailsLocalMode(t *testing.T) {
	ep := &configurablePTY{writeErr: fmt.Errorf("write error")}
	sess := &Session{
		pty:   ep,
		Mode:  "local",
		State: StateRunning,
	}

	err := sess.writeInputToPTY("hello\n")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "write input") {
		t.Errorf("error = %q, want containing 'write input'", err.Error())
	}
	// State should revert to StateAwaitingInput on non-connection error
	if sess.State != StateAwaitingInput {
		t.Errorf("State = %q, want %q", sess.State, StateAwaitingInput)
	}
}

func TestPartial_WriteInputToPTY_ConnectionBrokenSSH(t *testing.T) {
	// Connection broken on SSH should trigger reconnect attempt
	ep := &configurablePTY{writeErr: io.EOF}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		ID:    "test_input_broken",
		pty:   ep,
		Mode:  "ssh",
		Host:  "nonexistent.invalid",
		User:  "testuser",
		Port:  22,
		State: StateRunning,
		clock: clock,
	}

	err := sess.writeInputToPTY("hello\n")
	if err == nil {
		t.Fatal("expected error when reconnect fails")
	}
	// Should contain connection lost message
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error = %q, want containing 'connection lost'", err.Error())
	}
}

// ============================================================================
// handleInputConnectionError tests
// ============================================================================

func TestPartial_HandleInputConnectionError_ReconnectFails(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		ID:    "test_input_conn_err",
		Mode:  "ssh",
		Host:  "nonexistent.invalid",
		User:  "testuser",
		Port:  22,
		State: StateRunning,
		clock: clock,
	}

	err := sess.handleInputConnectionError(io.EOF)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error = %q, want containing 'connection lost'", err.Error())
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q", sess.State, StateIdle)
	}
}

// ============================================================================
// processLegacyRead with fatal read error
// ============================================================================

func TestPartial_ProcessLegacyRead_FatalReadError(t *testing.T) {
	// An error that is not timeout and not EOF should be fatal
	ep := &configurablePTY{readErr: fmt.Errorf("device disconnected")}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_fatal", "local",
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.pty = ep // Override with error PTY
	sess.State = StateRunning

	ctx := context.Background()
	buf := make([]byte, 4096)
	_, _, err := sess.processLegacyRead(ctx, buf, "cmd", 0, 15)
	if err == nil {
		t.Fatal("expected fatal error")
	}
	if !strings.Contains(err.Error(), "read output") {
		t.Errorf("error = %q, want containing 'read output'", err.Error())
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q after fatal error", sess.State, StateIdle)
	}
}

// ============================================================================
// processMarkedRead with fatal read error
// ============================================================================

func TestPartial_ProcessMarkedRead_FatalReadError(t *testing.T) {
	ep := &configurablePTY{readErr: fmt.Errorf("device disconnected")}
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_marked_fatal", "local",
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.pty = ep
	sess.State = StateRunning

	cmdID := "ffeedd00"
	execCtx := newExecContext(cmdID,
		startMarkerPrefix+cmdID+markerSuffix,
		endMarkerPrefix+cmdID+markerSuffix,
		"cmd")

	ctx := context.Background()
	buf := make([]byte, 4096)
	_, _, err := sess.processMarkedRead(ctx, buf, execCtx, 0, 15)
	if err == nil {
		t.Fatal("expected fatal error")
	}
	if !strings.Contains(err.Error(), "read output") {
		t.Errorf("error = %q, want containing 'read output'", err.Error())
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q after fatal error", sess.State, StateIdle)
	}
}

// ============================================================================
// handleLegacyReadError at stall threshold with password prompt
// ============================================================================

func TestPartial_HandleLegacyReadError_StallThresholdPasswordPrompt(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_stall_pwd", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("[sudo] password for user: ")

	timeoutErr := &timeoutError{}
	// At stall threshold (15)
	result, _, cont := sess.handleLegacyReadError(timeoutErr, "sudo cmd", 15, 15)
	if result == nil {
		t.Fatal("expected result when password prompt detected at stall threshold")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "password" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "password")
	}
	if cont {
		t.Error("expected cont=false")
	}
}

func TestPartial_HandleLegacyReadError_StallThresholdPeakTTY(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_stall_peak", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("waiting" + peakTTYSignal)

	timeoutErr := &timeoutError{}
	result, _, _ := sess.handleLegacyReadError(timeoutErr, "cmd", 15, 15)
	if result == nil {
		t.Fatal("expected result when peak-tty detected at stall threshold")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "interactive" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "interactive")
	}
}

func TestPartial_HandleLegacyReadError_StallThresholdNothingDetected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_legacy_stall_none", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.State = StateRunning
	sess.outputBuffer.WriteString("normal output")

	timeoutErr := &timeoutError{}
	result, stall, cont := sess.handleLegacyReadError(timeoutErr, "cmd", 15, 15)
	if result != nil {
		t.Error("expected nil result when nothing detected")
	}
	if !cont {
		t.Error("expected cont=true to continue")
	}
	// Should partially reset stall count to threshold/2
	if stall != 7 {
		t.Errorf("stallCount = %d, want 7 (15/2)", stall)
	}
}

// ============================================================================
// Exec error paths
// ============================================================================

func TestPartial_Exec_ClosedSession(t *testing.T) {
	pty := fakepty.New()
	sess := &Session{
		pty:   pty,
		State: StateClosed,
	}

	_, err := sess.Exec("cmd", 5000)
	if err == nil {
		t.Fatal("expected error for closed session")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error = %q, want containing 'closed'", err.Error())
	}
}

func TestPartial_Exec_NilPTY(t *testing.T) {
	sess := &Session{
		pty:   nil,
		State: StateIdle,
	}

	_, err := sess.Exec("cmd", 5000)
	if err == nil {
		t.Fatal("expected error for nil PTY")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want containing 'not initialized'", err.Error())
	}
}

// ============================================================================
// isConnectionBroken tests
// ============================================================================

func TestPartial_IsConnectionBroken_VariousErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"io.EOF", io.EOF, true},
		{"broken pipe", fmt.Errorf("broken pipe"), true},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"use of closed", fmt.Errorf("use of closed network connection"), true},
		{"closed network connection", fmt.Errorf("closed network connection"), true},
		{"channel closed", fmt.Errorf("channel closed"), true},
		{"contains EOF", fmt.Errorf("read: EOF"), true},
		{"regular error", fmt.Errorf("some other error"), false},
		{"timeout", fmt.Errorf("timeout"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConnectionBroken(tt.err)
			if got != tt.want {
				t.Errorf("isConnectionBroken(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ============================================================================
// isTimeoutError tests
// ============================================================================

func TestPartial_IsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"timeout", fmt.Errorf("timeout"), true},
		{"i/o timeout", fmt.Errorf("i/o timeout"), true},
		{"contains timeout", fmt.Errorf("read: i/o timeout"), true},
		{"other error", fmt.Errorf("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTimeoutError(tt.err)
			if got != tt.want {
				t.Errorf("isTimeoutError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Helper: timeoutError implements os.IsTimeout()
// ============================================================================

type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

// Verify it works with os.IsTimeout
var _ error = (*timeoutError)(nil)

func init() {
	// Sanity check: our timeoutError should be detected by os.IsTimeout
	if !os.IsTimeout(&timeoutError{}) {
		panic("timeoutError must satisfy os.IsTimeout")
	}
}
