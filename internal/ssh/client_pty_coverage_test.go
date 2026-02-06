package ssh

import (
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesshdialer"
	"golang.org/x/crypto/ssh"
)

// =============================================================================
// client.go tests
// =============================================================================

func TestDefaultClientOptions(t *testing.T) {
	opts := DefaultClientOptions()

	if opts.Port != 22 {
		t.Errorf("expected default port 22, got %d", opts.Port)
	}
	if opts.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", opts.Timeout)
	}
	if opts.KeepaliveInterval != 30*time.Second {
		t.Errorf("expected default keepalive 30s, got %v", opts.KeepaliveInterval)
	}
	if opts.HostKeyCallback == nil {
		t.Error("expected non-nil HostKeyCallback")
	}
}

func TestNewClient_RequiresHost(t *testing.T) {
	opts := ClientOptions{
		User:        "testuser",
		AuthMethods: []ssh.AuthMethod{ssh.Password("pass")},
	}
	_, err := NewClient(opts)
	if err == nil {
		t.Fatal("expected error for empty host")
	}
	if err.Error() != "host is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewClient_RequiresUser(t *testing.T) {
	opts := ClientOptions{
		Host:        "example.com",
		AuthMethods: []ssh.AuthMethod{ssh.Password("pass")},
	}
	_, err := NewClient(opts)
	if err == nil {
		t.Fatal("expected error for empty user")
	}
	if err.Error() != "user is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewClient_RequiresAuthMethods(t *testing.T) {
	opts := ClientOptions{
		Host: "example.com",
		User: "testuser",
	}
	_, err := NewClient(opts)
	if err == nil {
		t.Fatal("expected error for empty auth methods")
	}
	if err.Error() != "at least one auth method is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewClient_DefaultsApplied(t *testing.T) {
	opts := ClientOptions{
		Host:        "example.com",
		User:        "testuser",
		AuthMethods: []ssh.AuthMethod{ssh.Password("pass")},
		// Port, Timeout, KeepaliveInterval, HostKeyCallback all zero/nil
	}
	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.port != 22 {
		t.Errorf("expected default port 22, got %d", client.port)
	}
	if client.keepaliveInterval != 30*time.Second {
		t.Errorf("expected default keepalive 30s, got %v", client.keepaliveInterval)
	}
}

func TestNewClient_CustomValues(t *testing.T) {
	clk := fakeclock.New(time.Now())
	dialer := fakesshdialer.New()

	opts := ClientOptions{
		Host:              "myhost.com",
		Port:              2222,
		User:              "admin",
		AuthMethods:       []ssh.AuthMethod{ssh.Password("secret")},
		Timeout:           10 * time.Second,
		KeepaliveInterval: 15 * time.Second,
		HostKeyCallback:   ssh.InsecureIgnoreHostKey(),
		Clock:             clk,
		Dialer:            dialer,
	}
	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.host != "myhost.com" {
		t.Errorf("expected host myhost.com, got %s", client.host)
	}
	if client.port != 2222 {
		t.Errorf("expected port 2222, got %d", client.port)
	}
	if client.keepaliveInterval != 15*time.Second {
		t.Errorf("expected keepalive 15s, got %v", client.keepaliveInterval)
	}
}

func TestClient_HostAccessor(t *testing.T) {
	client := &Client{host: "test.example.com"}
	if client.Host() != "test.example.com" {
		t.Errorf("expected host test.example.com, got %s", client.Host())
	}
}

func TestClient_PortAccessor(t *testing.T) {
	client := &Client{port: 2222}
	if client.Port() != 2222 {
		t.Errorf("expected port 2222, got %d", client.Port())
	}
}

func TestClient_IsConnected_NotConnected(t *testing.T) {
	client := &Client{}
	if client.IsConnected() {
		t.Error("expected IsConnected() to be false when no connection")
	}
}

func TestClient_RemoteAddr_NotConnected(t *testing.T) {
	client := &Client{}
	addr := client.RemoteAddr()
	if addr != nil {
		t.Errorf("expected nil RemoteAddr when not connected, got %v", addr)
	}
}

