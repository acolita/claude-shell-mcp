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
	localpty "github.com/acolita/claude-shell-mcp/internal/pty"
	"github.com/acolita/claude-shell-mcp/internal/ssh"
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

	// SSH connection info (for ssh mode)
	Host     string
	Port     int
	User     string
	Password string // For password-based auth (not persisted)

	// Internal fields
	config         *config.Config
	mu             sync.Mutex
	pty            PTY // Common interface for local and SSH PTY
	sshClient      *ssh.Client
	promptDetector *prompt.Detector

	// Pending prompt info when awaiting input
	pendingPrompt *prompt.Detection
	outputBuffer  bytes.Buffer
}

// Initialize initializes the session with a PTY.
func (s *Session) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	if s.Mode == "ssh" {
		return s.initializeSSH()
	}

	return s.initializeLocal()
}

// initializeLocal sets up a local PTY session.
func (s *Session) initializeLocal() error {
	opts := localpty.DefaultOptions()
	localPTY, err := localpty.NewLocalPTY(opts)
	if err != nil {
		return fmt.Errorf("create local pty: %w", err)
	}

	s.pty = &localPTYAdapter{pty: localPTY}
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
	s.pty.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	s.pty.Read(buf) // Ignore output

	// Set simple prompt based on shell type
	s.pty.WriteString(s.shellPromptCommand())
	time.Sleep(100 * time.Millisecond)
	s.pty.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	s.pty.Read(buf) // Drain the output

	return nil
}

// shellPromptCommand returns the command to set a simple prompt for the current shell.
func (s *Session) shellPromptCommand() string {
	shellName := s.Shell
	if idx := strings.LastIndex(shellName, "/"); idx >= 0 {
		shellName = shellName[idx+1:]
	}

	switch shellName {
	case "zsh":
		// Zsh uses PROMPT; also disable hooks and special features
		return "PROMPT='$ '; RPROMPT=''; unset precmd_functions; unset preexec_functions; setopt NO_PROMPT_SUBST\n"
	case "fish":
		// Fish requires a function definition
		return "function fish_prompt; echo -n '$ '; end; set -U fish_greeting ''\n"
	default:
		// Bash and other POSIX shells
		return "PS1='$ '; PROMPT_COMMAND=''; set +H\n"
	}
}

// initializeSSH sets up an SSH PTY session.
func (s *Session) initializeSSH() error {
	if s.Host == "" {
		return fmt.Errorf("host is required for ssh mode")
	}
	if s.User == "" {
		return fmt.Errorf("user is required for ssh mode")
	}
	if s.Port == 0 {
		s.Port = 22
	}

	// Build auth methods
	authCfg := ssh.AuthConfig{
		UseAgent: true, // Try SSH agent first
		Password: s.Password,
	}

	// Check for key path in config
	if s.config != nil {
		for _, srv := range s.config.Servers {
			if srv.Host == s.Host || srv.Name == s.Host {
				if srv.KeyPath != "" {
					authCfg.KeyPath = srv.KeyPath
				}
				if srv.Auth.PassphraseEnv != "" {
					authCfg.KeyPassphrase = os.Getenv(srv.Auth.PassphraseEnv)
				}
				break
			}
		}
	}

	authMethods, err := ssh.BuildAuthMethods(authCfg)
	if err != nil {
		return fmt.Errorf("build auth methods: %w", err)
	}

	// Build host key callback
	hostKeyCallback, err := ssh.BuildHostKeyCallback("")
	if err != nil {
		// Fall back to insecure if known_hosts parsing fails
		hostKeyCallback = ssh.InsecureHostKeyCallback()
	}

	// Create SSH client
	clientOpts := ssh.ClientOptions{
		Host:            s.Host,
		Port:            s.Port,
		User:            s.User,
		AuthMethods:     authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         30 * time.Second,
	}

	client, err := ssh.NewClient(clientOpts)
	if err != nil {
		return fmt.Errorf("create ssh client: %w", err)
	}

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	s.sshClient = client

	// Create SSH PTY
	ptyOpts := ssh.DefaultSSHPTYOptions()
	sshPTY, err := ssh.NewSSHPTY(client, ptyOpts)
	if err != nil {
		client.Close()
		return fmt.Errorf("create ssh pty: %w", err)
	}

	s.pty = &sshPTYAdapter{pty: sshPTY}
	s.Shell = "/bin/bash" // Default assumption, will try to detect
	s.State = StateIdle
	s.CreatedAt = time.Now()
	s.LastUsed = time.Now()
	s.Cwd = "~" // Will be updated on first command

	// Wait for shell to be ready
	time.Sleep(500 * time.Millisecond)

	// Drain initial output
	buf := make([]byte, 8192)
	s.readWithTimeout(buf, 500*time.Millisecond)

	// Try to detect remote shell
	s.detectRemoteShell()

	// Set simple prompt based on detected shell
	s.pty.WriteString(s.shellPromptCommand())
	time.Sleep(200 * time.Millisecond)
	s.readWithTimeout(buf, 300*time.Millisecond)

	return nil
}

