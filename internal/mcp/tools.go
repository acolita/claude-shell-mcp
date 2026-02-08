package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/ssh"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// saveToFileThreshold is the size at which we save full output to a file.
	// Output exceeding this is saved to .claude-shell-mcp/ and only the file path is returned.
	saveToFileThreshold = 50 * 1024 // 50KB
)

// heredocPattern detects heredoc syntax which is not supported over PTY.
// Matches: <<EOF, <<'EOF', <<"EOF", <<-EOF, << EOF, etc.
var heredocPattern = regexp.MustCompile(`<<-?\s*['"]?\w+['"]?`)

// registerTools registers all MCP tools with the server.
func (s *Server) registerTools() {
	s.mcpServer.AddTool(shellSessionCreateTool(), s.handleShellSessionCreate)
	s.mcpServer.AddTool(shellSessionListTool(), s.handleShellSessionList)
	s.mcpServer.AddTool(shellExecTool(), s.handleShellExec)
	s.mcpServer.AddTool(shellProvideInputTool(), s.handleShellProvideInput)
	s.mcpServer.AddTool(shellSendRawTool(), s.handleShellSendRaw)
	s.mcpServer.AddTool(shellInterruptTool(), s.handleShellInterrupt)
	s.mcpServer.AddTool(shellSessionStatusTool(), s.handleShellSessionStatus)
	s.mcpServer.AddTool(shellSessionCloseTool(), s.handleShellSessionClose)
	s.mcpServer.AddTool(shellSudoAuthTool(), s.handleShellSudoAuth)
	s.mcpServer.AddTool(shellServerListTool(), s.handleShellServerList)
	s.mcpServer.AddTool(shellServerTestTool(), s.handleShellServerTest)

	// Register file transfer tools
	s.registerFileTransferTools()
	s.registerRecursiveTransferTools()
	s.registerChunkedTransferTools()

	// Register SSH tunnel tools
	s.registerTunnelTools()

	// Register peak-tty management tools
	s.registerPeakTTYTools()

	// Register debug tool
	s.mcpServer.AddTool(shellDebugTool(), s.handleShellDebug)
}

// Tool definitions

func shellSessionCreateTool() mcp.Tool {
	return mcp.NewTool("shell_session_create",
		mcp.WithDescription(`Initialize a persistent shell session (local PTY or SSH).

The session maintains state (working directory, environment variables, shell history) across multiple shell_exec calls. Use 'local' mode for commands on the local machine, or 'ssh' mode for remote servers.

For SSH mode, authentication uses SSH keys (agent or key_path). The session auto-reconnects if the connection drops.

Returns a session_id to use with other shell_* tools.`),
		mcp.WithString("mode",
			mcp.Description("Session mode: 'local' for local PTY or 'ssh' for remote SSH"),
			mcp.DefaultString("local"),
		),
		mcp.WithString("host",
			mcp.Description("SSH host (required for ssh mode)"),
		),
		mcp.WithNumber("port",
			mcp.Description("SSH port (default: 22)"),
		),
		mcp.WithString("user",
			mcp.Description("SSH username (required for ssh mode)"),
		),
		mcp.WithString("key_path",
			mcp.Description("Path to SSH private key file (e.g., ~/.ssh/id_ed25519)"),
		),
	)
}

func shellSessionListTool() mcp.Tool {
	return mcp.NewTool("shell_session_list",
		mcp.WithDescription(`List all active shell sessions.

Returns a list of all open sessions with their details including:
- session_id: The ID to use with other shell_* tools
- mode: "local" or "ssh"
- host/user: Connection info for SSH sessions
- state: Current state (idle, running, awaiting_input)
- cwd: Current working directory
- created_at: When the session was created
- last_used: When the session was last used
- idle_for: How long the session has been idle

Use this to recover session IDs after context compaction, or to find and close orphaned sessions.`),
	)
}

func shellServerListTool() mcp.Tool {
	return mcp.NewTool("shell_server_list",
		mcp.WithDescription(`List configured SSH servers from the config file.

Returns the list of pre-configured servers with their connection details.
Use the server name with shell_session_create to connect quickly.

Each server includes:
- name: Short name for the server (e.g., "s1", "production")
- host: SSH hostname or IP
- port: SSH port
- user: SSH username
- key_path: Path to SSH key (if configured)
- has_sudo_password: Whether sudo password is configured (never reveals the password)

Returns an empty list if no config file is loaded or no servers are defined.`),
	)
}

