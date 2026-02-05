package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/sftp"
	"github.com/mark3labs/mcp-go/mcp"
)

// File size threshold for direct content return (1MB)
const maxContentSize = 1024 * 1024

// registerFileTransferTools registers file transfer MCP tools.
func (s *Server) registerFileTransferTools() {
	s.mcpServer.AddTool(shellFileGetTool(), s.handleShellFileGet)
	s.mcpServer.AddTool(shellFilePutTool(), s.handleShellFilePut)
	s.mcpServer.AddTool(shellFileMvTool(), s.handleShellFileMv)
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
			mcp.Description(descSessionID),
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
			mcp.Description(descSessionID),
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

func shellFileMvTool() mcp.Tool {
	return mcp.NewTool("shell_file_mv",
		mcp.WithDescription(`Move or rename a file in a shell session.

Moves a file from source to destination path. Can be used to rename files
or move them to a different directory.

For SSH sessions, uses SFTP rename which is atomic on most filesystems.
For local sessions, uses os.Rename which is atomic within the same filesystem.

Returns the old and new paths along with file metadata.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description(descSessionID),
		),
		mcp.WithString("source",
			mcp.Required(),
			mcp.Description("Source file path (relative paths use session's cwd)"),
		),
		mcp.WithString("destination",
			mcp.Required(),
			mcp.Description("Destination file path (relative paths use session's cwd)"),
		),
		mcp.WithBoolean("overwrite",
			mcp.Description("Whether to overwrite if destination exists (default: false)"),
		),
		mcp.WithBoolean("create_dirs",
			mcp.Description("Create parent directories of destination if they don't exist (default: false)"),
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

// FileMvResult represents the result of a file move operation.
type FileMvResult struct {
	Status      string `json:"status"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Size        int64  `json:"size"`
	Mode        string `json:"mode,omitempty"`
	DirsCreated bool   `json:"dirs_created,omitempty"`
	Overwritten bool   `json:"overwritten,omitempty"`
}

// FileMvOptions contains options for file move operations.
type FileMvOptions struct {
	Overwrite  bool
	CreateDirs bool
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
		return mcp.NewToolResultError(errSessionIDRequired), nil
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
		return mcp.NewToolResultError(fmt.Sprintf(errGetSFTPClient, err)), nil
	}

	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat remote file: %v", err)), nil
	}
	if info.IsDir() {
		return mcp.NewToolResultError("path is a directory, use recursive option for directories"), nil
	}

	if info.Size() > maxContentSize && opts.LocalPath == "" {
		return mcp.NewToolResultError(fmt.Sprintf("file size (%d bytes) exceeds limit (%d bytes), please specify local_path to save the file", info.Size(), maxContentSize)), nil
	}

	data, _, err := sftpClient.GetFile(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("download file: %v", err)), nil
	}

	result := FileGetResult{
		Status:     "completed",
		RemotePath: remotePath,
		Size:       info.Size(),
		Mode:       fmt.Sprintf("%04o", info.Mode().Perm()),
		ModTime:    info.ModTime().Unix(),
	}

	if errResult := processFileChecksum(data, opts, &result); errResult != nil {
		return errResult, nil
	}

	if opts.LocalPath != "" {
		if errResult := s.copyToLocalPath(data, opts.LocalPath, info, opts.Preserve); errResult != nil {
			return errResult, nil
		}
		result.LocalPath = opts.LocalPath
	} else {
		setContentWithEncoding(data, remotePath, opts, &result)
	}

	return jsonResult(result)
}

