// Package session provides shell session management.
package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/prompt"
	localpty "github.com/acolita/claude-shell-mcp/internal/pty"
	"github.com/acolita/claude-shell-mcp/internal/sftp"
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

// Command markers for output isolation.
// Each command gets a unique ID to separate its output from async background data.
const (
	startMarkerPrefix = "___CMD_START_"
	endMarkerPrefix   = "___CMD_END_"
	markerSuffix      = "___"
)

// Legacy end marker for backward compatibility
const endMarker = "___CMD_END_MARKER___"

// Session represents a shell session.
type Session struct {
	ID        string
	State     State
	Mode      string // "local" or "ssh"
	Shell     string
	Cwd       string
	EnvVars   map[string]string
	Aliases   map[string]string // Shell aliases
	CreatedAt time.Time
	LastUsed  time.Time

	// SSH connection info (for ssh mode)
	Host     string
	Port     int
	User     string
	Password string // For password-based auth (not persisted)
	KeyPath  string // Path to SSH private key file

	// PTY info for control plane
	PTYName string // e.g., "3" for /dev/pts/3

	// Internal fields
	config         *config.Config
	mu             sync.Mutex
	pty            PTY // Common interface for local and SSH PTY
	sshClient      *ssh.Client
	promptDetector *prompt.Detector

	// Pending prompt info when awaiting input
	pendingPrompt *prompt.Detection
	outputBuffer  bytes.Buffer

	// Control session reference for process management
	controlSession *ControlSession
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

	// Apply shell config if available
	if s.config != nil {
		if s.config.Shell.Path != "" {
			opts.Shell = s.config.Shell.Path
		}
		opts.NoRC = !s.config.Shell.SourceRC
	}

	localPTY, err := localpty.NewLocalPTY(opts)
	if err != nil {
		return fmt.Errorf("create local pty: %w", err)
	}

	s.pty = &localPTYAdapter{pty: localPTY}
	s.Shell = localPTY.Shell()
	s.State = StateIdle
	s.CreatedAt = time.Now()
	s.LastUsed = time.Now()

	// Get PTY name for control plane (e.g., "3" from "/dev/pts/3")
	if f := localPTY.File(); f != nil {
		ptyPath := f.Name()
		if strings.HasPrefix(ptyPath, "/dev/pts/") {
			s.PTYName = strings.TrimPrefix(ptyPath, "/dev/pts/")
		}
	}

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
		// PS2/PROMPT2 is the continuation prompt for multi-line commands
		return "PROMPT='$ '; PROMPT2='> '; RPROMPT=''; unset precmd_functions; unset preexec_functions; setopt NO_PROMPT_SUBST\n"
	case "fish":
		// Fish requires a function definition
		return "function fish_prompt; echo -n '$ '; end; set -U fish_greeting ''\n"
	default:
		// Bash and other POSIX shells
		// PS2 is the continuation prompt for multi-line commands
		return "PS1='$ '; PS2='> '; PROMPT_COMMAND=''; set +H\n"
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
		KeyPath:  s.KeyPath, // Use key_path from session if provided
		Host:     s.Host,    // For SSH config lookup
	}

	// Check for key path in config (only if not already set)
	if authCfg.KeyPath == "" && s.config != nil {
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

	// Detect PTY name for control plane
	s.detectPTYName()

	// Set simple prompt based on detected shell
	s.pty.WriteString(s.shellPromptCommand())
	time.Sleep(200 * time.Millisecond)
	s.readWithTimeout(buf, 300*time.Millisecond)

	return nil
}

// detectPTYName detects the PTY device name for SSH sessions.
func (s *Session) detectPTYName() {
	s.pty.WriteString("tty 2>/dev/null\n")
	time.Sleep(100 * time.Millisecond)

	buf := make([]byte, 1024)
	n, _ := s.readWithTimeout(buf, 200*time.Millisecond)
	if n > 0 {
		output := string(buf[:n])
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "/dev/pts/") {
				s.PTYName = strings.TrimPrefix(line, "/dev/pts/")
				return
			}
		}
	}
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

