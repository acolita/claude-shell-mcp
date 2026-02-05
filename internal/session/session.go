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
	gossh "golang.org/x/crypto/ssh"
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

	// Saved tunnel configs from before MCP restart (for user to restore)
	SavedTunnels []TunnelConfig

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
		if strings.HasPrefix(ptyPath, devPtsPrefix) {
			s.PTYName = strings.TrimPrefix(ptyPath, devPtsPrefix)
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
	if err := s.validateSSHConfig(); err != nil {
		return err
	}

	authCfg := s.buildSSHAuthConfig()
	authMethods, err := ssh.BuildAuthMethods(authCfg)
	if err != nil {
		return fmt.Errorf("build auth methods: %w", err)
	}

	client, err := s.createSSHClient(authMethods)
	if err != nil {
		return err
	}

	if err := s.setupSSHPTY(client); err != nil {
		client.Close()
		return err
	}

	s.initializeSSHShell()
	return nil
}

// validateSSHConfig validates SSH configuration.
func (s *Session) validateSSHConfig() error {
	if s.Host == "" {
		return fmt.Errorf("host is required for ssh mode")
	}
	if s.User == "" {
		return fmt.Errorf("user is required for ssh mode")
	}
	if s.Port == 0 {
		s.Port = 22
	}
	return nil
}

// buildSSHAuthConfig constructs authentication configuration.
func (s *Session) buildSSHAuthConfig() ssh.AuthConfig {
	authCfg := ssh.AuthConfig{
		UseAgent: true,
		Password: s.Password,
		KeyPath:  s.KeyPath,
		Host:     s.Host,
	}

	if authCfg.KeyPath == "" && s.config != nil {
		s.applyServerAuthConfig(&authCfg)
	}
	return authCfg
}

// applyServerAuthConfig applies authentication settings from server config.
func (s *Session) applyServerAuthConfig(authCfg *ssh.AuthConfig) {
	for _, srv := range s.config.Servers {
		if srv.Host != s.Host && srv.Name != s.Host {
			continue
		}
		if srv.KeyPath != "" {
			authCfg.KeyPath = srv.KeyPath
		}
		if srv.Auth.PassphraseEnv != "" {
			authCfg.KeyPassphrase = os.Getenv(srv.Auth.PassphraseEnv)
		}
		break
	}
}

// createSSHClient creates and connects an SSH client.
func (s *Session) createSSHClient(authMethods []gossh.AuthMethod) (*ssh.Client, error) {
	hostKeyCallback, err := ssh.BuildHostKeyCallback("")
	if err != nil {
		hostKeyCallback = ssh.InsecureHostKeyCallback()
	}

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
		return nil, fmt.Errorf("create ssh client: %w", err)
	}

	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	s.sshClient = client
	return client, nil
}

// setupSSHPTY creates and configures the SSH PTY.
func (s *Session) setupSSHPTY(client *ssh.Client) error {
	ptyOpts := ssh.DefaultSSHPTYOptions()
	sshPTY, err := ssh.NewSSHPTY(client, ptyOpts)
	if err != nil {
		return fmt.Errorf("create ssh pty: %w", err)
	}

	s.pty = &sshPTYAdapter{pty: sshPTY}
	s.Shell = "/bin/bash"
	s.State = StateIdle
	s.CreatedAt = time.Now()
	s.LastUsed = time.Now()
	s.Cwd = "~"
	return nil
}

// initializeSSHShell initializes the shell environment.
func (s *Session) initializeSSHShell() {
	time.Sleep(500 * time.Millisecond)
	buf := make([]byte, 8192)
	s.readWithTimeout(buf, 500*time.Millisecond)

	s.detectRemoteShell()
	s.captureEnvAndPTY()

	s.pty.WriteString(s.shellPromptCommand())
	time.Sleep(200 * time.Millisecond)
	s.readWithTimeout(buf, 300*time.Millisecond)
}

