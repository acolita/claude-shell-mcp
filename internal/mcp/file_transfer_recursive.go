package mcp

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/sftp"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/mark3labs/mcp-go/mcp"
)

// Default exclusion patterns for directory transfers
var defaultExclusions = []string{
	".git",
	".svn",
	".hg",
	"node_modules",
	"__pycache__",
	".DS_Store",
	"*.pyc",
	"*.pyo",
	".env",
	".env.local",
}

// registerRecursiveTransferTools registers recursive file transfer MCP tools.
func (s *Server) registerRecursiveTransferTools() {
	s.mcpServer.AddTool(shellDirGetTool(), s.handleShellDirGet)
	s.mcpServer.AddTool(shellDirPutTool(), s.handleShellDirPut)
}

func shellDirGetTool() mcp.Tool {
	return mcp.NewTool("shell_dir_get",
		mcp.WithDescription(`Download a directory recursively from a remote SSH session.

Walks the remote directory tree and downloads all files to a local directory.
Supports glob patterns, exclusion patterns, and symlink handling.

Glob pattern examples:
- "*.log" - all .log files
- "**/*.go" - all .go files in any subdirectory
- "src/**/*.ts" - all .ts files under src/

Returns transfer summary including file count, total size, and any errors.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
		mcp.WithString("remote_path",
			mcp.Required(),
			mcp.Description("Path to the directory on the remote server"),
		),
		mcp.WithString("local_path",
			mcp.Required(),
			mcp.Description("Local directory to save files to"),
		),
		mcp.WithString("pattern",
			mcp.Description("Glob pattern to filter files (e.g., '*.log', '**/*.go')"),
		),
		mcp.WithBoolean("preserve",
			mcp.Description("Preserve file timestamps and permissions (default: true)"),
		),
		mcp.WithString("symlinks",
			mcp.Description("Symlink handling: 'follow' (default), 'preserve', or 'skip'"),
			mcp.DefaultString("follow"),
		),
		mcp.WithNumber("max_depth",
			mcp.Description("Maximum directory depth to traverse (default: 20)"),
		),
	)
}

func shellDirPutTool() mcp.Tool {
	return mcp.NewTool("shell_dir_put",
		mcp.WithDescription(`Upload a directory recursively to a remote SSH session.

Walks the local directory tree and uploads all files to the remote server.
Supports glob patterns, exclusion patterns, and symlink handling.

Glob pattern examples:
- "*.log" - all .log files
- "**/*.go" - all .go files in any subdirectory
- "src/**/*.ts" - all .ts files under src/

Returns transfer summary including file count, total size, and any errors.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
		mcp.WithString("local_path",
			mcp.Required(),
			mcp.Description("Local directory to upload"),
		),
		mcp.WithString("remote_path",
			mcp.Required(),
			mcp.Description("Destination directory on the remote server"),
		),
		mcp.WithString("pattern",
			mcp.Description("Glob pattern to filter files (e.g., '*.log', '**/*.go')"),
		),
		mcp.WithBoolean("preserve",
			mcp.Description("Preserve file timestamps and permissions (default: true)"),
		),
		mcp.WithString("symlinks",
			mcp.Description("Symlink handling: 'follow' (default), 'preserve', or 'skip'"),
			mcp.DefaultString("follow"),
		),
		mcp.WithNumber("max_depth",
			mcp.Description("Maximum directory depth to traverse (default: 20)"),
		),
		mcp.WithBoolean("overwrite",
			mcp.Description("Overwrite existing files (default: false)"),
		),
	)
}

// DirTransferResult represents the result of a directory transfer operation.
type DirTransferResult struct {
	Status           string          `json:"status"`
	FilesTransferred int             `json:"files_transferred"`
	DirsCreated      int             `json:"dirs_created"`
	TotalBytes       int64           `json:"total_bytes"`
	SymlinksHandled  int             `json:"symlinks_handled,omitempty"`
	Errors           []TransferError `json:"errors,omitempty"`
	DurationMs       int64           `json:"duration_ms,omitempty"`
	BytesPerSecond   int64           `json:"bytes_per_second,omitempty"`
}

// TransferError represents an error during transfer of a specific file.
type TransferError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// DirGetOptions contains options for directory download operations.
type DirGetOptions struct {
	LocalPath  string
	Preserve   bool
	Symlinks   string // "follow", "preserve", "skip"
	MaxDepth   int
	Exclusions []string
	Pattern    string // Glob pattern to filter files
}

// DirPutOptions contains options for directory upload operations.
type DirPutOptions struct {
	RemotePath string
	Preserve   bool
	Symlinks   string
	MaxDepth   int
	Overwrite  bool
	Exclusions []string
	Pattern    string // Glob pattern to filter files
}

func (s *Server) handleShellDirGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	remotePath := mcp.ParseString(req, "remote_path", "")

	opts := DirGetOptions{
		LocalPath:  mcp.ParseString(req, "local_path", ""),
		Preserve:   mcp.ParseBoolean(req, "preserve", true),
		Symlinks:   mcp.ParseString(req, "symlinks", "follow"),
		MaxDepth:   mcp.ParseInt(req, "max_depth", 20),
		Exclusions: defaultExclusions,
		Pattern:    mcp.ParseString(req, "pattern", ""),
	}

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if remotePath == "" {
		return mcp.NewToolResultError("remote_path is required"), nil
	}
	if opts.LocalPath == "" {
		return mcp.NewToolResultError("local_path is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resolvedPath := sess.ResolvePath(remotePath)

	slog.Info("downloading directory",
		slog.String("session_id", sessionID),
		slog.String("remote_path", resolvedPath),
		slog.String("local_path", opts.LocalPath),
	)

	if sess.IsSSH() {
		return s.handleSSHDirGet(sess, resolvedPath, opts)
	}
	return s.handleLocalDirCopy(resolvedPath, opts.LocalPath, opts)
}

func (s *Server) handleSSHDirGet(sess *session.Session, remotePath string, opts DirGetOptions) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	// Verify remote path is a directory
	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat remote path: %v", err)), nil
	}
	if !info.IsDir() {
		return mcp.NewToolResultError("remote_path is not a directory, use shell_file_get for files"), nil
	}

	// Create local base directory
	if err := os.MkdirAll(opts.LocalPath, 0755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create local directory: %v", err)), nil
	}

	result := DirTransferResult{
		Status: "completed",
	}

	// Walk remote directory
	err = s.walkRemoteDir(sftpClient, remotePath, opts.LocalPath, "", 0, opts, &result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("walk directory: %v", err)), nil
	}

	// Calculate duration and transfer rate
	duration := time.Since(startTime)
	result.DurationMs = duration.Milliseconds()
	if duration.Seconds() > 0 {
		result.BytesPerSecond = int64(float64(result.TotalBytes) / duration.Seconds())
	}

	if len(result.Errors) > 0 {
		result.Status = "completed_with_errors"
	}

	return jsonResult(result)
}

