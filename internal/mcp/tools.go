package mcp

import (
	"context"
	"encoding/json"
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
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"session_id": sess.ID,
		"status":     "connected",
		"mode":       mode,
		"shell":      "/bin/bash",
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

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	slog.Info("executing command",
		slog.String("session_id", sessionID),
		slog.String("command", command),
	)

	result, err := sess.Exec(command, timeoutMs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// If a sudo password prompt is detected and we have a cached password, auto-inject it
	if result.Status == "awaiting_input" && result.PromptType == "password" {
		if cachedPwd := s.sudoCache.Get(sessionID); cachedPwd != nil {
			slog.Info("auto-injecting cached sudo password",
				slog.String("session_id", sessionID),
			)

			// Provide the cached password
			result, err = sess.ProvideInput(string(cachedPwd))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

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

	// Capture current env vars if requested (adds some latency)
	if status.EnvVars == nil || len(status.EnvVars) == 0 {
		status.EnvVars = sess.CaptureEnv()
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

	if err := s.sessionManager.Close(sessionID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText("Session closed"), nil
}

// jsonResult converts a value to a JSON tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
