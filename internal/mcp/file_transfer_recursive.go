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

// addError is a helper to record transfer errors consistently.
func (r *DirTransferResult) addError(path, errMsg string) {
	r.Errors = append(r.Errors, TransferError{Path: path, Error: errMsg})
}

// symlinkAction represents the result of symlink handling.
type symlinkAction int

const (
	symlinkSkip    symlinkAction = iota // Skip this entry entirely
	symlinkHandled                      // Symlink was handled (preserved)
	symlinkFollow                       // Continue processing the resolved target
	symlinkError                        // Error occurred, skip entry
)

// handleDownloadSymlink processes a symlink during download operations.
// Returns the action to take and potentially updated entry info.
func (s *Server) handleDownloadSymlink(
	client *sftp.Client,
	remoteEntryPath, localEntryPath string,
	symlinkMode string,
	result *DirTransferResult,
) (symlinkAction, os.FileInfo) {
	switch symlinkMode {
	case "skip":
		return symlinkSkip, nil
	case "preserve":
		target, err := client.ReadLink(remoteEntryPath)
		if err != nil {
			result.addError(remoteEntryPath, fmt.Sprintf("read symlink: %v", err))
			return symlinkError, nil
		}
		if err := s.fs.MkdirAll(filepath.Dir(localEntryPath), 0755); err != nil {
			result.addError(localEntryPath, fmt.Sprintf("create parent dir: %v", err))
			return symlinkError, nil
		}
		if err := s.fs.Symlink(target, localEntryPath); err != nil {
			result.addError(localEntryPath, fmt.Sprintf("create symlink: %v", err))
			return symlinkError, nil
		}
		result.SymlinksHandled++
		return symlinkHandled, nil
	default: // "follow"
		entry, err := client.Stat(remoteEntryPath)
		if err != nil {
			result.addError(remoteEntryPath, fmt.Sprintf("stat symlink target: %v", err))
			return symlinkError, nil
		}
		return symlinkFollow, entry
	}
}

// handleUploadSymlink processes a symlink during upload operations.
// Returns the action to take and potentially updated file info.
func (s *Server) handleUploadSymlink(
	sftpClient *sftp.Client,
	localPath, remoteEntryPath string,
	symlinkMode string,
	result *DirTransferResult,
) (symlinkAction, os.FileInfo) {
	switch symlinkMode {
	case "skip":
		return symlinkSkip, nil
	case "preserve":
		target, err := s.fs.Readlink(localPath)
		if err != nil {
			result.addError(localPath, fmt.Sprintf("read symlink: %v", err))
			return symlinkError, nil
		}
		if err := sftpClient.Symlink(target, remoteEntryPath); err != nil {
			result.addError(remoteEntryPath, fmt.Sprintf("create symlink: %v", err))
			return symlinkError, nil
		}
		result.SymlinksHandled++
		return symlinkHandled, nil
	default: // "follow"
		info, err := s.fs.Stat(localPath)
		if err != nil {
			result.addError(localPath, fmt.Sprintf("stat symlink target: %v", err))
			return symlinkError, nil
		}
		return symlinkFollow, info
	}
}

// downloadSingleFile downloads a single file from remote to local.
func (s *Server) downloadSingleFile(
	client *sftp.Client,
	remoteEntryPath, localEntryPath string,
	entry os.FileInfo,
	preserve bool,
	result *DirTransferResult,
) {
	data, _, err := client.GetFile(remoteEntryPath)
	if err != nil {
		result.addError(remoteEntryPath, err.Error())
		return
	}

	if err := s.fs.MkdirAll(filepath.Dir(localEntryPath), 0755); err != nil {
		result.addError(localEntryPath, err.Error())
		return
	}

	if err := s.fs.WriteFile(localEntryPath, data, entry.Mode().Perm()); err != nil {
		result.addError(localEntryPath, err.Error())
		return
	}

	if preserve {
		s.fs.Chtimes(localEntryPath, entry.ModTime(), entry.ModTime())
	}

	result.FilesTransferred++
	result.TotalBytes += entry.Size()
}