func (s *Server) walkRemoteDir(client *sftp.Client, remotePath, localBase, relPath string, depth int, opts DirGetOptions, result *DirTransferResult) error {
	if depth > opts.MaxDepth {
		return nil
	}

	currentRemote := remotePath
	if relPath != "" {
		currentRemote = remotePath + "/" + relPath
	}

	entries, err := client.ReadDir(currentRemote)
	if err != nil {
		result.Errors = append(result.Errors, TransferError{Path: currentRemote, Error: err.Error()})
		return nil
	}

	for _, entry := range entries {
		name := entry.Name()

		// Check exclusions
		if shouldExclude(name, opts.Exclusions) {
			continue
		}

		entryRelPath := name
		if relPath != "" {
			entryRelPath = relPath + "/" + name
		}
		remoteEntryPath := remotePath + "/" + entryRelPath
		localEntryPath := filepath.Join(localBase, entryRelPath)

		// Handle symlinks
		if entry.Mode()&os.ModeSymlink != 0 {
			switch opts.Symlinks {
			case "skip":
				continue
			case "preserve":
				target, err := client.ReadLink(remoteEntryPath)
				if err != nil {
					result.Errors = append(result.Errors, TransferError{Path: remoteEntryPath, Error: fmt.Sprintf("read symlink: %v", err)})
					continue
				}
				if err := os.MkdirAll(filepath.Dir(localEntryPath), 0755); err != nil {
					result.Errors = append(result.Errors, TransferError{Path: localEntryPath, Error: fmt.Sprintf("create parent dir: %v", err)})
					continue
				}
				if err := os.Symlink(target, localEntryPath); err != nil {
					result.Errors = append(result.Errors, TransferError{Path: localEntryPath, Error: fmt.Sprintf("create symlink: %v", err)})
					continue
				}
				result.SymlinksHandled++
				continue
			default: // "follow"
				// Re-stat to follow the symlink
				entry, err = client.Stat(remoteEntryPath)
				if err != nil {
					result.Errors = append(result.Errors, TransferError{Path: remoteEntryPath, Error: fmt.Sprintf("stat symlink target: %v", err)})
					continue
				}
			}
		}

		if entry.IsDir() {
			// Always traverse directories to find matching files
			// But only create the directory if we have files to put in it
			if err := s.walkRemoteDir(client, remotePath, localBase, entryRelPath, depth+1, opts, result); err != nil {
				return err
			}
		} else {
			// Check if file matches pattern (only filter files, not directories)
			if !matchesPattern(entryRelPath, opts.Pattern) {
				continue
			}

			// Download file
			data, _, err := client.GetFile(remoteEntryPath)
			if err != nil {
				result.Errors = append(result.Errors, TransferError{Path: remoteEntryPath, Error: err.Error()})
				continue
			}

			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(localEntryPath), 0755); err != nil {
				result.Errors = append(result.Errors, TransferError{Path: localEntryPath, Error: err.Error()})
				continue
			}

			// Write file
			if err := os.WriteFile(localEntryPath, data, entry.Mode().Perm()); err != nil {
				result.Errors = append(result.Errors, TransferError{Path: localEntryPath, Error: err.Error()})
				continue
			}

			// Preserve timestamps
			if opts.Preserve {
				os.Chtimes(localEntryPath, entry.ModTime(), entry.ModTime())
			}

			result.FilesTransferred++
			result.TotalBytes += entry.Size()
		}
	}

	return nil
}

