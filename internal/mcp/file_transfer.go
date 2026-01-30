package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/mark3labs/mcp-go/mcp"
)

// File size threshold for direct content return (1MB)
const maxContentSize = 1024 * 1024

// registerFileTransferTools registers file transfer MCP tools.
func (s *Server) registerFileTransferTools() {
	s.mcpServer.AddTool(shellFileGetTool(), s.handleShellFileGet)
	s.mcpServer.AddTool(shellFilePutTool(), s.handleShellFilePut)
}

func shellFileGetTool() mcp.Tool {
	return mcp.NewTool("shell_file_get",
		mcp.WithDescription(`Download a file from a remote SSH session.

For small files (<1MB), returns the content directly in the response.
For large files, use the local_path parameter to save to a local file.

For local sessions, use this tool to read files using the session's working directory context.

Returns file metadata (size, permissions, modification time) along with content.
Optionally calculates SHA256 checksum for verification.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
		mcp.WithString("remote_path",
			mcp.Required(),
			mcp.Description("Path to the file on the remote server (relative paths use session's cwd)"),
		),
		mcp.WithString("encoding",
			mcp.Description("Content encoding: 'text' (default) or 'base64' for binary files"),
			mcp.DefaultString("text"),
		),
		mcp.WithString("local_path",
			mcp.Description("Local path to save the file (required for files >1MB)"),
		),
		mcp.WithBoolean("checksum",
			mcp.Description("Calculate and return SHA256 checksum (default: true)"),
		),
		mcp.WithString("expected_checksum",
			mcp.Description("Expected SHA256 checksum to verify against"),
		),
		mcp.WithBoolean("preserve",
			mcp.Description("Preserve file timestamps when saving to local_path (default: true)"),
		),
		mcp.WithBoolean("compress",
			mcp.Description("Compress content with gzip (for text files, reduces transfer size)"),
		),
	)
}

func shellFilePutTool() mcp.Tool {
	return mcp.NewTool("shell_file_put",
		mcp.WithDescription(`Upload a file to a remote SSH session.

Provide content directly for small files, or use local_path to upload from a local file.
Uses atomic writes (temp file + rename) by default to prevent partial files.

For local sessions, use this tool to write files using the session's working directory context.

Returns upload status, file metadata, and SHA256 checksum.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
		mcp.WithString("remote_path",
			mcp.Required(),
			mcp.Description("Destination path on the remote server (relative paths use session's cwd)"),
		),
		mcp.WithString("content",
			mcp.Description("File content to upload (for small files)"),
		),
		mcp.WithString("encoding",
			mcp.Description("Content encoding: 'text' (default) or 'base64' for binary content"),
			mcp.DefaultString("text"),
		),
		mcp.WithString("local_path",
			mcp.Description("Local file path to upload from (alternative to content)"),
		),
		mcp.WithString("mode",
			mcp.Description("File permissions in octal (e.g., '0644')"),
		),
		mcp.WithBoolean("overwrite",
			mcp.Description("Whether to overwrite if file exists (default: false)"),
		),
		mcp.WithBoolean("create_dirs",
			mcp.Description("Create parent directories if they don't exist (default: false)"),
		),
		mcp.WithBoolean("atomic",
			mcp.Description("Use atomic write (temp file + rename) to prevent partial files (default: true)"),
		),
		mcp.WithBoolean("checksum",
			mcp.Description("Calculate and return SHA256 checksum (default: true)"),
		),
		mcp.WithBoolean("preserve",
			mcp.Description("Preserve timestamps from local file when uploading (default: false)"),
		),
		mcp.WithBoolean("compress",
			mcp.Description("Compress content with gzip before upload (for text files)"),
		),
	)
}

// FileGetResult represents the result of a file get operation.
type FileGetResult struct {
	Status           string  `json:"status"`
	RemotePath       string  `json:"remote_path"`
	LocalPath        string  `json:"local_path,omitempty"`
	Size             int64   `json:"size"`
	Mode             string  `json:"mode"`
	ModTime          int64   `json:"mod_time"`
	Content          string  `json:"content,omitempty"`
	Encoding         string  `json:"encoding,omitempty"`
	ContentSize      int     `json:"content_size,omitempty"`
	Truncated        bool    `json:"truncated,omitempty"`
	Checksum         string  `json:"checksum,omitempty"`
	ChecksumVerified bool    `json:"checksum_verified,omitempty"`
	Compressed       bool    `json:"compressed,omitempty"`
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
}

// FilePutResult represents the result of a file put operation.
type FilePutResult struct {
	Status           string  `json:"status"`
	RemotePath       string  `json:"remote_path"`
	Size             int64   `json:"size"`
	Mode             string  `json:"mode,omitempty"`
	DirsCreated      bool    `json:"dirs_created,omitempty"`
	Overwritten      bool    `json:"overwritten,omitempty"`
	Checksum         string  `json:"checksum,omitempty"`
	AtomicWrite      bool    `json:"atomic_write,omitempty"`
	Compressed       bool    `json:"compressed,omitempty"`
	OriginalSize     int64   `json:"original_size,omitempty"`
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
}

// FileGetOptions contains options for file get operations.
type FileGetOptions struct {
	Encoding         string
	LocalPath        string
	Checksum         bool
	ExpectedChecksum string
	Preserve         bool
	Compress         bool
}

func (s *Server) handleShellFileGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	remotePath := mcp.ParseString(req, "remote_path", "")

	opts := FileGetOptions{
		Encoding:         mcp.ParseString(req, "encoding", "text"),
		LocalPath:        mcp.ParseString(req, "local_path", ""),
		Checksum:         mcp.ParseBoolean(req, "checksum", true),
		ExpectedChecksum: mcp.ParseString(req, "expected_checksum", ""),
		Preserve:         mcp.ParseBoolean(req, "preserve", true),
		Compress:         mcp.ParseBoolean(req, "compress", false),
	}

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if remotePath == "" {
		return mcp.NewToolResultError("remote_path is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Resolve relative path using session's cwd
	resolvedPath := sess.ResolvePath(remotePath)

	slog.Info("downloading file",
		slog.String("session_id", sessionID),
		slog.String("remote_path", resolvedPath),
		slog.String("encoding", opts.Encoding),
	)

	// Handle based on session mode
	if sess.IsSSH() {
		return s.handleSSHFileGet(sess, resolvedPath, opts)
	}
	return s.handleLocalFileGet(resolvedPath, opts)
}

func (s *Server) handleSSHFileGet(sess *session.Session, remotePath string, opts FileGetOptions) (*mcp.CallToolResult, error) {
	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	// First, stat the file to get metadata and check size
	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat remote file: %v", err)), nil
	}

	if info.IsDir() {
		return mcp.NewToolResultError("path is a directory, use recursive option for directories"), nil
	}

	result := FileGetResult{
		Status:     "completed",
		RemotePath: remotePath,
		Size:       info.Size(),
		Mode:       fmt.Sprintf("%04o", info.Mode().Perm()),
		ModTime:    info.ModTime().Unix(),
	}

	// For large files, require local_path
	if info.Size() > maxContentSize && opts.LocalPath == "" {
		return mcp.NewToolResultError(fmt.Sprintf("file size (%d bytes) exceeds limit (%d bytes), please specify local_path to save the file", info.Size(), maxContentSize)), nil
	}

	// Download file content
	data, _, err := sftpClient.GetFile(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("download file: %v", err)), nil
	}

	// Calculate checksum if requested
	if opts.Checksum {
		hash := sha256.Sum256(data)
		result.Checksum = hex.EncodeToString(hash[:])

		// Verify against expected checksum if provided
		if opts.ExpectedChecksum != "" {
			if strings.EqualFold(result.Checksum, opts.ExpectedChecksum) {
				result.ChecksumVerified = true
			} else {
				return mcp.NewToolResultError(fmt.Sprintf("checksum mismatch: expected %s, got %s", opts.ExpectedChecksum, result.Checksum)), nil
			}
		}
	}

	// Save to local file if specified
	if opts.LocalPath != "" {
		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(opts.LocalPath), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("create local directory: %v", err)), nil
		}
		if err := os.WriteFile(opts.LocalPath, data, info.Mode().Perm()); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write local file: %v", err)), nil
		}

		// Preserve timestamps if requested
		if opts.Preserve {
			if err := os.Chtimes(opts.LocalPath, info.ModTime(), info.ModTime()); err != nil {
				slog.Warn("failed to preserve timestamps", slog.String("error", err.Error()))
			}
		}

		result.LocalPath = opts.LocalPath
	} else {
		// Compress if requested and file is compressible
		contentData := data
		if opts.Compress && isCompressible(remotePath) {
			compressed, err := compressData(data)
			if err == nil && len(compressed) < len(data) {
				contentData = compressed
				result.Compressed = true
				result.CompressionRatio = float64(len(compressed)) / float64(len(data))
			}
		}

		// Return content directly
		result.ContentSize = len(contentData)
		if opts.Encoding == "base64" || result.Compressed {
			result.Content = base64.StdEncoding.EncodeToString(contentData)
			result.Encoding = "base64"
		} else {
			result.Content = string(contentData)
			result.Encoding = "text"
		}
	}

	return jsonResult(result)
}

