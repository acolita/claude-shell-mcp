package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	peakTTYDefaultPath = "/tmp/peak-tty"
	peakTTYProcessName = "peak-tty"
)

// registerPeakTTYTools registers peak-tty management tools.
func (s *Server) registerPeakTTYTools() {
	s.mcpServer.AddTool(peakTTYStatusTool(), s.handlePeakTTYStatus)
	s.mcpServer.AddTool(peakTTYStartTool(), s.handlePeakTTYStart)
	s.mcpServer.AddTool(peakTTYStopTool(), s.handlePeakTTYStop)
	s.mcpServer.AddTool(peakTTYDeployTool(), s.handlePeakTTYDeploy)
}

func peakTTYStatusTool() mcp.Tool {
	return mcp.NewTool("peak_tty_status",
		mcp.WithDescription(`Check if peak-tty daemon is running in a session.

peak-tty is an eBPF-based daemon that detects when processes are waiting for TTY input
and sends a signal (13 NUL bytes) to the PTY. This enables accurate input-waiting detection.

Returns:
- running: Whether peak-tty is currently running
- pid: Process ID if running
- binary_exists: Whether the peak-tty binary exists at the expected path`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
		mcp.WithString("binary_path",
			mcp.Description("Path to check for peak-tty binary (default: /tmp/peak-tty)"),
		),
	)
}

func peakTTYStartTool() mcp.Tool {
	return mcp.NewTool("peak_tty_start",
		mcp.WithDescription(`Start the peak-tty daemon in a session.

IMPORTANT: peak-tty requires root privileges to hook kernel syscalls via eBPF.
The command will be run with sudo, which may prompt for a password.

If peak-tty is already running, this will return an error (it refuses to start twice).

The daemon runs in the background and monitors all TTY reads on the system.
When a process is detected waiting for input, it sends 13 NUL bytes to the PTY
as a signal that can be detected by claude-shell-mcp.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
		mcp.WithString("binary_path",
			mcp.Description("Path to peak-tty binary (default: /tmp/peak-tty)"),
		),
	)
}

func peakTTYStopTool() mcp.Tool {
	return mcp.NewTool("peak_tty_stop",
		mcp.WithDescription(`Stop the peak-tty daemon in a session.

Sends SIGTERM to the peak-tty process. Requires sudo.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
	)
}

func peakTTYDeployTool() mcp.Tool {
	return mcp.NewTool("peak_tty_deploy",
		mcp.WithDescription(`Deploy the peak-tty binary to a session.

This uploads the embedded peak-tty binary to the specified path on the remote/local system.
For SSH sessions, uses SFTP file transfer. For local sessions, copies the file.

After deployment, use peak_tty_start to run the daemon (requires sudo).`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
		mcp.WithString("binary_path",
			mcp.Description("Destination path for the binary (default: /tmp/peak-tty)"),
		),
		mcp.WithBoolean("overwrite",
			mcp.Description("Overwrite existing binary (default: false)"),
		),
	)
}

func (s *Server) handlePeakTTYStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	binaryPath := mcp.ParseString(req, "binary_path", peakTTYDefaultPath)

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"session_id":  sessionID,
		"binary_path": binaryPath,
	}

	// Check if peak-tty is running (look for process)
	pgrepResult, err := sess.Exec(peakTTYPgrepCmd, 5000)
	if err != nil {
		result["error"] = fmt.Sprintf("failed to check process: %v", err)
		return jsonResult(result)
	}

	pid := strings.TrimSpace(pgrepResult.Stdout)
	if pid != "" {
		result["running"] = true
		result["pid"] = pid
	} else {
		result["running"] = false
	}

	// Check if binary exists
	testResult, err := sess.Exec(fmt.Sprintf("test -x %s && echo exists || echo missing", binaryPath), 5000)
	if err != nil {
		result["binary_check_error"] = err.Error()
	} else {
		result["binary_exists"] = strings.TrimSpace(testResult.Stdout) == "exists"
	}

	return jsonResult(result)
}

func (s *Server) handlePeakTTYStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	binaryPath := mcp.ParseString(req, "binary_path", peakTTYDefaultPath)

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Check if already running
	pgrepResult, err := sess.Exec(peakTTYPgrepCmd, 5000)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to check if running: %v", err)), nil
	}
	if strings.TrimSpace(pgrepResult.Stdout) != "" {
		return mcp.NewToolResultError("peak-tty is already running (pid: " + strings.TrimSpace(pgrepResult.Stdout) + ")"), nil
	}

	// Check binary exists
	testResult, err := sess.Exec(fmt.Sprintf("test -x %s && echo exists || echo missing", binaryPath), 5000)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to check binary: %v", err)), nil
	}
	if strings.TrimSpace(testResult.Stdout) != "exists" {
		return mcp.NewToolResultError(fmt.Sprintf("peak-tty binary not found at %s - use peak_tty_deploy first", binaryPath)), nil
	}

	slog.Info("starting peak-tty daemon",
		slog.String("session_id", sessionID),
		slog.String("binary_path", binaryPath),
	)

	// Start peak-tty with sudo in background
	// Must use sudo bash -c '... &' pattern for proper backgrounding
	// Note: This may trigger a sudo password prompt (awaiting_input)
	logPath := binaryPath + ".log"
	startCmd := fmt.Sprintf("sudo bash -c '%s > %s 2>&1 & disown'", binaryPath, logPath)
	startResult, err := sess.Exec(startCmd, 10000)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to start: %v", err)), nil
	}

	// If awaiting input (sudo password), return that status
	if startResult.Status == "awaiting_input" {
		return jsonResult(startResult)
	}

	// Verify it started
	verifyResult, err := sess.Exec("sleep 0.5 && pgrep -x peak-tty", 5000)
	if err != nil || strings.TrimSpace(verifyResult.Stdout) == "" {
		// Check log for error message
		logResult, _ := sess.Exec(fmt.Sprintf("cat %s 2>/dev/null | head -3", logPath), 3000)
		errMsg := "peak-tty failed to start - check if it has root privileges"
		if logResult != nil && logResult.Stdout != "" {
			errMsg += "\nLog output: " + strings.TrimSpace(logResult.Stdout)
		}
		return mcp.NewToolResultError(errMsg), nil
	}

	result := map[string]any{
		"status":      "started",
		"pid":         strings.TrimSpace(verifyResult.Stdout),
		"binary_path": binaryPath,
		"log_path":    logPath,
		"note":        "peak-tty daemon is now running and monitoring TTY waits",
	}

	return jsonResult(result)
}