func (s *Server) handleLocalDirCopy(srcPath, dstPath string, opts DirGetOptions) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	// Verify source is a directory
	info, err := os.Stat(srcPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat source: %v", err)), nil
	}
	if !info.IsDir() {
		return mcp.NewToolResultError("source path is not a directory"), nil
	}

	result := DirTransferResult{
		Status: "completed",
	}

	err = filepath.WalkDir(srcPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, TransferError{Path: path, Error: err.Error()})
			return nil
		}

		relPath, _ := filepath.Rel(srcPath, path)
		if relPath == "." {
			return nil
		}

		// Check exclusions
		if shouldExclude(d.Name(), opts.Exclusions) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstEntryPath := filepath.Join(dstPath, relPath)

		if d.IsDir() {
			// Always traverse directories
			return nil
		}

		// Check if file matches pattern
		if !matchesPattern(relPath, opts.Pattern) {
			return nil
		}

		// Copy file - ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(dstEntryPath), 0755); err != nil {
			result.Errors = append(result.Errors, TransferError{Path: dstEntryPath, Error: err.Error()})
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			result.Errors = append(result.Errors, TransferError{Path: path, Error: err.Error()})
			return nil
		}

		info, _ := d.Info()
		if err := os.WriteFile(dstEntryPath, data, info.Mode().Perm()); err != nil {
			result.Errors = append(result.Errors, TransferError{Path: dstEntryPath, Error: err.Error()})
			return nil
		}

		if opts.Preserve {
			os.Chtimes(dstEntryPath, info.ModTime(), info.ModTime())
		}

		result.FilesTransferred++
		result.TotalBytes += info.Size()
		return nil
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("walk directory: %v", err)), nil
	}

	// Calculate duration and transfer rate
	duration := time.Since(startTime)
	result.DurationMs = duration.Milliseconds()
	if duration.Seconds() > 0 {
		result.BytesPerSecond = int64(float64(result.TotalBytes) / duration.Seconds())
	}

	if len(result.Errors) > 0 {
		result.Status = "completed_with_errors"
	}

	return jsonResult(result)
}