// detectRemoteShell attempts to detect the remote shell from $SHELL.
func (s *Session) detectRemoteShell() {
	s.pty.WriteString("echo $SHELL\n")
	time.Sleep(100 * time.Millisecond)

	buf := make([]byte, 1024)
	n, _ := s.readWithTimeout(buf, 200*time.Millisecond)
	if n > 0 {
		output := string(buf[:n])
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "/") && strings.Contains(line, "sh") {
				s.Shell = line
				return
			}
		}
	}
}

// readWithTimeout reads from PTY with a timeout.
func (s *Session) readWithTimeout(buf []byte, timeout time.Duration) (int, error) {
	s.pty.SetReadDeadline(time.Now().Add(timeout))
	return s.pty.Read(buf)
}

// reconnectSSH attempts to reconnect an SSH session with state restoration.
func (s *Session) reconnectSSH() error {
	// Save current state before reconnecting
	savedCwd := s.Cwd
	savedEnvVars := make(map[string]string)
	for k, v := range s.EnvVars {
		savedEnvVars[k] = v
	}

	// Close existing connections
	if s.pty != nil {
		s.pty.Close()
	}
	if s.sshClient != nil {
		s.sshClient.Close()
	}

	// Re-initialize SSH with exponential backoff
	var lastErr error
	delays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

	for i, delay := range delays {
		if err := s.initializeSSH(); err != nil {
			lastErr = err
			if i < len(delays)-1 {
				time.Sleep(delay)
			}
			continue
		}

		// Restore state after successful reconnect
		s.restoreState(savedCwd, savedEnvVars)
		return nil
	}

	return fmt.Errorf("reconnect failed after %d attempts: %w", len(delays), lastErr)
}

// restoreState restores cwd and environment variables after reconnect.
func (s *Session) restoreState(cwd string, envVars map[string]string) {
	if s.pty == nil {
		return
	}

	buf := make([]byte, 4096)

	// Restore working directory
	if cwd != "" && cwd != "~" {
		s.pty.WriteString(fmt.Sprintf("cd %q 2>/dev/null\n", cwd))
		time.Sleep(100 * time.Millisecond)
		s.readWithTimeout(buf, 200*time.Millisecond)
		s.Cwd = cwd
	}

	// Restore critical environment variables (skip internal ones)
	for key, value := range envVars {
		// Skip variables that are set by shell or system
		if key == "PWD" || key == "OLDPWD" || key == "SHLVL" || key == "_" ||
			key == "TERM" || key == "SHELL" || key == "HOME" || key == "USER" ||
			key == "LOGNAME" || key == "PATH" || key == "PS1" || key == "PROMPT_COMMAND" {
			continue
		}
		// Export the variable
		s.pty.WriteString(fmt.Sprintf("export %s=%q\n", key, value))
		time.Sleep(50 * time.Millisecond)
	}

	// Drain any output from the restore commands
	s.readWithTimeout(buf, 300*time.Millisecond)

	// Update stored env vars
	s.EnvVars = envVars
}