func (s *Server) handlePeakTTYStop(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Check if running
	pgrepResult, err := sess.Exec(peakTTYPgrepCmd, 5000)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to check if running: %v", err)), nil
	}
	pid := strings.TrimSpace(pgrepResult.Stdout)
	if pid == "" {
		return mcp.NewToolResultError("peak-tty is not running"), nil
	}

	slog.Info("stopping peak-tty daemon",
		slog.String("session_id", sessionID),
		slog.String("pid", pid),
	)

	// Stop with sudo pkill
	stopResult, err := sess.Exec("sudo pkill -x peak-tty", 10000)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to stop: %v", err)), nil
	}

	// If awaiting input (sudo password), return that status
	if stopResult.Status == "awaiting_input" {
		return jsonResult(stopResult)
	}

	result := map[string]any{
		"status":     "stopped",
		"killed_pid": pid,
	}

	return jsonResult(result)
}

func (s *Server) handlePeakTTYDeploy(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	binaryPath := mcp.ParseString(req, "binary_path", peakTTYDefaultPath)
	overwrite := mcp.ParseBoolean(req, "overwrite", false)

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Check if binary already exists
	if !overwrite {
		testResult, err := sess.Exec(fmt.Sprintf("test -e %s && echo exists || echo missing", binaryPath), 5000)
		if err == nil && strings.TrimSpace(testResult.Stdout) == "exists" {
			return mcp.NewToolResultError(fmt.Sprintf("binary already exists at %s - use overwrite=true to replace", binaryPath)), nil
		}
	}

	// Find the peak-tty binary
	// Search in several possible locations
	searchPaths := []string{
		"peak-tty/peak-tty",        // Submodule path (relative to cwd)
		"./peak-tty",               // Current directory
		"/usr/local/bin/peak-tty",  // System path
		"/usr/bin/peak-tty",        // System path
	}

	// Also check relative to the executable
	if execPath, err := s.fs.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		searchPaths = append(searchPaths,
			filepath.Join(execDir, peakTTYBinaryName),
			filepath.Join(execDir, "..", peakTTYBinaryName, peakTTYBinaryName),
		)
	}

	var binaryData []byte
	var foundPath string
	for _, p := range searchPaths {
		data, err := s.fs.ReadFile(p)
		if err == nil {
			binaryData = data
			foundPath = p
			break
		}
	}

	if binaryData == nil {
		return mcp.NewToolResultError(
			"peak-tty binary not found. Build it first with: cd peak-tty && make build\n" +
				"Searched paths: " + strings.Join(searchPaths, ", "),
		), nil
	}

	slog.Debug("found peak-tty binary", slog.String("path", foundPath))

	slog.Info("deploying peak-tty binary",
		slog.String("session_id", sessionID),
		slog.String("binary_path", binaryPath),
		slog.Int("binary_size", len(binaryData)),
	)

	// Use existing file transfer infrastructure
	opts := FilePutOptions{
		Overwrite:  overwrite,
		CreateDirs: false,
		Atomic:     false,
		Mode:       0755,
	}

	// Delegate to the appropriate handler based on session mode
	status := sess.Status()
	var uploadResult *mcp.CallToolResult
	if status.Mode == "ssh" {
		uploadResult, err = s.handleSSHFilePut(sess, binaryPath, binaryData, opts, time.Time{})
	} else {
		uploadResult, err = s.handleLocalFilePut(binaryPath, binaryData, opts, time.Time{})
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to upload binary: %v", err)), nil
	}
	if uploadResult.IsError {
		return uploadResult, nil
	}

	result := map[string]any{
		"status":      "deployed",
		"binary_path": binaryPath,
		"size_bytes":  len(binaryData),
		"note":        "Binary deployed. Use peak_tty_start to run (requires sudo).",
	}

	return jsonResult(result)
}
