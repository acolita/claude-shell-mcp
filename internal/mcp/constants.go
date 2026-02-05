package mcp

// Common error messages and descriptions used across MCP tools.
const (
	// Tool parameter descriptions
	descSessionID    = "The session ID"
	descSSHSessionID = "The SSH session ID"

	// Common error messages
	errSessionIDRequired = "session_id is required"
	errGetSFTPClient     = "get SFTP client: %v"
	errCreateDirs        = "create directories: %v"
	errPreserveTimestamp = "failed to preserve timestamps"
	errWalkDir           = "walk directory: %v"
	errOpenRemoteFile    = "open remote file: %v"
	errOpenLocalFile     = "open local file: %v"

	// Peak-tty constants
	peakTTYPgrepCmd    = "pgrep -x peak-tty 2>/dev/null || true"
	peakTTYBinaryName  = "peak-tty"

	// Input detection hint
	hintPeakTTYWaiting = "Process is waiting for input (detected by peak-tty)."
)
