package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/ssh"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerTunnelTools registers SSH tunnel MCP tools.
func (s *Server) registerTunnelTools() {
	s.mcpServer.AddTool(shellTunnelCreateTool(), s.handleShellTunnelCreate)
	s.mcpServer.AddTool(shellTunnelListTool(), s.handleShellTunnelList)
	s.mcpServer.AddTool(shellTunnelCloseTool(), s.handleShellTunnelClose)
	s.mcpServer.AddTool(shellTunnelRestoreTool(), s.handleShellTunnelRestore)
}

func shellTunnelCreateTool() mcp.Tool {
	return mcp.NewTool("shell_tunnel_create",
		mcp.WithDescription(`Create an SSH tunnel (port forward) for a session.

Supports two tunnel types:
- "local" (-L): Listen locally and forward through SSH to a remote destination.
  Example: Access remote database at localhost:5432 → forwards to db.internal:5432
- "reverse" (-R): Listen on the remote server and forward back to local machine.
  Example: Expose local web server to remote at remote:8080 → forwards to localhost:3000

Common use cases:
- Access internal services (databases, APIs) through SSH jump host
- Expose local development server to remote environment
- Create secure tunnels for services that don't support encryption

Returns tunnel ID and connection details. Use shell_tunnel_list to see all tunnels
and shell_tunnel_close to stop a tunnel.

Note: Tunnels are only available for SSH sessions, not local sessions.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSSHSessionID),
		),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description("Tunnel type: 'local' (-L) or 'reverse' (-R)"),
		),
		mcp.WithNumber("local_port",
			mcp.Required(),
			mcp.Description("Local port (for local: listen port, for reverse: destination port). Use 0 for auto-assign."),
		),
		mcp.WithString("local_host",
			mcp.Description("Local host (default: '127.0.0.1')"),
		),
		mcp.WithNumber("remote_port",
			mcp.Required(),
			mcp.Description("Remote port (for local: destination port, for reverse: listen port). Use 0 for auto-assign."),
		),
		mcp.WithString("remote_host",
			mcp.Description("Remote host (default: '127.0.0.1' for local, '0.0.0.0' for reverse)"),
		),
	)
}

func shellTunnelListTool() mcp.Tool {
	return mcp.NewTool("shell_tunnel_list",
		mcp.WithDescription(`List all active SSH tunnels for a session.

Returns details for each tunnel including:
- Tunnel ID and type
- Local and remote endpoints
- Connection statistics (active connections, bytes transferred)

Use this to monitor tunnel status and find tunnel IDs for closing.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSSHSessionID),
		),
	)
}

