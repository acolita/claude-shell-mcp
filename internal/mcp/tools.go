package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTools registers all MCP tools with the server.
func (s *Server) registerTools() {
	s.mcpServer.AddTool(shellSessionCreateTool(), s.handleShellSessionCreate)
	s.mcpServer.AddTool(shellExecTool(), s.handleShellExec)
	s.mcpServer.AddTool(shellProvideInputTool(), s.handleShellProvideInput)
	s.mcpServer.AddTool(shellInterruptTool(), s.handleShellInterrupt)
	s.mcpServer.AddTool(shellSessionStatusTool(), s.handleShellSessionStatus)
	s.mcpServer.AddTool(shellSessionCloseTool(), s.handleShellSessionClose)
}

// Tool definitions

func shellSessionCreateTool() mcp.Tool {
	return mcp.NewTool("shell_session_create",
		mcp.WithDescription("Initialize a persistent shell session (local PTY or SSH)"),
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
	)
}

func shellExecTool() mcp.Tool {
	return mcp.NewTool("shell_exec",
		mcp.WithDescription("Execute a command in a shell session with interactive prompt detection"),
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
	)
}

func shellProvideInputTool() mcp.Tool {
	return mcp.NewTool("shell_provide_input",
		mcp.WithDescription("Resume a paused session by providing input (password, confirmation, etc.)"),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
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

func shellInterruptTool() mcp.Tool {
	return mcp.NewTool("shell_interrupt",
		mcp.WithDescription("Send SIGINT (Ctrl+C) to interrupt a running command"),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
	)
}

func shellSessionStatusTool() mcp.Tool {
	return mcp.NewTool("shell_session_status",
		mcp.WithDescription("Check session health, current directory, and environment"),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
	)
}

func shellSessionCloseTool() mcp.Tool {
	return mcp.NewTool("shell_session_close",
		mcp.WithDescription("Close and cleanup a shell session"),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
	)
}

// Tool handlers

func (s *Server) handleShellSessionCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	mode := mcp.ParseString(req, "mode", s.config.Mode)
	if mode == "" {
		mode = "local"
	}

	host := mcp.ParseString(req, "host", "")
	port := mcp.ParseInt(req, "port", 22)
	user := mcp.ParseString(req, "user", "")

	// Validate SSH parameters
	if mode == "ssh" {
		if host == "" {
			return mcp.NewToolResultError("host is required for ssh mode"), nil
		}
		if user == "" {
			return mcp.NewToolResultError("user is required for ssh mode"), nil
		}

		// Check rate limiting for SSH connections
		if locked, remaining := s.authRateLimiter.IsLocked(host, user); locked {
			slog.Warn("auth rate limited",
				slog.String("host", host),
				slog.String("user", user),
				slog.Duration("remaining", remaining),
			)
			return mcp.NewToolResultError(
				fmt.Sprintf("authentication locked due to too many failures, try again in %v", remaining),
			), nil
		}
	}

	slog.Info("creating shell session",
		slog.String("mode", mode),
		slog.String("host", host),
	)

	sess, err := s.sessionManager.Create(session.CreateOptions{
		Mode: mode,
		Host: host,
		Port: port,
		User: user,
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

func (s *Server) handleShellExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	command := mcp.ParseString(req, "command", "")
	timeoutMs := mcp.ParseInt(req, "timeout_ms", 30000)

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if command == "" {
		return mcp.NewToolResultError("command is required"), nil
	}

	// Check command filter
	if allowed, reason := s.commandFilter.IsAllowed(command); !allowed {
		slog.Warn("command blocked by filter",
			slog.String("command", command),
			slog.String("reason", reason),
		)
		return mcp.NewToolResultError("command blocked: " + reason), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	slog.Info("executing command",
		slog.String("session_id", sessionID),
		slog.String("command", command),
	)

	// Record command input
	s.recordingManager.RecordInput(sessionID, command+"\n", false)

	result, err := sess.Exec(command, timeoutMs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Record output
	s.recordingManager.RecordOutput(sessionID, result.Stdout)

	// If a sudo password prompt is detected and we have a cached password, auto-inject it
	if result.Status == "awaiting_input" && result.PromptType == "password" {
		if cachedPwd := s.sudoCache.Get(sessionID); cachedPwd != nil {
			slog.Info("auto-injecting cached sudo password",
				slog.String("session_id", sessionID),
			)

			// Record masked password input
			s.recordingManager.RecordInput(sessionID, string(cachedPwd), true)

			// Provide the cached password
			result, err = sess.ProvideInput(string(cachedPwd))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// Record output after password
			s.recordingManager.RecordOutput(sessionID, result.Stdout)

			// Mark that we used cached sudo
			result.SudoAuthenticated = true
			result.SudoExpiresInSeconds = int(s.sudoCache.ExpiresIn(sessionID).Seconds())
		}
	}

	return jsonResult(result)
}

func (s *Server) handleShellProvideInput(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	input := mcp.ParseString(req, "input", "")
	cacheForSudo := mcp.ParseBoolean(req, "cache_for_sudo", false)

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
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

	return jsonResult(result)
}

func (s *Server) handleShellInterrupt(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
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
		return mcp.NewToolResultError("session_id is required"), nil
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
		return mcp.NewToolResultError("session_id is required"), nil
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