func (s *Server) handleLocalFileGet(path string, opts FileGetOptions) (*mcp.CallToolResult, error) {
	// For local sessions, read directly from filesystem
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("file not found: %s", path)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("stat file: %v", err)), nil
	}

	if info.IsDir() {
		return mcp.NewToolResultError("path is a directory, use recursive option for directories"), nil
	}

	result := FileGetResult{
		Status:     "completed",
		RemotePath: path,
		Size:       info.Size(),
		Mode:       fmt.Sprintf("%04o", info.Mode().Perm()),
		ModTime:    info.ModTime().Unix(),
	}

	// For large files, require local_path
	if info.Size() > maxContentSize && opts.LocalPath == "" {
		return mcp.NewToolResultError(fmt.Sprintf("file size (%d bytes) exceeds limit (%d bytes), please specify local_path", info.Size(), maxContentSize)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read file: %v", err)), nil
	}

	// Calculate checksum if requested
	if opts.Checksum {
		hash := sha256.Sum256(data)
		result.Checksum = hex.EncodeToString(hash[:])

		// Verify against expected checksum if provided
		if opts.ExpectedChecksum != "" {
			if strings.EqualFold(result.Checksum, opts.ExpectedChecksum) {
				result.ChecksumVerified = true
			} else {
				return mcp.NewToolResultError(fmt.Sprintf("checksum mismatch: expected %s, got %s", opts.ExpectedChecksum, result.Checksum)), nil
			}
		}
	}

	// Copy to local path if specified
	if opts.LocalPath != "" && opts.LocalPath != path {
		if err := os.MkdirAll(filepath.Dir(opts.LocalPath), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("create directory: %v", err)), nil
		}
		if err := os.WriteFile(opts.LocalPath, data, info.Mode().Perm()); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write file: %v", err)), nil
		}

		// Preserve timestamps if requested
		if opts.Preserve {
			if err := os.Chtimes(opts.LocalPath, info.ModTime(), info.ModTime()); err != nil {
				slog.Warn("failed to preserve timestamps", slog.String("error", err.Error()))
			}
		}

		result.LocalPath = opts.LocalPath
	} else {
		// Compress if requested and file is compressible
		contentData := data
		if opts.Compress && isCompressible(path) {
			compressed, err := compressData(data)
			if err == nil && len(compressed) < len(data) {
				contentData = compressed
				result.Compressed = true
				result.CompressionRatio = float64(len(compressed)) / float64(len(data))
			}
		}

		result.ContentSize = len(contentData)
		if opts.Encoding == "base64" || result.Compressed {
			result.Content = base64.StdEncoding.EncodeToString(contentData)
			result.Encoding = "base64"
		} else {
			result.Content = string(contentData)
			result.Encoding = "text"
		}
	}

	return jsonResult(result)
}

