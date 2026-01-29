// Package mcp implements the MCP protocol server for claude-shell-mcp.
package mcp

import (
	"log/slog"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/security"
	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the MCP server implementation.
type Server struct {
	mcpServer      *server.MCPServer
	sessionManager *session.Manager
	sudoCache      *security.SudoCache
	config         *config.Config
}

// NewServer creates a new MCP server with the given configuration.
func NewServer(cfg *config.Config) *Server {
	mcpServer := server.NewMCPServer(
		"claude-shell-mcp",
		"0.1.0-alpha",
		server.WithToolCapabilities(false),
		server.WithLogging(),
	)

	// Use sudo cache TTL from config, or default
	sudoTTL := cfg.Security.SudoCacheTTL
	if sudoTTL == 0 {
		sudoTTL = security.DefaultSudoTTL
	}

	s := &Server{
		mcpServer:      mcpServer,
		sessionManager: session.NewManager(cfg),
		sudoCache:      security.NewSudoCache(sudoTTL),
		config:         cfg,
	}

	s.registerTools()

	return s
}

// Run starts the MCP server on stdio transport.
func (s *Server) Run() error {
	slog.Info("starting MCP server on stdio transport")
	return server.ServeStdio(s.mcpServer)
}