func (s *Server) handleShellDirPut(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	localPath := mcp.ParseString(req, "local_path", "")

	opts := DirPutOptions{
		RemotePath: mcp.ParseString(req, "remote_path", ""),
		Preserve:   mcp.ParseBoolean(req, "preserve", true),
		Symlinks:   mcp.ParseString(req, "symlinks", "follow"),
		MaxDepth:   mcp.ParseInt(req, "max_depth", 20),
		Overwrite:  mcp.ParseBoolean(req, "overwrite", false),
		Exclusions: defaultExclusions,
		Pattern:    mcp.ParseString(req, "pattern", ""),
	}

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if localPath == "" {
		return mcp.NewToolResultError("local_path is required"), nil
	}
	if opts.RemotePath == "" {
		return mcp.NewToolResultError("remote_path is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resolvedRemote := sess.ResolvePath(opts.RemotePath)

	slog.Info("uploading directory",
		slog.String("session_id", sessionID),
		slog.String("local_path", localPath),
		slog.String("remote_path", resolvedRemote),
	)

	if sess.IsSSH() {
		return s.handleSSHDirPut(sess, localPath, resolvedRemote, opts)
	}
	return s.handleLocalDirCopyPut(localPath, resolvedRemote, opts)
}

func (s *Server) handleSSHDirPut(sess *session.Session, localPath, remotePath string, opts DirPutOptions) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	// Verify local path is a directory
	info, err := os.Stat(localPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat local path: %v", err)), nil
	}
	if !info.IsDir() {
		return mcp.NewToolResultError("local_path is not a directory, use shell_file_put for files"), nil
	}

	// Create remote base directory
	if err := sftpClient.MkdirAll(remotePath); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create remote directory: %v", err)), nil
	}

	result := DirTransferResult{
		Status: "completed",
	}

	// Walk local directory
	err = filepath.WalkDir(localPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, TransferError{Path: path, Error: err.Error()})
			return nil
		}

		relPath, _ := filepath.Rel(localPath, path)
		if relPath == "." {
			return nil
		}

		// Check exclusions
		if shouldExclude(d.Name(), opts.Exclusions) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Use forward slashes for remote paths
		remoteEntryPath := remotePath + "/" + strings.ReplaceAll(relPath, "\\", "/")

		// Handle symlinks
		if d.Type()&os.ModeSymlink != 0 {
			switch opts.Symlinks {
			case "skip":
				return nil
			case "preserve":
				target, err := os.Readlink(path)
				if err != nil {
					result.Errors = append(result.Errors, TransferError{Path: path, Error: fmt.Sprintf("read symlink: %v", err)})
					return nil
				}
				if err := sftpClient.Symlink(target, remoteEntryPath); err != nil {
					result.Errors = append(result.Errors, TransferError{Path: remoteEntryPath, Error: fmt.Sprintf("create symlink: %v", err)})
					return nil
				}
				result.SymlinksHandled++
				return nil
			default: // "follow"
				// Continue with the resolved target
				info, err := os.Stat(path)
				if err != nil {
					result.Errors = append(result.Errors, TransferError{Path: path, Error: fmt.Sprintf("stat symlink target: %v", err)})
					return nil
				}
				if info.IsDir() {
					d = dirEntryFromInfo{info}
				}
			}
		}

		if d.IsDir() {
			// Always traverse directories to find matching files
			return nil
		}

		// Check if file matches pattern
		if !matchesPattern(relPath, opts.Pattern) {
			return nil
		}

		// Upload file
		data, err := os.ReadFile(path)
		if err != nil {
			result.Errors = append(result.Errors, TransferError{Path: path, Error: err.Error()})
			return nil
		}

		// Ensure parent directory exists
		remoteDir := filepath.Dir(remoteEntryPath)
		remoteDir = strings.ReplaceAll(remoteDir, "\\", "/")
		if err := sftpClient.MkdirAll(remoteDir); err != nil {
			result.Errors = append(result.Errors, TransferError{Path: remoteDir, Error: err.Error()})
			return nil
		}

		// Check if file exists and skip if not overwriting
		if !opts.Overwrite {
			if _, err := sftpClient.Stat(remoteEntryPath); err == nil {
				result.Errors = append(result.Errors, TransferError{Path: remoteEntryPath, Error: "file exists (use overwrite=true)"})
				return nil
			}
		}

		info, _ := d.Info()
		if err := sftpClient.PutFile(remoteEntryPath, data, info.Mode().Perm()); err != nil {
			result.Errors = append(result.Errors, TransferError{Path: remoteEntryPath, Error: err.Error()})
			return nil
		}

		// Preserve timestamps
		if opts.Preserve {
			sftpClient.Chtimes(remoteEntryPath, info.ModTime(), info.ModTime())
		}

		result.FilesTransferred++
		result.TotalBytes += info.Size()
		return nil
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("walk directory: %v", err)), nil
	}

	// Calculate duration and transfer rate
	duration := time.Since(startTime)
	result.DurationMs = duration.Milliseconds()
	if duration.Seconds() > 0 {
		result.BytesPerSecond = int64(float64(result.TotalBytes) / duration.Seconds())
	}

	if len(result.Errors) > 0 {
		result.Status = "completed_with_errors"
	}

	return jsonResult(result)
}