func TestClient_NewSession_NotConnected(t *testing.T) {
	client := &Client{}
	_, err := client.NewSession()
	if err == nil {
		t.Fatal("expected error from NewSession when not connected")
	}
	if err.Error() != "not connected" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_SFTPClient_NotConnected(t *testing.T) {
	client := &Client{}
	_, err := client.SFTPClient()
	if err == nil {
		t.Fatal("expected error from SFTPClient when not connected")
	}
	if err.Error() != "not connected" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestClient_CloseSFTP_NoClient(t *testing.T) {
	client := &Client{}
	err := client.CloseSFTP()
	if err != nil {
		t.Errorf("expected nil error from CloseSFTP with no sftp client, got %v", err)
	}
}

func TestClient_TunnelManager_NotConnected(t *testing.T) {
	client := &Client{}
	tm := client.TunnelManager()
	if tm != nil {
		t.Error("expected nil TunnelManager when not connected")
	}
}

func TestClient_Close_NoConnection(t *testing.T) {
	client := &Client{}
	err := client.Close()
	if err != nil {
		t.Errorf("expected nil error from Close with no connection, got %v", err)
	}
}

func TestClient_Close_Idempotent(t *testing.T) {
	client := &Client{}
	// First close
	if err := client.Close(); err != nil {
		t.Errorf("first Close() error: %v", err)
	}
	// Second close should also succeed
	if err := client.Close(); err != nil {
		t.Errorf("second Close() error: %v", err)
	}
}

func TestClient_Close_StopsKeepalive(t *testing.T) {
	client := &Client{
		keepaliveStop: make(chan struct{}),
	}

	err := client.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify the keepaliveStop channel was closed (nil'd out)
	if client.keepaliveStop != nil {
		t.Error("expected keepaliveStop to be nil after Close")
	}
}

func TestClient_Connect_AlreadyConnected(t *testing.T) {
	// Create a client with a fake dialer that would fail if called
	dialer := fakesshdialer.New()
	dialer.SetError(fmt.Errorf("should not be called"))
	clk := fakeclock.New(time.Now())

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &ssh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
		conn:              &ssh.Client{}, // simulate already connected
	}

	err := client.Connect()
	if err != nil {
		t.Errorf("Connect on already-connected client should return nil, got: %v", err)
	}

	// Dialer should NOT have been called
	if len(dialer.Calls()) != 0 {
		t.Error("dialer should not have been called when already connected")
	}
}

func TestClient_Connect_DialError(t *testing.T) {
	dialer := fakesshdialer.New()
	dialer.SetError(fmt.Errorf("connection refused"))
	clk := fakeclock.New(time.Now())

	client := &Client{
		host:              "badhost.com",
		port:              22,
		config:            &ssh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	err := client.Connect()
	if err == nil {
		t.Fatal("expected error from Connect with failing dialer")
	}
	if client.conn != nil {
		t.Error("conn should be nil after failed connect")
	}

	// Verify dialer was called with correct address
	calls := dialer.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 dial call, got %d", len(calls))
	}
	if calls[0].Addr != "badhost.com:22" {
		t.Errorf("expected addr badhost.com:22, got %s", calls[0].Addr)
	}
}

// =============================================================================
// pty.go tests
// =============================================================================

func TestDefaultSSHPTYOptions(t *testing.T) {
	opts := DefaultSSHPTYOptions()

	if opts.Term != "dumb" {
		t.Errorf("expected term=dumb, got %s", opts.Term)
	}
	if opts.Rows != 24 {
		t.Errorf("expected rows=24, got %d", opts.Rows)
	}
	if opts.Cols != 120 {
		t.Errorf("expected cols=120, got %d", opts.Cols)
	}

	// Check environment variables
	if opts.Env == nil {
		t.Fatal("expected non-nil Env map")
	}
	if opts.Env["PS1"] != "$ " {
		t.Errorf("expected PS1='$ ', got %q", opts.Env["PS1"])
	}
	if opts.Env["PROMPT_COMMAND"] != "" {
		t.Errorf("expected empty PROMPT_COMMAND, got %q", opts.Env["PROMPT_COMMAND"])
	}
	if opts.Env["NO_COLOR"] != "1" {
		t.Errorf("expected NO_COLOR=1, got %q", opts.Env["NO_COLOR"])
	}
}

func TestTimeoutError(t *testing.T) {
	err := &timeoutError{}

	if err.Error() != "i/o timeout" {
		t.Errorf("expected 'i/o timeout', got %q", err.Error())
	}
	if !err.Timeout() {
		t.Error("Timeout() should return true")
	}
	if !err.Temporary() {
		t.Error("Temporary() should return true")
	}

	// Verify it implements net.Error
	var netErr net.Error = err
	if !netErr.Timeout() {
		t.Error("net.Error Timeout() should return true")
	}
}

func TestSignalToSSH(t *testing.T) {
	tests := []struct {
		input    syscall.Signal
		expected ssh.Signal
	}{
		{syscall.SIGINT, ssh.SIGINT},
		{syscall.SIGTERM, ssh.SIGTERM},
		{syscall.SIGKILL, ssh.SIGKILL},
		{syscall.SIGHUP, ssh.SIGHUP},
		{syscall.SIGQUIT, ssh.SIGQUIT},
		{syscall.Signal(99), ssh.SIGTERM}, // unknown signal defaults to SIGTERM
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("signal_%d", tt.input), func(t *testing.T) {
			result := signalToSSH(tt.input)
			if result != tt.expected {
				t.Errorf("signalToSSH(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSSHPTY_TermAccessor(t *testing.T) {
	pty := &SSHPTY{term: "xterm-256color"}
	if pty.Term() != "xterm-256color" {
		t.Errorf("expected term=xterm-256color, got %s", pty.Term())
	}
}

func TestSSHPTY_SizeAccessor(t *testing.T) {
	pty := &SSHPTY{rows: 40, cols: 160}
	rows, cols := pty.Size()
	if rows != 40 {
		t.Errorf("expected rows=40, got %d", rows)
	}
	if cols != 160 {
		t.Errorf("expected cols=160, got %d", cols)
	}
}

func TestSSHPTY_SetReadDeadline(t *testing.T) {
	pty := &SSHPTY{}
	deadline := time.Now().Add(5 * time.Second)

	err := pty.SetReadDeadline(deadline)
	if err != nil {
		t.Errorf("SetReadDeadline returned error: %v", err)
	}

	pty.deadlineMu.Lock()
	if !pty.readDeadline.Equal(deadline) {
		t.Errorf("expected deadline %v, got %v", deadline, pty.readDeadline)
	}
	pty.deadlineMu.Unlock()
}

func TestSSHPTY_SetReadDeadline_Zero(t *testing.T) {
	pty := &SSHPTY{}

	// First set a real deadline
	pty.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Clear it with zero value
	err := pty.SetReadDeadline(time.Time{})
	if err != nil {
		t.Errorf("SetReadDeadline returned error: %v", err)
	}

	pty.deadlineMu.Lock()
	if !pty.readDeadline.IsZero() {
		t.Error("expected zero deadline after clearing")
	}
	pty.deadlineMu.Unlock()
}

func TestSSHPTY_Close_Idempotent(t *testing.T) {
	pty := &SSHPTY{
		closeCh: make(chan struct{}),
	}

	// First close
	err := pty.Close()
	if err != nil {
		t.Errorf("first Close() error: %v", err)
	}
	if !pty.closed {
		t.Error("expected closed=true after first Close")
	}

	// Second close should be safe
	err = pty.Close()
	if err != nil {
		t.Errorf("second Close() error: %v", err)
	}
}

func TestSSHPTY_Close_NilSession(t *testing.T) {
	pty := &SSHPTY{
		closeCh: make(chan struct{}),
		session: nil, // nil session
	}

	err := pty.Close()
	if err != nil {
		t.Errorf("Close with nil session should return nil, got: %v", err)
	}
}

func TestSSHPTY_Read_DataFromChannel(t *testing.T) {
	clk := fakeclock.New(time.Now())
	pty := &SSHPTY{
		dataCh:  make(chan []byte, 10),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Send some data into the channel
	expected := []byte("hello world")
	pty.dataCh <- expected

	buf := make([]byte, 1024)
	n, err := pty.Read(buf)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if string(buf[:n]) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(buf[:n]))
	}
}

func TestSSHPTY_Read_ErrorFromChannel(t *testing.T) {
	clk := fakeclock.New(time.Now())
	pty := &SSHPTY{
		dataCh:  make(chan []byte, 10),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Send an error into the error channel
	expectedErr := fmt.Errorf("connection lost")
	pty.errCh <- expectedErr

	buf := make([]byte, 1024)
	_, err := pty.Read(buf)
	if err == nil {
		t.Fatal("expected error from Read")
	}
	if err.Error() != "connection lost" {
		t.Errorf("expected 'connection lost', got %q", err.Error())
	}
}

func TestSSHPTY_Read_DeadlineExpired(t *testing.T) {
	clk := fakeclock.New(time.Now())
	pty := &SSHPTY{
		dataCh:  make(chan []byte, 10),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Set a deadline that is already in the past
	pty.SetReadDeadline(clk.Now().Add(-1 * time.Second))

	buf := make([]byte, 1024)
	_, err := pty.Read(buf)
	if err == nil {
		t.Fatal("expected timeout error from Read with expired deadline")
	}

	netErr, ok := err.(net.Error)
	if !ok {
		t.Fatal("expected net.Error")
	}
	if !netErr.Timeout() {
		t.Error("expected timeout error")
	}
}

func TestSSHPTY_Read_DeadlineFires(t *testing.T) {
	clk := fakeclock.New(time.Now())
	pty := &SSHPTY{
		dataCh:  make(chan []byte, 10),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Set a deadline 1 second in the future
	pty.SetReadDeadline(clk.Now().Add(1 * time.Second))

	// Start a goroutine that advances the clock past the deadline
	go func() {
		time.Sleep(10 * time.Millisecond) // Give Read time to set up
		clk.Advance(2 * time.Second)
	}()

	buf := make([]byte, 1024)
	_, err := pty.Read(buf)
	if err == nil {
		t.Fatal("expected timeout error from Read")
	}

	netErr, ok := err.(net.Error)
	if !ok {
		t.Fatalf("expected net.Error, got %T", err)
	}
	if !netErr.Timeout() {
		t.Error("expected timeout error")
	}
}

func TestSSHPTY_Read_NoDeadline(t *testing.T) {
	clk := fakeclock.New(time.Now())
	pty := &SSHPTY{
		dataCh:  make(chan []byte, 10),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Send data, then verify no-deadline read works
	pty.dataCh <- []byte("data without deadline")

	buf := make([]byte, 1024)
	n, err := pty.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf[:n]) != "data without deadline" {
		t.Errorf("unexpected data: %q", string(buf[:n]))
	}
}

func TestNewSSHPTY_ClientNotConnected(t *testing.T) {
	dialer := fakesshdialer.New()
	dialer.SetError(fmt.Errorf("connection refused"))
	clk := fakeclock.New(time.Now())

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &ssh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	opts := DefaultSSHPTYOptions()
	_, err := NewSSHPTY(client, opts)
	if err == nil {
		t.Fatal("expected error creating SSHPTY with disconnected client")
	}
}

func TestNewSSHPTY_OptsDefaults(t *testing.T) {
	// This tests the default-application logic in NewSSHPTY.
	// We pass zero-value options and verify they get defaulted.
	// We need the client connection to fail (so we don't need a real server),
	// but we can at least verify the function is called.
	dialer := fakesshdialer.New()
	dialer.SetError(fmt.Errorf("connection refused"))
	clk := fakeclock.New(time.Now())

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &ssh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	// Pass empty options (all zero) to trigger default logic
	opts := SSHPTYOptions{}
	_, err := NewSSHPTY(client, opts)
	// We expect an error because dial fails, but defaults get applied before that
	if err == nil {
		t.Fatal("expected error from NewSSHPTY with failing dialer")
	}
}

func TestSSHPTY_WriteString(t *testing.T) {
	// Create a mock writer to capture writes
	w := &mockWriteCloser{}
	pty := &SSHPTY{
		stdin: w,
	}

	n, err := pty.WriteString("echo hello\n")
	if err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if n != 11 {
		t.Errorf("expected 11 bytes written, got %d", n)
	}
	if string(w.data) != "echo hello\n" {
		t.Errorf("expected 'echo hello\\n', got %q", string(w.data))
	}
}

func TestSSHPTY_Write(t *testing.T) {
	w := &mockWriteCloser{}
	pty := &SSHPTY{
		stdin: w,
	}

	data := []byte{0x68, 0x69} // "hi"
	n, err := pty.Write(data)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 bytes written, got %d", n)
	}
	if string(w.data) != "hi" {
		t.Errorf("expected 'hi', got %q", string(w.data))
	}
}

func TestSSHPTY_Interrupt(t *testing.T) {
	w := &mockWriteCloser{}
	pty := &SSHPTY{
		stdin: w,
	}

	err := pty.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt error: %v", err)
	}
	if len(w.data) != 1 || w.data[0] != 0x03 {
		t.Errorf("expected Ctrl+C byte (0x03), got %v", w.data)
	}
}

// =============================================================================
// Additional edge cases
// =============================================================================

func TestClient_NewClient_AllValidations(t *testing.T) {
	// Test with all required fields, minimal config
	opts := ClientOptions{
		Host:        "host.example.com",
		User:        "user",
		AuthMethods: []ssh.AuthMethod{ssh.Password("p")},
	}
	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Host() != "host.example.com" {
		t.Errorf("unexpected host: %s", client.Host())
	}
	if client.Port() != 22 {
		t.Errorf("unexpected port: %d", client.Port())
	}
	if client.IsConnected() {
		t.Error("new client should not be connected")
	}
}

func TestSSHPTY_Size_ThreadSafe(t *testing.T) {
	pty := &SSHPTY{rows: 24, cols: 80}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			pty.Size()
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		pty.Size()
	}
	<-done
}

func TestSSHPTYOptions_Struct(t *testing.T) {
	opts := SSHPTYOptions{
		Term: "xterm",
		Rows: 50,
		Cols: 200,
		Env: map[string]string{
			"TERM_PROGRAM": "test",
		},
	}

	if opts.Term != "xterm" {
		t.Errorf("expected term=xterm, got %s", opts.Term)
	}
	if opts.Rows != 50 {
		t.Errorf("expected rows=50, got %d", opts.Rows)
	}
	if opts.Cols != 200 {
		t.Errorf("expected cols=200, got %d", opts.Cols)
	}
	if opts.Env["TERM_PROGRAM"] != "test" {
		t.Errorf("unexpected env value: %s", opts.Env["TERM_PROGRAM"])
	}
}

func TestClientOptions_Struct(t *testing.T) {
	opts := ClientOptions{
		Host:              "remote.host",
		Port:              2222,
		User:              "deploy",
		Timeout:           10 * time.Second,
		KeepaliveInterval: 60 * time.Second,
	}

	if opts.Host != "remote.host" {
		t.Errorf("unexpected host: %s", opts.Host)
	}
	if opts.Port != 2222 {
		t.Errorf("unexpected port: %d", opts.Port)
	}
	if opts.User != "deploy" {
		t.Errorf("unexpected user: %s", opts.User)
	}
}

// =============================================================================
// Helper types for testing
// =============================================================================

// mockWriteCloser is a simple io.WriteCloser that captures all writes.
type mockWriteCloser struct {
	data   []byte
	closed bool
}

func (m *mockWriteCloser) Write(p []byte) (int, error) {
	m.data = append(m.data, p...)
	return len(p), nil
}

func (m *mockWriteCloser) Close() error {
	m.closed = true
	return nil
}
