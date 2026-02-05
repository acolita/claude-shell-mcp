// Package mcp implements the MCP protocol server for claude-shell-mcp.
package mcp

import (
	"log/slog"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realfs"
	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/ports"
	"github.com/acolita/claude-shell-mcp/internal/recording"
	"github.com/acolita/claude-shell-mcp/internal/security"
	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the MCP server implementation.
type Server struct {
	mcpServer        *server.MCPServer
	sessionManager   *session.Manager
	sudoCache        *security.SudoCache
	commandFilter    *security.CommandFilter
	authRateLimiter  *security.AuthRateLimiter
	recordingManager *recording.Manager
	config           *config.Config
	fs               ports.FileSystem
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithFileSystem sets the filesystem used by Server.
func WithFileSystem(fs ports.FileSystem) ServerOption {
	return func(s *Server) {
		s.fs = fs
	}
}

// NewServer creates a new MCP server with the given configuration.
func NewServer(cfg *config.Config, opts ...ServerOption) *Server {
	mcpServer := server.NewMCPServer(
		"claude-shell-mcp",
		"1.5.1",
		server.WithToolCapabilities(false),
		server.WithLogging(),
	)

	// Use sudo cache TTL from config, or default
	sudoTTL := cfg.Security.SudoCacheTTL
	if sudoTTL == 0 {
		sudoTTL = security.DefaultSudoTTL
	}

	// Initialize recording manager
	recordingPath := cfg.Recording.Path
	if recordingPath == "" {
		recordingPath = "/tmp/claude-shell-mcp/recordings"
	}

	// Initialize command filter
	commandFilter, err := security.NewCommandFilter(
		cfg.Security.CommandBlocklist,
		cfg.Security.CommandAllowlist,
	)
	if err != nil {
		slog.Warn("failed to initialize command filter, using permissive mode",
			slog.String("error", err.Error()),
		)
		commandFilter, _ = security.NewCommandFilter(nil, nil)
	}

	// Initialize auth rate limiter
	maxAuthFailures := cfg.Security.MaxAuthFailures
	if maxAuthFailures <= 0 {
		maxAuthFailures = security.DefaultMaxAuthFailures
	}
	authLockoutDuration := cfg.Security.AuthLockoutDuration
	if authLockoutDuration <= 0 {
		authLockoutDuration = security.DefaultAuthLockoutDuration
	}

	s := &Server{
		mcpServer:        mcpServer,
		sessionManager:   session.NewManager(cfg),
		sudoCache:        security.NewSudoCache(sudoTTL),
		commandFilter:    commandFilter,
		authRateLimiter:  security.NewAuthRateLimiter(maxAuthFailures, authLockoutDuration),
		recordingManager: recording.NewManager(recordingPath, cfg.Recording.Enabled),
		config:           cfg,
		fs:               realfs.New(), // default to real filesystem
	}

	// Apply options
	for _, opt := range opts {
		opt(s)
	}

	s.registerTools()

	return s
}

// Run starts the MCP server on stdio transport.
func (s *Server) Run() error {
	slog.Info("starting MCP server on stdio transport")
	return server.ServeStdio(s.mcpServer)
}

// UpdateConfig applies a new configuration at runtime.
// Only certain settings can be hot-reloaded; others require a restart.
func (s *Server) UpdateConfig(cfg *config.Config) {
	slog.Debug("applying config update")

	// Update command filter
	newFilter, err := security.NewCommandFilter(
		cfg.Security.CommandBlocklist,
		cfg.Security.CommandAllowlist,
	)
	if err != nil {
		slog.Warn("failed to update command filter, keeping previous",
			slog.String("error", err.Error()),
		)
	} else {
		s.commandFilter = newFilter
		slog.Debug("command filter updated")
	}

	// Update rate limiter settings
	maxAuthFailures := cfg.Security.MaxAuthFailures
	if maxAuthFailures <= 0 {
		maxAuthFailures = security.DefaultMaxAuthFailures
	}
	authLockoutDuration := cfg.Security.AuthLockoutDuration
	if authLockoutDuration <= 0 {
		authLockoutDuration = security.DefaultAuthLockoutDuration
	}
	s.authRateLimiter = security.NewAuthRateLimiter(maxAuthFailures, authLockoutDuration)
	slog.Debug("auth rate limiter updated")

	// Update recording settings
	recordingPath := cfg.Recording.Path
	if recordingPath == "" {
		recordingPath = "/tmp/claude-shell-mcp/recordings"
	}
	s.recordingManager = recording.NewManager(recordingPath, cfg.Recording.Enabled)
	slog.Debug("recording manager updated")

	// Update config reference
	s.config = cfg

	slog.Info("configuration hot-reloaded successfully")
}
