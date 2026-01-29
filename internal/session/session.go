// Package session provides shell session management.
package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
)

// State represents the session state.
type State string

const (
	StateIdle          State = "idle"
	StateRunning       State = "running"
	StateAwaitingInput State = "awaiting_input"
	StateClosed        State = "closed"
)

// Session represents a shell session.
type Session struct {
	ID        string
	State     State
	Mode      string // "local" or "ssh"
	Shell     string
	Cwd       string
	EnvVars   map[string]string
	CreatedAt time.Time
	LastUsed  time.Time

	// Internal fields
	config *config.Config
	mu     sync.Mutex

	// PTY/SSH connection (to be implemented)
	// pty  *os.File (for local)
	// ssh  *ssh.Session (for ssh)
}

// Status returns the current session status.
func (s *Session) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	return SessionStatus{
		ID:          s.ID,
		State:       s.State,
		Mode:        s.Mode,
		Shell:       s.Shell,
		Cwd:         s.Cwd,
		IdleSeconds: int(time.Since(s.LastUsed).Seconds()),
	}
}

// Exec executes a command in the session.
func (s *Session) Exec(command string, timeoutMs int) (*ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateClosed {
		return nil, fmt.Errorf("session is closed")
	}

	// TODO: Implement actual execution
	// This is a stub that returns not-implemented error
	return nil, fmt.Errorf("exec not implemented yet")
}

// ProvideInput provides input to a session waiting for input.
func (s *Session) ProvideInput(input string) (*ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != StateAwaitingInput {
		return nil, fmt.Errorf("session is not awaiting input (state: %s)", s.State)
	}

	// TODO: Implement actual input provision
	return nil, fmt.Errorf("provide_input not implemented yet")
}

// Interrupt sends an interrupt signal to the session.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != StateRunning && s.State != StateAwaitingInput {
		return fmt.Errorf("session is not running (state: %s)", s.State)
	}

	// TODO: Implement actual interrupt
	return fmt.Errorf("interrupt not implemented yet")
}

// Close closes the session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateClosed {
		return nil
	}

	// TODO: Close PTY/SSH connection
	s.State = StateClosed
	return nil
}

// SessionStatus represents the status of a session.
type SessionStatus struct {
	ID          string            `json:"session_id"`
	State       State             `json:"state"`
	Mode        string            `json:"mode"`
	Shell       string            `json:"shell"`
	Cwd         string            `json:"cwd"`
	IdleSeconds int               `json:"idle_seconds"`
	EnvVars     map[string]string `json:"env_vars,omitempty"`
}

// ExecResult represents the result of command execution.
type ExecResult struct {
	Status        string            `json:"status"` // "completed", "awaiting_input"
	ExitCode      *int              `json:"exit_code,omitempty"`
	Stdout        string            `json:"stdout,omitempty"`
	Stderr        string            `json:"stderr,omitempty"`
	Cwd           string            `json:"cwd,omitempty"`
	EnvVars       map[string]string `json:"env_vars,omitempty"`
	PromptType    string            `json:"prompt_type,omitempty"`
	PromptText    string            `json:"prompt_text,omitempty"`
	ContextBuffer string            `json:"context_buffer,omitempty"`
	MaskInput     bool              `json:"mask_input,omitempty"`
	Hint          string            `json:"hint,omitempty"`
}
