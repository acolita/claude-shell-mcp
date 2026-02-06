// Package mcp implements the MCP protocol server for claude-shell-mcp.
package mcp

import (
	"context"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/sftp"
	"github.com/acolita/claude-shell-mcp/internal/ssh"
)

// sessionManager abstracts session lifecycle management for testing.
type sessionManager interface {
	Create(opts session.CreateOptions) (*session.Session, error)
	Get(id string) (*session.Session, error)
	Close(id string) error
	ListDetailed() []session.SessionInfo
}

// managedSession abstracts the operations MCP handlers call on a session.
type managedSession interface {
	// Command execution
	Exec(command string, timeoutMs int) (*session.ExecResult, error)
	ProvideInput(input string) (*session.ExecResult, error)
	SendRaw(input string) (*session.ExecResult, error)
	Interrupt() error

	// Session info
	Status() session.SessionStatus
	ResolvePath(path string) string
	IsSSH() bool
	CaptureEnv() map[string]string
	CaptureAliases() map[string]string

	// File transfer
	SFTPClient() (*sftp.Client, error)

	// Tunnels
	TunnelManager() (*ssh.TunnelManager, error)
	ClearSavedTunnels()

	// Control plane
	ControlExec(ctx context.Context, command string) (string, error)

	// Lifecycle
	Close() error
}

// Verify concrete types satisfy the interfaces at compile time.
var _ sessionManager = (*session.Manager)(nil)
var _ managedSession = (*session.Session)(nil)
