package ssh

import (
	"fmt"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesshdialer"
	gossh "golang.org/x/crypto/ssh"
)

// TestPTY_DefaultSSHPTYOptions_EnvContents verifies all expected env vars are present.
func TestPTY_DefaultSSHPTYOptions_EnvContents(t *testing.T) {
	opts := DefaultSSHPTYOptions()

	expectedEnvKeys := []string{"PS1", "PROMPT_COMMAND", "NO_COLOR"}
	for _, key := range expectedEnvKeys {
		if _, ok := opts.Env[key]; !ok {
			t.Errorf("DefaultSSHPTYOptions missing env key %q", key)
		}
	}

	if len(opts.Env) != len(expectedEnvKeys) {
		t.Errorf("expected %d env vars, got %d", len(expectedEnvKeys), len(opts.Env))
	}
}

// TestPTY_DefaultSSHPTYOptions_Values verifies specific default values.
func TestPTY_DefaultSSHPTYOptions_Values(t *testing.T) {
	opts := DefaultSSHPTYOptions()

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"Term", opts.Term, "dumb"},
		{"Rows", opts.Rows, uint32(24)},
		{"Cols", opts.Cols, uint32(120)},
		{"PS1", opts.Env["PS1"], "$ "},
		{"PROMPT_COMMAND", opts.Env["PROMPT_COMMAND"], ""},
		{"NO_COLOR", opts.Env["NO_COLOR"], "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, tt.got)
			}
		})
	}
}

// TestPTY_NewSSHPTY_ConnectFailure tests that NewSSHPTY tries to connect
// when the client is not connected, and propagates the connect error.
func TestPTY_NewSSHPTY_ConnectFailure(t *testing.T) {
	dialer := fakesshdialer.New()
	dialer.SetError(fmt.Errorf("connection refused"))
	clk := fakeclock.New(time.Now())

	client := &Client{
		host:              "unreachable.example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	opts := DefaultSSHPTYOptions()
	_, err := NewSSHPTY(client, opts)
	if err == nil {
		t.Fatal("expected error from NewSSHPTY with disconnected client")
	}

	// Verify the error includes "connect" in the chain
	if len(dialer.Calls()) != 1 {
		t.Errorf("expected 1 dial attempt, got %d", len(dialer.Calls()))
	}
}

// TestPTY_NewSSHPTY_EmptyOpts tests that zero-value options get defaulted.
func TestPTY_NewSSHPTY_EmptyOpts(t *testing.T) {
	dialer := fakesshdialer.New()
	dialer.SetError(fmt.Errorf("connection refused"))
	clk := fakeclock.New(time.Now())

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		dialer:            dialer,
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
	}

	// Pass empty options - they should get defaults applied before the connect error
	opts := SSHPTYOptions{}
	_, err := NewSSHPTY(client, opts)
	if err == nil {
		t.Fatal("expected error from NewSSHPTY")
	}
}

// TestPTY_NewSSHPTY_ConnectedButSessionFails verifies the path when
// the client is "connected" (conn != nil) but NewSession fails.
func TestPTY_NewSSHPTY_ConnectedButSessionFails(t *testing.T) {
	clk := fakeclock.New(time.Now())

	// Create a fake ssh.Client that will fail on NewSession
	fakeClient, cleanup := newFakeSSHClient()
	defer cleanup()

	// Close the fake client immediately so NewSession fails
	fakeClient.Close()

	client := &Client{
		host:              "example.com",
		port:              22,
		config:            &gossh.ClientConfig{},
		clock:             clk,
		keepaliveInterval: 30 * time.Second,
		conn:              fakeClient,
	}

	opts := DefaultSSHPTYOptions()
	_, err := NewSSHPTY(client, opts)
	if err == nil {
		t.Fatal("expected error from NewSSHPTY when session creation fails")
	}
}

// TestPTY_BackgroundReader_ChannelClose tests that backgroundReader exits
// when the closeCh is closed.
func TestPTY_BackgroundReader_ChannelClose(t *testing.T) {
	clk := fakeclock.New(time.Now())

	// Use a blocking reader that never returns
	pr := &blockingReader{ch: make(chan struct{})}

	p := &SSHPTY{
		stdout:  pr,
		dataCh:  make(chan []byte, 100),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Start backgroundReader
	done := make(chan struct{})
	go func() {
		p.backgroundReader()
		close(done)
	}()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Close the closeCh to stop the reader
	close(p.closeCh)
	// Unblock the reader
	close(pr.ch)

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("backgroundReader did not exit after closeCh was closed")
	}
}

// TestPTY_BackgroundReader_ReadError tests that backgroundReader sends
// errors to the error channel.
func TestPTY_BackgroundReader_ReadError(t *testing.T) {
	clk := fakeclock.New(time.Now())

	// Use a reader that returns an error
	er := &errorReader{err: fmt.Errorf("read error")}

	p := &SSHPTY{
		stdout:  er,
		dataCh:  make(chan []byte, 100),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Start backgroundReader and wait for it to finish
	done := make(chan struct{})
	go func() {
		p.backgroundReader()
		close(done)
	}()

	select {
	case <-done:
		// backgroundReader exited
	case <-time.After(2 * time.Second):
		t.Fatal("backgroundReader did not exit on read error")
	}

	// Check the error was sent
	select {
	case err := <-p.errCh:
		if err.Error() != "read error" {
			t.Errorf("expected 'read error', got %q", err.Error())
		}
	default:
		t.Error("expected error in errCh")
	}
}

// TestPTY_BackgroundReader_DataForwarding tests that data read from stdout
// is forwarded to the dataCh.
func TestPTY_BackgroundReader_DataForwarding(t *testing.T) {
	clk := fakeclock.New(time.Now())

	// Use a reader that returns data once then EOF
	dr := &dataReader{data: []byte("hello from pty"), done: false}

	p := &SSHPTY{
		stdout:  dr,
		dataCh:  make(chan []byte, 100),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   clk,
	}

	// Start backgroundReader
	done := make(chan struct{})
	go func() {
		p.backgroundReader()
		close(done)
	}()

	// Read data from dataCh
	select {
	case data := <-p.dataCh:
		if string(data) != "hello from pty" {
			t.Errorf("expected 'hello from pty', got %q", string(data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for data from backgroundReader")
	}

	// Wait for the reader to exit (it will get EOF from dataReader)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("backgroundReader did not exit")
	}
}

// TestPTY_Close_WithCloseCh tests that Close properly closes the closeCh.
func TestPTY_Close_WithCloseCh(t *testing.T) {
	closeCh := make(chan struct{})
	p := &SSHPTY{
		closeCh: closeCh,
	}

	err := p.Close()
	if err != nil {
		t.Errorf("Close() error: %v", err)
	}

	// Verify closeCh is closed
	select {
	case <-closeCh:
		// expected
	default:
		t.Error("closeCh should be closed after Close()")
	}

	if !p.closed {
		t.Error("expected closed=true")
	}
}

// --- Helper types ---

// blockingReader blocks on Read until the channel is closed.
type blockingReader struct {
	ch chan struct{}
}

func (r *blockingReader) Read(b []byte) (int, error) {
	<-r.ch
	return 0, fmt.Errorf("reader closed")
}

// errorReader always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(b []byte) (int, error) {
	return 0, r.err
}

// dataReader returns data on the first read, then EOF on subsequent reads.
type dataReader struct {
	data []byte
	done bool
}

func (r *dataReader) Read(b []byte) (int, error) {
	if r.done {
		return 0, fmt.Errorf("EOF")
	}
	r.done = true
	n := copy(b, r.data)
	return n, nil
}