func (s *Server) handleLocalFileGet(path string, opts FileGetOptions) (*mcp.CallToolResult, error) {
	info, err := s.fs.Stat(path)
	if err != nil {
		return fileStatError(path, err), nil
	}
	if info.IsDir() {
		return mcp.NewToolResultError("path is a directory, use recursive option for directories"), nil
	}

	if info.Size() > maxContentSize && opts.LocalPath == "" {
		return mcp.NewToolResultError(fmt.Sprintf("file size (%d bytes) exceeds limit (%d bytes), please specify local_path", info.Size(), maxContentSize)), nil
	}

	data, err := s.fs.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("read file: %v", err)), nil
	}

	result := FileGetResult{
		Status:     "completed",
		RemotePath: path,
		Size:       info.Size(),
		Mode:       fmt.Sprintf("%04o", info.Mode().Perm()),
		ModTime:    info.ModTime().Unix(),
	}

	if errResult := processFileChecksum(data, opts, &result); errResult != nil {
		return errResult, nil
	}

	if opts.LocalPath != "" && opts.LocalPath != path {
		if errResult := s.copyToLocalPath(data, opts.LocalPath, info, opts.Preserve); errResult != nil {
			return errResult, nil
		}
		result.LocalPath = opts.LocalPath
	} else {
		setContentWithEncoding(data, path, opts, &result)
	}

	return jsonResult(result)
}

// fileStatError returns appropriate error for file stat failures.
func fileStatError(path string, err error) *mcp.CallToolResult {
	if errors.Is(err, fs.ErrNotExist) {
		return mcp.NewToolResultError(fmt.Sprintf("file not found: %s", path))
	}
	return mcp.NewToolResultError(fmt.Sprintf("stat file: %v", err))
}

// processFileChecksum calculates and verifies checksum if requested.
func processFileChecksum(data []byte, opts FileGetOptions, result *FileGetResult) *mcp.CallToolResult {
	if !opts.Checksum {
		return nil
	}

	hash := sha256.Sum256(data)
	result.Checksum = hex.EncodeToString(hash[:])

	if opts.ExpectedChecksum != "" && !strings.EqualFold(result.Checksum, opts.ExpectedChecksum) {
		return mcp.NewToolResultError(fmt.Sprintf("checksum mismatch: expected %s, got %s", opts.ExpectedChecksum, result.Checksum))
	}
	if opts.ExpectedChecksum != "" {
		result.ChecksumVerified = true
	}
	return nil
}

// copyToLocalPath copies file data to a local path with optional timestamp preservation.
func (s *Server) copyToLocalPath(data []byte, localPath string, info os.FileInfo, preserve bool) *mcp.CallToolResult {
	if err := s.fs.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create directory: %v", err))
	}
	if err := s.fs.WriteFile(localPath, data, info.Mode().Perm()); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write file: %v", err))
	}
	if preserve {
		if err := s.fs.Chtimes(localPath, info.ModTime(), info.ModTime()); err != nil {
			slog.Warn(errPreserveTimestamp, slog.String("error", err.Error()))
		}
	}
	return nil
}

// setContentWithEncoding sets result content with appropriate encoding and compression.
func setContentWithEncoding(data []byte, path string, opts FileGetOptions, result *FileGetResult) {
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

// parseFilePutMode parses the mode string and updates opts.Mode.
func parseFilePutMode(modeStr string, opts *FilePutOptions) *mcp.CallToolResult {
	if modeStr == "" {
		return nil
	}
	var mode uint32
	if _, err := fmt.Sscanf(modeStr, "%o", &mode); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid mode '%s': %v", modeStr, err))
	}
	opts.Mode = os.FileMode(mode)
	return nil
}

// validateFilePutInputs validates the required inputs for file put.
func validateFilePutInputs(sessionID, remotePath string, opts FilePutOptions) *mcp.CallToolResult {
	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired)
	}
	if remotePath == "" {
		return mcp.NewToolResultError("remote_path is required")
	}
	if opts.Content == "" && opts.LocalPath == "" {
		return mcp.NewToolResultError("either content or local_path is required")
	}
	return nil
}