// Status returns the current session status.
func (s *Session) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	shellInfo := ShellInfo{
		Path: s.Shell,
		Type: "bash", // Default assumption
	}
	if idx := strings.LastIndex(s.Shell, "/"); idx >= 0 {
		shellName := s.Shell[idx+1:]
		shellInfo.Type = shellName
		shellInfo.SupportsHistory = shellName == "bash" || shellName == "zsh"
	}

	status := SessionStatus{
		ID:            s.ID,
		State:         s.State,
		Mode:          s.Mode,
		Shell:         s.Shell,
		ShellInfo:     &shellInfo,
		Cwd:           s.Cwd,
		IdleSeconds:   int(time.Since(s.LastUsed).Seconds()),
		UptimeSeconds: int(time.Since(s.CreatedAt).Seconds()),
		EnvVars:       s.EnvVars,
		Connected:     s.pty != nil && s.State != StateClosed,
	}

	if s.Mode == "ssh" {
		status.Host = s.Host
		status.User = s.User
		if s.sshClient != nil {
			status.Connected = s.sshClient.IsConnected()
		}
	}

	return status
}

// Exec executes a command in the session.
func (s *Session) Exec(command string, timeoutMs int) (*ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State == StateClosed {
		return nil, fmt.Errorf("session is closed")
	}

	if s.pty == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	// For SSH sessions, check if we need to reconnect
	if s.Mode == "ssh" && s.sshClient != nil && !s.sshClient.IsConnected() {
		if err := s.reconnectSSH(); err != nil {
			return nil, fmt.Errorf("reconnect failed: %w", err)
		}
	}

	s.State = StateRunning
	s.LastUsed = time.Now()
	s.outputBuffer.Reset()

	// Create command with end marker for completion detection
	fullCommand := fmt.Sprintf("%s; echo '%s'$?\n", command, endMarker)

	// Write command to PTY
	if _, err := s.pty.WriteString(fullCommand); err != nil {
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
		s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		n, err := s.pty.Read(buf)
		if err != nil {
			if os.IsTimeout(err) || err == io.EOF || isTimeoutError(err) {
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

// isTimeoutError checks if error is a timeout (works for both local and SSH).
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "i/o timeout")
}

// ProvideInput provides input to a session waiting for input.
func (s *Session) ProvideInput(input string) (*ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != StateAwaitingInput {
		return nil, fmt.Errorf("session is not awaiting input (state: %s)", s.State)
	}

	if s.pty == nil {
		return nil, fmt.Errorf("session not initialized")
	}

	s.State = StateRunning
	s.LastUsed = time.Now()

	// Write input followed by newline
	if _, err := s.pty.WriteString(input + "\n"); err != nil {
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

	if s.pty == nil {
		return fmt.Errorf("session not initialized")
	}

	if err := s.pty.Interrupt(); err != nil {
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

	var errs []error

	if s.pty != nil {
		if err := s.pty.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close pty: %w", err))
		}
	}

	if s.sshClient != nil {
		if err := s.sshClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close ssh: %w", err))
		}
	}

	s.State = StateClosed

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// extractExitCode extracts the exit code from output if the end marker is present.
func (s *Session) extractExitCode(output string) (int, bool) {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")

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
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "")

	lines := strings.Split(output, "\n")
	var cleaned []string

	seenCommand := false

	for _, line := range lines {
		if strings.HasPrefix(line, "$ ") {
			continue
		}

		if command != "" && !seenCommand && strings.Contains(line, command) {
			seenCommand = true
			continue
		}

		if strings.Contains(line, endMarker) {
			continue
		}

		cleaned = append(cleaned, line)
	}

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
	s.pty.WriteString("pwd\n")
	time.Sleep(50 * time.Millisecond)

	buf := make([]byte, 1024)
	s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	n, _ := s.pty.Read(buf)

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

// CaptureEnv captures current environment variables from the session.
func (s *Session) CaptureEnv() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty == nil || s.State == StateClosed {
		return s.EnvVars
	}

	// Send env command
	s.pty.WriteString("env\n")
	time.Sleep(100 * time.Millisecond)

	// Read output
	buf := make([]byte, 32768) // Env can be large
	s.pty.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, _ := s.pty.Read(buf)

	if n == 0 {
		return s.EnvVars
	}

	output := string(buf[:n])
	envMap := parseEnvOutput(output)

	// Update stored env vars
	if len(envMap) > 0 {
		s.EnvVars = envMap
	}

	return s.EnvVars
}

// parseEnvOutput parses the output of the 'env' command into a map.
func parseEnvOutput(output string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines, prompt lines, and the command itself
		if line == "" || line == "env" || strings.HasPrefix(line, "$ ") {
			continue
		}

		// Parse KEY=VALUE format
		idx := strings.Index(line, "=")
		if idx > 0 {
			key := line[:idx]
			value := line[idx+1:]
			// Skip internal variables
			if !strings.HasPrefix(key, "_") && key != "SHLVL" && key != "OLDPWD" {
				result[key] = value
			}
		}
	}

	return result
}

// GetShellInfo returns information about the shell being used.
func (s *Session) GetShellInfo() ShellInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	info := ShellInfo{
		Path: s.Shell,
	}

	// Detect shell type from path
	shellName := s.Shell
	if idx := strings.LastIndex(shellName, "/"); idx >= 0 {
		shellName = shellName[idx+1:]
	}

	switch shellName {
	case "bash":
		info.Type = "bash"
		info.SupportsHistory = true
	case "zsh":
		info.Type = "zsh"
		info.SupportsHistory = true
	case "sh", "dash", "ash":
		info.Type = "sh"
		info.SupportsHistory = false
	default:
		info.Type = "unknown"
	}

	return info
}

