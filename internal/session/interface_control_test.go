package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
)

// =============================================================================
// pty_interface.go tests - sshPTYAdapter
// =============================================================================

// mockSSHPTY implements the interface embedded in sshPTYAdapter.
type mockSSHPTY struct {
	readFunc         func(b []byte) (int, error)
	writeFunc        func(b []byte) (int, error)
	writeStringFunc  func(s string) (int, error)
	interruptFunc    func() error
	closeFunc        func() error
	setDeadlineFunc  func(t time.Time) error

	readCalled        bool
	writeCalled       bool
	writeStringCalled bool
	interruptCalled   bool
	closeCalled       bool
	deadlineSet       time.Time
}

func newMockSSHPTY() *mockSSHPTY {
	return &mockSSHPTY{
		readFunc:        func(b []byte) (int, error) { return 0, nil },
		writeFunc:       func(b []byte) (int, error) { return len(b), nil },
		writeStringFunc: func(s string) (int, error) { return len(s), nil },
		interruptFunc:   func() error { return nil },
		closeFunc:       func() error { return nil },
		setDeadlineFunc: func(t time.Time) error { return nil },
	}
}

func (m *mockSSHPTY) Read(b []byte) (int, error) {
	m.readCalled = true
	return m.readFunc(b)
}

func (m *mockSSHPTY) Write(b []byte) (int, error) {
	m.writeCalled = true
	return m.writeFunc(b)
}

func (m *mockSSHPTY) WriteString(s string) (int, error) {
	m.writeStringCalled = true
	return m.writeStringFunc(s)
}

func (m *mockSSHPTY) Interrupt() error {
	m.interruptCalled = true
	return m.interruptFunc()
}

func (m *mockSSHPTY) Close() error {
	m.closeCalled = true
	return m.closeFunc()
}

func (m *mockSSHPTY) SetReadDeadline(t time.Time) error {
	m.deadlineSet = t
	return m.setDeadlineFunc(t)
}

// --- sshPTYAdapter.Write ---

func TestIntf_SSHAdapter_Write(t *testing.T) {
	mock := newMockSSHPTY()
	mock.writeFunc = func(b []byte) (int, error) {
		return len(b), nil
	}
	adapter := &sshPTYAdapter{pty: mock}

	data := []byte("hello ssh pty")
	n, err := adapter.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %d, want %d", n, len(data))
	}
	if !mock.writeCalled {
		t.Error("expected inner Write to be called")
	}
}

func TestIntf_SSHAdapter_WriteError(t *testing.T) {
	mock := newMockSSHPTY()
	mock.writeFunc = func(b []byte) (int, error) {
		return 0, errors.New("write failed")
	}
	adapter := &sshPTYAdapter{pty: mock}

	_, err := adapter.Write([]byte("data"))
	if err == nil {
		t.Fatal("expected error from Write")
	}
	if err.Error() != "write failed" {
		t.Errorf("error = %q, want 'write failed'", err.Error())
	}
}

// --- sshPTYAdapter.Interrupt ---

func TestIntf_SSHAdapter_Interrupt(t *testing.T) {
	mock := newMockSSHPTY()
	adapter := &sshPTYAdapter{pty: mock}

	err := adapter.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if !mock.interruptCalled {
		t.Error("expected inner Interrupt to be called")
	}
}

func TestIntf_SSHAdapter_InterruptError(t *testing.T) {
	mock := newMockSSHPTY()
	mock.interruptFunc = func() error {
		return errors.New("interrupt failed")
	}
	adapter := &sshPTYAdapter{pty: mock}

	err := adapter.Interrupt()
	if err == nil {
		t.Fatal("expected error from Interrupt")
	}
	if err.Error() != "interrupt failed" {
		t.Errorf("error = %q, want 'interrupt failed'", err.Error())
	}
}

// --- sshPTYAdapter.Read ---

func TestIntf_SSHAdapter_Read(t *testing.T) {
	mock := newMockSSHPTY()
	mock.readFunc = func(b []byte) (int, error) {
		copy(b, "hello")
		return 5, nil
	}
	adapter := &sshPTYAdapter{pty: mock}

	buf := make([]byte, 16)
	n, err := adapter.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Read() = %d, want 5", n)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("Read() data = %q, want 'hello'", string(buf[:n]))
	}
	if !mock.readCalled {
		t.Error("expected inner Read to be called")
	}
}