func shellServerTestTool() mcp.Tool {
	return mcp.NewTool("shell_server_test",
		mcp.WithDescription(`Test SSH connectivity to a configured server without creating a session.

Performs an SSH handshake and authentication against a configured server,
then immediately disconnects. Use this to verify a server is reachable
before creating a persistent session.

Returns:
- reachable: Whether the server accepted the SSH connection and authentication
- latency_ms: Round-trip time for the SSH handshake in milliseconds
- error: Error message if the connection failed
- server: The server's connection details (name, host, port, user)`),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Server name from config (as shown by shell_server_list)"),
		),
		mcp.WithNumber("timeout_ms",
			mcp.Description("Connection timeout in milliseconds (default: 10000)"),
		),
	)
}

func shellExecTool() mcp.Tool {
	return mcp.NewTool("shell_exec",
		mcp.WithDescription(`Execute a command in a shell session with interactive prompt detection.

Returns one of three statuses:
- "completed": Command finished. Check exit_code and stdout.
- "awaiting_input": Command is waiting for input (password, confirmation, or interactive app like vim). Use shell_provide_input to send input, or shell_interrupt to cancel.
- "timeout": Command exceeded timeout_ms. The command was interrupted and the session is ready for new commands.

Interactive prompts are auto-detected:
- Password prompts (sudo, ssh) - prompt_type: "password", mask_input: true
- Confirmations ([Y/n]) - prompt_type: "confirmation"
- Interactive apps (vim, less) - prompt_type: "interactive"

The session preserves state (cwd, env vars) across commands.

OUTPUT ISOLATION:
Each command uses unique markers to separate its output from background noise:
- stdout: Output from this specific command
- async_output: Any output from background processes or previous commands (if present)
- command_id: Unique ID for this command execution
This prevents confusion when background processes produce output asynchronously.

OUTPUT LIMITING (built-in tail/head):
For commands that produce long output (logs, large files, build output), use tail_lines or head_lines to limit the response. This is more reliable than piping to tail/head and avoids separate commands:
- tail_lines: Return only the last N lines (like "| tail -N")
- head_lines: Return only the first N lines (like "| head -N")
When output is truncated, the response includes:
- truncated: true
- total_lines: Original line count before truncation
- shown_lines: Number of lines actually returned
Example: shell_exec(command="cat /var/log/syslog", tail_lines=50) returns last 50 lines with truncation info.

SUDO PASSWORD HANDLING:
Password prompts are auto-injected from server configuration (sudo_password_env).
If a password prompt still appears as "awaiting_input", call shell_sudo_auth(session_id)
to inject the configured password. Do NOT ask the user for passwords.

HEREDOCS NOT SUPPORTED: Commands with heredocs (<<EOF, <<'EOF', <<"EOF", <<-EOF) are NOT supported and will be rejected. Heredocs cause PTY issues due to shell continuation prompts.

For multi-line file content, use these alternatives:
1. shell_file_put: Write content directly to a file (RECOMMENDED for config files, scripts, etc.)
2. printf: printf '%s\n' 'line1' 'line2' | sudo tee /path/to/file
3. echo -e: echo -e 'line1\nline2' | sudo tee /path/to/file`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID returned by shell_session_create"),
		),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("The command to execute"),
		),
		mcp.WithNumber("timeout_ms",
			mcp.Description("Command timeout in milliseconds (default: 30000)"),
		),
		mcp.WithNumber("tail_lines",
			mcp.Description("Return only the last N lines of output (built-in tail). Use for logs, long output. Cannot be combined with head_lines."),
		),
		mcp.WithNumber("head_lines",
			mcp.Description("Return only the first N lines of output (built-in head). Use for previewing large files. Cannot be combined with tail_lines."),
		),
	)
}