// uploadSingleFile uploads a single file from local to remote.
func (s *Server) uploadSingleFile(
	sftpClient *sftp.Client,
	localPath, remoteEntryPath string,
	info os.FileInfo,
	opts DirPutOptions,
	result *DirTransferResult,
) {
	data, err := s.fs.ReadFile(localPath)
	if err != nil {
		result.addError(localPath, err.Error())
		return
	}

	remoteDir := filepath.Dir(remoteEntryPath)
	remoteDir = strings.ReplaceAll(remoteDir, "\\", "/")
	if err := sftpClient.MkdirAll(remoteDir); err != nil {
		result.addError(remoteDir, err.Error())
		return
	}

	if !opts.Overwrite {
		if _, err := sftpClient.Stat(remoteEntryPath); err == nil {
			result.addError(remoteEntryPath, "file exists (use overwrite=true)")
			return
		}
	}

	if err := sftpClient.PutFile(remoteEntryPath, data, info.Mode().Perm()); err != nil {
		result.addError(remoteEntryPath, err.Error())
		return
	}

	if opts.Preserve {
		sftpClient.Chtimes(remoteEntryPath, info.ModTime(), info.ModTime())
	}

	result.FilesTransferred++
	result.TotalBytes += info.Size()
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
		return mcp.NewToolResultError(errSessionIDRequired), nil
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
	startTime := s.clock.Now()

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
	if err := s.fs.MkdirAll(opts.LocalPath, 0755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create local directory: %v", err)), nil
	}

	result := DirTransferResult{Status: "completed"}

	ctx := &remoteWalkContext{
		client:     sftpClient,
		remotePath: remotePath,
		localBase:  opts.LocalPath,
		opts:       opts,
		result:     &result,
	}
	if err = s.walkRemoteDir(ctx, "", 0); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(errWalkDir, err)), nil
	}

	s.finalizeTransferResult(&result, startTime)
	return jsonResult(result)
}

// remoteWalkContext holds the constant context during remote directory traversal.
type remoteWalkContext struct {
	client     *sftp.Client
	remotePath string
	localBase  string
	opts       DirGetOptions
	result     *DirTransferResult
}

// processRemoteEntry handles a single entry during remote directory walk.
func (s *Server) processRemoteEntry(ctx *remoteWalkContext, entry os.FileInfo, entryRelPath string, depth int) error {
	remoteEntryPath := ctx.remotePath + "/" + entryRelPath
	localEntryPath := filepath.Join(ctx.localBase, entryRelPath)

	// Process symlinks
	if entry.Mode()&os.ModeSymlink != 0 {
		action, resolved := s.handleDownloadSymlink(ctx.client, remoteEntryPath, localEntryPath, ctx.opts.Symlinks, ctx.result)
		if action != symlinkFollow {
			return nil // Skip this entry
		}
		entry = resolved
	}

	if entry.IsDir() {
		return s.walkRemoteDir(ctx, entryRelPath, depth+1)
	}

	if !matchesPattern(entryRelPath, ctx.opts.Pattern) {
		return nil
	}

	s.downloadSingleFile(ctx.client, remoteEntryPath, localEntryPath, entry, ctx.opts.Preserve, ctx.result)
	return nil
}

func (s *Server) walkRemoteDir(ctx *remoteWalkContext, relPath string, depth int) error {
	if depth > ctx.opts.MaxDepth {
		return nil
	}

	currentRemote := ctx.remotePath
	if relPath != "" {
		currentRemote = ctx.remotePath + "/" + relPath
	}

	entries, err := ctx.client.ReadDir(currentRemote)
	if err != nil {
		ctx.result.addError(currentRemote, err.Error())
		return nil
	}

	for _, entry := range entries {
		if shouldExclude(entry.Name(), ctx.opts.Exclusions) {
			continue
		}

		entryRelPath := buildRelPath(relPath, entry.Name())
		if err := s.processRemoteEntry(ctx, entry, entryRelPath, depth); err != nil {
			return err
		}
	}

	return nil
}

// buildRelPath constructs a relative path from parent and name.
func buildRelPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func (s *Server) handleLocalDirCopy(srcPath, dstPath string, opts DirGetOptions) (*mcp.CallToolResult, error) {
	startTime := s.clock.Now()

	info, err := s.fs.Stat(srcPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat source: %v", err)), nil
	}
	if !info.IsDir() {
		return mcp.NewToolResultError("source path is not a directory"), nil
	}

	result := DirTransferResult{Status: "completed"}

	err = filepath.WalkDir(srcPath, func(path string, d fs.DirEntry, walkErr error) error {
		return s.processLocalCopyEntry(srcPath, dstPath, path, d, walkErr, opts, &result)
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(errWalkDir, err)), nil
	}

	s.finalizeTransferResult(&result, startTime)
	return jsonResult(result)
}