// extractPTYNumber extracts the PTY number from an SSH_TTY path like "/dev/pts/5".
func extractPTYNumber(sshTTY string) string {
	sshTTY = strings.TrimSpace(strings.ReplaceAll(sshTTY, "\r", ""))
	if !strings.HasPrefix(sshTTY, devPtsPrefix) {
		return ""
	}

	ptyNum := strings.TrimPrefix(sshTTY, devPtsPrefix)
	var cleanNum strings.Builder
	for _, c := range ptyNum {
		if c >= '0' && c <= '9' {
			cleanNum.WriteRune(c)
		} else {
			break
		}
	}
	return cleanNum.String()
}

// captureEnvAndPTY captures environment variables and extracts PTY name from SSH_TTY.
func (s *Session) captureEnvAndPTY() {
	s.pty.WriteString("env\n")
	time.Sleep(100 * time.Millisecond)

	buf := make([]byte, 32768)
	n, _ := s.readWithTimeout(buf, 500*time.Millisecond)
	if n == 0 {
		slog.Debug("failed to capture environment")
		return
	}

	envMap := parseEnvOutput(string(buf[:n]))
	if len(envMap) > 0 {
		s.EnvVars = envMap
	}

	sshTTY, ok := s.EnvVars["SSH_TTY"]
	if !ok {
		slog.Debug("SSH_TTY not found in environment")
		return
	}

	if ptyNum := extractPTYNumber(sshTTY); ptyNum != "" {
		s.PTYName = ptyNum
		slog.Debug("detected PTY name from SSH_TTY", slog.String("pty", s.PTYName))
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

	// Control plane info for debugging
	status.PTYName = s.PTYName
	status.HasControlSession = s.controlSession != nil

	// Include saved tunnels if any (from before MCP restart)
	if len(s.SavedTunnels) > 0 {
		status.SavedTunnels = s.SavedTunnels
	}

	return status
}

// ControlExec executes a command via the control session (for debugging).
// This runs the command on a separate PTY, not the main session PTY.
func (s *Session) ControlExec(ctx context.Context, command string) (string, error) {
	s.mu.Lock()
	cs := s.controlSession
	s.mu.Unlock()

	if cs == nil {
		return "", fmt.Errorf("control session not available")
	}

	return cs.Exec(ctx, command)
}

// ControlExecRaw executes a command via the control session and returns raw output.
func (s *Session) ControlExecRaw(ctx context.Context, command string) (string, error) {
	s.mu.Lock()
	cs := s.controlSession
	s.mu.Unlock()

	if cs == nil {
		return "", fmt.Errorf("control session not available")
	}

	return cs.ExecRaw(ctx, command)
}

// Exec executes a command in the session.
func (s *Session) Exec(command string, timeoutMs int) (*ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateExecPreconditions(); err != nil {
		return nil, err
	}

	if err := s.ensureConnectionHealthy(); err != nil {
		return nil, err
	}

	s.State = StateRunning
	s.LastUsed = time.Now()
	s.outputBuffer.Reset()

	cmdID := generateCommandID()
	fullCommand := s.buildWrappedCommand(command, cmdID)

	if err := s.writeCommandWithReconnect(fullCommand); err != nil {
		return nil, err
	}

	applyMultilineDelay(command)

	timeout := s.getTimeout(timeoutMs)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.readOutputWithMarkers(ctx, command, cmdID)
}

// validateExecPreconditions checks if session is ready for command execution.
func (s *Session) validateExecPreconditions() error {
	if s.State == StateClosed {
		return fmt.Errorf("session is closed")
	}
	if s.pty == nil {
		return fmt.Errorf(errSessionNotInitialized)
	}
	return nil
}

// ensureConnectionHealthy checks and restores connection if needed.
func (s *Session) ensureConnectionHealthy() error {
	if err := s.checkSSHConnection(); err != nil {
		return err
	}
	return s.checkPTYAlive()
}

// checkSSHConnection reconnects SSH if disconnected.
func (s *Session) checkSSHConnection() error {
	if s.Mode != "ssh" || s.sshClient == nil || s.sshClient.IsConnected() {
		return nil
	}
	if err := s.reconnectSSH(); err != nil {
		return fmt.Errorf("reconnect failed: %w", err)
	}
	return nil
}

// checkPTYAlive uses control plane to verify PTY health.
func (s *Session) checkPTYAlive() error {
	if s.controlSession == nil || s.PTYName == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	alive, err := s.controlSession.IsPTYAlive(ctx, s.PTYName)
	cancel()

	if err != nil || alive {
		return nil
	}

	slog.Warn("PTY has no processes, session is dead - attempting reconnect",
		slog.String("session_id", s.ID),
		slog.String("pty", s.PTYName),
	)

	if s.Mode == "ssh" {
		if err := s.reconnectSSH(); err != nil {
			return fmt.Errorf("session dead and reconnect failed: %w", err)
		}
		return nil
	}
	return fmt.Errorf("local session is dead (PTY has no processes)")
}

// buildWrappedCommand creates the full command with markers.
func (s *Session) buildWrappedCommand(command, cmdID string) string {
	startMarker := startMarkerPrefix + cmdID + markerSuffix
	endMarker := endMarkerPrefix + cmdID + markerSuffix
	escapedCommand := strings.ReplaceAll(command, "'", "'\\''")
	return fmt.Sprintf("echo '%s'; bash -c 'trap \"\" SIGTTOU; %s'; echo '%s'$?\n", startMarker, escapedCommand, endMarker)
}

// writeCommandWithReconnect writes command to PTY, reconnecting if needed.
func (s *Session) writeCommandWithReconnect(fullCommand string) error {
	_, err := s.pty.WriteString(fullCommand)
	if err == nil {
		return nil
	}

	if !isConnectionBroken(err) || s.Mode != "ssh" {
		s.State = StateIdle
		return fmt.Errorf("write command: %w", err)
	}

	slog.Warn("SSH connection broken, attempting reconnect",
		slog.String("session_id", s.ID),
		slog.String("error", err.Error()),
	)

	if reconnErr := s.reconnectSSH(); reconnErr != nil {
		s.State = StateIdle
		return fmt.Errorf(errConnectionLostFmt, reconnErr, err)
	}

	if _, err := s.pty.WriteString(fullCommand); err != nil {
		s.State = StateIdle
		return fmt.Errorf("write command after reconnect: %w", err)
	}
	return nil
}

// applyMultilineDelay adds delay for multi-line commands.
func applyMultilineDelay(command string) {
	if !strings.Contains(command, "\n") {
		return
	}
	lineCount := strings.Count(command, "\n")
	delay := time.Duration(lineCount*50) * time.Millisecond
	if delay > 500*time.Millisecond {
		delay = 500 * time.Millisecond
	}
	time.Sleep(delay)
}

// getTimeout returns the command timeout duration.
func (s *Session) getTimeout(timeoutMs int) time.Duration {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return timeout
}

// processLegacyRead performs a single read iteration for legacy output reading.
// Returns (result, newStallCount, error) where error is fatal.
func (s *Session) processLegacyRead(ctx context.Context, buf []byte, command string, stallCount, stallThreshold int) (*ExecResult, int, error) {
	if result := s.handleLegacyContextTimeout(ctx, command); result != nil {
		return result, stallCount, nil
	}

	s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

	n, err := s.pty.Read(buf)
	if err != nil {
		result, newStall, cont := s.handleLegacyReadError(err, command, stallCount, stallThreshold)
		if result != nil {
			return result, newStall, nil
		}
		if cont {
			return nil, newStall, nil
		}
		s.State = StateIdle
		return nil, newStall, fmt.Errorf("read output: %w", err)
	}

	if n > 0 {
		s.outputBuffer.Write(buf[:n])
		if result := s.checkLegacyOutputForResult(command); result != nil {
			return result, 0, nil
		}
		return nil, 0, nil
	}
	return nil, stallCount, nil
}

// readOutput reads output from PTY until completion or prompt detection.
// Used by ProvideInput for continuing after user input.
func (s *Session) readOutput(ctx context.Context, command string) (*ExecResult, error) {
	buf := make([]byte, 4096)
	stallCount := 0
	const stallThreshold = 15

	for {
		result, newStall, err := s.processLegacyRead(ctx, buf, command, stallCount, stallThreshold)
		stallCount = newStall
		if result != nil {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// handleLegacyContextTimeout handles timeout for legacy output reading.
func (s *Session) handleLegacyContextTimeout(ctx context.Context, command string) *ExecResult {
	select {
	case <-ctx.Done():
		s.forceKillCommand()
		s.State = StateIdle
		return &ExecResult{
			Status: "timeout",
			Stdout: s.cleanOutput(s.outputBuffer.String(), command),
		}
	default:
		return nil
	}
}

// handleLegacyReadError processes read errors for legacy output.
func (s *Session) handleLegacyReadError(err error, command string, stallCount, stallThreshold int) (*ExecResult, int, bool) {
	if !os.IsTimeout(err) && err != io.EOF && !isTimeoutError(err) {
		return nil, stallCount, false
	}

	output := s.outputBuffer.String()
	if result := s.checkLegacyCompletion(output, command); result != nil {
		return result, stallCount, false
	}

	stallCount++

	if stallCount >= stallThreshold {
		if result := s.checkLegacyStallSignals(output, command); result != nil {
			return result, stallCount, false
		}
		stallCount = stallThreshold / 2
	}

	return nil, stallCount, true
}

// checkLegacyCompletion checks for command completion using legacy markers.
func (s *Session) checkLegacyCompletion(output, command string) *ExecResult {
	exitCode, found := s.extractExitCode(output)
	if !found {
		return nil
	}
	s.State = StateIdle
	s.updateCwd()
	return &ExecResult{
		Status:   "completed",
		ExitCode: &exitCode,
		Stdout:   s.cleanOutput(output, command),
		Cwd:      s.Cwd,
	}
}

// checkLegacyStallSignals checks for input signals after stall threshold.
func (s *Session) checkLegacyStallSignals(output, command string) *ExecResult {
	cleanedStdout := s.cleanOutput(output, command)
	strippedOutput := stripANSI(output)

	// Check peak-tty signal
	if containsPeakTTYSignal(output) {
		slog.Debug("peak-tty signal detected (13 NUL bytes)")
		s.State = StateAwaitingInput
		return &ExecResult{
			Status:        "awaiting_input",
			Stdout:        strings.ReplaceAll(cleanedStdout, "\x00", ""),
			PromptType:    "interactive",
			PromptText:    "",
			ContextBuffer: stripANSI(strings.ReplaceAll(output, "\x00", "")),
			Hint:          hintPeakTTYWaiting,
		}
	}

	// Check password prompt
	detection := s.promptDetector.Detect(strippedOutput)
	if detection != nil && detection.Pattern.Type == "password" {
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
		}
	}

	return nil
}

// checkLegacyOutputForResult checks output for completion or prompts.
func (s *Session) checkLegacyOutputForResult(command string) *ExecResult {
	output := s.outputBuffer.String()

	if result := s.checkLegacyCompletion(output, command); result != nil {
		return result
	}

	detection := s.promptDetector.Detect(output)
	if detection == nil {
		return nil
	}

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
	}
}

// processMarkedRead performs a single read iteration for marker-based output reading.
// Returns (result, newStallCount, error) where error is fatal.
func (s *Session) processMarkedRead(ctx context.Context, buf []byte, execCtx *execContext, stallCount, stallThreshold int) (*ExecResult, int, error) {
	if result := s.handleContextTimeout(ctx, execCtx); result != nil {
		return result, stallCount, nil
	}

	s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

	n, err := s.pty.Read(buf)
	if err != nil {
		result, newStall, cont := s.handleReadError(err, execCtx, stallCount, stallThreshold)
		if result != nil {
			return result, newStall, nil
		}
		if cont {
			return nil, newStall, nil
		}
		s.State = StateIdle
		return nil, newStall, fmt.Errorf("read output: %w", err)
	}

	if n > 0 {
		s.outputBuffer.Write(buf[:n])
		if result := s.checkOutputForResult(execCtx); result != nil {
			return result, 0, nil
		}
		return nil, 0, nil
	}
	return nil, stallCount, nil
}

// readOutputWithMarkers reads output using command markers for isolation.
// Output before the start marker is captured as async_output (background noise).
// Output between start and end markers is the actual command output.
func (s *Session) readOutputWithMarkers(ctx context.Context, command string, cmdID string) (*ExecResult, error) {
	execCtx := newExecContext(cmdID, startMarkerPrefix+cmdID+markerSuffix, endMarkerPrefix+cmdID+markerSuffix, command)
	buf := make([]byte, 4096)
	stallCount := 0
	const stallThreshold = 15

	for {
		result, newStall, err := s.processMarkedRead(ctx, buf, execCtx, stallCount, stallThreshold)
		stallCount = newStall
		if result != nil {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// handleContextTimeout checks for context cancellation and returns timeout result.
func (s *Session) handleContextTimeout(ctx context.Context, execCtx *execContext) *ExecResult {
	select {
	case <-ctx.Done():
		s.forceKillCommand()
		s.State = StateIdle
		return s.buildTimeoutResult(execCtx)
	default:
		return nil
	}
}

// handleReadError processes read errors and returns result if command completed.
// Returns: (result, newStallCount, shouldContinue)
func (s *Session) handleReadError(err error, execCtx *execContext, stallCount, stallThreshold int) (*ExecResult, int, bool) {
	if !os.IsTimeout(err) && err != io.EOF && !isTimeoutError(err) {
		return nil, stallCount, false
	}

	// Check for completion
	if result, found := s.checkForCompletion(execCtx); found {
		return result, stallCount, false
	}

	stallCount++

	// After stall threshold, check for prompts
	if stallCount >= stallThreshold {
		strippedOutput := stripANSI(s.outputBuffer.String())

		// Check for peak-tty signal first
		if result, found := s.checkForPeakTTYSignal(execCtx); found {
			slog.Debug("peak-tty signal detected (13 NUL bytes)")
			return result, stallCount, false
		}

		// Check for password prompt
		if result, found := s.checkForPasswordPrompt(execCtx, strippedOutput); found {
			slog.Debug("password prompt detected via pattern")
			return result, stallCount, false
		}

		// No signals detected - partially reset stall counter
		stallCount = stallThreshold / 2
	}

	return nil, stallCount, true
}

// checkOutputForResult checks the output buffer for completion or prompts.
func (s *Session) checkOutputForResult(execCtx *execContext) *ExecResult {
	// Check for completion
	if result, found := s.checkForCompletion(execCtx); found {
		return result
	}

	// Check for peak-tty signal
	if result, found := s.checkForPeakTTYSignal(execCtx); found {
		slog.Debug("peak-tty signal detected immediately")
		return result
	}

	// Check for interactive prompt
	output := s.outputBuffer.String()
	if result, found := s.checkForInteractivePrompt(execCtx, output); found {
		return result
	}

	return nil
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
		// Check if the line contains the end marker (not just starts with it)
		// Commands like curl may output without a trailing newline, so the marker
		// can appear in the middle of a line: "000___CMD_END_xxx___7"
		if idx := strings.Index(line, endMarker); idx != -1 {
			rest := line[idx+len(endMarker):]
			var exitCode int
			if _, err := fmt.Sscanf(rest, "%d", &exitCode); err == nil {
				return exitCode, true
			}
		}
	}

	return 0, false
}

// peakTTYSignal is 13 consecutive NUL bytes injected by peak-tty daemon
// when a process is blocked on n_tty_read (waiting for TTY input).
const peakTTYSignal = "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"

// containsPeakTTYSignal checks if the output contains the peak-tty signal.
// peak-tty is an eBPF-based daemon that injects 13 NUL bytes to the TTY
// when it detects a process blocked on n_tty_read.
func containsPeakTTYSignal(s string) bool {
	return strings.Contains(s, peakTTYSignal)
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	// Match ANSI escape sequences: ESC[ followed by parameters and a letter
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-Za-z]`)
	return ansiRegex.ReplaceAllString(s, "")
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

	if err := s.validateAwaitingInputState(); err != nil {
		return nil, err
	}

	s.State = StateRunning
	s.LastUsed = time.Now()

	s.prepareForPasswordInput()

	toWrite := input + "\n"
	if err := s.writeInputToPTY(toWrite); err != nil {
		return nil, err
	}

	s.outputBuffer.Reset()
	s.pendingPrompt = nil

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.readOutput(ctx, "")
}

// validateAwaitingInputState checks if session is ready for input.
func (s *Session) validateAwaitingInputState() error {
	if s.State != StateAwaitingInput {
		return fmt.Errorf("session is not awaiting input (state: %s)", s.State)
	}
	if s.pty == nil {
		return fmt.Errorf(errSessionNotInitialized)
	}
	return nil
}

// prepareForPasswordInput waits for echo to be disabled for password prompts.
func (s *Session) prepareForPasswordInput() {
	isPasswordPrompt := s.pendingPrompt != nil && s.pendingPrompt.Pattern.MaskInput
	if isPasswordPrompt {
		slog.Debug("waiting for echo disabled before password input")
		s.waitForEchoDisabled()
	}
}

// writeInputToPTY writes input and handles connection errors.
func (s *Session) writeInputToPTY(toWrite string) error {
	_, err := s.pty.WriteString(toWrite)
	if err == nil {
		return nil
	}

	slog.Error("failed to write input", "error", err)

	if isConnectionBroken(err) && s.Mode == "ssh" {
		return s.handleInputConnectionError(err)
	}

	s.State = StateAwaitingInput
	return fmt.Errorf("write input: %w", err)
}

// handleInputConnectionError handles broken connection during input.
func (s *Session) handleInputConnectionError(originalErr error) error {
	slog.Warn("SSH connection broken during input, attempting reconnect",
		slog.String("session_id", s.ID),
	)
	s.State = StateIdle
	if reconnErr := s.reconnectSSH(); reconnErr != nil {
		return fmt.Errorf(errConnectionLostFmt, reconnErr, originalErr)
	}
	return fmt.Errorf("connection was lost (reconnected - please retry command)")
}

// SendRaw sends raw bytes to the PTY without any processing.
// This is a low-level tool for sending control characters, escape sequences, or
// binary data that shell_provide_input cannot handle.
//
// The input string can contain escape sequences that will be interpreted:
//   - \x04 or \004 = EOF (Ctrl+D)
//   - \x03 or \003 = Interrupt (Ctrl+C)
//   - \x1b or \033 = Escape
//   - \n = newline, \r = carriage return, \t = tab
//   - \\ = literal backslash
//
// No newline is automatically appended - you must include it if needed.
func (s *Session) SendRaw(input string) (*ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.State != StateAwaitingInput {
		return nil, fmt.Errorf("session is not awaiting input (state: %s)", s.State)
	}

	if s.pty == nil {
		return nil, fmt.Errorf(errSessionNotInitialized)
	}

	s.State = StateRunning
	s.LastUsed = time.Now()

	// Interpret escape sequences in the input
	rawBytes := interpretEscapeSequences(input)

	slog.Debug("sending raw bytes to PTY",
		"len", len(rawBytes),
		"bytes", fmt.Sprintf("%v", rawBytes))

	n, err := s.pty.Write(rawBytes)
	if err != nil {
		slog.Error("failed to write raw input", "error", err)
		if isConnectionBroken(err) && s.Mode == "ssh" {
			slog.Warn("SSH connection broken during raw input, attempting reconnect",
				slog.String("session_id", s.ID),
			)
			s.State = StateIdle
			if reconnErr := s.reconnectSSH(); reconnErr != nil {
				return nil, fmt.Errorf(errConnectionLostFmt, reconnErr, err)
			}
			return nil, fmt.Errorf("connection was lost (reconnected - please retry)")
		}
		s.State = StateAwaitingInput
		return nil, fmt.Errorf("write raw input: %w", err)
	}
	slog.Debug("wrote raw bytes to PTY", "bytesWritten", n)

	// Clear output buffer
	s.outputBuffer.Reset()
	s.pendingPrompt = nil

	// Continue reading output
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return s.readOutput(ctx, "")
}

// simpleEscapes maps single-character escape sequences to their byte values.
var simpleEscapes = map[byte]byte{
	'n':  '\n',
	'r':  '\r',
	't':  '\t',
	'\\': '\\',
	'e':  0x1b,
}

// tryParseHexEscape attempts to parse a hex escape like \xNN.
func tryParseHexEscape(s string, i int) (byte, int, bool) {
	if i+3 >= len(s) {
		return 0, 0, false
	}
	b, ok := parseHexByte(s[i+2 : i+4])
	if !ok {
		return 0, 0, false
	}
	return b, 4, true
}

// tryParseOctalEscape attempts to parse an octal escape like \NNN.
func tryParseOctalEscape(s string, i int) (byte, int, bool) {
	if i+3 >= len(s) {
		return 0, 0, false
	}
	b, ok := parseOctalByte(s[i+1 : i+4])
	if !ok {
		return 0, 0, false
	}
	return b, 4, true
}

// tryParseEscape attempts to parse an escape sequence at position i.
// Returns (byte, skipCount, true) if successful, or (0, 0, false) if not an escape.
func tryParseEscape(s string, i int) (byte, int, bool) {
	if i+1 >= len(s) {
		return 0, 0, false
	}

	next := s[i+1]

	if b, ok := simpleEscapes[next]; ok {
		return b, 2, true
	}

	if next == 'x' || next == 'X' {
		if b, skip, ok := tryParseHexEscape(s, i); ok {
			return b, skip, true
		}
	}

	if next >= '0' && next <= '3' {
		if b, skip, ok := tryParseOctalEscape(s, i); ok {
			return b, skip, true
		}
	}

	return 0, 0, false
}

// interpretEscapeSequences converts escape sequences in a string to actual bytes.
// Supports: \xNN (hex), \NNN (octal), \n, \r, \t, \\, and common control chars.
func interpretEscapeSequences(s string) []byte {
	var result []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			result = append(result, s[i])
			continue
		}

		if b, skip, ok := tryParseEscape(s, i); ok {
			result = append(result, b)
			i += skip - 1
			continue
		}

		result = append(result, s[i])
	}
	return result
}

// parseHexByte parses a 2-character hex string into a byte.
func parseHexByte(s string) (byte, bool) {
	if len(s) != 2 {
		return 0, false
	}
	var b byte
	for _, c := range s {
		b <<= 4
		switch {
		case c >= '0' && c <= '9':
			b |= byte(c - '0')
		case c >= 'a' && c <= 'f':
			b |= byte(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			b |= byte(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return b, true
}

// parseOctalByte parses a 3-character octal string into a byte.
func parseOctalByte(s string) (byte, bool) {
	if len(s) != 3 {
		return 0, false
	}
	var b byte
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, false
		}
		b = b*8 + byte(c-'0')
	}
	return b, true
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
		return fmt.Errorf(errSessionNotInitialized)
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
// Supports both legacy marker and new dynamic markers.
func (s *Session) extractExitCode(output string) (int, bool) {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Check legacy marker
		if strings.HasPrefix(line, endMarker) {
			rest := strings.TrimPrefix(line, endMarker)
			var exitCode int
			if _, err := fmt.Sscanf(rest, "%d", &exitCode); err == nil {
				return exitCode, true
			}
		}
		// Check new dynamic markers (___CMD_END_xxx___N)
		if strings.HasPrefix(line, endMarkerPrefix) {
			// Find the marker suffix AFTER the prefix (not at the beginning of line)
			// Line format: ___CMD_END_abc123___0
			afterPrefix := line[len(endMarkerPrefix):]
			suffixIdx := strings.Index(afterPrefix, markerSuffix)
			if suffixIdx != -1 {
				rest := afterPrefix[suffixIdx+len(markerSuffix):]
				var exitCode int
				if _, err := fmt.Sscanf(rest, "%d", &exitCode); err == nil {
					return exitCode, true
				}
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

		// Skip legacy end marker
		if strings.Contains(line, endMarker) {
			continue
		}

		// Skip new dynamic markers (___CMD_START_xxx___ and ___CMD_END_xxx___N)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, startMarkerPrefix) || strings.HasPrefix(trimmed, endMarkerPrefix) {
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
	ID                string            `json:"session_id"`
	State             State             `json:"state"`
	Mode              string            `json:"mode"`
	Shell             string            `json:"shell"`
	ShellInfo         *ShellInfo        `json:"shell_info,omitempty"`
	Cwd               string            `json:"cwd"`
	IdleSeconds       int               `json:"idle_seconds"`
	UptimeSeconds     int               `json:"uptime_seconds"`
	EnvVars           map[string]string `json:"env_vars,omitempty"`
	Aliases           map[string]string `json:"aliases,omitempty"`
	Host              string            `json:"host,omitempty"`
	User              string            `json:"user,omitempty"`
	Connected         bool              `json:"connected"`
	SudoCached        bool              `json:"sudo_cached,omitempty"`
	SudoExpiresIn     int               `json:"sudo_expires_in_seconds,omitempty"`
	PTYName           string            `json:"pty_name,omitempty"`
	HasControlSession bool              `json:"has_control_session,omitempty"`
	SavedTunnels      []TunnelConfig    `json:"saved_tunnels,omitempty"` // Tunnels from before MCP restart
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
	// Output truncation info (when tail_lines or head_lines is used, or auto-truncation)
	Truncated      bool   `json:"truncated,omitempty"`
	TotalLines     int    `json:"total_lines,omitempty"`
	ShownLines     int    `json:"shown_lines,omitempty"`
	TotalBytes     int    `json:"total_bytes,omitempty"`      // Original output size in bytes
	TruncatedBytes int    `json:"truncated_bytes,omitempty"`  // Bytes shown after truncation
	Warning        string `json:"warning,omitempty"`          // Warning message for large outputs
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

// TunnelManager returns the SSH tunnel manager for this session.
// For local sessions, returns nil with an error.
func (s *Session) TunnelManager() (*ssh.TunnelManager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Mode != "ssh" {
		return nil, fmt.Errorf("tunnels not available for local sessions (SSH only)")
	}

	if s.sshClient == nil {
		return nil, fmt.Errorf("SSH client not initialized")
	}

	return s.sshClient.TunnelManager(), nil
}

// GetTunnelConfigs returns configurations of active tunnels for persistence.
func (s *Session) GetTunnelConfigs() []TunnelConfig {
	if s.Mode != "ssh" || s.sshClient == nil {
		return nil
	}

	tm := s.sshClient.TunnelManager()
	if tm == nil {
		return nil
	}

	tunnels := tm.ListTunnels()
	if len(tunnels) == 0 {
		return nil
	}

	configs := make([]TunnelConfig, 0, len(tunnels))
	for _, t := range tunnels {
		configs = append(configs, TunnelConfig{
			Type:       string(t.Type),
			LocalHost:  t.LocalHost,
			LocalPort:  t.LocalPort,
			RemoteHost: t.RemoteHost,
			RemotePort: t.RemotePort,
		})
	}
	return configs
}

// ClearSavedTunnels clears the saved tunnel configs after restoration.
func (s *Session) ClearSavedTunnels() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SavedTunnels = nil
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