func shellProvideInputTool() mcp.Tool {
	return mcp.NewTool("shell_provide_input",
		mcp.WithDescription(`Resume a paused session by providing non-sensitive input (confirmations, interactive prompts).

Use this after shell_exec returns status "awaiting_input". A newline is automatically appended to the input.

Returns the same status types as shell_exec - the command may complete, request more input, or timeout.

IMPORTANT: Do NOT use this tool for password prompts. Passwords are auto-injected from
server configuration (sudo_password_env). If a password prompt appears as awaiting_input,
it means auto-injection failed and the user needs to configure sudo_password_env.

For confirmation prompts (prompt_type: "confirmation"), provide "yes", "y", "Y", or "n" as appropriate.
For interactive apps (prompt_type: "interactive"), provide the appropriate command (e.g., ":q!" for vim).`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
		mcp.WithString("input",
			mcp.Required(),
			mcp.Description("The input to provide (password, 'yes', 'Y', etc.)"),
		),
		mcp.WithBoolean("cache_for_sudo",
			mcp.Description("Cache this input for subsequent sudo prompts (default: false)"),
		),
	)
}

func shellSudoAuthTool() mcp.Tool {
	return mcp.NewTool("shell_sudo_auth",
		mcp.WithDescription(`Authenticate a sudo password prompt using the server's pre-configured credentials.

Call this when shell_exec returns status "awaiting_input" with prompt_type "password".
The password is read from the server's sudo_password_env configuration — it never
passes through the LLM.

If this tool returns an error, inform the user that they need to configure
sudo_password_env for the server in config.yaml and set the corresponding
environment variable.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
	)
}

func shellSendRawTool() mcp.Tool {
	return mcp.NewTool("shell_send_raw",
		mcp.WithDescription(`Send raw bytes to a session, including control characters and escape sequences.

THIS IS A LOW-LEVEL TOOL - Use shell_provide_input for normal text input.

=== WHEN TO USE THIS TOOL ===
ONLY use shell_send_raw when you need to send:
- EOF (Ctrl+D) to signal end of input to commands like sort, cat, bc
- Escape sequences for terminal control
- Binary data or special control characters
- Input WITHOUT an automatic trailing newline

=== WHEN NOT TO USE THIS TOOL ===
DO NOT use shell_send_raw for:
- Normal text input (use shell_provide_input instead)
- Passwords (handled automatically via server config, never sent manually)
- Confirmations like "yes" or "Y" (use shell_provide_input)
- Any input where you want a newline appended automatically

=== ESCAPE SEQUENCES SUPPORTED ===
- \x04 or \004 = EOF (Ctrl+D) - signals end of input
- \x03 or \003 = Interrupt (Ctrl+C) - use shell_interrupt instead
- \x1b or \033 or \e = Escape character
- \n = newline (LF)
- \r = carriage return (CR)
- \t = tab
- \\ = literal backslash

=== EXAMPLES ===
1. Send EOF to finish input for 'sort':
   shell_send_raw(input="\x04")

2. Send text followed by EOF (no trailing newline):
   shell_send_raw(input="final line\n\x04")

3. Send escape sequence:
   shell_send_raw(input="\x1b[A")  // Arrow up

=== IMPORTANT NOTES ===
- No newline is appended - include \n explicitly if needed
- Session must be in awaiting_input state
- For Ctrl+C interrupts, prefer shell_interrupt tool instead`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
		mcp.WithString("input",
			mcp.Required(),
			mcp.Description("Raw input with escape sequences (e.g., '\\x04' for EOF, '\\n' for newline)"),
		),
	)
}

func shellInterruptTool() mcp.Tool {
	return mcp.NewTool("shell_interrupt",
		mcp.WithDescription(`Send SIGINT (Ctrl+C) to interrupt a running command.

Use this to cancel long-running commands or exit interactive prompts. Note: Some programs like vim ignore SIGINT - for those, use shell_provide_input with the appropriate exit command (e.g., ":q!" for vim).

After interrupt, the session returns to idle state and is ready for new commands.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
	)
}

func shellSessionStatusTool() mcp.Tool {
	return mcp.NewTool("shell_session_status",
		mcp.WithDescription(`Check session health, current directory, and environment.

Returns session state (idle, running, awaiting_input), current working directory, environment variables, shell aliases, connection status, and sudo cache status.

Useful for debugging or verifying session state before executing commands.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
	)
}

func shellSessionCloseTool() mcp.Tool {
	return mcp.NewTool("shell_session_close",
		mcp.WithDescription(`Close and cleanup a shell session.

Terminates the shell process and releases resources. For SSH sessions, closes the connection. Always close sessions when done to free resources.

If session recording was enabled, returns the path to the recording file.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
	)
}

// Tool handlers

// validateSSHParams validates SSH mode parameters and rate limiting.
func (s *Server) validateSSHParams(host, user string) *mcp.CallToolResult {
	if host == "" {
		return mcp.NewToolResultError("host is required for ssh mode")
	}
	if user == "" {
		return mcp.NewToolResultError("user is required for ssh mode")
	}

	if locked, remaining := s.authRateLimiter.IsLocked(host, user); locked {
		slog.Warn("auth rate limited",
			slog.String("host", host),
			slog.String("user", user),
			slog.Duration("remaining", remaining),
		)
		return mcp.NewToolResultError(
			fmt.Sprintf("authentication locked due to too many failures, try again in %v", remaining),
		)
	}
	return nil
}

func (s *Server) handleShellSessionCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	mode := mcp.ParseString(req, "mode", "local")

	host := mcp.ParseString(req, "host", "")
	port := mcp.ParseInt(req, "port", 22)
	user := mcp.ParseString(req, "user", "")
	keyPath := mcp.ParseString(req, "key_path", "")

	if mode == "ssh" {
		if errResult := s.validateSSHParams(host, user); errResult != nil {
			return errResult, nil
		}
	}

	slog.Info("creating shell session",
		slog.String("mode", mode),
		slog.String("host", host),
	)

	sess, err := s.sessionManager.Create(session.CreateOptions{
		Mode:    mode,
		Host:    host,
		Port:    port,
		User:    user,
		KeyPath: keyPath,
	})
	if err != nil {
		// Record auth failure for SSH
		if mode == "ssh" {
			s.authRateLimiter.RecordFailure(host, user)
		}
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Record auth success for SSH
	if mode == "ssh" {
		s.authRateLimiter.RecordSuccess(host, user)
	}

	// Start recording if enabled
	if s.recordingManager.IsEnabled() {
		if err := s.recordingManager.StartRecording(sess.ID, 120, 24); err != nil {
			slog.Warn("failed to start recording",
				slog.String("session_id", sess.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	result := map[string]any{
		"session_id": sess.ID,
		"status":     "connected",
		"mode":       mode,
		"shell":      "/bin/bash",
	}

	if path := s.recordingManager.GetRecordingPath(sess.ID); path != "" {
		result["recording_path"] = path
	}

	return jsonResult(result)
}

func (s *Server) handleShellSessionList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessions := s.sessionManager.ListDetailed()

	result := map[string]any{
		"count":    len(sessions),
		"sessions": sessions,
	}

	return jsonResult(result)
}

func (s *Server) handleShellServerList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	servers := make([]map[string]any, 0)

	// Build host → session IDs map from active sessions
	hostSessions := make(map[string][]string)
	for _, info := range s.sessionManager.ListDetailed() {
		if info.Host != "" && info.Host != "local" {
			hostSessions[info.Host] = append(hostSessions[info.Host], info.ID)
		}
	}

	if s.config != nil {
		for _, srv := range s.config.Servers {
			port := srv.Port
			if port == 0 {
				port = 22
			}
			sessionIDs := hostSessions[srv.Host]
			if sessionIDs == nil {
				sessionIDs = []string{}
			}
			entry := map[string]any{
				"name":              srv.Name,
				"host":              srv.Host,
				"port":              port,
				"user":              srv.User,
				"key_path":          srv.KeyPath,
				"has_sudo_password": srv.SudoPasswordEnv != "",
				"active_sessions":   len(sessionIDs),
				"session_ids":       sessionIDs,
			}
			servers = append(servers, entry)
		}
	}

	result := map[string]any{
		"count":   len(servers),
		"servers": servers,
	}
	return jsonResult(result)
}

func (s *Server) handleShellServerTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(req, "name", "")
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	timeoutMs := mcp.ParseInt(req, "timeout_ms", 10000)

	srv := s.lookupServer(name)
	if srv == nil {
		return mcp.NewToolResultError(fmt.Sprintf("server %q not found in config", name)), nil
	}

	port := srv.Port
	if port == 0 {
		port = 22
	}

	serverInfo := map[string]any{
		"name": srv.Name,
		"host": srv.Host,
		"port": port,
		"user": srv.User,
	}

	// Build auth methods
	authCfg := ssh.AuthConfig{
		UseAgent: true,
		KeyPath:  srv.KeyPath,
		Host:     srv.Host,
	}
	if srv.Auth.Type == "password" && srv.Auth.PasswordEnv != "" {
		authCfg.Password = s.fs.Getenv(srv.Auth.PasswordEnv)
	}
	if srv.Auth.PassphraseEnv != "" {
		authCfg.KeyPassphrase = s.fs.Getenv(srv.Auth.PassphraseEnv)
	}
	if srv.Auth.Path != "" {
		authCfg.KeyPath = srv.Auth.Path
	}

	authMethods, err := ssh.BuildAuthMethods(authCfg)
	if err != nil {
		result := map[string]any{
			"reachable": false,
			"error":     fmt.Sprintf("build auth methods: %v", err),
			"server":    serverInfo,
		}
		return jsonResult(result)
	}

	hostKeyCallback, err := ssh.BuildHostKeyCallback("")
	if err != nil {
		hostKeyCallback = ssh.InsecureHostKeyCallback()
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	client, err := ssh.NewClient(ssh.ClientOptions{
		Host:            srv.Host,
		Port:            port,
		User:            srv.User,
		AuthMethods:     authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	})
	if err != nil {
		result := map[string]any{
			"reachable": false,
			"error":     fmt.Sprintf("create client: %v", err),
			"server":    serverInfo,
		}
		return jsonResult(result)
	}

	start := time.Now()
	err = client.Connect()
	latency := time.Since(start)
	client.Close()

	if err != nil {
		result := map[string]any{
			"reachable":  false,
			"latency_ms": latency.Milliseconds(),
			"error":      err.Error(),
			"server":     serverInfo,
		}
		return jsonResult(result)
	}

	result := map[string]any{
		"reachable":  true,
		"latency_ms": latency.Milliseconds(),
		"server":     serverInfo,
	}
	return jsonResult(result)
}

// tryCachedSudoInjection attempts to auto-inject a sudo password.
// It checks (in order): the sudo cache, then the server's sudo_password_env config.
// Returns updated result and any error that occurred.
func (s *Server) tryCachedSudoInjection(sessionID string, sess *session.Session, result *session.ExecResult) (*session.ExecResult, error) {
	if result.Status != "awaiting_input" || result.PromptType != "password" {
		return result, nil
	}

	// 1. Check the in-memory sudo cache first
	cachedPwd := s.sudoCache.Get(sessionID)

	// 2. Fall back to server config's sudo_password_env
	if cachedPwd == nil {
		cachedPwd = s.lookupSudoPasswordFromConfig(sess.Host)
	}

	if cachedPwd == nil {
		return result, nil
	}

	slog.Info("auto-injecting sudo password", slog.String("session_id", sessionID))
	s.recordingManager.RecordInput(sessionID, "***", true)

	newResult, err := sess.ProvideInput(string(cachedPwd))
	if err != nil {
		return nil, err
	}

	// Cache for subsequent sudo calls in this session
	s.sudoCache.Set(sessionID, cachedPwd)

	s.recordingManager.RecordOutput(sessionID, newResult.Stdout)
	newResult.SudoAuthenticated = true
	newResult.SudoExpiresInSeconds = int(s.sudoCache.ExpiresIn(sessionID).Seconds())
	return newResult, nil
}

// lookupServer finds a configured server by name or host.
func (s *Server) lookupServer(name string) *config.ServerConfig {
	if s.config == nil || name == "" {
		return nil
	}
	for i, srv := range s.config.Servers {
		if srv.Name == name || srv.Host == name {
			return &s.config.Servers[i]
		}
	}
	return nil
}

// lookupSudoPasswordFromConfig reads the sudo password from a server's configured env var.
func (s *Server) lookupSudoPasswordFromConfig(host string) []byte {
	srv := s.lookupServer(host)
	if srv == nil || srv.SudoPasswordEnv == "" {
		return nil
	}
	pwd := s.fs.Getenv(srv.SudoPasswordEnv)
	if pwd == "" {
		slog.Warn("sudo_password_env configured but env var is empty",
			slog.String("server", srv.Name),
			slog.String("env_var", srv.SudoPasswordEnv),
		)
		return nil
	}
	return []byte(pwd)
}

// applyAutoTruncation saves large outputs to a file and clears stdout.
// The LLM must explicitly read the file to get the content.
func (s *Server) applyAutoTruncation(sessionID string, result *session.ExecResult) {
	outputLen := len(result.Stdout)
	if outputLen <= saveToFileThreshold {
		return
	}

	result.TotalBytes = outputLen
	result.Truncated = true

	outputFile, err := s.saveOutputToFile(sessionID, result.Stdout)
	if err != nil {
		slog.Warn("failed to save output to file",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
		// Clear stdout completely - we can't return it inline
		result.Stdout = ""
		result.Warning = fmt.Sprintf(
			"Output too large (%d bytes) and failed to save to file: %v. Use shell_file_get for large outputs.",
			outputLen, err,
		)
		return
	}

	// Clear stdout - only return the file path
	result.Stdout = ""
	result.OutputFile = outputFile
	result.Warning = fmt.Sprintf(
		"Output too large (%d bytes). Full output saved to: %s. Read the file to analyze the content.",
		outputLen, outputFile,
	)
	slog.Info("large output saved to file",
		slog.String("session_id", sessionID),
		slog.Int("total_bytes", outputLen),
		slog.String("output_file", outputFile),
	)
}

// saveOutputToFile saves command output to a file in the working directory and returns the path.
func (s *Server) saveOutputToFile(sessionID, output string) (string, error) {
	cwd, err := s.fs.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working dir: %w", err)
	}

	outputDir := cwd + "/.claude-shell-mcp"
	if err := s.fs.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	// Generate unique filename: session_timestamp.txt
	timestamp := fmt.Sprintf("%d", s.clock.Now().UnixMilli())
	filename := fmt.Sprintf("%s_%s.txt", sessionID, timestamp)
	filepath := outputDir + "/" + filename

	if err := s.fs.WriteFile(filepath, []byte(output), 0644); err != nil {
		return "", fmt.Errorf("write output file: %w", err)
	}

	return filepath, nil
}

// validateExecParams validates parameters for shell_exec.
func validateExecParams(sessionID, command string, tailLines, headLines int) *mcp.CallToolResult {
	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired)
	}
	if command == "" {
		return mcp.NewToolResultError("command is required")
	}
	if tailLines > 0 && headLines > 0 {
		return mcp.NewToolResultError("cannot use both tail_lines and head_lines")
	}
	if heredocPattern.MatchString(command) {
		return mcp.NewToolResultError(
			"Heredocs (<<EOF, <<'EOF', etc.) are not supported over PTY sessions. " +
				"Use one of these alternatives instead:\n" +
				"1. shell_file_put: Write multi-line content directly to a file (recommended)\n" +
				"2. printf: printf '%s\\n' 'line1' 'line2' | sudo tee /path/to/file\n" +
				"3. echo -e: echo -e 'line1\\nline2' | sudo tee /path/to/file",
		)
	}
	return nil
}

func (s *Server) handleShellExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	command := mcp.ParseString(req, "command", "")
	timeoutMs := mcp.ParseInt(req, "timeout_ms", 30000)
	tailLines := mcp.ParseInt(req, "tail_lines", 0)
	headLines := mcp.ParseInt(req, "head_lines", 0)

	if errResult := validateExecParams(sessionID, command, tailLines, headLines); errResult != nil {
		return errResult, nil
	}

	if allowed, reason := s.commandFilter.IsAllowed(command); !allowed {
		slog.Warn("command blocked by filter", slog.String("command", command), slog.String("reason", reason))
		return mcp.NewToolResultError("command blocked: " + reason), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	slog.Info("executing command", slog.String("session_id", sessionID), slog.String("command", command))
	s.recordingManager.RecordInput(sessionID, command+"\n", false)

	result, err := sess.Exec(command, timeoutMs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s.recordingManager.RecordOutput(sessionID, result.Stdout)

	result, err = s.tryCachedSudoInjection(sessionID, sess, result)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if result.Stdout != "" && (tailLines > 0 || headLines > 0) {
		result.Stdout, result.Truncated, result.TotalLines, result.ShownLines = truncateOutput(result.Stdout, tailLines, headLines)
	}

	s.applyAutoTruncation(sessionID, result)

	return jsonResult(result)
}

func (s *Server) handleShellProvideInput(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	input := mcp.ParseString(req, "input", "")
	cacheForSudo := mcp.ParseBoolean(req, "cache_for_sudo", false)

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	slog.Info("providing input to session",
		slog.String("session_id", sessionID),
		slog.Bool("cache_for_sudo", cacheForSudo),
	)

	// Record input (masked if it's a password)
	s.recordingManager.RecordInput(sessionID, input+"\n", cacheForSudo)

	// Cache the password if requested (for sudo prompts)
	if cacheForSudo && input != "" {
		s.sudoCache.Set(sessionID, []byte(input))
		slog.Info("cached sudo password",
			slog.String("session_id", sessionID),
			slog.Duration("ttl", s.sudoCache.ExpiresIn(sessionID)),
		)
	}

	result, err := sess.ProvideInput(input)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Record output
	s.recordingManager.RecordOutput(sessionID, result.Stdout)

	// Add sudo authentication info to result
	if cacheForSudo {
		result.SudoAuthenticated = true
		expiresIn := s.sudoCache.ExpiresIn(sessionID)
		result.SudoExpiresInSeconds = int(expiresIn.Seconds())
	}

	s.applyAutoTruncation(sessionID, result)

	return jsonResult(result)
}

func (s *Server) handleShellSudoAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Look up password from config
	pwd := s.lookupSudoPasswordFromConfig(sess.Host)
	if pwd == nil {
		return mcp.NewToolResultError(
			"No sudo password configured for this server. " +
				"Set sudo_password_env in the server's config.yaml entry and provide the " +
				"corresponding environment variable when starting the MCP server.",
		), nil
	}

	slog.Info("sudo_auth: injecting password from config", slog.String("session_id", sessionID))
	s.recordingManager.RecordInput(sessionID, "***", true)

	result, err := sess.ProvideInput(string(pwd))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Cache for subsequent sudo calls
	s.sudoCache.Set(sessionID, pwd)

	s.recordingManager.RecordOutput(sessionID, result.Stdout)
	result.SudoAuthenticated = true
	result.SudoExpiresInSeconds = int(s.sudoCache.ExpiresIn(sessionID).Seconds())

	s.applyAutoTruncation(sessionID, result)

	return jsonResult(result)
}

func (s *Server) handleShellSendRaw(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	input := mcp.ParseString(req, "input", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}
	if input == "" {
		return mcp.NewToolResultError("input is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	slog.Info("sending raw input to session",
		slog.String("session_id", sessionID),
		slog.Int("input_len", len(input)),
	)

	// Record raw input (not masked - user is responsible for sensitive data)
	s.recordingManager.RecordInput(sessionID, input, false)

	result, err := sess.SendRaw(input)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Record output
	s.recordingManager.RecordOutput(sessionID, result.Stdout)

	s.applyAutoTruncation(sessionID, result)

	return jsonResult(result)
}

func (s *Server) handleShellInterrupt(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	slog.Info("interrupting session",
		slog.String("session_id", sessionID),
	)

	if err := sess.Interrupt(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText("Interrupt signal sent"), nil
}

func (s *Server) handleShellSessionStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	status := sess.Status()

	// Add sudo cache info
	if s.sudoCache.IsValid(sessionID) {
		status.SudoCached = true
		status.SudoExpiresIn = int(s.sudoCache.ExpiresIn(sessionID).Seconds())
	}

	// Capture current env vars if not already populated
	if status.EnvVars == nil || len(status.EnvVars) == 0 {
		status.EnvVars = sess.CaptureEnv()
	}

	// Capture aliases if not already populated
	if status.Aliases == nil || len(status.Aliases) == 0 {
		status.Aliases = sess.CaptureAliases()
	}

	return jsonResult(status)
}

func (s *Server) handleShellSessionClose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	slog.Info("closing session",
		slog.String("session_id", sessionID),
	)

	// Get recording path before closing
	recordingPath := s.recordingManager.GetRecordingPath(sessionID)

	// Stop recording
	s.recordingManager.StopRecording(sessionID)

	if err := s.sessionManager.Close(sessionID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"status": "closed",
	}
	if recordingPath != "" {
		result["recording_path"] = recordingPath
	}

	return jsonResult(result)
}

// jsonResult converts a value to a JSON tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// truncateOutput applies tail_lines or head_lines limiting to output.
// Returns: (truncatedOutput, wasTruncated, totalLines, shownLines)
func truncateOutput(output string, tailLines, headLines int) (string, bool, int, int) {
	lines := strings.Split(output, "\n")
	totalLines := len(lines)

	// Remove trailing empty line if present (split artifact)
	if totalLines > 0 && lines[totalLines-1] == "" {
		lines = lines[:totalLines-1]
		totalLines = len(lines)
	}

	if tailLines > 0 {
		if tailLines >= totalLines {
			// No truncation needed
			return output, false, totalLines, totalLines
		}
		// Take last N lines
		truncated := lines[totalLines-tailLines:]
		return strings.Join(truncated, "\n"), true, totalLines, tailLines
	}

	if headLines > 0 {
		if headLines >= totalLines {
			// No truncation needed
			return output, false, totalLines, totalLines
		}
		// Take first N lines
		truncated := lines[:headLines]
		return strings.Join(truncated, "\n"), true, totalLines, headLines
	}

	return output, false, totalLines, totalLines
}

func shellDebugTool() mcp.Tool {
	return mcp.NewTool("shell_debug",
		mcp.WithDescription(`Debug tool to inspect internal session state.

Returns detailed internal information about a session including:
- PTY name and control session availability
- Internal state for debugging issues

This tool is for debugging only and may change without notice.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID to inspect"),
		),
		mcp.WithString("action",
			mcp.Description("Debug action: 'status' (default), 'foreground', 'control_exec'"),
		),
		mcp.WithString("command",
			mcp.Description("Command to run via control session (only for action='control_exec')"),
		),
	)
}

// handleDebugForegroundAction handles the "foreground" debug action.
func handleDebugForegroundAction(sess *session.Session, status session.SessionStatus, result map[string]any) {
	tm, err := sess.TunnelManager()
	if err != nil {
		result["error"] = "no tunnel manager (not SSH or not connected)"
	} else {
		result["tunnel_manager"] = tm != nil
	}

	if status.HasControlSession && status.PTYName != "" {
		result["control_plane_available"] = true
		result["note"] = "Use control_exec action with command='ps -t pts/" + status.PTYName + " -o pid,stat,comm,wchan --no-headers' to check foreground process"
		return
	}

	result["control_plane_available"] = false
	if status.PTYName == "" {
		result["issue"] = "PTYName not detected"
	}
	if !status.HasControlSession {
		result["issue"] = "controlSession not initialized"
	}
}

// handleDebugControlExecAction handles the "control_exec" debug action.
func handleDebugControlExecAction(ctx context.Context, sess *session.Session, status session.SessionStatus, command string, result map[string]any) *mcp.CallToolResult {
	if command == "" {
		return mcp.NewToolResultError("command is required for control_exec action")
	}
	if !status.HasControlSession {
		return mcp.NewToolResultError("control session not available")
	}

	output, err := sess.ControlExec(ctx, command)
	if err != nil {
		result["error"] = err.Error()
	}
	result["output"] = output
	return nil
}

func (s *Server) handleShellDebug(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	action := mcp.ParseString(req, "action", "status")
	command := mcp.ParseString(req, "command", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"session_id": sessionID,
		"action":     action,
	}

	status := sess.Status()
	result["pty_name"] = status.PTYName
	result["has_control_session"] = status.HasControlSession
	result["state"] = status.State
	result["mode"] = status.Mode

	switch action {
	case "status":
		// Basic debug info already populated above
	case "foreground":
		handleDebugForegroundAction(sess, status, result)
	case "control_exec":
		if errResult := handleDebugControlExecAction(ctx, sess, status, command, result); errResult != nil {
			return errResult, nil
		}
	}

	return jsonResult(result)
}