// --- sshPTYAdapter.WriteString ---

func TestIntf_SSHAdapter_WriteString(t *testing.T) {
	mock := newMockSSHPTY()
	mock.writeStringFunc = func(s string) (int, error) {
		return len(s), nil
	}
	adapter := &sshPTYAdapter{pty: mock}

	n, err := adapter.WriteString("test string")
	if err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if n != len("test string") {
		t.Errorf("WriteString() = %d, want %d", n, len("test string"))
	}
	if !mock.writeStringCalled {
		t.Error("expected inner WriteString to be called")
	}
}

// --- sshPTYAdapter.Close ---

func TestIntf_SSHAdapter_Close(t *testing.T) {
	mock := newMockSSHPTY()
	adapter := &sshPTYAdapter{pty: mock}

	err := adapter.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !mock.closeCalled {
		t.Error("expected inner Close to be called")
	}
}

// --- sshPTYAdapter.SetReadDeadline ---

func TestIntf_SSHAdapter_SetReadDeadline(t *testing.T) {
	mock := newMockSSHPTY()
	adapter := &sshPTYAdapter{pty: mock}

	deadline := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	err := adapter.SetReadDeadline(deadline)
	if err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	if !mock.deadlineSet.Equal(deadline) {
		t.Errorf("deadline = %v, want %v", mock.deadlineSet, deadline)
	}
}

// =============================================================================
// pty_interface.go tests - localPTYAdapter
// =============================================================================

// mockLocalPTY implements the interface embedded in localPTYAdapter.
type mockLocalPTY struct {
	readFunc         func(b []byte) (int, error)
	writeFunc        func(b []byte) (int, error)
	writeStringFunc  func(s string) (int, error)
	interruptFunc    func() error
	closeFunc        func() error
	file             *os.File // can be nil
}

func newMockLocalPTY() *mockLocalPTY {
	return &mockLocalPTY{
		readFunc:        func(b []byte) (int, error) { return 0, nil },
		writeFunc:       func(b []byte) (int, error) { return len(b), nil },
		writeStringFunc: func(s string) (int, error) { return len(s), nil },
		interruptFunc:   func() error { return nil },
		closeFunc:       func() error { return nil },
	}
}

func (m *mockLocalPTY) Read(b []byte) (int, error)         { return m.readFunc(b) }
func (m *mockLocalPTY) Write(b []byte) (int, error)        { return m.writeFunc(b) }
func (m *mockLocalPTY) WriteString(s string) (int, error)   { return m.writeStringFunc(s) }
func (m *mockLocalPTY) Interrupt() error                    { return m.interruptFunc() }
func (m *mockLocalPTY) Close() error                        { return m.closeFunc() }
func (m *mockLocalPTY) File() *os.File                      { return m.file }

// --- localPTYAdapter.Write ---

func TestIntf_LocalAdapter_Write(t *testing.T) {
	mock := newMockLocalPTY()
	var captured []byte
	mock.writeFunc = func(b []byte) (int, error) {
		captured = append(captured, b...)
		return len(b), nil
	}
	adapter := &localPTYAdapter{pty: mock}

	data := []byte("local pty data")
	n, err := adapter.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %d, want %d", n, len(data))
	}
	if string(captured) != "local pty data" {
		t.Errorf("captured = %q, want 'local pty data'", string(captured))
	}
}

func TestIntf_LocalAdapter_WriteError(t *testing.T) {
	mock := newMockLocalPTY()
	mock.writeFunc = func(b []byte) (int, error) {
		return 0, errors.New("local write error")
	}
	adapter := &localPTYAdapter{pty: mock}

	_, err := adapter.Write([]byte("data"))
	if err == nil {
		t.Fatal("expected error from Write")
	}
	if err.Error() != "local write error" {
		t.Errorf("error = %q, want 'local write error'", err.Error())
	}
}

// --- localPTYAdapter.Interrupt ---

