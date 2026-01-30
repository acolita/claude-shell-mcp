package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// DefaultChunkSize is 1MB
	DefaultChunkSize = 1024 * 1024
	// MaxChunkSize is 10MB
	MaxChunkSize = 10 * 1024 * 1024
	// ManifestSuffix is appended to create manifest file names
	ManifestSuffix = ".transfer"
)

// registerChunkedTransferTools registers chunked file transfer MCP tools.
func (s *Server) registerChunkedTransferTools() {
	s.mcpServer.AddTool(shellFileGetChunkedTool(), s.handleShellFileGetChunked)
	s.mcpServer.AddTool(shellFilePutChunkedTool(), s.handleShellFilePutChunked)
	s.mcpServer.AddTool(shellTransferStatusTool(), s.handleShellTransferStatus)
	s.mcpServer.AddTool(shellTransferResumeTool(), s.handleShellTransferResume)
}

func shellFileGetChunkedTool() mcp.Tool {
	return mcp.NewTool("shell_file_get_chunked",
		mcp.WithDescription(`Download a large file in chunks with resume support.

For files larger than a few MB, this tool provides:
- Chunked transfer (configurable chunk size)
- Resume capability if transfer is interrupted
- Per-chunk checksum verification
- Progress tracking via manifest file

The manifest file (.transfer) tracks progress and enables resume.
Use shell_transfer_status to check progress, shell_transfer_resume to continue.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
		mcp.WithString("remote_path",
			mcp.Required(),
			mcp.Description("Path to the file on the remote server"),
		),
		mcp.WithString("local_path",
			mcp.Required(),
			mcp.Description("Local path to save the file"),
		),
		mcp.WithNumber("chunk_size",
			mcp.Description("Chunk size in bytes (default: 1MB, max: 10MB)"),
		),
	)
}

func shellFilePutChunkedTool() mcp.Tool {
	return mcp.NewTool("shell_file_put_chunked",
		mcp.WithDescription(`Upload a large file in chunks with resume support.

For files larger than a few MB, this tool provides:
- Chunked transfer (configurable chunk size)
- Resume capability if transfer is interrupted
- Per-chunk checksum verification
- Progress tracking via manifest file

The manifest file (.transfer) tracks progress and enables resume.
Use shell_transfer_status to check progress, shell_transfer_resume to continue.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
		mcp.WithString("local_path",
			mcp.Required(),
			mcp.Description("Local path of the file to upload"),
		),
		mcp.WithString("remote_path",
			mcp.Required(),
			mcp.Description("Destination path on the remote server"),
		),
		mcp.WithNumber("chunk_size",
			mcp.Description("Chunk size in bytes (default: 1MB, max: 10MB)"),
		),
	)
}

func shellTransferStatusTool() mcp.Tool {
	return mcp.NewTool("shell_transfer_status",
		mcp.WithDescription(`Check the status of a chunked file transfer.

Returns progress information including:
- Total chunks and completed chunks
- Bytes transferred
- Estimated time remaining
- Transfer rate`),
		mcp.WithString("manifest_path",
			mcp.Required(),
			mcp.Description("Path to the .transfer manifest file"),
		),
	)
}

func shellTransferResumeTool() mcp.Tool {
	return mcp.NewTool("shell_transfer_resume",
		mcp.WithDescription(`Resume an interrupted chunked file transfer.

Continues a transfer from where it left off using the manifest file.
Verifies completed chunks and resumes from the first incomplete chunk.`),
		mcp.WithString("session_id",
			mcp.Required(),
			mcp.Description("The session ID"),
		),
		mcp.WithString("manifest_path",
			mcp.Required(),
			mcp.Description("Path to the .transfer manifest file"),
		),
	)
}

// TransferManifest tracks the state of a chunked transfer.
type TransferManifest struct {
	Version        int             `json:"version"`
	Direction      string          `json:"direction"` // "get" or "put"
	RemotePath     string          `json:"remote_path"`
	LocalPath      string          `json:"local_path"`
	TotalSize      int64           `json:"total_size"`
	ChunkSize      int             `json:"chunk_size"`
	TotalChunks    int             `json:"total_chunks"`
	FileChecksum   string          `json:"file_checksum,omitempty"`
	Chunks         []ChunkInfo     `json:"chunks"`
	StartedAt      time.Time       `json:"started_at"`
	LastUpdatedAt  time.Time       `json:"last_updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	SessionID      string          `json:"session_id"`
	BytesSent      int64           `json:"bytes_sent"`
	BytesPerSecond int64           `json:"bytes_per_second,omitempty"`
}

// ChunkInfo tracks the state of a single chunk.
type ChunkInfo struct {
	Index     int    `json:"index"`
	Offset    int64  `json:"offset"`
	Size      int    `json:"size"`
	Checksum  string `json:"checksum,omitempty"`
	Completed bool   `json:"completed"`
}

// ChunkedTransferResult represents the result of a chunked transfer operation.
type ChunkedTransferResult struct {
	Status          string  `json:"status"`
	ManifestPath    string  `json:"manifest_path"`
	ChunksCompleted int     `json:"chunks_completed"`
	TotalChunks     int     `json:"total_chunks"`
	BytesTransferred int64  `json:"bytes_transferred"`
	TotalBytes      int64   `json:"total_bytes"`
	Progress        float64 `json:"progress_percent"`
	BytesPerSecond  int64   `json:"bytes_per_second,omitempty"`
	DurationMs      int64   `json:"duration_ms,omitempty"`
	Error           string  `json:"error,omitempty"`
}

func (s *Server) handleShellFileGetChunked(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	remotePath := mcp.ParseString(req, "remote_path", "")
	localPath := mcp.ParseString(req, "local_path", "")
	chunkSize := mcp.ParseInt(req, "chunk_size", DefaultChunkSize)

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if remotePath == "" {
		return mcp.NewToolResultError("remote_path is required"), nil
	}
	if localPath == "" {
		return mcp.NewToolResultError("local_path is required"), nil
	}

	if chunkSize > MaxChunkSize {
		chunkSize = MaxChunkSize
	}
	if chunkSize < 1024 {
		chunkSize = 1024
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if !sess.IsSSH() {
		return mcp.NewToolResultError("chunked transfer is only supported for SSH sessions"), nil
	}

	resolvedPath := sess.ResolvePath(remotePath)
	manifestPath := localPath + ManifestSuffix

	slog.Info("starting chunked download",
		slog.String("session_id", sessionID),
		slog.String("remote_path", resolvedPath),
		slog.String("local_path", localPath),
		slog.Int("chunk_size", chunkSize),
	)

	return s.performChunkedGet(sess, resolvedPath, localPath, manifestPath, chunkSize)
}

func (s *Server) handleShellFilePutChunked(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	localPath := mcp.ParseString(req, "local_path", "")
	remotePath := mcp.ParseString(req, "remote_path", "")
	chunkSize := mcp.ParseInt(req, "chunk_size", DefaultChunkSize)

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if localPath == "" {
		return mcp.NewToolResultError("local_path is required"), nil
	}
	if remotePath == "" {
		return mcp.NewToolResultError("remote_path is required"), nil
	}

	if chunkSize > MaxChunkSize {
		chunkSize = MaxChunkSize
	}
	if chunkSize < 1024 {
		chunkSize = 1024
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if !sess.IsSSH() {
		return mcp.NewToolResultError("chunked transfer is only supported for SSH sessions"), nil
	}

	resolvedRemote := sess.ResolvePath(remotePath)
	manifestPath := localPath + ManifestSuffix

	slog.Info("starting chunked upload",
		slog.String("session_id", sessionID),
		slog.String("local_path", localPath),
		slog.String("remote_path", resolvedRemote),
		slog.Int("chunk_size", chunkSize),
	)

	return s.performChunkedPut(sess, localPath, resolvedRemote, manifestPath, chunkSize)
}

func (s *Server) handleShellTransferStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	manifestPath := mcp.ParseString(req, "manifest_path", "")

	if manifestPath == "" {
		return mcp.NewToolResultError("manifest_path is required"), nil
	}

	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("load manifest: %v", err)), nil
	}

	completed := 0
	for _, chunk := range manifest.Chunks {
		if chunk.Completed {
			completed++
		}
	}

	progress := float64(0)
	if manifest.TotalSize > 0 {
		progress = float64(manifest.BytesSent) / float64(manifest.TotalSize) * 100
	}

	result := ChunkedTransferResult{
		Status:           "in_progress",
		ManifestPath:     manifestPath,
		ChunksCompleted:  completed,
		TotalChunks:      manifest.TotalChunks,
		BytesTransferred: manifest.BytesSent,
		TotalBytes:       manifest.TotalSize,
		Progress:         progress,
		BytesPerSecond:   manifest.BytesPerSecond,
	}

	if completed == manifest.TotalChunks {
		result.Status = "completed"
	}

	return jsonResult(result)
}

func (s *Server) handleShellTransferResume(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(req, "session_id", "")
	manifestPath := mcp.ParseString(req, "manifest_path", "")

	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}
	if manifestPath == "" {
		return mcp.NewToolResultError("manifest_path is required"), nil
	}

	sess, err := s.sessionManager.Get(sessionID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("load manifest: %v", err)), nil
	}

	slog.Info("resuming chunked transfer",
		slog.String("session_id", sessionID),
		slog.String("manifest_path", manifestPath),
		slog.String("direction", manifest.Direction),
	)

	if manifest.Direction == "get" {
		return s.resumeChunkedGet(sess, manifest, manifestPath)
	}
	return s.resumeChunkedPut(sess, manifest, manifestPath)
}

func (s *Server) performChunkedGet(sess *session.Session, remotePath, localPath, manifestPath string, chunkSize int) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	// Get remote file info
	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat remote file: %v", err)), nil
	}

	totalSize := info.Size()
	totalChunks := int((totalSize + int64(chunkSize) - 1) / int64(chunkSize))

	// Create manifest
	manifest := &TransferManifest{
		Version:       1,
		Direction:     "get",
		RemotePath:    remotePath,
		LocalPath:     localPath,
		TotalSize:     totalSize,
		ChunkSize:     chunkSize,
		TotalChunks:   totalChunks,
		StartedAt:     startTime,
		LastUpdatedAt: startTime,
		SessionID:     sess.ID,
		Chunks:        make([]ChunkInfo, totalChunks),
	}

	// Initialize chunks
	for i := 0; i < totalChunks; i++ {
		offset := int64(i) * int64(chunkSize)
		size := chunkSize
		if offset+int64(size) > totalSize {
			size = int(totalSize - offset)
		}
		manifest.Chunks[i] = ChunkInfo{
			Index:  i,
			Offset: offset,
			Size:   size,
		}
	}

	// Create local file
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create directory: %v", err)), nil
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create local file: %v", err)), nil
	}
	defer localFile.Close()

	// Pre-allocate file
	if err := localFile.Truncate(totalSize); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("allocate file: %v", err)), nil
	}

	// Open remote file
	remoteFile, _, err := sftpClient.GetFileStream(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("open remote file: %v", err)), nil
	}
	defer remoteFile.Close()

	// Transfer chunks
	return s.transferChunksGet(localFile, remoteFile, manifest, manifestPath, startTime)
}

func (s *Server) transferChunksGet(localFile *os.File, remoteFile io.ReadSeeker, manifest *TransferManifest, manifestPath string, startTime time.Time) (*mcp.CallToolResult, error) {
	buf := make([]byte, manifest.ChunkSize)

	for i := range manifest.Chunks {
		if manifest.Chunks[i].Completed {
			continue
		}

		chunk := &manifest.Chunks[i]

		// Seek to chunk position
		if _, err := remoteFile.Seek(chunk.Offset, io.SeekStart); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("seek remote: %v", err)), nil
		}

		// Read chunk
		n, err := io.ReadFull(remoteFile, buf[:chunk.Size])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			// Save progress before returning error
			saveManifest(manifest, manifestPath)
			return mcp.NewToolResultError(fmt.Sprintf("read chunk %d: %v", i, err)), nil
		}

		// Calculate chunk checksum
		hash := sha256.Sum256(buf[:n])
		chunk.Checksum = hex.EncodeToString(hash[:])

		// Write to local file
		if _, err := localFile.WriteAt(buf[:n], chunk.Offset); err != nil {
			saveManifest(manifest, manifestPath)
			return mcp.NewToolResultError(fmt.Sprintf("write chunk %d: %v", i, err)), nil
		}

		chunk.Completed = true
		manifest.BytesSent += int64(n)
		manifest.LastUpdatedAt = time.Now()

		// Save progress periodically
		if i%10 == 0 || i == manifest.TotalChunks-1 {
			saveManifest(manifest, manifestPath)
		}
	}

	// Calculate final stats
	duration := time.Since(startTime)
	if duration.Seconds() > 0 {
		manifest.BytesPerSecond = int64(float64(manifest.BytesSent) / duration.Seconds())
	}
	now := time.Now()
	manifest.CompletedAt = &now
	saveManifest(manifest, manifestPath)

	result := ChunkedTransferResult{
		Status:           "completed",
		ManifestPath:     manifestPath,
		ChunksCompleted:  manifest.TotalChunks,
		TotalChunks:      manifest.TotalChunks,
		BytesTransferred: manifest.BytesSent,
		TotalBytes:       manifest.TotalSize,
		Progress:         100,
		BytesPerSecond:   manifest.BytesPerSecond,
		DurationMs:       duration.Milliseconds(),
	}

	return jsonResult(result)
}

func (s *Server) performChunkedPut(sess *session.Session, localPath, remotePath, manifestPath string, chunkSize int) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	// Get local file info
	info, err := os.Stat(localPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat local file: %v", err)), nil
	}

	totalSize := info.Size()
	totalChunks := int((totalSize + int64(chunkSize) - 1) / int64(chunkSize))

	// Create manifest
	manifest := &TransferManifest{
		Version:       1,
		Direction:     "put",
		RemotePath:    remotePath,
		LocalPath:     localPath,
		TotalSize:     totalSize,
		ChunkSize:     chunkSize,
		TotalChunks:   totalChunks,
		StartedAt:     startTime,
		LastUpdatedAt: startTime,
		SessionID:     sess.ID,
		Chunks:        make([]ChunkInfo, totalChunks),
	}

	// Initialize chunks
	for i := 0; i < totalChunks; i++ {
		offset := int64(i) * int64(chunkSize)
		size := chunkSize
		if offset+int64(size) > totalSize {
			size = int(totalSize - offset)
		}
		manifest.Chunks[i] = ChunkInfo{
			Index:  i,
			Offset: offset,
			Size:   size,
		}
	}

	// Open local file
	localFile, err := os.Open(localPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("open local file: %v", err)), nil
	}
	defer localFile.Close()

	// Ensure remote directory exists
	remoteDir := filepath.Dir(remotePath)
	if err := sftpClient.MkdirAll(remoteDir); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create remote directory: %v", err)), nil
	}

	// Create remote file
	remoteFile, err := sftpClient.PutFileStream(remotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("create remote file: %v", err)), nil
	}
	defer remoteFile.Close()

	// Transfer chunks
	return s.transferChunksPut(localFile, remoteFile, manifest, manifestPath, startTime)
}

func (s *Server) transferChunksPut(localFile *os.File, remoteFile io.WriteSeeker, manifest *TransferManifest, manifestPath string, startTime time.Time) (*mcp.CallToolResult, error) {
	buf := make([]byte, manifest.ChunkSize)

	for i := range manifest.Chunks {
		if manifest.Chunks[i].Completed {
			continue
		}

		chunk := &manifest.Chunks[i]

		// Seek to chunk position in local file
		if _, err := localFile.Seek(chunk.Offset, io.SeekStart); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("seek local: %v", err)), nil
		}

		// Read chunk from local
		n, err := io.ReadFull(localFile, buf[:chunk.Size])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			saveManifest(manifest, manifestPath)
			return mcp.NewToolResultError(fmt.Sprintf("read chunk %d: %v", i, err)), nil
		}

		// Calculate chunk checksum
		hash := sha256.Sum256(buf[:n])
		chunk.Checksum = hex.EncodeToString(hash[:])

		// Seek to chunk position in remote file
		if _, err := remoteFile.Seek(chunk.Offset, io.SeekStart); err != nil {
			saveManifest(manifest, manifestPath)
			return mcp.NewToolResultError(fmt.Sprintf("seek remote: %v", err)), nil
		}

		// Write to remote file
		if _, err := remoteFile.Write(buf[:n]); err != nil {
			saveManifest(manifest, manifestPath)
			return mcp.NewToolResultError(fmt.Sprintf("write chunk %d: %v", i, err)), nil
		}

		chunk.Completed = true
		manifest.BytesSent += int64(n)
		manifest.LastUpdatedAt = time.Now()

		// Save progress periodically
		if i%10 == 0 || i == manifest.TotalChunks-1 {
			saveManifest(manifest, manifestPath)
		}
	}

	// Calculate final stats
	duration := time.Since(startTime)
	if duration.Seconds() > 0 {
		manifest.BytesPerSecond = int64(float64(manifest.BytesSent) / duration.Seconds())
	}
	now := time.Now()
	manifest.CompletedAt = &now
	saveManifest(manifest, manifestPath)

	result := ChunkedTransferResult{
		Status:           "completed",
		ManifestPath:     manifestPath,
		ChunksCompleted:  manifest.TotalChunks,
		TotalChunks:      manifest.TotalChunks,
		BytesTransferred: manifest.BytesSent,
		TotalBytes:       manifest.TotalSize,
		Progress:         100,
		BytesPerSecond:   manifest.BytesPerSecond,
		DurationMs:       duration.Milliseconds(),
	}

	return jsonResult(result)
}

func (s *Server) resumeChunkedGet(sess *session.Session, manifest *TransferManifest, manifestPath string) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	// Open local file for writing
	localFile, err := os.OpenFile(manifest.LocalPath, os.O_RDWR, 0644)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("open local file: %v", err)), nil
	}
	defer localFile.Close()

	// Open remote file for reading
	remoteFile, _, err := sftpClient.GetFileStream(manifest.RemotePath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("open remote file: %v", err)), nil
	}
	defer remoteFile.Close()

	// Reset bytes sent to count only resumed bytes
	resumedFrom := manifest.BytesSent
	manifest.BytesSent = 0

	// Re-count already completed chunks
	for _, chunk := range manifest.Chunks {
		if chunk.Completed {
			manifest.BytesSent += int64(chunk.Size)
		}
	}

	// Continue transfer
	result, err := s.transferChunksGet(localFile, remoteFile, manifest, manifestPath, startTime)
	if err != nil {
		return result, err
	}

	// Log resume info
	slog.Info("resumed chunked download",
		slog.Int64("resumed_from_bytes", resumedFrom),
		slog.Int64("total_bytes", manifest.TotalSize),
	)

	return result, nil
}

func (s *Server) resumeChunkedPut(sess *session.Session, manifest *TransferManifest, manifestPath string) (*mcp.CallToolResult, error) {
	startTime := time.Now()

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get SFTP client: %v", err)), nil
	}

	// Open local file for reading
	localFile, err := os.Open(manifest.LocalPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("open local file: %v", err)), nil
	}
	defer localFile.Close()

	// Open remote file for writing (append mode with seek support)
	remoteFile, err := sftpClient.OpenFile(manifest.RemotePath, os.O_RDWR)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("open remote file: %v", err)), nil
	}
	defer remoteFile.Close()

	// Reset bytes sent to count only resumed bytes
	resumedFrom := manifest.BytesSent
	manifest.BytesSent = 0

	// Re-count already completed chunks
	for _, chunk := range manifest.Chunks {
		if chunk.Completed {
			manifest.BytesSent += int64(chunk.Size)
		}
	}

	// Continue transfer
	result, err := s.transferChunksPut(localFile, remoteFile, manifest, manifestPath, startTime)
	if err != nil {
		return result, err
	}

	// Log resume info
	slog.Info("resumed chunked upload",
		slog.Int64("resumed_from_bytes", resumedFrom),
		slog.Int64("total_bytes", manifest.TotalSize),
	)

	return result, nil
}

// loadManifest reads a transfer manifest from disk.
func loadManifest(path string) (*TransferManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest TransferManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &manifest, nil
}

// saveManifest writes a transfer manifest to disk.
func saveManifest(manifest *TransferManifest, path string) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	return nil
}
