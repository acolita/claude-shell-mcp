// Package session provides shell session management.
package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/prompt"
	"github.com/acolita/claude-shell-mcp/internal/pty"
)

// State represents the session state.
type State string

const (
	StateIdle          State = "idle"
	StateRunning       State = "running"
	StateAwaitingInput State = "awaiting_input"
	StateClosed        State = "closed"
)

// endMarker is used to detect command completion.
const endMarker = "___CMD_END_MARKER___"

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
	config         *config.Config
	mu             sync.Mutex
	localPTY       *pty.LocalPTY
	promptDetector *prompt.Detector

	// Pending prompt info when awaiting input
	pendingPrompt *prompt.Detection
	outputBuffer  bytes.Buffer
}

// Initialize initializes the session with a PTY.
func (s *Session) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Mode != "local" {
		return fmt.Errorf("ssh mode not implemented yet")
	}

	// Create prompt detector
	s.promptDetector = prompt.NewDetector()

	// Add custom patterns from config
	if s.config != nil {
		for _, p := range s.config.PromptDetection.CustomPatterns {
			if err := s.promptDetector.AddPatternFromConfig(p.Name, p.Regex, p.Type, p.MaskInput); err != nil {
				return fmt.Errorf("add custom pattern %s: %w", p.Name, err)
			}
		}
	}

	// Create local PTY
	opts := pty.DefaultOptions()
	localPTY, err := pty.NewLocalPTY(opts)
	if err != nil {
		return fmt.Errorf("create local pty: %w", err)
	}

	s.localPTY = localPTY
	s.Shell = localPTY.Shell()
	s.State = StateIdle
	s.CreatedAt = time.Now()
	s.LastUsed = time.Now()

	// Get initial cwd
	cwd, err := os.Getwd()
	if err == nil {
		s.Cwd = cwd
	}

	// Wait for shell to be ready
	time.Sleep(200 * time.Millisecond)

	// Drain initial output (shell prompt, etc.)
	buf := make([]byte, 8192)
	s.localPTY.File().SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	s.localPTY.Read(buf) // Ignore output

	// Set simple prompt to avoid complex prompts from .bashrc
	// Also disable history expansion which can interfere with commands
	s.localPTY.WriteString("PS1='$ '; PROMPT_COMMAND=''; set +H\n")
	time.Sleep(100 * time.Millisecond)
	s.localPTY.File().SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	s.localPTY.Read(buf) // Drain the output

	return nil
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

	if s.localPTY == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	s.State = StateRunning
	s.LastUsed = time.Now()
	s.outputBuffer.Reset()

	// Create command with end marker for completion detection
	// We use a unique marker that will be echoed back when the command completes
	fullCommand := fmt.Sprintf("%s; echo '%s'$?\n", command, endMarker)

	// Write command to PTY
	if _, err := s.localPTY.WriteString(fullCommand); err != nil {
		s.State = StateIdle
		return nil, fmt.Errorf("write command: %w", err)
	}

	// Read output with timeout
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.readOutput(ctx, command)
}

// readOutput reads output from PTY until completion or prompt detection.
func (s *Session) readOutput(ctx context.Context, command string) (*ExecResult, error) {
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			s.State = StateIdle
			return &ExecResult{
				Status: "timeout",
				Stdout: s.cleanOutput(s.outputBuffer.String(), command),
			}, nil
		default:
		}

		// Set short read deadline for non-blocking reads
		if f := s.localPTY.File(); f != nil {
			f.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		}

		n, err := s.localPTY.Read(buf)
		if err != nil {
			if os.IsTimeout(err) || err == io.EOF {
				// Check if we have an end marker
				output := s.outputBuffer.String()
				if exitCode, found := s.extractExitCode(output); found {
					s.State = StateIdle
					s.updateCwd()
					return &ExecResult{
						Status:   "completed",
						ExitCode: &exitCode,
						Stdout:   s.cleanOutput(output, command),
						Cwd:      s.Cwd,
					}, nil
				}
				continue
			}
			s.State = StateIdle
			return nil, fmt.Errorf("read output: %w", err)
		}

		if n > 0 {
			s.outputBuffer.Write(buf[:n])
			output := s.outputBuffer.String()

			// Check for command completion
			if exitCode, found := s.extractExitCode(output); found {
				s.State = StateIdle
				s.updateCwd()
				return &ExecResult{
					Status:   "completed",
					ExitCode: &exitCode,
					Stdout:   s.cleanOutput(output, command),
					Cwd:      s.Cwd,
				}, nil
			}

			// Check for interactive prompt
			if detection := s.promptDetector.Detect(output); detection != nil {
				s.State = StateAwaitingInput
				s.pendingPrompt = detection
				return &ExecResult{
					Status:        "awaiting_input",
					Stdout:        s.cleanOutput(output, command),
					PromptType:    string(detection.Pattern.Type),
					PromptText:    detection.MatchedText,
					ContextBuffer: detection.ContextBuffer,
					MaskInput:     detection.Pattern.MaskInput,
					Hint:          detection.Hint(),
				}, nil
			}
		}
	}
}