// FilePutOptions contains options for file put operations.
type FilePutOptions struct {
	Content    string
	Encoding   string
	LocalPath  string
	Mode       os.FileMode
	Overwrite  bool
	CreateDirs bool
	Atomic     bool
	Checksum   bool
	Preserve   bool
	Compress   bool
}

func (s *Server) handleShellFilePut(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	remotePath := mcp.ParseString(req, "remote_path", "")

	opts := FilePutOptions{
		Content:    mcp.ParseString(req, "content", ""),
		Encoding:   mcp.ParseString(req, "encoding", "text"),
		LocalPath:  mcp.ParseString(req, "local_path", ""),
		Mode:       0644,
		Overwrite:  mcp.ParseBoolean(req, "overwrite", false),
		CreateDirs: mcp.ParseBoolean(req, "create_dirs", false),
		Atomic:     mcp.ParseBoolean(req, "atomic", true),
		Checksum:   mcp.ParseBoolean(req, "checksum", true),
		Preserve:   mcp.ParseBoolean(req, "preserve", false),
		Compress:   mcp.ParseBoolean(req, "compress", false),
	}

	// Parse file mode
	modeStr := mcp.ParseString(req, "mode", "")
	if modeStr != "" {
		var mode uint32
		if _, err := fmt.Sscanf(modeStr, "%o", &mode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid mode '%s': %v", modeStr, err)), nil
		}
		opts.Mode = os.FileMode(mode)
	}

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if remotePath == "" {
		return mcp.NewToolResultError("remote_path is required"), nil
	}
	if opts.Content == "" && opts.LocalPath == "" {
		return mcp.NewToolResultError("either content or local_path is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Resolve relative path using session's cwd
	resolvedPath := sess.ResolvePath(remotePath)

	slog.Info("uploading file",
		slog.String("session_id", sessionID),
		slog.String("remote_path", resolvedPath),
		slog.Bool("atomic", opts.Atomic),
	)

	// Determine file content and source modification time
	var data []byte
	var sourceModTime time.Time
	if opts.LocalPath != "" {
		// Read from local file
		info, err := os.Stat(opts.LocalPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("stat local file: %v", err)), nil
		}
		sourceModTime = info.ModTime()

		data, err = os.ReadFile(opts.LocalPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("read local file: %v", err)), nil
		}
	} else {
		// Use provided content
		if opts.Encoding == "base64" {
			var err error
			data, err = base64.StdEncoding.DecodeString(opts.Content)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("decode base64 content: %v", err)), nil
			}
		} else {
			data = []byte(opts.Content)
		}
	}

	// Handle based on session mode
	if sess.IsSSH() {
		return s.handleSSHFilePut(sess, resolvedPath, data, opts, sourceModTime)
	}
	return s.handleLocalFilePut(resolvedPath, data, opts, sourceModTime)
}

