package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/ports"
	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerConfigTools() {
	s.mcpServer.AddTool(shellConfigAddTool(), s.handleShellConfigAdd)
}

func shellConfigAddTool() mcp.Tool {
	return mcp.NewTool("shell_config_add",
		mcp.WithDescription(`Add an SSH server configuration interactively.

Opens a TUI form on the user's terminal to confirm and optionally edit
server details before saving. The LLM provides known fields as parameters;
the user sees a pre-filled form and can adjust values or cancel.

This keeps sensitive information (key paths, credentials) under user control
via direct terminal interaction, never passing through the LLM context.

The server is immediately available after saving (config hot-reload picks up the change).

Requires a config file path (--config flag at startup).`),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Short name for the server (e.g., 'production', 's1')"),
		),
		mcp.WithString("host",
			mcp.Required(),
			mcp.Description("SSH hostname or IP address"),
		),
		mcp.WithNumber("port",
			mcp.Description("SSH port (default: 22)"),
		),
		mcp.WithString("user",
			mcp.Required(),
			mcp.Description("SSH username"),
		),
		mcp.WithString("auth_type",
			mcp.Description("Authentication type: 'key' (default) or 'password'"),
		),
		mcp.WithString("key_path",
			mcp.Description("Path to SSH private key (optional, user can set in form)"),
		),
		mcp.WithString("sudo_password_env",
			mcp.Description("Environment variable name containing the sudo password (optional)"),
		),
	)
}

func (s *Server) handleShellConfigAdd(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.configPath == "" {
		return mcp.NewToolResultError(
			"No config file path set. Start the server with --config flag to enable config management.",
		), nil
	}

	name := mcp.ParseString(req, "name", "")
	host := mcp.ParseString(req, "host", "")
	port := mcp.ParseInt(req, "port", 22)
	user := mcp.ParseString(req, "user", "")
	authType := mcp.ParseString(req, "auth_type", "")
	keyPath := mcp.ParseString(req, "key_path", "")
	sudoPasswordEnv := mcp.ParseString(req, "sudo_password_env", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	if host == "" {
		return mcp.NewToolResultError("host is required"), nil
	}
	if user == "" {
		return mcp.NewToolResultError("user is required"), nil
	}
	if authType == "" {
		authType = "key"
	}

	if existing := s.lookupServer(name); existing != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("server %q already exists in config", name),
		), nil
	}

	prefill := ports.ServerFormData{
		Name:            name,
		Host:            host,
		Port:            port,
		User:            user,
		KeyPath:         keyPath,
		AuthType:        authType,
		SudoPasswordEnv: sudoPasswordEnv,
	}

	slog.Info("showing server config form", slog.String("server_name", name))

	result, err := s.dialogProvider.ServerConfigForm(prefill)
	if err != nil {
		slog.Info("server config form error", slog.String("error", err.Error()))
		return mcp.NewToolResultError(fmt.Sprintf("dialog error: %v", err)), nil
	}

	if !result.Confirmed {
		slog.Info("server configuration cancelled by user", slog.String("server_name", name))
		return jsonResult(map[string]any{
			"status":  "cancelled",
			"message": "User cancelled the configuration",
		})
	}

	newServer := config.ServerConfig{
		Name:            result.Name,
		Host:            result.Host,
		Port:            result.Port,
		User:            result.User,
		KeyPath:         result.KeyPath,
		SudoPasswordEnv: result.SudoPasswordEnv,
		Auth: config.AuthConfig{
			Type: result.AuthType,
			Path: result.KeyPath,
		},
	}

	if err := s.config.AddServer(newServer); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("add server: %v", err)), nil
	}

	if err := config.Save(s.config, s.configPath); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save config: %v", err)), nil
	}

	slog.Info("server configuration saved",
		slog.String("server_name", result.Name),
		slog.String("host", result.Host),
		slog.String("config_path", s.configPath),
	)

	return jsonResult(map[string]any{
		"status":      "saved",
		"server_name": result.Name,
		"host":        result.Host,
		"port":        result.Port,
		"user":        result.User,
		"key_path":    result.KeyPath,
		"auth_type":   result.AuthType,
		"config_path": s.configPath,
		"message":     "Server added. Config will hot-reload automatically.",
	})
}
