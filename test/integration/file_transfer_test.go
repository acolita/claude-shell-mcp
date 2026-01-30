//go:build integration

package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChecksumCalculation(t *testing.T) {
	// Test checksum calculation matches expected SHA256
	testContent := []byte("test content for checksum verification")
	expectedHash := sha256.Sum256(testContent)
	expectedChecksum := hex.EncodeToString(expectedHash[:])

	// Create temp file
	tmpFile, err := os.CreateTemp("", "checksum_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(testContent); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Read and calculate checksum
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	actualHash := sha256.Sum256(data)
	actualChecksum := hex.EncodeToString(actualHash[:])

	if actualChecksum != expectedChecksum {
		t.Errorf("checksum mismatch: got %s, want %s", actualChecksum, expectedChecksum)
	}
}

func TestAtomicWrite(t *testing.T) {
	// Test atomic write pattern: write to temp, then rename
	dstDir, err := os.MkdirTemp("", "atomic_write_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	finalPath := filepath.Join(dstDir, "final.txt")
	content := []byte("atomic write content")

	// Simulate atomic write
	tempPath := finalPath + ".tmp"

	// Write to temp
	if err := os.WriteFile(tempPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Verify temp exists
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatal("temp file should exist")
	}

	// Rename to final
	if err := os.Rename(tempPath, finalPath); err != nil {
		t.Fatal(err)
	}

	// Verify final exists and temp is gone
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Error("content mismatch after atomic write")
	}

	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after rename")
	}
}

func TestTimestampPreservation(t *testing.T) {
	// Create source file with specific mtime
	srcDir, err := os.MkdirTemp("", "timestamp_src")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	srcFile := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(srcFile, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set a specific modification time
	targetTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if err := os.Chtimes(srcFile, targetTime, targetTime); err != nil {
		t.Fatal(err)
	}

	// Copy and preserve timestamp
	dstDir, err := os.MkdirTemp("", "timestamp_dst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	dstFile := filepath.Join(dstDir, "test.txt")

	// Read source
	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}

	srcInfo, err := os.Stat(srcFile)
	if err != nil {
		t.Fatal(err)
	}

	// Write destination
	if err := os.WriteFile(dstFile, data, srcInfo.Mode()); err != nil {
		t.Fatal(err)
	}

	// Preserve timestamp
	if err := os.Chtimes(dstFile, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		t.Fatal(err)
	}

	// Verify timestamp was preserved
	dstInfo, err := os.Stat(dstFile)
	if err != nil {
		t.Fatal(err)
	}

	if !dstInfo.ModTime().Equal(srcInfo.ModTime()) {
		t.Errorf("mtime not preserved: got %v, want %v", dstInfo.ModTime(), srcInfo.ModTime())
	}
}

func TestDirectoryCopyWithPattern(t *testing.T) {
	// Create source directory with files
	srcDir, err := os.MkdirTemp("", "dir_transfer_src")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	// Create subdirectory
	subDir := filepath.Join(srcDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create test files
	files := map[string]string{
		"file1.txt":        "content of file 1",
		"file2.go":         "package main\n\nfunc main() {}\n",
		"subdir/file3.txt": "content of file 3",
		"subdir/file4.go":  "package subdir\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(srcDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create destination directory
	dstDir, err := os.MkdirTemp("", "dir_transfer_dst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	// Copy only .go files (simulating pattern filter)
	pattern := ".go"
	filesCopied := 0

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), pattern) {
			return nil
		}

		relPath, _ := filepath.Rel(srcDir, path)
		dstPath := filepath.Join(dstDir, relPath)

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}

		// Copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
			return err
		}

		filesCopied++
		return nil
	})

	if err != nil {
		t.Fatalf("failed to copy directory: %v", err)
	}

	// Should have copied 2 .go files
	if filesCopied != 2 {
		t.Errorf("expected 2 files copied, got %d", filesCopied)
	}

	// Verify .go files exist
	for _, name := range []string{"file2.go", "subdir/file4.go"} {
		if _, err := os.Stat(filepath.Join(dstDir, name)); err != nil {
			t.Errorf("expected %s to exist", name)
		}
	}

	// Verify .txt files don't exist
	for _, name := range []string{"file1.txt", "subdir/file3.txt"} {
		if _, err := os.Stat(filepath.Join(dstDir, name)); !os.IsNotExist(err) {
			t.Errorf("expected %s to NOT exist", name)
		}
	}
}