// readWithTimeout reads from PTY with a timeout, draining all available data.
func (s *Session) readWithTimeout(buf []byte, timeout time.Duration) (int, error) {
	totalRead := 0
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		s.pty.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, err := s.pty.Read(buf[totalRead:])
		if n > 0 {
			totalRead += n
			if totalRead >= len(buf) {
				break
			}
		}
		if err != nil {
			break
		}
	}

	return totalRead, nil
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
		Aliases:       s.Aliases,
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

	// Use control plane to verify PTY is alive (if available)
	if s.controlSession != nil && s.PTYName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		alive, err := s.controlSession.IsPTYAlive(ctx, s.PTYName)
		cancel()
		if err == nil && !alive {
			slog.Warn("PTY has no processes, session is dead - attempting reconnect",
				slog.String("session_id", s.ID),
				slog.String("pty", s.PTYName),
			)
			if s.Mode == "ssh" {
				if err := s.reconnectSSH(); err != nil {
					return nil, fmt.Errorf("session dead and reconnect failed: %w", err)
				}
			} else {
				return nil, fmt.Errorf("local session is dead (PTY has no processes)")
			}
		}
	}

	s.State = StateRunning
	s.LastUsed = time.Now()
	s.outputBuffer.Reset()

	// Generate unique command ID for marker-based output isolation
	cmdID := generateCommandID()
	startMarker := startMarkerPrefix + cmdID + markerSuffix
	endMarker := endMarkerPrefix + cmdID + markerSuffix

	// Create command wrapped with start/end markers for output isolation
	// Format: echo 'START'; command; echo 'END'$?
	fullCommand := fmt.Sprintf("echo '%s'; %s; echo '%s'$?\n", startMarker, command, endMarker)

	// Write command to PTY
	if _, err := s.pty.WriteString(fullCommand); err != nil {
		// Check if this is a broken connection (EOF, closed pipe, etc.)
		if isConnectionBroken(err) && s.Mode == "ssh" {
			slog.Warn("SSH connection broken, attempting reconnect",
				slog.String("session_id", s.ID),
				slog.String("error", err.Error()),
			)
			if reconnErr := s.reconnectSSH(); reconnErr != nil {
				s.State = StateIdle
				return nil, fmt.Errorf("connection lost and reconnect failed: %w (original: %v)", reconnErr, err)
			}
			// Retry the write after reconnect
			if _, err := s.pty.WriteString(fullCommand); err != nil {
				s.State = StateIdle
				return nil, fmt.Errorf("write command after reconnect: %w", err)
			}
		} else {
			s.State = StateIdle
			return nil, fmt.Errorf("write command: %w", err)
		}
	}

	// For multi-line commands, give bash time to process all lines
	// before we start reading output. Each embedded newline causes bash
	// to show a continuation prompt (PS2) while processing.
	if strings.Contains(command, "\n") {
		lineCount := strings.Count(command, "\n")
		delay := time.Duration(lineCount*50) * time.Millisecond
		if delay > 500*time.Millisecond {
			delay = 500 * time.Millisecond
		}
		time.Sleep(delay)
	}

	// Read output with timeout
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.readOutputWithMarkers(ctx, command, cmdID)
}

