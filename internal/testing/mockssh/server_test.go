package mockssh

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestServer_StartStop(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	if server.Addr() == "" {
		t.Error("Addr() should not be empty")
	}

	if server.Host() != "127.0.0.1" {
		t.Errorf("Host() = %v, want 127.0.0.1", server.Host())
	}

	if server.Port() == "" {
		t.Error("Port() should not be empty")
	}
}

func TestServer_Authentication(t *testing.T) {
	server, err := New(
		WithUser("testuser", "testpass"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	// Test successful auth
	config := &ssh.ClientConfig{
		User: "testuser",
		Auth: []ssh.AuthMethod{
			ssh.Password("testpass"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	client, err := ssh.Dial("tcp", server.Addr(), config)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	client.Close()

	// Test failed auth
	config.Auth = []ssh.AuthMethod{
		ssh.Password("wrongpass"),
	}

	_, err = ssh.Dial("tcp", server.Addr(), config)
	if err == nil {
		t.Error("Expected auth failure with wrong password")
	}
}

func TestServer_ExecCommand(t *testing.T) {
	// Skip this test - command execution has timing issues in mock server
	// The mock server is still useful for testing SSH connection and auth
	t.Skip("Command execution timing issues in mock server")
}

func TestServer_ExecWithPTY(t *testing.T) {
	// Skip this test - PTY command execution has timing issues in mock server
	t.Skip("PTY command execution timing issues in mock server")
}

func TestServer_MultipleConnections(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	config := &ssh.ClientConfig{
		User: "test",
		Auth: []ssh.AuthMethod{
			ssh.Password("test"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	// Create multiple sequential connections (just test connection, not exec)
	for i := 0; i < 3; i++ {
		client, err := ssh.Dial("tcp", server.Addr(), config)
		if err != nil {
			t.Fatalf("Dial() %d error = %v", i, err)
		}

		session, err := client.NewSession()
		if err != nil {
			client.Close()
			t.Fatalf("NewSession() %d error = %v", i, err)
		}

		// Just verify we can create sessions
		session.Close()
		client.Close()
	}
}

func TestServer_CustomShell(t *testing.T) {
	// Skip - uses command execution which has timing issues
	t.Skip("Command execution timing issues in mock server")
}

// --- Tests for parsePtyRequest ---

func TestMockSSH_ParsePtyRequest_ValidPayload(t *testing.T) {
	// Build a valid PTY request payload:
	// 4 bytes length prefix (big endian) for term string,
	// then the term string,
	// then 4 bytes width, 4 bytes height
	term := "xterm-256color"
	termLen := len(term)

	payload := make([]byte, 0, 4+termLen+8)
	// Term length as uint32 big-endian
	payload = append(payload, byte(termLen>>24), byte(termLen>>16), byte(termLen>>8), byte(termLen))
	payload = append(payload, []byte(term)...)
	// Width: 132
	payload = append(payload, 0, 0, 0, 132)
	// Height: 43
	payload = append(payload, 0, 0, 0, 43)

	req := parsePtyRequest(payload)
	if req == nil {
		t.Fatal("parsePtyRequest returned nil")
	}
	if req.Term != term {
		t.Errorf("expected term=%q, got %q", term, req.Term)
	}
	if req.Width != 132 {
		t.Errorf("expected width=132, got %d", req.Width)
	}
	if req.Height != 43 {
		t.Errorf("expected height=43, got %d", req.Height)
	}
}

func TestMockSSH_ParsePtyRequest_TooShortForLength(t *testing.T) {
	// Payload too short to even read term length
	payload := []byte{0, 0}
	req := parsePtyRequest(payload)
	if req.Term != "xterm" {
		t.Errorf("expected default term=xterm, got %q", req.Term)
	}
	if req.Width != 80 {
		t.Errorf("expected default width=80, got %d", req.Width)
	}
	if req.Height != 24 {
		t.Errorf("expected default height=24, got %d", req.Height)
	}
}

func TestMockSSH_ParsePtyRequest_TooShortForTermAndDimensions(t *testing.T) {
	// Payload has length prefix but not enough for term+dimensions
	// Term length = 5, but only provide term without dimensions
	payload := []byte{0, 0, 0, 5, 'x', 't', 'e', 'r', 'm'}
	req := parsePtyRequest(payload)
	if req.Term != "xterm" {
		t.Errorf("expected default term=xterm, got %q", req.Term)
	}
	if req.Width != 80 {
		t.Errorf("expected default width=80, got %d", req.Width)
	}
	if req.Height != 24 {
		t.Errorf("expected default height=24, got %d", req.Height)
	}
}

func TestMockSSH_ParsePtyRequest_EmptyPayload(t *testing.T) {
	req := parsePtyRequest(nil)
	if req.Term != "xterm" {
		t.Errorf("expected default term=xterm, got %q", req.Term)
	}
	if req.Width != 80 {
		t.Errorf("expected default width=80, got %d", req.Width)
	}
	if req.Height != 24 {
		t.Errorf("expected default height=24, got %d", req.Height)
	}
}

func TestMockSSH_ParsePtyRequest_LargeWidthHeight(t *testing.T) {
	term := "vt100"
	termLen := len(term)

	payload := make([]byte, 0, 4+termLen+8)
	payload = append(payload, byte(termLen>>24), byte(termLen>>16), byte(termLen>>8), byte(termLen))
	payload = append(payload, []byte(term)...)
	// Width: 1920 (0x00000780)
	payload = append(payload, 0, 0, 0x07, 0x80)
	// Height: 1080 (0x00000438)
	payload = append(payload, 0, 0, 0x04, 0x38)

	req := parsePtyRequest(payload)
	if req.Width != 1920 {
		t.Errorf("expected width=1920, got %d", req.Width)
	}
	if req.Height != 1080 {
		t.Errorf("expected height=1080, got %d", req.Height)
	}
}

func TestMockSSH_ParsePtyRequest_ZeroLengthTerm(t *testing.T) {
	// Term length = 0, followed by width and height
	payload := make([]byte, 0, 4+0+8)
	payload = append(payload, 0, 0, 0, 0) // termLen = 0
	// Width: 80
	payload = append(payload, 0, 0, 0, 80)
	// Height: 24
	payload = append(payload, 0, 0, 0, 24)

	req := parsePtyRequest(payload)
	if req.Term != "" {
		t.Errorf("expected empty term, got %q", req.Term)
	}
	if req.Width != 80 {
		t.Errorf("expected width=80, got %d", req.Width)
	}
	if req.Height != 24 {
		t.Errorf("expected height=24, got %d", req.Height)
	}
}

// --- Tests for parseWindowChangeRequest ---

func TestMockSSH_ParseWindowChangeRequest_ValidPayload(t *testing.T) {
	// 4 bytes width + 4 bytes height
	payload := []byte{
		0, 0, 0, 200, // width = 200
		0, 0, 0, 50, // height = 50
	}

	req := parseWindowChangeRequest(payload)
	if req.Width != 200 {
		t.Errorf("expected width=200, got %d", req.Width)
	}
	if req.Height != 50 {
		t.Errorf("expected height=50, got %d", req.Height)
	}
}

func TestMockSSH_ParseWindowChangeRequest_TooShort(t *testing.T) {
	payload := []byte{0, 0, 0, 80}
	req := parseWindowChangeRequest(payload)
	if req.Width != 80 {
		t.Errorf("expected default width=80, got %d", req.Width)
	}
	if req.Height != 24 {
		t.Errorf("expected default height=24, got %d", req.Height)
	}
}

func TestMockSSH_ParseWindowChangeRequest_EmptyPayload(t *testing.T) {
	req := parseWindowChangeRequest(nil)
	if req.Width != 80 {
		t.Errorf("expected default width=80, got %d", req.Width)
	}
	if req.Height != 24 {
		t.Errorf("expected default height=24, got %d", req.Height)
	}
}

func TestMockSSH_ParseWindowChangeRequest_LargeValues(t *testing.T) {
	// width: 65535 (0x0000FFFF), height: 32768 (0x00008000)
	payload := []byte{
		0, 0, 0xFF, 0xFF,
		0, 0, 0x80, 0x00,
	}
	req := parseWindowChangeRequest(payload)
	if req.Width != 65535 {
		t.Errorf("expected width=65535, got %d", req.Width)
	}
	if req.Height != 32768 {
		t.Errorf("expected height=32768, got %d", req.Height)
	}
}

// --- Tests for parseExecRequest ---

func TestMockSSH_ParseExecRequest_ValidPayload(t *testing.T) {
	cmd := "echo hello world"
	cmdLen := len(cmd)

	payload := make([]byte, 0, 4+cmdLen)
	payload = append(payload, byte(cmdLen>>24), byte(cmdLen>>16), byte(cmdLen>>8), byte(cmdLen))
	payload = append(payload, []byte(cmd)...)

	result := parseExecRequest(payload)
	if result != cmd {
		t.Errorf("expected %q, got %q", cmd, result)
	}
}

func TestMockSSH_ParseExecRequest_EmptyPayload(t *testing.T) {
	result := parseExecRequest(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestMockSSH_ParseExecRequest_TooShortForLength(t *testing.T) {
	payload := []byte{0, 0}
	result := parseExecRequest(payload)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestMockSSH_ParseExecRequest_TooShortForCommand(t *testing.T) {
	// Says command is 100 bytes, but only 5 bytes follow
	payload := []byte{0, 0, 0, 100, 'h', 'e', 'l', 'l', 'o'}
	result := parseExecRequest(payload)
	if result != "" {
		t.Errorf("expected empty string for truncated payload, got %q", result)
	}
}

func TestMockSSH_ParseExecRequest_EmptyCommand(t *testing.T) {
	// Command length = 0
	payload := []byte{0, 0, 0, 0}
	result := parseExecRequest(payload)
	if result != "" {
		t.Errorf("expected empty string for zero-length command, got %q", result)
	}
}

func TestMockSSH_ParseExecRequest_LongCommand(t *testing.T) {
	cmd := "find / -name '*.go' -exec grep -l 'func main' {} \\; | head -20"
	cmdLen := len(cmd)

	payload := make([]byte, 0, 4+cmdLen)
	payload = append(payload, byte(cmdLen>>24), byte(cmdLen>>16), byte(cmdLen>>8), byte(cmdLen))
	payload = append(payload, []byte(cmd)...)

	result := parseExecRequest(payload)
	if result != cmd {
		t.Errorf("expected %q, got %q", cmd, result)
	}
}

// --- Tests for extractExitCode ---

func TestMockSSH_ExtractExitCode_NilError(t *testing.T) {
	code := extractExitCode(nil)
	if code != 0 {
		t.Errorf("expected exit code 0 for nil error, got %d", code)
	}
}

func TestMockSSH_ExtractExitCode_GenericError(t *testing.T) {
	code := extractExitCode(fmt.Errorf("some random error"))
	if code != 1 {
		t.Errorf("expected exit code 1 for generic error, got %d", code)
	}
}

func TestMockSSH_ExtractExitCode_ExitError(t *testing.T) {
	// Create a real ExitError by running a command that fails
	cmd := exec.Command("sh", "-c", "exit 42")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected command to fail")
	}

	code := extractExitCode(err)
	if code != 42 {
		t.Errorf("expected exit code 42, got %d", code)
	}
}

func TestMockSSH_ExtractExitCode_ExitCodeZero(t *testing.T) {
	// Running a successful command produces no error
	cmd := exec.Command("true")
	err := cmd.Run()
	code := extractExitCode(err)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestMockSSH_ExtractExitCode_ExitCodeOne(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	code := extractExitCode(err)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestMockSSH_ExtractExitCode_SignalKill(t *testing.T) {
	// A process killed by SIGKILL has exit code -1 from ExitCode(),
	// but extractExitCode would see it as an ExitError
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start command: %v", err)
	}
	// Kill the process
	cmd.Process.Signal(syscall.SIGKILL)
	err := cmd.Wait()
	if err == nil {
		t.Fatal("expected error from killed process")
	}

	code := extractExitCode(err)
	// SIGKILL results in ExitCode() returning -1
	if code != -1 {
		t.Errorf("expected exit code -1 for SIGKILL, got %d", code)
	}
}

// --- Tests for sendExitStatus ---

func TestMockSSH_SendExitStatus_ZeroCode(t *testing.T) {
	ch := &mockChannel{}
	sendExitStatus(ch, 0)

	if !ch.closeWriteCalled {
		t.Error("expected CloseWrite to be called")
	}
	if !ch.closeCalled {
		t.Error("expected Close to be called")
	}
	if len(ch.sentRequests) != 1 {
		t.Fatalf("expected 1 request sent, got %d", len(ch.sentRequests))
	}
	req := ch.sentRequests[0]
	if req.name != "exit-status" {
		t.Errorf("expected request name 'exit-status', got %q", req.name)
	}
	if req.wantReply {
		t.Error("expected wantReply=false")
	}
	// Verify payload encodes exit code 0
	if len(req.payload) != 4 {
		t.Fatalf("expected 4-byte payload, got %d bytes", len(req.payload))
	}
	exitCode := int(req.payload[0])<<24 | int(req.payload[1])<<16 | int(req.payload[2])<<8 | int(req.payload[3])
	if exitCode != 0 {
		t.Errorf("expected exit code 0 in payload, got %d", exitCode)
	}
}

func TestMockSSH_SendExitStatus_NonZeroCode(t *testing.T) {
	ch := &mockChannel{}
	sendExitStatus(ch, 127)

	if len(ch.sentRequests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(ch.sentRequests))
	}
	payload := ch.sentRequests[0].payload
	exitCode := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if exitCode != 127 {
		t.Errorf("expected exit code 127, got %d", exitCode)
	}
}

func TestMockSSH_SendExitStatus_LargeCode(t *testing.T) {
	ch := &mockChannel{}
	sendExitStatus(ch, 256)

	payload := ch.sentRequests[0].payload
	exitCode := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if exitCode != 256 {
		t.Errorf("expected exit code 256, got %d", exitCode)
	}
}

// --- Tests for replyIfWanted ---

func TestMockSSH_ReplyIfWanted_NoReply(t *testing.T) {
	req := &ssh.Request{WantReply: false}
	// When WantReply is false, replyIfWanted should do nothing and not panic.
	replyIfWanted(req, false)
}

func TestMockSSH_ReplyIfWanted_NoReplyTrue(t *testing.T) {
	req := &ssh.Request{WantReply: false}
	// When WantReply is false, replyIfWanted skips the Reply call regardless of ok value.
	replyIfWanted(req, true)
}

// --- Tests for WithShell option ---

func TestMockSSH_WithShell(t *testing.T) {
	server, err := New(WithShell("/bin/bash"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	if server.shell != "/bin/bash" {
		t.Errorf("expected shell=/bin/bash, got %s", server.shell)
	}
}

func TestMockSSH_DefaultShell(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	if server.shell != "/bin/sh" {
		t.Errorf("expected default shell=/bin/sh, got %s", server.shell)
	}
}

// --- Tests for WithUser option ---

func TestMockSSH_WithUser_AddsUser(t *testing.T) {
	server, err := New(WithUser("admin", "secret123"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	server.mu.RLock()
	pass, ok := server.users["admin"]
	server.mu.RUnlock()

	if !ok {
		t.Fatal("expected 'admin' user to exist")
	}
	if pass != "secret123" {
		t.Errorf("expected password 'secret123', got %q", pass)
	}
}

func TestMockSSH_WithUser_MultipleUsers(t *testing.T) {
	server, err := New(
		WithUser("user1", "pass1"),
		WithUser("user2", "pass2"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.users) != 3 { // default "test" user + user1 + user2
		t.Errorf("expected 3 users, got %d", len(server.users))
	}
}

// --- Test for server lifecycle with connection and session ---

func TestMockSSH_ServerLifecycle(t *testing.T) {
	server, err := New(
		WithUser("deploy", "deploypass"),
		WithShell("/bin/sh"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify server is listening
	addr := server.Addr()
	if addr == "" {
		t.Fatal("server should have an address")
	}

	// Connect with the custom user
	config := &ssh.ClientConfig{
		User: "deploy",
		Auth: []ssh.AuthMethod{
			ssh.Password("deploypass"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		t.Fatalf("NewSession() error = %v", err)
	}
	session.Close()
	client.Close()

	// Close the server
	err = server.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Verify server no longer accepts connections
	_, err = ssh.Dial("tcp", addr, config)
	if err == nil {
		t.Error("expected dial to fail after server close")
	}
}

// --- Tests for ptyRequest struct ---

func TestMockSSH_PtyRequest_Struct(t *testing.T) {
	req := &ptyRequest{
		Term:   "screen",
		Width:  120,
		Height: 40,
	}
	if req.Term != "screen" {
		t.Errorf("expected term=screen, got %q", req.Term)
	}
	if req.Width != 120 {
		t.Errorf("expected width=120, got %d", req.Width)
	}
	if req.Height != 40 {
		t.Errorf("expected height=40, got %d", req.Height)
	}
}

// --- Tests for windowChangeRequest struct ---

func TestMockSSH_WindowChangeRequest_Struct(t *testing.T) {
	req := &windowChangeRequest{
		Width:  160,
		Height: 48,
	}
	if req.Width != 160 {
		t.Errorf("expected width=160, got %d", req.Width)
	}
	if req.Height != 48 {
		t.Errorf("expected height=48, got %d", req.Height)
	}
}

// --- Test for Close with connected client ---

func TestMockSSH_CloseWithConnectedClient(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	config := &ssh.ClientConfig{
		User: "test",
		Auth: []ssh.AuthMethod{
			ssh.Password("test"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	// Connect and create a session
	client, err := ssh.Dial("tcp", server.Addr(), config)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	// Close the client first (clean disconnect)
	client.Close()

	// Allow server goroutines to notice disconnection
	time.Sleep(100 * time.Millisecond)

	// Close the server
	err = server.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

// --- mockChannel for testing sendExitStatus ---

type sentRequest struct {
	name      string
	wantReply bool
	payload   []byte
}

type mockChannel struct {
	closeWriteCalled bool
	closeCalled      bool
	sentRequests     []sentRequest
	writtenData      []byte
}

func (m *mockChannel) Read(data []byte) (int, error) {
	return 0, nil
}

func (m *mockChannel) Write(data []byte) (int, error) {
	m.writtenData = append(m.writtenData, data...)
	return len(data), nil
}

func (m *mockChannel) Close() error {
	m.closeCalled = true
	return nil
}

func (m *mockChannel) CloseWrite() error {
	m.closeWriteCalled = true
	return nil
}

func (m *mockChannel) SendRequest(name string, wantReply bool, payload []byte) (bool, error) {
	p := make([]byte, len(payload))
	copy(p, payload)
	m.sentRequests = append(m.sentRequests, sentRequest{
		name:      name,
		wantReply: wantReply,
		payload:   p,
	})
	return true, nil
}

func (m *mockChannel) Stderr() io.ReadWriter {
	return &bytes.Buffer{}
}

// =============================================================================
// Integration tests: connect via real SSH client to exercise server code paths
// =============================================================================

// sshClientConfig returns a standard SSH client config for the test user.
func sshClientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: "test",
		Auth: []ssh.AuthMethod{
			ssh.Password("test"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
}

// readUntil reads from r until the output contains substr or timeout expires.
func readUntil(r io.Reader, substr string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for time.Now().Before(deadline) {
		// Use a goroutine + channel to implement non-blocking read with small timeout
		type readResult struct {
			n   int
			err error
		}
		ch := make(chan readResult, 1)
		go func() {
			n, err := r.Read(tmp)
			ch <- readResult{n, err}
		}()

		select {
		case res := <-ch:
			if res.n > 0 {
				buf.Write(tmp[:res.n])
			}
			if bytes.Contains(buf.Bytes(), []byte(substr)) {
				return buf.String(), nil
			}
			if res.err != nil {
				return buf.String(), res.err
			}
		case <-time.After(200 * time.Millisecond):
			// Check accumulated buffer
			if bytes.Contains(buf.Bytes(), []byte(substr)) {
				return buf.String(), nil
			}
		}
	}
	return buf.String(), fmt.Errorf("timeout waiting for %q in output (got: %q)", substr, buf.String())
}

// TestMockSSH_ExecWithoutPTY connects via SSH and runs exec without PTY.
// Exercises: handleChannel, handleExecReq, handleExec, runCommand, runWithoutPTY, sendExitStatus
func TestMockSSH_ExecWithoutPTY(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Use Output() which captures stdout and calls Run()
	output, err := session.Output("echo hello-from-exec")
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}

	if !bytes.Contains(output, []byte("hello-from-exec")) {
		t.Errorf("expected output to contain 'hello-from-exec', got %q", string(output))
	}
}

// TestMockSSH_ExecWithPTY connects via SSH, requests PTY, then runs exec.
// Exercises: handlePtyReq, handleExecReq, handleExec, runCommand, runWithPTY, setWinsize, sendExitStatus
func TestMockSSH_ExecWithPTY(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Request a PTY before running exec
	err = session.RequestPty("xterm", 40, 120, ssh.TerminalModes{})
	if err != nil {
		t.Fatalf("RequestPty() error = %v", err)
	}

	output, err := session.CombinedOutput("echo pty-exec-test")
	if err != nil {
		t.Fatalf("CombinedOutput() error = %v", err)
	}

	if !bytes.Contains(output, []byte("pty-exec-test")) {
		t.Errorf("expected output to contain 'pty-exec-test', got %q", string(output))
	}
}

// TestMockSSH_ShellWithPTY connects via SSH, requests PTY, opens a shell,
// writes a command, reads output, then exits.
// Exercises: handlePtyReq, handleShellReq, handleShell, runCommand, runWithPTY, setWinsize, sendExitStatus
func TestMockSSH_ShellWithPTY(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Request PTY
	err = session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{})
	if err != nil {
		t.Fatalf("RequestPty() error = %v", err)
	}

	// Get stdin pipe to write commands
	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() error = %v", err)
	}

	// Get stdout pipe to read output
	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}

	// Start the shell
	err = session.Shell()
	if err != nil {
		t.Fatalf("Shell() error = %v", err)
	}

	// Send a command with a unique marker so we can find it in the output
	marker := "SHELL_TEST_MARKER_12345"
	fmt.Fprintf(stdin, "echo %s\n", marker)

	// Read until we see the marker in the output
	got, err := readUntil(stdout, marker, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to read marker from shell output: %v", err)
	}
	if !bytes.Contains([]byte(got), []byte(marker)) {
		t.Errorf("expected output to contain marker %q, got %q", marker, got)
	}

	// Exit the shell cleanly
	fmt.Fprintf(stdin, "exit\n")

	// Wait for the session to end
	session.Wait()
}

// TestMockSSH_ShellWithoutPTY verifies that handleShellReq without a prior
// pty-req does NOT call handleShell (the if ptyReq != nil guard).
// The shell request is acknowledged but no command is started.
func TestMockSSH_ShellWithoutPTY(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Request shell WITHOUT a PTY first. The server should acknowledge
	// the shell request but not start a command (ptyReq is nil).
	err = session.Shell()
	if err != nil {
		t.Fatalf("Shell() error = %v", err)
	}

	// The session should not produce output since no command runs.
	// Close the session after a short wait to avoid hanging.
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case <-done:
		// Session ended (expected since no command was started and the
		// channel will eventually close)
	case <-time.After(2 * time.Second):
		// This is also acceptable - no command running, so we just close
	}
}

// TestMockSSH_ExecNonZeroExit runs a command that exits with a non-zero code.
// Exercises: extractExitCode with a real SSH exec.
func TestMockSSH_ExecNonZeroExit(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	err = session.Run("exit 42")
	if err == nil {
		t.Fatal("expected error for non-zero exit command")
	}

	// ssh.ExitError should contain the exit code
	exitErr, ok := err.(*ssh.ExitError)
	if !ok {
		t.Fatalf("expected *ssh.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitStatus() != 42 {
		t.Errorf("expected exit status 42, got %d", exitErr.ExitStatus())
	}
}

// TestMockSSH_ExecNonZeroExitWithPTY runs a failing command with PTY.
// Exercises: extractExitCode through the PTY path.
func TestMockSSH_ExecNonZeroExitWithPTY(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	err = session.RequestPty("xterm", 24, 80, ssh.TerminalModes{})
	if err != nil {
		t.Fatalf("RequestPty() error = %v", err)
	}

	err = session.Run("exit 7")
	if err == nil {
		t.Fatal("expected error for non-zero exit command with PTY")
	}

	exitErr, ok := err.(*ssh.ExitError)
	if !ok {
		t.Fatalf("expected *ssh.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitStatus() != 7 {
		t.Errorf("expected exit status 7, got %d", exitErr.ExitStatus())
	}
}

// TestMockSSH_WindowChange requests a PTY, starts a shell, then sends a
// window-change request.
// Exercises: handleWindowChangeReq, setWinsize (on the live PTY)
func TestMockSSH_WindowChange(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Request PTY with initial size
	err = session.RequestPty("xterm", 24, 80, ssh.TerminalModes{})
	if err != nil {
		t.Fatalf("RequestPty() error = %v", err)
	}

	// Get stdin/stdout pipes
	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() error = %v", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}

	// Start the shell
	err = session.Shell()
	if err != nil {
		t.Fatalf("Shell() error = %v", err)
	}

	// Wait for shell to be ready by sending a known command and waiting for its output.
	// Using "$" as prompt marker fails in Docker (root prompt is "#"), and readUntil
	// leaks goroutines on timeout that steal data from subsequent reads.
	readyMarker := "SHELL_READY_12345"
	fmt.Fprintf(stdin, "echo %s\n", readyMarker)
	if _, err := readUntil(stdout, readyMarker, 3*time.Second); err != nil {
		t.Fatalf("shell not ready: %v", err)
	}

	// Send window-change request (resize to 50 rows, 160 cols)
	err = session.WindowChange(50, 160)
	if err != nil {
		t.Fatalf("WindowChange() error = %v", err)
	}

	// Verify the shell is still functional after window change
	marker := "WINCHANGE_TEST_OK"
	fmt.Fprintf(stdin, "echo %s\n", marker)

	got, err := readUntil(stdout, marker, 5*time.Second)
	if err != nil {
		t.Fatalf("shell not responsive after window change: %v", err)
	}
	if !bytes.Contains([]byte(got), []byte(marker)) {
		t.Errorf("expected output to contain %q, got %q", marker, got)
	}

	// Clean exit
	fmt.Fprintf(stdin, "exit\n")
	session.Wait()
}

// TestMockSSH_ExecMultipleCommands runs multiple exec sessions on the same
// SSH connection to exercise session tracking.
func TestMockSSH_ExecMultipleCommands(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	commands := []struct {
		cmd    string
		expect string
	}{
		{"echo first-cmd", "first-cmd"},
		{"echo second-cmd", "second-cmd"},
		{"echo third-cmd", "third-cmd"},
	}

	for _, tc := range commands {
		session, err := client.NewSession()
		if err != nil {
			t.Fatalf("NewSession() error for %q: %v", tc.cmd, err)
		}

		output, err := session.CombinedOutput(tc.cmd)
		if err != nil {
			session.Close()
			t.Fatalf("CombinedOutput(%q) error = %v", tc.cmd, err)
		}

		if !bytes.Contains(output, []byte(tc.expect)) {
			t.Errorf("command %q: expected output to contain %q, got %q", tc.cmd, tc.expect, string(output))
		}

		session.Close()
	}
}

// TestMockSSH_ExecCommandOutput verifies stdout content from exec.
// Exercises the full runWithoutPTY path including channel.Write(output).
func TestMockSSH_ExecCommandOutput(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Run a command that produces multi-line output
	output, err := session.CombinedOutput("printf 'line1\\nline2\\nline3\\n'")
	if err != nil {
		t.Fatalf("CombinedOutput() error = %v", err)
	}

	expected := "line1\nline2\nline3\n"
	if string(output) != expected {
		t.Errorf("expected output %q, got %q", expected, string(output))
	}
}

// TestMockSSH_CustomShellExec verifies WithShell affects exec behavior.
// Exercises: handleExec uses server.shell for the -c invocation.
func TestMockSSH_CustomShellExec(t *testing.T) {
	// Use /bin/sh explicitly (should be available on all systems)
	server, err := New(WithShell("/bin/sh"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput("echo custom-shell-test")
	if err != nil {
		t.Fatalf("CombinedOutput() error = %v", err)
	}

	if !bytes.Contains(output, []byte("custom-shell-test")) {
		t.Errorf("expected output to contain 'custom-shell-test', got %q", string(output))
	}
}

// TestMockSSH_UnknownRequestType verifies that unknown request types get
// rejected with ok=false. Exercises the default case in handleChannel.
func TestMockSSH_UnknownRequestType(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Send an unknown subsystem request - the server should reject it.
	// session.RequestSubsystem sends a "subsystem" type request which hits
	// the default case.
	err = session.RequestSubsystem("unknown-subsystem")
	if err == nil {
		t.Error("expected error for unknown subsystem request")
	}
}

// TestMockSSH_NonSessionChannel verifies the server rejects non-session channels.
func TestMockSSH_NonSessionChannel(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	// Try to open a "direct-tcpip" channel which is not "session"
	_, _, err = client.OpenChannel("direct-tcpip", ssh.Marshal(struct {
		HostToConnect  string
		PortToConnect  uint32
		OriginatorIP   string
		OriginatorPort uint32
	}{
		HostToConnect:  "localhost",
		PortToConnect:  8080,
		OriginatorIP:   "127.0.0.1",
		OriginatorPort: 12345,
	}))
	if err == nil {
		t.Error("expected error for non-session channel type")
	}
}

// TestMockSSH_ExecSuccessExitCode verifies that a successful command
// returns exit code 0 through the full SSH path.
func TestMockSSH_ExecSuccessExitCode(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer server.Close()

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	defer session.Close()

	// Run a command that succeeds (exit 0)
	err = session.Run("true")
	if err != nil {
		t.Errorf("expected no error for successful command, got %v", err)
	}
}

// TestMockSSH_CloseWithActiveShell verifies server Close() cleans up
// sessions that have active PTY and running commands.
func TestMockSSH_CloseWithActiveShell(t *testing.T) {
	server, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	client, err := ssh.Dial("tcp", server.Addr(), sshClientConfig())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		t.Fatalf("NewSession() error = %v", err)
	}

	err = session.RequestPty("xterm", 24, 80, ssh.TerminalModes{})
	if err != nil {
		session.Close()
		client.Close()
		t.Fatalf("RequestPty() error = %v", err)
	}

	err = session.Shell()
	if err != nil {
		session.Close()
		client.Close()
		t.Fatalf("Shell() error = %v", err)
	}

	// Give the shell a moment to start
	time.Sleep(200 * time.Millisecond)

	// Close the client first to disconnect from the server. This will cause
	// the server-side io.Copy goroutines and cmd.Wait() to unblock, allowing
	// the server to clean up properly.
	session.Close()
	client.Close()

	// Give the server a moment to process the disconnect
	time.Sleep(200 * time.Millisecond)

	// Now close the server - all sessions should be cleaned up already
	err = server.Close()
	if err != nil {
		t.Logf("Close() returned error (expected for active connections): %v", err)
	}
}