// processLocalCopyEntry handles a single entry during local directory copy.
func (s *Server) processLocalCopyEntry(
	srcPath, dstPath, path string,
	d fs.DirEntry,
	walkErr error,
	opts DirGetOptions,
	result *DirTransferResult,
) error {
	if walkErr != nil {
		result.addError(path, walkErr.Error())
		return nil
	}

	relPath, _ := filepath.Rel(srcPath, path)
	if relPath == "." {
		return nil
	}

	if shouldExclude(d.Name(), opts.Exclusions) {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	if d.IsDir() {
		return nil
	}

	if !matchesPattern(relPath, opts.Pattern) {
		return nil
	}

	dstEntryPath := filepath.Join(dstPath, relPath)
	s.copyLocalFile(path, dstEntryPath, d, opts.Preserve, result)
	return nil
}

// copyLocalFile copies a single file locally.
func (s *Server) copyLocalFile(srcPath, dstPath string, d fs.DirEntry, preserve bool, result *DirTransferResult) {
	if err := s.fs.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		result.addError(dstPath, err.Error())
		return
	}

	data, err := s.fs.ReadFile(srcPath)
	if err != nil {
		result.addError(srcPath, err.Error())
		return
	}

	info, _ := d.Info()
	if err := s.fs.WriteFile(dstPath, data, info.Mode().Perm()); err != nil {
		result.addError(dstPath, err.Error())
		return
	}

	if preserve {
		s.fs.Chtimes(dstPath, info.ModTime(), info.ModTime())
	}

	result.FilesTransferred++
	result.TotalBytes += info.Size()
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
		return mcp.NewToolResultError(errSessionIDRequired), nil
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
	startTime := s.clock.Now()

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	info, err := s.fs.Stat(localPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat local path: %v", err)), nil
	}
	if !info.IsDir() {
		return mcp.NewToolResultError("local_path is not a directory, use shell_file_put for files"), nil
	}

	if err := sftpClient.MkdirAll(remotePath); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create remote directory: %v", err)), nil
	}

	result := DirTransferResult{Status: "completed"}

	ctx := &uploadWalkContext{
		client:     sftpClient,
		localBase:  localPath,
		remotePath: remotePath,
		opts:       opts,
		result:     &result,
	}
	err = filepath.WalkDir(localPath, func(path string, d fs.DirEntry, walkErr error) error {
		return s.processUploadEntry(ctx, path, d, walkErr)
	})

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(errWalkDir, err)), nil
	}

	s.finalizeTransferResult(&result, startTime)
	return jsonResult(result)
}

// uploadWalkContext holds the constant context during directory upload traversal.
type uploadWalkContext struct {
	client     *sftp.Client
	localBase  string
	remotePath string
	opts       DirPutOptions
	result     *DirTransferResult
}

// processUploadEntry handles a single entry during directory upload.
func (s *Server) processUploadEntry(ctx *uploadWalkContext, path string, d fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		ctx.result.addError(path, walkErr.Error())
		return nil
	}

	relPath, _ := filepath.Rel(ctx.localBase, path)
	if relPath == "." {
		return nil
	}

	if shouldExclude(d.Name(), ctx.opts.Exclusions) {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	remoteEntryPath := ctx.remotePath + "/" + strings.ReplaceAll(relPath, "\\", "/")

	if d.Type()&os.ModeSymlink != 0 {
		action, resolved := s.handleUploadSymlink(ctx.client, path, remoteEntryPath, ctx.opts.Symlinks, ctx.result)
		switch action {
		case symlinkSkip, symlinkHandled, symlinkError:
			return nil
		case symlinkFollow:
			if resolved.IsDir() {
				d = dirEntryFromInfo{resolved}
			}
		}
	}

	if d.IsDir() {
		return nil
	}

	if !matchesPattern(relPath, ctx.opts.Pattern) {
		return nil
	}

	info, _ := d.Info()
	s.uploadSingleFile(ctx.client, path, remoteEntryPath, info, ctx.opts, ctx.result)
	return nil
}

// finalizeTransferResult calculates duration and sets final status.
func (s *Server) finalizeTransferResult(result *DirTransferResult, startTime time.Time) {
	duration := s.clock.Now().Sub(startTime)
	result.DurationMs = duration.Milliseconds()
	if duration.Seconds() > 0 {
		result.BytesPerSecond = int64(float64(result.TotalBytes) / duration.Seconds())
	}
	if len(result.Errors) > 0 {
		result.Status = "completed_with_errors"
	}
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