// readOutput reads output from PTY until completion or prompt detection.
// Used by ProvideInput for continuing after user input.
func (s *Session) readOutput(ctx context.Context, command string) (*ExecResult, error) {
	buf := make([]byte, 4096)
	stallCount := 0
	const stallThreshold = 15 // 15 x 100ms = 1.5 seconds of no output

	for {
		select {
		case <-ctx.Done():
			// Aggressively kill the running command
			s.forceKillCommand()
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

				// Increment stall counter when no data received
				stallCount++

				// After stall threshold, check if there's meaningful output beyond command echo
				// This detects interactive apps like vim that produce output then wait
				if stallCount >= stallThreshold {
					cleanedStdout := s.cleanOutput(output, command)
					// Only trigger awaiting_input if there's actual output (not just command echo)
					if len(strings.TrimSpace(cleanedStdout)) > 0 {
						// Check if this looks like a shell continuation prompt (PS2)
						// This happens with multi-line commands - don't treat as interactive
						if isContinuationPrompt(output) && strings.Contains(command, "\n") {
							// Multi-line command still processing, wait longer
							stallCount = stallThreshold / 2 // Reset partially to give more time
							continue
						}

						// Check for interactive prompt patterns with ANSI stripped
						strippedOutput := stripANSI(output)
						if detection := s.promptDetector.Detect(strippedOutput); detection != nil {
							s.State = StateAwaitingInput
							s.pendingPrompt = detection
							return &ExecResult{
								Status:        "awaiting_input",
								Stdout:        cleanedStdout,
								PromptType:    string(detection.Pattern.Type),
								PromptText:    detection.MatchedText,
								ContextBuffer: detection.ContextBuffer,
								MaskInput:     detection.Pattern.MaskInput,
								Hint:          detection.Hint(),
							}, nil
						}

						// If output exists but no pattern matched, treat as stalled interactive
						s.State = StateAwaitingInput
						return &ExecResult{
							Status:        "awaiting_input",
							Stdout:        cleanedStdout,
							PromptType:    "interactive",
							PromptText:    "",
							ContextBuffer: output,
							Hint:          "Command appears to be waiting for input. Send input or interrupt with shell_interrupt.",
						}, nil
					}
				}

				continue
			}
			s.State = StateIdle
			return nil, fmt.Errorf("read output: %w", err)
		}

		if n > 0 {
			// Reset stall counter when we receive data
			stallCount = 0
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

// readOutputWithMarkers reads output using command markers for isolation.
// Output before the start marker is captured as async_output (background noise).
// Output between start and end markers is the actual command output.
func (s *Session) readOutputWithMarkers(ctx context.Context, command string, cmdID string) (*ExecResult, error) {
	buf := make([]byte, 4096)
	stallCount := 0
	const stallThreshold = 15 // 15 x 100ms = 1.5 seconds of no output

	startMarker := startMarkerPrefix + cmdID + markerSuffix
	endMarker := endMarkerPrefix + cmdID + markerSuffix

	for {
		select {
		case <-ctx.Done():
			// Aggressively kill the running command
			s.forceKillCommand()
			s.State = StateIdle
			asyncOutput, stdout := s.parseMarkedOutput(s.outputBuffer.String(), startMarker, endMarker, command)
			return &ExecResult{
				Status:      "timeout",
				Stdout:      stdout,
				AsyncOutput: asyncOutput,
				CommandID:   cmdID,
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
				if exitCode, found := s.extractExitCodeWithMarker(output, endMarker); found {
					s.State = StateIdle
					s.updateCwd()
					asyncOutput, stdout := s.parseMarkedOutput(output, startMarker, endMarker, command)
					return &ExecResult{
						Status:      "completed",
						ExitCode:    &exitCode,
						Stdout:      stdout,
						AsyncOutput: asyncOutput,
						CommandID:   cmdID,
						Cwd:         s.Cwd,
					}, nil
				}

				// Increment stall counter when no data received
				stallCount++

				// After stall threshold, check if there's meaningful output
				if stallCount >= stallThreshold {
					asyncOutput, stdout := s.parseMarkedOutput(output, startMarker, endMarker, command)
					// Only trigger awaiting_input if there's actual output
					if len(strings.TrimSpace(stdout)) > 0 || len(strings.TrimSpace(asyncOutput)) > 0 {
						// Check if this looks like a shell continuation prompt (PS2)
						if isContinuationPrompt(output) && strings.Contains(command, "\n") {
							stallCount = stallThreshold / 2
							continue
						}

						// Check for interactive prompt patterns
						strippedOutput := stripANSI(output)
						if detection := s.promptDetector.Detect(strippedOutput); detection != nil {
							s.State = StateAwaitingInput
							s.pendingPrompt = detection
							return &ExecResult{
								Status:        "awaiting_input",
								Stdout:        stdout,
								AsyncOutput:   asyncOutput,
								CommandID:     cmdID,
								PromptType:    string(detection.Pattern.Type),
								PromptText:    detection.MatchedText,
								ContextBuffer: detection.ContextBuffer,
								MaskInput:     detection.Pattern.MaskInput,
								Hint:          detection.Hint(),
							}, nil
						}

						// If output exists but no pattern matched, treat as stalled interactive
						s.State = StateAwaitingInput
						return &ExecResult{
							Status:        "awaiting_input",
							Stdout:        stdout,
							AsyncOutput:   asyncOutput,
							CommandID:     cmdID,
							PromptType:    "interactive",
							PromptText:    "",
							ContextBuffer: output,
							Hint:          "Command appears to be waiting for input. Send input or interrupt with shell_interrupt.",
						}, nil
					}
				}

				continue
			}
			s.State = StateIdle
			return nil, fmt.Errorf("read output: %w", err)
		}

		if n > 0 {
			// Reset stall counter when we receive data
			stallCount = 0
			s.outputBuffer.Write(buf[:n])
			output := s.outputBuffer.String()

			// Check for command completion (end marker present)
			if exitCode, found := s.extractExitCodeWithMarker(output, endMarker); found {
				s.State = StateIdle
				s.updateCwd()
				asyncOutput, stdout := s.parseMarkedOutput(output, startMarker, endMarker, command)
				return &ExecResult{
					Status:      "completed",
					ExitCode:    &exitCode,
					Stdout:      stdout,
					AsyncOutput: asyncOutput,
					CommandID:   cmdID,
					Cwd:         s.Cwd,
				}, nil
			}

			// Check for interactive prompt
			if detection := s.promptDetector.Detect(output); detection != nil {
				s.State = StateAwaitingInput
				s.pendingPrompt = detection
				asyncOutput, stdout := s.parseMarkedOutput(output, startMarker, endMarker, command)
				return &ExecResult{
					Status:        "awaiting_input",
					Stdout:        stdout,
					AsyncOutput:   asyncOutput,
					CommandID:     cmdID,
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

// parseMarkedOutput separates async output from command output using markers.
// Returns (asyncOutput, commandOutput).
func (s *Session) parseMarkedOutput(output, startMarker, endMarker, command string) (string, string) {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "")

	var asyncOutput, cmdOutput string

	// Find start marker on its own line (not within the echoed command)
	// The marker appears as: \n___CMD_START_xxx___\n (output from echo command)
	// NOT within: echo '___CMD_START_xxx___'; ... (the command being echoed)
	startIdx := findMarkerOnOwnLine(output, startMarker)
	if startIdx == -1 {
		// No start marker yet - all output is async/pre-command
		return s.cleanAsyncOutput(output), ""
	}

	// Everything before start marker line is async output
	if startIdx > 0 {
		asyncOutput = s.cleanAsyncOutput(output[:startIdx])
	}

	// Find end marker on its own line
	afterStart := output[startIdx+len(startMarker):]
	// Skip the newline after start marker
	if len(afterStart) > 0 && afterStart[0] == '\n' {
		afterStart = afterStart[1:]
	}

	endIdx := findMarkerOnOwnLine(afterStart, endMarker)
	if endIdx == -1 {
		// No end marker yet - everything after start is command output (in progress)
		cmdOutput = strings.TrimSpace(afterStart)
	} else {
		// Extract output between markers
		cmdOutput = strings.TrimSpace(afterStart[:endIdx])
	}

	return asyncOutput, cmdOutput
}

// findMarkerOnOwnLine finds a marker that appears at the start of a line.
// Returns the index of the marker, or -1 if not found on its own line.
func findMarkerOnOwnLine(output, marker string) int {
	// Check if output starts with marker
	if strings.HasPrefix(output, marker) {
		return 0
	}

	// Look for marker after a newline (on its own line)
	search := "\n" + marker
	idx := strings.Index(output, search)
	if idx != -1 {
		return idx + 1 // Return position of marker, not the newline
	}

	return -1
}

// cleanAsyncOutput cleans up async output (removes shell prompts, trims whitespace).
func (s *Session) cleanAsyncOutput(output string) string {
	lines := strings.Split(output, "\n")
	var cleaned []string

	for _, line := range lines {
		// Skip shell prompt lines
		if strings.HasPrefix(line, "$ ") {
			continue
		}
		// Skip empty lines at start/end
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleaned = append(cleaned, line)
		}
	}

	result := strings.Join(cleaned, "\n")
	return strings.TrimSpace(result)
}

// cleanCommandOutput cleans the command output between markers.
// With proper marker parsing, this is now simpler - just trim and remove end marker artifacts.
func (s *Session) cleanCommandOutput(output, command, startMarker, endMarker string) string {
	// Output between markers should already be clean, just trim
	output = strings.TrimSpace(output)

	// Remove any trailing end marker with exit code (e.g., "___CMD_END_xxx___0")
	lines := strings.Split(output, "\n")
	var cleaned []string

	for _, line := range lines {
		// Skip lines that are just the end marker with exit code
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, endMarkerPrefix) {
			continue
		}
		cleaned = append(cleaned, line)
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// extractExitCodeWithMarker extracts exit code from the specific end marker.
func (s *Session) extractExitCodeWithMarker(output, endMarker string) (int, bool) {
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

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	// Match ANSI escape sequences: ESC[ followed by parameters and a letter
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-Za-z]`)
	return ansiRegex.ReplaceAllString(s, "")
}

// isContinuationPrompt checks if output ends with a shell continuation prompt (PS2).
// This is typically "> " shown when a command spans multiple lines.
func isContinuationPrompt(output string) bool {
	output = strings.TrimRight(output, "\r\n")
	// Common PS2 patterns: "> " or just ">"
	return strings.HasSuffix(output, "> ") || strings.HasSuffix(output, ">")
}

// drainOutput drains any remaining output from the PTY after an interrupt or timeout.
func (s *Session) drainOutput() {
	buf := make([]byte, 4096)
	// Read with short deadline until we get no more data
	for i := 0; i < 10; i++ { // Max 10 attempts (1 second total)
		s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := s.pty.Read(buf)
		if err != nil || n == 0 {
			break
		}
	}
}

// generateCommandID generates a unique 8-character hex ID for command markers.
func generateCommandID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(b)
}

// forceKillCommand terminates any running command.
// Uses control plane (pkill) if available, falls back to Ctrl+C and 'q'.
func (s *Session) forceKillCommand() {
	// Strategy 1: Use control plane to kill all processes on this PTY
	if s.controlSession != nil && s.PTYName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Kill all processes on the PTY
		if err := s.controlSession.KillPTY(ctx, s.PTYName); err == nil {
			// Give processes time to die
			time.Sleep(100 * time.Millisecond)
			s.drainOutput()

			// Send newline to get fresh prompt
			s.pty.WriteString("\n")
			time.Sleep(100 * time.Millisecond)
			s.drainOutput()
			return
		}
		// If control plane fails, fall through to manual methods
	}

	// Fallback: Manual interrupt methods
	s.forceKillCommandFallback()
}

// forceKillCommandFallback uses Ctrl+C and 'q' to terminate commands.
// Used when control plane is not available.
func (s *Session) forceKillCommandFallback() {
	buf := make([]byte, 4096)

	// Send Ctrl+C multiple times with delays
	for i := 0; i < 3; i++ {
		s.pty.Interrupt()
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)

	// Drain output after interrupts
	s.drainOutput()

	// Check if still producing output
	s.pty.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	n, _ := s.pty.Read(buf)

	if n > 0 {
		// Send 'q' for apps like top, less that use q to quit
		s.pty.WriteString("q")
		time.Sleep(100 * time.Millisecond)
		s.drainOutput()

		// Check again
		s.pty.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		n, _ = s.pty.Read(buf)
	}

	if n > 0 {
		// Final attempt: more Ctrl+C
		for i := 0; i < 3; i++ {
			s.pty.Interrupt()
			time.Sleep(30 * time.Millisecond)
		}
		s.drainOutput()
	}

	// Send newline to get fresh prompt
	s.pty.WriteString("\n")
	time.Sleep(100 * time.Millisecond)
	s.drainOutput()
}

// isTimeoutError checks if error is a timeout (works for both local and SSH).
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "i/o timeout")
}

// isConnectionBroken checks if an error indicates the connection/PTY is dead.
func isConnectionBroken(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	errStr := err.Error()
	return strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "use of closed") ||
		strings.Contains(errStr, "closed network connection") ||
		strings.Contains(errStr, "channel closed")
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

	// For password prompts, wait until the program has disabled echo on the terminal
	// (sudo disables echo AFTER printing the prompt - we must wait for it)
	isPasswordPrompt := s.pendingPrompt != nil && s.pendingPrompt.Pattern.MaskInput
	if isPasswordPrompt {
		slog.Debug("waiting for echo disabled before password input")
		s.waitForEchoDisabled()
	}

	// Write input followed by newline
	lineEnding := "\n"
	toWrite := input + lineEnding

	slog.Debug("writing input to PTY",
		"len", len(toWrite),
		"isPassword", isPasswordPrompt,
		"bytes", fmt.Sprintf("%v", []byte(toWrite)))

	n, err := s.pty.WriteString(toWrite)
	if err != nil {
		slog.Error("failed to write input", "error", err)
		// Check if connection is broken and attempt reconnect for SSH
		if isConnectionBroken(err) && s.Mode == "ssh" {
			slog.Warn("SSH connection broken during input, attempting reconnect",
				slog.String("session_id", s.ID),
			)
			s.State = StateIdle // Reset state before reconnect
			if reconnErr := s.reconnectSSH(); reconnErr != nil {
				return nil, fmt.Errorf("connection lost and reconnect failed: %w (original: %v)", reconnErr, err)
			}
			return nil, fmt.Errorf("connection was lost (reconnected - please retry command)")
		}
		s.State = StateAwaitingInput
		return nil, fmt.Errorf("write input: %w", err)
	}
	slog.Debug("wrote input to PTY", "bytesWritten", n)

	// Clear output buffer to avoid re-detecting the old prompt
	s.outputBuffer.Reset()
	s.pendingPrompt = nil

	// Continue reading output
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.readOutput(ctx, "")
}

// waitForEchoDisabled waits for the terminal to be ready for password input.
// Programs like sudo print the prompt THEN disable echo. Per pexpect's approach,
// a small fixed delay (50-100ms) is sufficient and simpler than polling stty.
// See: https://pexpect.readthedocs.io/en/stable/commonissues.html
func (s *Session) waitForEchoDisabled() {
	// Pexpect uses 50ms delay by default, we use 100ms to be safe
	time.Sleep(100 * time.Millisecond)
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
			// Ignore EOF/broken connection errors - connection is already dead
			if !isConnectionBroken(err) {
				errs = append(errs, fmt.Errorf("close pty: %w", err))
			}
		}
	}

	if s.sshClient != nil {
		if err := s.sshClient.Close(); err != nil {
			// Ignore EOF/broken connection errors - connection is already dead
			if !isConnectionBroken(err) {
				errs = append(errs, fmt.Errorf("close ssh: %w", err))
			}
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

// CaptureAliases captures current shell aliases from the session.
func (s *Session) CaptureAliases() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty == nil || s.State == StateClosed {
		return s.Aliases
	}

	// Send alias command
	s.pty.WriteString("alias\n")
	time.Sleep(100 * time.Millisecond)

	// Read output
	buf := make([]byte, 16384)
	s.pty.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, _ := s.pty.Read(buf)

	if n == 0 {
		return s.Aliases
	}

	output := string(buf[:n])
	aliasMap := parseAliasOutput(output)

	// Update stored aliases
	if len(aliasMap) > 0 {
		s.Aliases = aliasMap
	}

	return s.Aliases
}

// parseAliasOutput parses the output of the 'alias' command into a map.
func parseAliasOutput(output string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines, prompt lines, and the command itself
		if line == "" || line == "alias" || strings.HasPrefix(line, "$ ") {
			continue
		}

		// Parse alias formats:
		// bash: alias name='value'
		// zsh:  name='value' or name=value
		line = strings.TrimPrefix(line, "alias ")

		idx := strings.Index(line, "=")
		if idx > 0 {
			name := line[:idx]
			value := line[idx+1:]
			// Remove surrounding quotes if present
			value = strings.Trim(value, "'\"")
			result[name] = value
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
	Aliases        map[string]string `json:"aliases,omitempty"`
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
	// Output truncation info (when tail_lines or head_lines is used)
	Truncated  bool `json:"truncated,omitempty"`
	TotalLines int  `json:"total_lines,omitempty"`
	ShownLines int  `json:"shown_lines,omitempty"`
	// Async output from background processes (not from this command)
	AsyncOutput string `json:"async_output,omitempty"`
	// Command ID used for marker-based output isolation
	CommandID string `json:"command_id,omitempty"`
}

// SFTPClient returns an SFTP client for file transfer operations.
// For SSH sessions, this returns an SFTP client that uses the existing SSH connection.
// For local sessions, this returns an error (use direct file operations instead).
func (s *Session) SFTPClient() (*sftp.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Mode != "ssh" {
		return nil, fmt.Errorf("SFTP not available for local sessions (use direct file operations)")
	}

	if s.sshClient == nil {
		return nil, fmt.Errorf("SSH client not initialized")
	}

	return s.sshClient.SFTPClient()
}

// IsSSH returns true if this is an SSH session.
func (s *Session) IsSSH() bool {
	return s.Mode == "ssh"
}

// ResolvePath resolves a relative path using the session's current working directory.
func (s *Session) ResolvePath(path string) string {
	if path == "" {
		return s.Cwd
	}
	// If path is absolute, return as-is
	if len(path) > 0 && path[0] == '/' {
		return path
	}
	// If path starts with ~, it's relative to home (leave as-is for remote handling)
	if len(path) > 0 && path[0] == '~' {
		return path
	}
	// Relative path - prepend cwd
	if s.Cwd == "" || s.Cwd == "~" {
		return path
	}
	return s.Cwd + "/" + path
}