// resolveFileContent reads content from local file or decodes provided content.
func (s *Server) resolveFileContent(opts FilePutOptions) ([]byte, time.Time, *mcp.CallToolResult) {
	if opts.LocalPath != "" {
		info, err := s.fs.Stat(opts.LocalPath)
		if err != nil {
			return nil, time.Time{}, mcp.NewToolResultError(fmt.Sprintf("stat local file: %v", err))
		}
		data, err := s.fs.ReadFile(opts.LocalPath)
		if err != nil {
			return nil, time.Time{}, mcp.NewToolResultError(fmt.Sprintf("read local file: %v", err))
		}
		return data, info.ModTime(), nil
	}

	if opts.Encoding == "base64" {
		data, err := base64.StdEncoding.DecodeString(opts.Content)
		if err != nil {
			return nil, time.Time{}, mcp.NewToolResultError(fmt.Sprintf("decode base64 content: %v", err))
		}
		return data, time.Time{}, nil
	}
	return []byte(opts.Content), time.Time{}, nil
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

	if errResult := parseFilePutMode(mcp.ParseString(req, "mode", ""), &opts); errResult != nil {
		return errResult, nil
	}

	if errResult := validateFilePutInputs(sessionID, remotePath, opts); errResult != nil {
		return errResult, nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resolvedPath := sess.ResolvePath(remotePath)
	slog.Info("uploading file", slog.String("session_id", sessionID), slog.String("remote_path", resolvedPath), slog.Bool("atomic", opts.Atomic))

	data, sourceModTime, errResult := s.resolveFileContent(opts)
	if errResult != nil {
		return errResult, nil
	}

	if sess.IsSSH() {
		return s.handleSSHFilePut(sess, resolvedPath, data, opts, sourceModTime)
	}
	return s.handleLocalFilePut(resolvedPath, data, opts, sourceModTime)
}

func (s *Server) handleSSHFilePut(sess *session.Session, remotePath string, data []byte, opts FilePutOptions, sourceModTime time.Time) (*mcp.CallToolResult, error) {
	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(errGetSFTPClient, err)), nil
	}

	result := newFilePutResult(remotePath, data, opts.Mode)
	setPutChecksum(data, opts.Checksum, &result)

	if errResult := checkSSHFileOverwrite(sftpClient, remotePath, opts.Overwrite, &result); errResult != nil {
		return errResult, nil
	}

	dir := strings.ReplaceAll(filepath.Dir(remotePath), "\\", "/")
	if opts.CreateDirs {
		if err := sftpClient.MkdirAll(dir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(errCreateDirs, err)), nil
		}
		result.DirsCreated = true
	}

	if errResult := writeSSHFile(sftpClient, remotePath, dir, data, opts, &result); errResult != nil {
		return errResult, nil
	}

	preserveSSHTimestamp(sftpClient, remotePath, opts.Preserve, sourceModTime)
	return jsonResult(result)
}

// newFilePutResult creates a new FilePutResult with common fields.
func newFilePutResult(path string, data []byte, mode os.FileMode) FilePutResult {
	return FilePutResult{
		Status:     "completed",
		RemotePath: path,
		Size:       int64(len(data)),
		Mode:       fmt.Sprintf("%04o", mode),
	}
}

// setPutChecksum calculates and sets checksum if requested.
func setPutChecksum(data []byte, calculate bool, result *FilePutResult) {
	if calculate {
		hash := sha256.Sum256(data)
		result.Checksum = hex.EncodeToString(hash[:])
	}
}

// checkSSHFileOverwrite checks if SSH file exists and handles overwrite logic.
func checkSSHFileOverwrite(client *sftp.Client, path string, overwrite bool, result *FilePutResult) *mcp.CallToolResult {
	_, err := client.Stat(path)
	fileExists := err == nil
	if fileExists && !overwrite {
		return mcp.NewToolResultError(fmt.Sprintf("file exists: %s (use overwrite=true to replace)", path))
	}
	if fileExists {
		result.Overwritten = true
	}
	return nil
}