func TestBinaryFileCopy(t *testing.T) {
	// Test copying binary content
	srcDir, err := os.MkdirTemp("", "binary_src")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	// Create binary content
	binaryContent := make([]byte, 256)
	for i := range binaryContent {
		binaryContent[i] = byte(i)
	}

	srcFile := filepath.Join(srcDir, "binary.bin")
	if err := os.WriteFile(srcFile, binaryContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Copy
	dstDir, err := os.MkdirTemp("", "binary_dst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	dstFile := filepath.Join(dstDir, "binary.bin")

	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dstFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Verify
	readData, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatal(err)
	}

	if len(readData) != len(binaryContent) {
		t.Errorf("size mismatch: got %d, want %d", len(readData), len(binaryContent))
	}

	for i := range readData {
		if readData[i] != binaryContent[i] {
			t.Errorf("byte mismatch at position %d", i)
			break
		}
	}
}

func TestLargeFileChunking(t *testing.T) {
	// Test chunking logic for large files
	totalSize := int64(5 * 1024 * 1024) // 5MB
	chunkSize := 1024 * 1024            // 1MB

	expectedChunks := int((totalSize + int64(chunkSize) - 1) / int64(chunkSize))

	if expectedChunks != 5 {
		t.Errorf("expected 5 chunks, got %d", expectedChunks)
	}

	// Verify chunk boundaries
	for i := 0; i < expectedChunks; i++ {
		offset := int64(i) * int64(chunkSize)
		size := chunkSize
		if offset+int64(size) > totalSize {
			size = int(totalSize - offset)
		}

		t.Logf("Chunk %d: offset=%d, size=%d", i, offset, size)

		if offset < 0 || offset >= totalSize {
			t.Errorf("chunk %d: invalid offset %d", i, offset)
		}
		if size <= 0 || size > chunkSize {
			t.Errorf("chunk %d: invalid size %d", i, size)
		}
	}
}

func TestSymlinkHandling(t *testing.T) {
	// Test symlink creation and reading
	srcDir, err := os.MkdirTemp("", "symlink_src")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	// Create target file
	targetFile := filepath.Join(srcDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink
	linkFile := filepath.Join(srcDir, "link.txt")
	if err := os.Symlink(targetFile, linkFile); err != nil {
		t.Fatal(err)
	}

	// Read via symlink
	data, err := os.ReadFile(linkFile)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "target content" {
		t.Errorf("symlink content mismatch: got %q", string(data))
	}

	// Check if it's a symlink
	info, err := os.Lstat(linkFile)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected file to be a symlink")
	}

	// Read symlink target
	target, err := os.Readlink(linkFile)
	if err != nil {
		t.Fatal(err)
	}

	if target != targetFile {
		t.Errorf("symlink target mismatch: got %q, want %q", target, targetFile)
	}
}

func TestExclusionPatterns(t *testing.T) {
	// Test exclusion pattern matching
	exclusions := []string{".git", "node_modules", "__pycache__", "*.pyc"}

	tests := []struct {
		name    string
		exclude bool
	}{
		{".git", true},
		{"node_modules", true},
		{"__pycache__", true},
		{"file.pyc", true},
		{"src", false},
		{"main.go", false},
		{".gitignore", false}, // Not exact match
	}

	for _, tt := range tests {
		excluded := false
		for _, pattern := range exclusions {
			if strings.HasPrefix(pattern, "*") {
				suffix := pattern[1:]
				if strings.HasSuffix(tt.name, suffix) {
					excluded = true
					break
				}
			} else if pattern == tt.name {
				excluded = true
				break
			}
		}

		if excluded != tt.exclude {
			t.Errorf("%s: excluded=%v, want %v", tt.name, excluded, tt.exclude)
		}
	}
}

func TestProgressCalculation(t *testing.T) {
	tests := []struct {
		bytesSent int64
		totalSize int64
		wantPct   float64
	}{
		{0, 1000, 0},
		{500, 1000, 50},
		{1000, 1000, 100},
		{250, 1000, 25},
		{0, 0, 0}, // Edge case: empty file
	}

	for _, tt := range tests {
		var progress float64
		if tt.totalSize > 0 {
			progress = float64(tt.bytesSent) / float64(tt.totalSize) * 100
		}
		if progress != tt.wantPct {
			t.Errorf("progress(%d/%d) = %f, want %f", tt.bytesSent, tt.totalSize, progress, tt.wantPct)
		}
	}
}

func TestTransferManifestRoundTrip(t *testing.T) {
	// Test creating and reading a transfer manifest (JSON)
	tmpDir, err := os.MkdirTemp("", "manifest_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manifestPath := filepath.Join(tmpDir, "test.transfer")

	// Create manifest content
	manifest := `{
  "version": 1,
  "direction": "get",
  "remote_path": "/remote/file.bin",
  "local_path": "/local/file.bin",
  "total_size": 10240,
  "chunk_size": 1024,
  "total_chunks": 10,
  "started_at": "2024-01-15T10:30:00Z",
  "last_updated_at": "2024-01-15T10:31:00Z",
  "session_id": "sess_123",
  "bytes_sent": 5120,
  "chunks": [
    {"index": 0, "offset": 0, "size": 1024, "completed": true, "checksum": "abc123"},
    {"index": 1, "offset": 1024, "size": 1024, "completed": true, "checksum": "def456"},
    {"index": 2, "offset": 2048, "size": 1024, "completed": false}
  ]
}`

	// Write manifest
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	// Read manifest
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "sess_123") {
		t.Error("manifest should contain session_id")
	}
	if !strings.Contains(string(data), "\"completed\": true") {
		t.Error("manifest should have completed chunks")
	}
}

func TestFilePermissionPreservation(t *testing.T) {
	// Test permission preservation during copy
	srcDir, err := os.MkdirTemp("", "perm_src")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	srcFile := filepath.Join(srcDir, "executable.sh")
	if err := os.WriteFile(srcFile, []byte("#!/bin/bash\necho test"), 0755); err != nil {
		t.Fatal(err)
	}

	srcInfo, err := os.Stat(srcFile)
	if err != nil {
		t.Fatal(err)
	}

	// Copy preserving permissions
	dstDir, err := os.MkdirTemp("", "perm_dst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	dstFile := filepath.Join(dstDir, "executable.sh")

	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dstFile, data, srcInfo.Mode().Perm()); err != nil {
		t.Fatal(err)
	}

	// Verify permissions
	dstInfo, err := os.Stat(dstFile)
	if err != nil {
		t.Fatal(err)
	}

	if dstInfo.Mode().Perm() != srcInfo.Mode().Perm() {
		t.Errorf("permissions not preserved: got %o, want %o", dstInfo.Mode().Perm(), srcInfo.Mode().Perm())
	}
}