func (s *Server) handleSSHFilePut(sess *session.Session, remotePath string, data []byte, opts FilePutOptions, sourceModTime time.Time) (*mcp.CallToolResult, error) {
	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	result := FilePutResult{
		Status:     "completed",
		RemotePath: remotePath,
		Size:       int64(len(data)),
		Mode:       fmt.Sprintf("%04o", opts.Mode),
	}

	// Calculate checksum if requested
	if opts.Checksum {
		hash := sha256.Sum256(data)
		result.Checksum = hex.EncodeToString(hash[:])
	}

	// Check if file exists
	_, err = sftpClient.Stat(remotePath)
	fileExists := err == nil
	if fileExists && !opts.Overwrite {
		return mcp.NewToolResultError(fmt.Sprintf("file exists: %s (use overwrite=true to replace)", remotePath)), nil
	}
	if fileExists {
		result.Overwritten = true
	}

	// Create parent directories if requested
	dir := filepath.Dir(remotePath)
	dir = strings.ReplaceAll(dir, "\\", "/")
	if opts.CreateDirs {
		if err := sftpClient.MkdirAll(dir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("create directories: %v", err)), nil
		}
		result.DirsCreated = true
	}

	// Upload file (with atomic write if requested)
	if opts.Atomic {
		// Generate temp file name
		tempPath := fmt.Sprintf("%s/.%s.tmp.%s", dir, filepath.Base(remotePath), randomSuffix())

		// Write to temp file
		if err := sftpClient.PutFile(tempPath, data, opts.Mode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("upload temp file: %v", err)), nil
		}

		// Rename to final destination
		if err := sftpClient.Rename(tempPath, remotePath); err != nil {
			// Try to clean up temp file on failure
			sftpClient.Remove(tempPath)
			return mcp.NewToolResultError(fmt.Sprintf("rename to final path: %v", err)), nil
		}

		result.AtomicWrite = true
	} else {
		// Direct write
		if err := sftpClient.PutFile(remotePath, data, opts.Mode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("upload file: %v", err)), nil
		}
	}

	// Preserve timestamps if requested
	if opts.Preserve && !sourceModTime.IsZero() {
		if err := sftpClient.Chtimes(remotePath, sourceModTime, sourceModTime); err != nil {
			slog.Warn("failed to preserve timestamps", slog.String("error", err.Error()))
		}
	}

	return jsonResult(result)
}

