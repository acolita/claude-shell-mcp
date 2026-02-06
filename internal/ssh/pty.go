package ssh

import (
	"fmt"
	"io"
	"sync"
	"syscall"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
	"golang.org/x/crypto/ssh"
)

// SSHPTY represents a PTY session over SSH.
type SSHPTY struct {
	client  *Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	mu      sync.Mutex

	// Terminal settings
	term string
	rows uint32
	cols uint32

	// Buffered reader for timeout support
	dataCh   chan []byte   // Channel for incoming data chunks
	errCh    chan error    // Channel for read errors
	closeCh  chan struct{} // Channel to signal close
	closed   bool
	closeMu  sync.Mutex

	// Read deadline support
	readDeadline time.Time
	deadlineMu   sync.Mutex

	// Injected dependencies
	clock ports.Clock
}

// SSHPTYOptions configures SSH PTY allocation.
type SSHPTYOptions struct {
	Term string // Terminal type (default: dumb)
	Rows uint32 // Terminal rows (default: 24)
	Cols uint32 // Terminal columns (default: 120)
	Env  map[string]string // Environment variables to set
}

// DefaultSSHPTYOptions returns default SSH PTY options.
func DefaultSSHPTYOptions() SSHPTYOptions {
	return SSHPTYOptions{
		Term: "dumb",
		Rows: 24,
		Cols: 120,
		Env: map[string]string{
			"PS1":            "$ ",
			"PROMPT_COMMAND": "",
			"NO_COLOR":       "1",
		},
	}
}

// NewSSHPTY creates a new SSH PTY session.
func NewSSHPTY(client *Client, opts SSHPTYOptions) (*SSHPTY, error) {
	if !client.IsConnected() {
		if err := client.Connect(); err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
	}

	// Apply defaults
	if opts.Term == "" {
		opts.Term = "dumb"
	}
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	if opts.Cols == 0 {
		opts.Cols = 120
	}

	// Create new SSH session
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}

	// Set environment variables
	for key, value := range opts.Env {
		// Note: Many SSH servers restrict which env vars can be set
		// This may silently fail depending on server config
		session.Setenv(key, value)
	}

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Enable echo
		ssh.TTY_OP_ISPEED: 14400, // Input speed
		ssh.TTY_OP_OSPEED: 14400, // Output speed
	}

	if err := session.RequestPty(opts.Term, int(opts.Rows), int(opts.Cols), modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	// Get stdin pipe
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	// Get stdout pipe
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Start shell
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	pty := &SSHPTY{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
		term:    opts.Term,
		rows:    opts.Rows,
		cols:    opts.Cols,
		dataCh:  make(chan []byte, 100), // Buffer up to 100 chunks
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
		clock:   client.clock,
	}

	// Start background reader
	go pty.backgroundReader()

	return pty, nil
}

// backgroundReader continuously reads from stdout and sends data to the channel.
func (p *SSHPTY) backgroundReader() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-p.closeCh:
			return
		default:
		}

		n, err := p.stdout.Read(buf)
		if err != nil {
			select {
			case p.errCh <- err:
			default:
				// Error channel full, discard
			}
			return
		}

		if n > 0 {
			// Make a copy of the data since buf will be reused
			data := make([]byte, n)
			copy(data, buf[:n])

			select {
			case p.dataCh <- data:
			case <-p.closeCh:
				return
			}
		}
	}
}

// Read reads from the PTY output with deadline support.
func (p *SSHPTY) Read(b []byte) (int, error) {
	p.deadlineMu.Lock()
	deadline := p.readDeadline
	p.deadlineMu.Unlock()

	// Calculate timeout
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		remaining := deadline.Sub(p.clock.Now())
		if remaining <= 0 {
			return 0, &timeoutError{}
		}
		timeout = p.clock.After(remaining)
	}

	// Wait for data, error, or timeout
	select {
	case data := <-p.dataCh:
		n := copy(b, data)
		return n, nil
	case err := <-p.errCh:
		return 0, err
	case <-timeout:
		return 0, &timeoutError{}
	}
}

// timeoutError implements net.Error for timeout detection.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

// Write writes to the PTY input.
func (p *SSHPTY) Write(b []byte) (int, error) {
	return p.stdin.Write(b)
}

// WriteString writes a string to the PTY.
func (p *SSHPTY) WriteString(s string) (int, error) {
	return p.stdin.Write([]byte(s))
}

// Resize resizes the PTY window.
func (p *SSHPTY) Resize(rows, cols uint32) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.session.WindowChange(int(rows), int(cols)); err != nil {
		return fmt.Errorf("window change: %w", err)
	}

	p.rows = rows
	p.cols = cols
	return nil
}

// Signal sends a signal to the remote process.
func (p *SSHPTY) Signal(sig string) error {
	return p.session.Signal(ssh.Signal(sig))
}

// Interrupt sends SIGINT to the remote process.
func (p *SSHPTY) Interrupt() error {
	// Write Ctrl+C character
	_, err := p.stdin.Write([]byte{0x03})
	return err
}

// Close closes the SSH PTY session.
func (p *SSHPTY) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	// Signal background reader to stop
	close(p.closeCh)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.session != nil {
		err := p.session.Close()
		p.session = nil
		return err
	}
	return nil
}

// Wait waits for the shell to exit.
func (p *SSHPTY) Wait() error {
	return p.session.Wait()
}

// SetReadDeadline sets a read deadline.
func (p *SSHPTY) SetReadDeadline(t time.Time) error {
	p.deadlineMu.Lock()
	defer p.deadlineMu.Unlock()
	p.readDeadline = t
	return nil
}

// Term returns the terminal type.
func (p *SSHPTY) Term() string {
	return p.term
}

// Size returns the terminal size.
func (p *SSHPTY) Size() (rows, cols uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rows, p.cols
}

// signalToSSH converts syscall signal to SSH signal name.
func signalToSSH(sig syscall.Signal) ssh.Signal {
	switch sig {
	case syscall.SIGINT:
		return ssh.SIGINT
	case syscall.SIGTERM:
		return ssh.SIGTERM
	case syscall.SIGKILL:
		return ssh.SIGKILL
	case syscall.SIGHUP:
		return ssh.SIGHUP
	case syscall.SIGQUIT:
		return ssh.SIGQUIT
	default:
		return ssh.SIGTERM
	}
}
