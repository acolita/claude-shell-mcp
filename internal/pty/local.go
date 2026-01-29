// Package pty provides PTY (pseudo-terminal) management for local shell sessions.
package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// LocalPTY represents a local pseudo-terminal session.
type LocalPTY struct {
	cmd   *exec.Cmd
	pty   *os.File
	shell string
	mu    sync.Mutex

	// Output buffer for reading
	outputBuf []byte
}

// PTYOptions configures PTY allocation.
type PTYOptions struct {
	Shell  string // Shell to use (defaults to user's shell or /bin/bash)
	Term   string // Terminal type (default: xterm-256color)
	Rows   uint16 // Terminal rows (default: 24)
	Cols   uint16 // Terminal columns (default: 80)
	Dir    string // Initial working directory
	Env    []string // Additional environment variables
}

// DefaultOptions returns default PTY options.
// Uses TERM=dumb to prevent ANSI escape codes in output.
func DefaultOptions() PTYOptions {
	shell := detectShell()
	return PTYOptions{
		Shell: shell,
		Term:  "dumb",
		Rows:  24,
		Cols:  120,
		Env:   ShellEnv(shell),
	}
}

// ShellEnv returns environment variables for the given shell.
func ShellEnv(shell string) []string {
	env := []string{
		"NO_COLOR=1", // Hint to programs to disable colors
	}

	// Detect shell type from path
	shellName := shell
	if idx := len(shell) - 1; idx >= 0 {
		for i := len(shell) - 1; i >= 0; i-- {
			if shell[i] == '/' {
				shellName = shell[i+1:]
				break
			}
		}
	}

	switch shellName {
	case "zsh":
		// Zsh uses PROMPT instead of PS1 for left prompt
		env = append(env,
			"PROMPT=$ ",         // Simple prompt for zsh
			"PS1=$ ",            // Also set PS1 for compatibility
			"PROMPT_COMMAND=",   // Disable prompt command
			"precmd_functions=", // Disable zsh precmd hooks
			"RPROMPT=",          // Disable right prompt
		)
	case "fish":
		// Fish uses functions, we'll handle it differently
		env = append(env,
			"PS1=$ ",
			"fish_greeting=", // Disable greeting
		)
	default:
		// Bash and other POSIX shells
		env = append(env,
			"PS1=$ ",
			"PROMPT_COMMAND=",
		)
	}

	return env
}

// NewLocalPTY creates a new local PTY session.
func NewLocalPTY(opts PTYOptions) (*LocalPTY, error) {
	if opts.Shell == "" {
		opts.Shell = detectShell()
	}
	if opts.Term == "" {
		opts.Term = "dumb"
	}
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	if opts.Cols == 0 {
		opts.Cols = 80
	}

	// Create shell command
	cmd := exec.Command(opts.Shell)

	// Set working directory if specified
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	// Set environment
	cmd.Env = append(os.Environ(), fmt.Sprintf("TERM=%s", opts.Term))
	cmd.Env = append(cmd.Env, opts.Env...)

	// Set window size
	winSize := &pty.Winsize{
		Rows: opts.Rows,
		Cols: opts.Cols,
	}

	// Start command with PTY
	ptmx, err := pty.StartWithSize(cmd, winSize)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	return &LocalPTY{
		cmd:   cmd,
		pty:   ptmx,
		shell: opts.Shell,
	}, nil
}

// Shell returns the shell being used.
func (p *LocalPTY) Shell() string {
	return p.shell
}

// Read reads from the PTY output.
func (p *LocalPTY) Read(b []byte) (int, error) {
	return p.pty.Read(b)
}

// Write writes to the PTY input.
func (p *LocalPTY) Write(b []byte) (int, error) {
	return p.pty.Write(b)
}

// WriteString writes a string to the PTY.
func (p *LocalPTY) WriteString(s string) (int, error) {
	return p.pty.WriteString(s)
}

// Resize resizes the PTY window.
func (p *LocalPTY) Resize(rows, cols uint16) error {
	return pty.Setsize(p.pty, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// Signal sends a signal to the shell process.
func (p *LocalPTY) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	return p.cmd.Process.Signal(sig)
}

// Interrupt sends SIGINT to the shell.
func (p *LocalPTY) Interrupt() error {
	return p.Signal(syscall.SIGINT)
}

// Wait waits for the shell process to exit.
func (p *LocalPTY) Wait() error {
	return p.cmd.Wait()
}

// Close closes the PTY and terminates the shell.
func (p *LocalPTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error

	// Close PTY
	if err := p.pty.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close pty: %w", err))
	}

	// Kill process if still running
	if p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil && err.Error() != "os: process already finished" {
			errs = append(errs, fmt.Errorf("kill process: %w", err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Fd returns the file descriptor of the PTY.
func (p *LocalPTY) Fd() uintptr {
	return p.pty.Fd()
}

// File returns the underlying file of the PTY.
func (p *LocalPTY) File() *os.File {
	return p.pty
}

// Reader returns an io.Reader for the PTY output.
func (p *LocalPTY) Reader() io.Reader {
	return p.pty
}

// Writer returns an io.Writer for the PTY input.
func (p *LocalPTY) Writer() io.Writer {
	return p.pty
}

// detectShell detects the user's default shell.
func detectShell() string {
	// Check SHELL environment variable
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}

	// Try common shells
	shells := []string{"/bin/bash", "/bin/zsh", "/bin/sh"}
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}

	return "/bin/sh"
}