// randomSuffix generates a random 8-character hex suffix for temp files.
func randomSuffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// compressData compresses data using gzip.
func compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("gzip write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// decompressData decompresses gzip data.
func decompressData(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer r.Close()

	decompressed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("gzip read: %w", err)
	}
	return decompressed, nil
}

// isCompressible checks if a file is likely to benefit from compression.
func isCompressible(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	// Text-based formats that compress well
	compressible := map[string]bool{
		".txt": true, ".log": true, ".json": true, ".xml": true,
		".yaml": true, ".yml": true, ".md": true, ".csv": true,
		".html": true, ".htm": true, ".css": true, ".js": true,
		".ts": true, ".tsx": true, ".jsx": true, ".go": true,
		".py": true, ".rb": true, ".java": true, ".c": true,
		".cpp": true, ".h": true, ".hpp": true, ".rs": true,
		".sh": true, ".bash": true, ".zsh": true, ".sql": true,
		".conf": true, ".cfg": true, ".ini": true, ".toml": true,
	}
	return compressible[ext]
}

func (s *Server) handleLocalFilePut(path string, data []byte, opts FilePutOptions, sourceModTime time.Time) (*mcp.CallToolResult, error) {
	result := FilePutResult{
		Status:     "completed",
		RemotePath: path,
		Size:       int64(len(data)),
		Mode:       fmt.Sprintf("%04o", opts.Mode),
	}

	// Calculate checksum if requested
	if opts.Checksum {
		hash := sha256.Sum256(data)
		result.Checksum = hex.EncodeToString(hash[:])
	}

	// Check if file exists
	_, err := os.Stat(path)
	fileExists := err == nil
	if fileExists && !opts.Overwrite {
		return mcp.NewToolResultError(fmt.Sprintf("file exists: %s (use overwrite=true to replace)", path)), nil
	}
	if fileExists {
		result.Overwritten = true
	}

	// Create parent directories if requested
	dir := filepath.Dir(path)
	if opts.CreateDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("create directories: %v", err)), nil
		}
		result.DirsCreated = true
	}

	// Write file (with atomic write if requested)
	if opts.Atomic {
		// Generate temp file name
		tempPath := filepath.Join(dir, fmt.Sprintf(".%s.tmp.%s", filepath.Base(path), randomSuffix()))

		// Write to temp file
		if err := os.WriteFile(tempPath, data, opts.Mode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write temp file: %v", err)), nil
		}

		// Rename to final destination
		if err := os.Rename(tempPath, path); err != nil {
			// Try to clean up temp file on failure
			os.Remove(tempPath)
			return mcp.NewToolResultError(fmt.Sprintf("rename to final path: %v", err)), nil
		}

		result.AtomicWrite = true
	} else {
		// Direct write
		if err := os.WriteFile(path, data, opts.Mode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write file: %v", err)), nil
		}
	}

	// Preserve timestamps if requested
	if opts.Preserve && !sourceModTime.IsZero() {
		if err := os.Chtimes(path, sourceModTime, sourceModTime); err != nil {
			slog.Warn("failed to preserve timestamps", slog.String("error", err.Error()))
		}
	}

	return jsonResult(result)
}