func TestIntf_LocalAdapter_Interrupt(t *testing.T) {
	mock := newMockLocalPTY()
	called := false
	mock.interruptFunc = func() error {
		called = true
		return nil
	}
	adapter := &localPTYAdapter{pty: mock}

	err := adapter.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if !called {
		t.Error("expected inner Interrupt to be called")
	}
}

func TestIntf_LocalAdapter_InterruptError(t *testing.T) {
	mock := newMockLocalPTY()
	mock.interruptFunc = func() error {
		return errors.New("signal failed")
	}
	adapter := &localPTYAdapter{pty: mock}

	err := adapter.Interrupt()
	if err == nil {
		t.Fatal("expected error from Interrupt")
	}
	if err.Error() != "signal failed" {
		t.Errorf("error = %q, want 'signal failed'", err.Error())
	}
}

// --- localPTYAdapter.Read ---

func TestIntf_LocalAdapter_Read(t *testing.T) {
	mock := newMockLocalPTY()
	mock.readFunc = func(b []byte) (int, error) {
		copy(b, "local data")
		return 10, nil
	}
	adapter := &localPTYAdapter{pty: mock}

	buf := make([]byte, 32)
	n, err := adapter.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 10 {
		t.Errorf("Read() = %d, want 10", n)
	}
	if string(buf[:n]) != "local data" {
		t.Errorf("Read() data = %q, want 'local data'", string(buf[:n]))
	}
}

// --- localPTYAdapter.WriteString ---

func TestIntf_LocalAdapter_WriteString(t *testing.T) {
	mock := newMockLocalPTY()
	mock.writeStringFunc = func(s string) (int, error) {
		return len(s), nil
	}
	adapter := &localPTYAdapter{pty: mock}

	n, err := adapter.WriteString("hello local")
	if err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if n != len("hello local") {
		t.Errorf("WriteString() = %d, want %d", n, len("hello local"))
	}
}

// --- localPTYAdapter.Close ---

func TestIntf_LocalAdapter_Close(t *testing.T) {
	mock := newMockLocalPTY()
	closed := false
	mock.closeFunc = func() error {
		closed = true
		return nil
	}
	adapter := &localPTYAdapter{pty: mock}

	err := adapter.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closed {
		t.Error("expected inner Close to be called")
	}
}

// --- localPTYAdapter.SetReadDeadline with nil File ---

func TestIntf_LocalAdapter_SetReadDeadline_NilFile(t *testing.T) {
	mock := newMockLocalPTY()
	mock.file = nil // File() returns nil
	adapter := &localPTYAdapter{pty: mock}

	err := adapter.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		t.Fatalf("SetReadDeadline with nil File should return nil, got %v", err)
	}
}

// =============================================================================
// control.go gap tests - Exec/ExecRaw successful path, GetPTYProcesses,
// IsPTYAlive, IsProcessRunning
// =============================================================================

// addResponseDelayed queues a response on the fakepty after a short delay.
// This ensures the drain phase of Exec/ExecRaw completes before the response
// appears, so the main read loop finds it.
func addResponseDelayed(p *fakepty.PTY, data string) {
	go func() {
		time.Sleep(20 * time.Millisecond)
		p.AddResponse(data)
	}()
}

// --- Exec successful completion ---