// ProvideInput provides input to a session waiting for input.
func (s *Session) ProvideInput(input string) (*ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != StateAwaitingInput {
		return nil, fmt.Errorf("session is not awaiting input (state: %s)", s.State)
	}

	if s.localPTY == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	s.State = StateRunning
	s.LastUsed = time.Now()

	// Write input followed by newline
	if _, err := s.localPTY.WriteString(input + "\n"); err != nil {
		s.State = StateAwaitingInput
		return nil, fmt.Errorf("write input: %w", err)
	}

	// Continue reading output
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.readOutput(ctx, "")
}

// Interrupt sends an interrupt signal to the session.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != StateRunning && s.State != StateAwaitingInput {
		return fmt.Errorf("session is not running (state: %s)", s.State)
	}

	if s.localPTY == nil {
		return fmt.Errorf("session not initialized")
	}

	if err := s.localPTY.Interrupt(); err != nil {
		return fmt.Errorf("send interrupt: %w", err)
	}

	s.State = StateIdle
	s.pendingPrompt = nil
	return nil
}

// Close closes the session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateClosed {
		return nil
	}

	if s.localPTY != nil {
		if err := s.localPTY.Close(); err != nil {
			return fmt.Errorf("close pty: %w", err)
		}
	}

	s.State = StateClosed
	return nil
}

// extractExitCode extracts the exit code from output if the end marker is present.
// The marker must appear at the start of a line followed by a number (the exit code).
func (s *Session) extractExitCode(output string) (int, bool) {
	// Clean carriage returns for consistent line parsing
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")

	// Look for the marker at the start of a line followed by exit code
	// Format: ___CMD_END_MARKER___<exit_code>
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, endMarker) {
			rest := strings.TrimPrefix(line, endMarker)
			var exitCode int
			if _, err := fmt.Sscanf(rest, "%d", &exitCode); err == nil {
				return exitCode, true
			}
		}
	}

	return 0, false
}

// cleanOutput removes the command echo, end marker, and carriage returns from output.
func (s *Session) cleanOutput(output, command string) string {
	// Remove carriage returns
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "")

	lines := strings.Split(output, "\n")
	var cleaned []string

	// Track if we've seen the command echo (first occurrence)
	seenCommand := false

	for _, line := range lines {
		// Skip the shell prompt line
		if strings.HasPrefix(line, "$ ") {
			continue
		}

		// Skip the first line containing the command we sent (command echo)
		if command != "" && !seenCommand && strings.Contains(line, command) {
			seenCommand = true
			continue
		}

		// Skip lines containing the end marker
		if strings.Contains(line, endMarker) {
			continue
		}

		cleaned = append(cleaned, line)
	}

	// Trim leading and trailing empty lines
	for len(cleaned) > 0 && strings.TrimSpace(cleaned[0]) == "" {
		cleaned = cleaned[1:]
	}
	for len(cleaned) > 0 && strings.TrimSpace(cleaned[len(cleaned)-1]) == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}

	return strings.Join(cleaned, "\n")
}

// updateCwd updates the current working directory.
func (s *Session) updateCwd() {
	// Send pwd command and read result
	s.localPTY.WriteString("pwd\n")
	time.Sleep(50 * time.Millisecond)

	buf := make([]byte, 1024)
	s.localPTY.File().SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	n, _ := s.localPTY.Read(buf)

	if n > 0 {
		output := string(buf[:n])
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && line != "pwd" && strings.HasPrefix(line, "/") {
				s.Cwd = line
				break
			}
		}
	}
}

// drainOutput drains any pending output from the PTY.
func (s *Session) drainOutput(timeout time.Duration) {
	buf := make([]byte, 4096)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if f := s.localPTY.File(); f != nil {
			f.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		}
		_, err := s.localPTY.Read(buf)
		if err != nil {
			break
		}
	}
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
	Status        string            `json:"status"` // "completed", "awaiting_input", "timeout"
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