// writeSSHFile writes data to SSH server with optional atomic write.
func writeSSHFile(client *sftp.Client, remotePath, dir string, data []byte, opts FilePutOptions, result *FilePutResult) *mcp.CallToolResult {
	if !opts.Atomic {
		if err := client.PutFile(remotePath, data, opts.Mode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("upload file: %v", err))
		}
		return nil
	}

	tempPath := fmt.Sprintf("%s/.%s.tmp.%s", dir, filepath.Base(remotePath), randomSuffix())
	if err := client.PutFile(tempPath, data, opts.Mode); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("upload temp file: %v", err))
	}

	if err := client.Rename(tempPath, remotePath); err != nil {
		client.Remove(tempPath)
		return mcp.NewToolResultError(fmt.Sprintf("rename to final path: %v", err))
	}
	result.AtomicWrite = true
	return nil
}

// preserveSSHTimestamp sets file timestamps if preserve is requested.
func preserveSSHTimestamp(client *sftp.Client, path string, preserve bool, modTime time.Time) {
	if preserve && !modTime.IsZero() {
		if err := client.Chtimes(path, modTime, modTime); err != nil {
			slog.Warn(errPreserveTimestamp, slog.String("error", err.Error()))
		}
	}
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
	result := newFilePutResult(path, data, opts.Mode)
	setPutChecksum(data, opts.Checksum, &result)

	if errResult := s.checkLocalFileOverwrite(path, opts.Overwrite, &result); errResult != nil {
		return errResult, nil
	}

	dir := filepath.Dir(path)
	if opts.CreateDirs {
		if err := s.fs.MkdirAll(dir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(errCreateDirs, err)), nil
		}
		result.DirsCreated = true
	}

	if errResult := s.writeLocalFile(path, dir, data, opts, &result); errResult != nil {
		return errResult, nil
	}

	s.preserveLocalTimestamp(path, opts.Preserve, sourceModTime)
	return jsonResult(result)
}

// checkLocalFileOverwrite checks if local file exists and handles overwrite logic.
func (s *Server) checkLocalFileOverwrite(path string, overwrite bool, result *FilePutResult) *mcp.CallToolResult {
	_, err := s.fs.Stat(path)
	fileExists := err == nil
	if fileExists && !overwrite {
		return mcp.NewToolResultError(fmt.Sprintf("file exists: %s (use overwrite=true to replace)", path))
	}
	if fileExists {
		result.Overwritten = true
	}
	return nil
}

// writeLocalFile writes data to local file with optional atomic write.
func (s *Server) writeLocalFile(path, dir string, data []byte, opts FilePutOptions, result *FilePutResult) *mcp.CallToolResult {
	if !opts.Atomic {
		if err := s.fs.WriteFile(path, data, opts.Mode); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write file: %v", err))
		}
		return nil
	}

	tempPath := filepath.Join(dir, fmt.Sprintf(".%s.tmp.%s", filepath.Base(path), randomSuffix()))
	if err := s.fs.WriteFile(tempPath, data, opts.Mode); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("write temp file: %v", err))
	}

	if err := s.fs.Rename(tempPath, path); err != nil {
		s.fs.Remove(tempPath)
		return mcp.NewToolResultError(fmt.Sprintf("rename to final path: %v", err))
	}
	result.AtomicWrite = true
	return nil
}

// preserveLocalTimestamp sets file timestamps if preserve is requested.
func (s *Server) preserveLocalTimestamp(path string, preserve bool, modTime time.Time) {
	if preserve && !modTime.IsZero() {
		if err := s.fs.Chtimes(path, modTime, modTime); err != nil {
			slog.Warn(errPreserveTimestamp, slog.String("error", err.Error()))
		}
	}
}