// ShellInfo contains information about the shell.
type ShellInfo struct {
	Type            string `json:"type"`
	Path            string `json:"path"`
	SupportsHistory bool   `json:"supports_history"`
}

// SessionStatus represents the status of a session.
type SessionStatus struct {
	ID             string            `json:"session_id"`
	State          State             `json:"state"`
	Mode           string            `json:"mode"`
	Shell          string            `json:"shell"`
	ShellInfo      *ShellInfo        `json:"shell_info,omitempty"`
	Cwd            string            `json:"cwd"`
	IdleSeconds    int               `json:"idle_seconds"`
	UptimeSeconds  int               `json:"uptime_seconds"`
	EnvVars        map[string]string `json:"env_vars,omitempty"`
	Host           string            `json:"host,omitempty"`
	User           string            `json:"user,omitempty"`
	Connected      bool              `json:"connected"`
	SudoCached     bool              `json:"sudo_cached,omitempty"`
	SudoExpiresIn  int               `json:"sudo_expires_in_seconds,omitempty"`
}

// ExecResult represents the result of command execution.
type ExecResult struct {
	Status               string            `json:"status"`
	ExitCode             *int              `json:"exit_code,omitempty"`
	Stdout               string            `json:"stdout,omitempty"`
	Stderr               string            `json:"stderr,omitempty"`
	Cwd                  string            `json:"cwd,omitempty"`
	EnvVars              map[string]string `json:"env_vars,omitempty"`
	PromptType           string            `json:"prompt_type,omitempty"`
	PromptText           string            `json:"prompt_text,omitempty"`
	ContextBuffer        string            `json:"context_buffer,omitempty"`
	MaskInput            bool              `json:"mask_input,omitempty"`
	Hint                 string            `json:"hint,omitempty"`
	SudoAuthenticated    bool              `json:"sudo_authenticated,omitempty"`
	SudoExpiresInSeconds int               `json:"sudo_expires_in_seconds,omitempty"`
}