func (s *Server) handleLocalDirCopyPut(srcPath, dstPath string, opts DirPutOptions) (*mcp.CallToolResult, error) {
	// For local sessions, this is essentially the same as DirGet but with different semantics
	getOpts := DirGetOptions{
		LocalPath:  dstPath,
		Preserve:   opts.Preserve,
		Symlinks:   opts.Symlinks,
		MaxDepth:   opts.MaxDepth,
		Exclusions: opts.Exclusions,
	}
	return s.handleLocalDirCopy(srcPath, dstPath, getOpts)
}

// shouldExclude checks if a file/directory name should be excluded.
func shouldExclude(name string, exclusions []string) bool {
	for _, pattern := range exclusions {
		// Simple glob matching
		if strings.HasPrefix(pattern, "*") {
			suffix := pattern[1:]
			if strings.HasSuffix(name, suffix) {
				return true
			}
		} else if pattern == name {
			return true
		}
	}
	return false
}

// matchesPattern checks if a relative path matches the glob pattern.
// If pattern is empty, all files match.
func matchesPattern(relPath, pattern string) bool {
	if pattern == "" {
		return true
	}

	// Normalize path separators
	relPath = strings.ReplaceAll(relPath, "\\", "/")

	// Use doublestar for ** support
	matched, err := doublestar.Match(pattern, relPath)
	if err != nil {
		// On error, include the file
		return true
	}
	return matched
}

// dirEntryFromInfo wraps os.FileInfo to implement fs.DirEntry
type dirEntryFromInfo struct {
	info os.FileInfo
}

func (d dirEntryFromInfo) Name() string               { return d.info.Name() }
func (d dirEntryFromInfo) IsDir() bool                { return d.info.IsDir() }
func (d dirEntryFromInfo) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d dirEntryFromInfo) Info() (fs.FileInfo, error) { return d.info, nil }