func shellTunnelCloseTool() mcp.Tool {
	return mcp.NewTool("shell_tunnel_close",
		mcp.WithDescription(`Close an SSH tunnel.

Stops the tunnel and closes all active connections through it.
The tunnel ID can be found using shell_tunnel_list.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSSHSessionID),
		),
		mcp.WithString("tunnel_id",
			mcp.Required(),
			mcp.Description("The tunnel ID to close"),
		),
	)
}

// TunnelCreateResult represents the result of tunnel creation.
type TunnelCreateResult struct {
	Status     string `json:"status"`
	TunnelID   string `json:"tunnel_id"`
	Type       string `json:"type"`
	LocalHost  string `json:"local_host"`
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
}

// TunnelInfo represents information about a tunnel.
type TunnelInfo struct {
	TunnelID          string `json:"tunnel_id"`
	Type              string `json:"type"`
	LocalHost         string `json:"local_host"`
	LocalPort         int    `json:"local_port"`
	RemoteHost        string `json:"remote_host"`
	RemotePort        int    `json:"remote_port"`
	ActiveConnections int64  `json:"active_connections"`
	TotalConnections  int64  `json:"total_connections"`
	BytesSent         int64  `json:"bytes_sent"`
	BytesReceived     int64  `json:"bytes_received"`
}

// TunnelListResult represents the result of listing tunnels.
type TunnelListResult struct {
	Status  string       `json:"status"`
	Count   int          `json:"count"`
	Tunnels []TunnelInfo `json:"tunnels"`
}

// TunnelCloseResult represents the result of closing a tunnel.
type TunnelCloseResult struct {
	Status   string `json:"status"`
	TunnelID string `json:"tunnel_id"`
}

func (s *Server) handleShellTunnelCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	tunnelType := mcp.ParseString(req, "type", "")
	localPort := mcp.ParseInt(req, "local_port", 0)
	localHost := mcp.ParseString(req, "local_host", "127.0.0.1")
	remotePort := mcp.ParseInt(req, "remote_port", 0)
	remoteHost := mcp.ParseString(req, "remote_host", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}
	if tunnelType != "local" && tunnelType != "reverse" {
		return mcp.NewToolResultError("type must be 'local' or 'reverse'"), nil
	}

	// Set default remote host based on tunnel type
	if remoteHost == "" {
		if tunnelType == "local" {
			remoteHost = "127.0.0.1"
		} else {
			remoteHost = "0.0.0.0" // Listen on all interfaces for reverse tunnels
		}
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tunnelManager, err := sess.TunnelManager()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var tunnel *ssh.Tunnel
	if tunnelType == "local" {
		slog.Info("creating local tunnel",
			slog.String("session_id", sessionID),
			slog.String("local", fmt.Sprintf("%s:%d", localHost, localPort)),
			slog.String("remote", fmt.Sprintf("%s:%d", remoteHost, remotePort)),
		)
		tunnel, err = tunnelManager.CreateLocalTunnel(localHost, localPort, remoteHost, remotePort)
	} else {
		slog.Info("creating reverse tunnel",
			slog.String("session_id", sessionID),
			slog.String("remote", fmt.Sprintf("%s:%d", remoteHost, remotePort)),
			slog.String("local", fmt.Sprintf("%s:%d", localHost, localPort)),
		)
		tunnel, err = tunnelManager.CreateReverseTunnel(remoteHost, remotePort, localHost, localPort)
	}

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create tunnel: %v", err)), nil
	}

	result := TunnelCreateResult{
		Status:     "created",
		TunnelID:   tunnel.ID,
		Type:       string(tunnel.Type),
		LocalHost:  tunnel.LocalHost,
		LocalPort:  tunnel.LocalPort,
		RemoteHost: tunnel.RemoteHost,
		RemotePort: tunnel.RemotePort,
	}

	return jsonResult(result)
}

func (s *Server) handleShellTunnelList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tunnelManager, err := sess.TunnelManager()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tunnels := tunnelManager.ListTunnels()
	tunnelInfos := make([]TunnelInfo, len(tunnels))
	for i, t := range tunnels {
		tunnelInfos[i] = TunnelInfo{
			TunnelID:          t.ID,
			Type:              string(t.Type),
			LocalHost:         t.LocalHost,
			LocalPort:         t.LocalPort,
			RemoteHost:        t.RemoteHost,
			RemotePort:        t.RemotePort,
			ActiveConnections: t.ActiveConns,
			TotalConnections:  t.TotalConns,
			BytesSent:         t.BytesSent,
			BytesReceived:     t.BytesReceived,
		}
	}

	result := TunnelListResult{
		Status:  "ok",
		Count:   len(tunnelInfos),
		Tunnels: tunnelInfos,
	}

	return jsonResult(result)
}

func (s *Server) handleShellTunnelClose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	tunnelID := mcp.ParseString(req, "tunnel_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}
	if tunnelID == "" {
		return mcp.NewToolResultError("tunnel_id is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tunnelManager, err := sess.TunnelManager()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := tunnelManager.CloseTunnel(tunnelID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	slog.Info("closed tunnel",
		slog.String("session_id", sessionID),
		slog.String("tunnel_id", tunnelID),
	)

	result := TunnelCloseResult{
		Status:   "closed",
		TunnelID: tunnelID,
	}

	return jsonResult(result)
}

func shellTunnelRestoreTool() mcp.Tool {
	return mcp.NewTool("shell_tunnel_restore",
		mcp.WithDescription(`Restore saved tunnels from before MCP restart.

When a session is recovered after MCP restart, any tunnels that were active are saved
but not automatically restored. Use this tool to restore them.

Check session status (shell_session_status) to see saved_tunnels that can be restored.

Use tunnel_index to restore a specific tunnel, or omit it to restore all saved tunnels.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSSHSessionID),
		),
		mcp.WithNumber("tunnel_index",
			mcp.Description("Index of specific tunnel to restore (0-based). Omit to restore all."),
		),
	)
}

// TunnelRestoreResult represents the result of restoring tunnels.
type TunnelRestoreResult struct {
	Status   string             `json:"status"`
	Restored []TunnelCreateResult `json:"restored,omitempty"`
	Errors   []string           `json:"errors,omitempty"`
}

func (s *Server) handleShellTunnelRestore(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	tunnelIndex := mcp.ParseInt(req, "tunnel_index", -1)

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	status := sess.Status()
	if len(status.SavedTunnels) == 0 {
		return mcp.NewToolResultError("no saved tunnels to restore"), nil
	}

	tunnelManager, err := sess.TunnelManager()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var tunnelsToRestore []session.TunnelConfig
	if tunnelIndex >= 0 {
		if tunnelIndex >= len(status.SavedTunnels) {
			return mcp.NewToolResultError(fmt.Sprintf("tunnel_index %d out of range (0-%d)", tunnelIndex, len(status.SavedTunnels)-1)), nil
		}
		tunnelsToRestore = []session.TunnelConfig{status.SavedTunnels[tunnelIndex]}
	} else {
		tunnelsToRestore = status.SavedTunnels
	}

	result := TunnelRestoreResult{
		Status: "ok",
	}

	for _, tc := range tunnelsToRestore {
		var tunnel *ssh.Tunnel
		var err error

		if tc.Type == "local" {
			tunnel, err = tunnelManager.CreateLocalTunnel(tc.LocalHost, tc.LocalPort, tc.RemoteHost, tc.RemotePort)
		} else {
			tunnel, err = tunnelManager.CreateReverseTunnel(tc.RemoteHost, tc.RemotePort, tc.LocalHost, tc.LocalPort)
		}

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s tunnel %s:%d -> %s:%d: %v",
				tc.Type, tc.LocalHost, tc.LocalPort, tc.RemoteHost, tc.RemotePort, err))
			continue
		}

		result.Restored = append(result.Restored, TunnelCreateResult{
			Status:     "restored",
			TunnelID:   tunnel.ID,
			Type:       string(tunnel.Type),
			LocalHost:  tunnel.LocalHost,
			LocalPort:  tunnel.LocalPort,
			RemoteHost: tunnel.RemoteHost,
			RemotePort: tunnel.RemotePort,
		})

		slog.Info("restored tunnel",
			slog.String("session_id", sessionID),
			slog.String("tunnel_id", tunnel.ID),
			slog.String("type", tc.Type),
		)
	}

	// Clear saved tunnels after restore attempt
	sess.ClearSavedTunnels()

	return jsonResult(result)
}