func (s *Server) handleShellFileMv(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	source := mcp.ParseString(req, "source", "")
	destination := mcp.ParseString(req, "destination", "")

	opts := FileMvOptions{
		Overwrite:  mcp.ParseBoolean(req, "overwrite", false),
		CreateDirs: mcp.ParseBoolean(req, "create_dirs", false),
	}

	if sessionID == "" {
		return mcp.NewToolResultError(errSessionIDRequired), nil
	}
	if source == "" {
		return mcp.NewToolResultError("source is required"), nil
	}
	if destination == "" {
		return mcp.NewToolResultError("destination is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Resolve relative paths using session's cwd
	resolvedSource := sess.ResolvePath(source)
	resolvedDest := sess.ResolvePath(destination)

	slog.Info("moving file",
		slog.String("session_id", sessionID),
		slog.String("source", resolvedSource),
		slog.String("destination", resolvedDest),
	)

	// Handle based on session mode
	if sess.IsSSH() {
		return s.handleSSHFileMv(sess, resolvedSource, resolvedDest, opts)
	}
	return s.handleLocalFileMv(resolvedSource, resolvedDest, opts)
}

func (s *Server) handleSSHFileMv(sess *session.Session, source, destination string, opts FileMvOptions) (*mcp.CallToolResult, error) {
	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(errGetSFTPClient, err)), nil
	}

	// Check source file exists and get metadata
	srcInfo, err := sftpClient.Stat(source)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("source file not found: %v", err)), nil
	}

	if srcInfo.IsDir() {
		return mcp.NewToolResultError("source is a directory, use shell_dir_* tools for directories"), nil
	}

	result := FileMvResult{
		Status:      "completed",
		Source:      source,
		Destination: destination,
		Size:        srcInfo.Size(),
		Mode:        fmt.Sprintf("%04o", srcInfo.Mode().Perm()),
	}

	// Check if destination exists
	_, err = sftpClient.Stat(destination)
	destExists := err == nil
	if destExists && !opts.Overwrite {
		return mcp.NewToolResultError(fmt.Sprintf("destination exists: %s (use overwrite=true to replace)", destination)), nil
	}
	if destExists {
		// Remove existing destination before rename
		if err := sftpClient.Remove(destination); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("remove existing destination: %v", err)), nil
		}
		result.Overwritten = true
	}

	// Create parent directories if requested
	destDir := filepath.Dir(destination)
	destDir = strings.ReplaceAll(destDir, "\\", "/")
	if opts.CreateDirs {
		if err := sftpClient.MkdirAll(destDir); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(errCreateDirs, err)), nil
		}
		result.DirsCreated = true
	}

	// Perform the rename/move
	if err := sftpClient.Rename(source, destination); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move file: %v", err)), nil
	}

	return jsonResult(result)
}

func (s *Server) handleLocalFileMv(source, destination string, opts FileMvOptions) (*mcp.CallToolResult, error) {
	// Check source file exists and get metadata
	srcInfo, err := s.fs.Stat(source)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return mcp.NewToolResultError(fmt.Sprintf("source file not found: %s", source)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("stat source: %v", err)), nil
	}

	if srcInfo.IsDir() {
		return mcp.NewToolResultError("source is a directory, use shell_dir_* tools for directories"), nil
	}

	result := FileMvResult{
		Status:      "completed",
		Source:      source,
		Destination: destination,
		Size:        srcInfo.Size(),
		Mode:        fmt.Sprintf("%04o", srcInfo.Mode().Perm()),
	}

	// Check if destination exists
	_, err = s.fs.Stat(destination)
	destExists := err == nil
	if destExists && !opts.Overwrite {
		return mcp.NewToolResultError(fmt.Sprintf("destination exists: %s (use overwrite=true to replace)", destination)), nil
	}
	if destExists {
		result.Overwritten = true
	}

	// Create parent directories if requested
	destDir := filepath.Dir(destination)
	if opts.CreateDirs {
		if err := s.fs.MkdirAll(destDir, 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(errCreateDirs, err)), nil
		}
		result.DirsCreated = true
	}

	// Perform the rename/move
	if err := s.fs.Rename(source, destination); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("move file: %v", err)), nil
	}

	return jsonResult(result)
}