func TestIntCtrl_Exec_SuccessfulCompletion(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	// Add response with a delay so it arrives after the drain phase.
	addResponseDelayed(pty, fmt.Sprintf("echo cmd\nresult_data\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cs.Exec(ctx, "echo cmd")
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if !strings.Contains(result, "result_data") {
		t.Errorf("Exec() result = %q, expected to contain 'result_data'", result)
	}
}

// --- ExecRaw successful completion ---

func TestIntCtrl_ExecRaw_SuccessfulCompletion(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	addResponseDelayed(pty, fmt.Sprintf("raw output data\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cs.ExecRaw(ctx, "some_cmd")
	if err != nil {
		t.Fatalf("ExecRaw() error = %v", err)
	}
	// ExecRaw returns raw output without cleaning.
	if !strings.Contains(result, "raw output data") {
		t.Errorf("ExecRaw() result = %q, expected to contain 'raw output data'", result)
	}
	if !strings.Contains(result, marker) {
		t.Errorf("ExecRaw() result = %q, expected to contain marker %q", result, marker)
	}
}

// --- Exec with timeout via context deadline ---

func TestIntCtrl_Exec_TimeoutViaDeadline(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	// No response with marker queued, so the read loop never finds it.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := cs.Exec(ctx, "slow_cmd")
	if err == nil {
		t.Fatal("expected error from Exec with deadline exceeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, expected context.DeadlineExceeded", err)
	}
}

// --- ExecRaw with timeout via context deadline ---

func TestIntCtrl_ExecRaw_TimeoutViaDeadline(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	// No response with marker queued.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := cs.ExecRaw(ctx, "slow_cmd")
	if err == nil {
		t.Fatal("expected error from ExecRaw with deadline exceeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, expected context.DeadlineExceeded", err)
	}
}

// --- GetPTYProcesses successful with PIDs ---

func TestIntCtrl_GetPTYProcesses_ParsesPIDs(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	addResponseDelayed(pty, fmt.Sprintf("ps -t pts/5 -o pid= 2>/dev/null\n1234\n5678\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pids, err := cs.GetPTYProcesses(ctx, "5")
	if err != nil {
		t.Fatalf("GetPTYProcesses() error = %v", err)
	}
	if len(pids) != 2 {
		t.Errorf("GetPTYProcesses() returned %d PIDs, want 2: %v", len(pids), pids)
	}
}

// --- GetPTYProcesses with empty result ---

func TestIntCtrl_GetPTYProcesses_Empty(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	addResponseDelayed(pty, fmt.Sprintf("ps -t pts/99 -o pid= 2>/dev/null\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pids, err := cs.GetPTYProcesses(ctx, "99")
	if err != nil {
		t.Fatalf("GetPTYProcesses() error = %v", err)
	}
	// May be nil or empty slice; both are acceptable.
	if len(pids) != 0 {
		t.Errorf("GetPTYProcesses() returned %d PIDs, want 0: %v", len(pids), pids)
	}
}

// --- IsPTYAlive with processes ---

func TestIntCtrl_IsPTYAlive_WithProcesses(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	addResponseDelayed(pty, fmt.Sprintf("ps -t pts/3\n42\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	alive, err := cs.IsPTYAlive(ctx, "3")
	if err != nil {
		t.Fatalf("IsPTYAlive() error = %v", err)
	}
	if !alive {
		t.Error("IsPTYAlive() = false, want true (processes exist)")
	}
}

// --- IsPTYAlive with no processes ---

func TestIntCtrl_IsPTYAlive_NoProcesses(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	addResponseDelayed(pty, fmt.Sprintf("ps -t pts/99 -o pid= 2>/dev/null\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	alive, err := cs.IsPTYAlive(ctx, "99")
	if err != nil {
		t.Fatalf("IsPTYAlive() error = %v", err)
	}
	if alive {
		t.Error("IsPTYAlive() = true, want false (no processes)")
	}
}

// --- IsProcessRunning with running process ---

func TestIntCtrl_IsProcessRunning_Running(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	addResponseDelayed(pty, fmt.Sprintf("ps -p 1234 -o pid= 2>/dev/null\n1234\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	running, err := cs.IsProcessRunning(ctx, "1234")
	if err != nil {
		t.Fatalf("IsProcessRunning() error = %v", err)
	}
	if !running {
		t.Error("IsProcessRunning() = false, want true")
	}
}

// --- IsProcessRunning with dead process ---

func TestIntCtrl_IsProcessRunning_NotRunning(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC))
	cs := newTestControlSession(pty, clock)

	marker := fmt.Sprintf("__CTRL_%d__", clock.Now().UnixNano())

	addResponseDelayed(pty, fmt.Sprintf("ps -p 9999 -o pid= 2>/dev/null\n%s 0\n", marker))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	running, err := cs.IsProcessRunning(ctx, "9999")
	if err != nil {
		t.Fatalf("IsProcessRunning() error = %v", err)
	}
	if running {
		t.Error("IsProcessRunning() = true, want false")
	}
}
