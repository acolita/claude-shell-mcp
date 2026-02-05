package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRandomSuffix(t *testing.T) {
	// Should generate unique suffixes
	s1 := randomSuffix()
	s2 := randomSuffix()

	if s1 == s2 {
		t.Error("randomSuffix should generate unique values")
	}

	// Should be 8 characters (4 bytes hex encoded)
	if len(s1) != 8 {
		t.Errorf("expected 8 characters, got %d", len(s1))
	}
}

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		exclusions []string
		want       bool
	}{
		{"exact match", "node_modules", []string{"node_modules"}, true},
		{"no match", "src", []string{"node_modules"}, false},
		{"wildcard suffix", "test.pyc", []string{"*.pyc"}, true},
		{"wildcard no match", "test.go", []string{"*.pyc"}, false},
		{"multiple exclusions", ".git", []string{"node_modules", ".git", "*.pyc"}, true},
		{"empty exclusions", "anything", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExclude(tt.filename, tt.exclusions)
			if got != tt.want {
				t.Errorf("shouldExclude(%q, %v) = %v, want %v", tt.filename, tt.exclusions, got, tt.want)
			}
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		pattern string
		want    bool
	}{
		{"empty pattern matches all", "any/file.txt", "", true},
		{"simple extension", "file.go", "*.go", true},
		{"simple extension no match", "file.txt", "*.go", false},
		{"doublestar", "src/pkg/file.go", "**/*.go", true},
		{"doublestar no match", "src/pkg/file.txt", "**/*.go", false},
		{"nested path", "src/internal/pkg/main.go", "src/**/*.go", true},
		{"specific path", "cmd/main.go", "cmd/*.go", true},
		{"windows separators converted", "src\\pkg\\file.go", "**/*.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPattern(tt.relPath, tt.pattern)
			if got != tt.want {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.relPath, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestFileGetResult(t *testing.T) {
	// Test struct can be created with all fields
	result := FileGetResult{
		Status:           "completed",
		RemotePath:       "/path/to/file",
		LocalPath:        "/local/path",
		Size:             1024,
		Mode:             "0644",
		ModTime:          1234567890,
		Content:          "file content",
		Encoding:         "text",
		ContentSize:      12,
		Checksum:         "abc123",
		ChecksumVerified: true,
	}

	if result.Status != "completed" {
		t.Error("Status not set correctly")
	}
	if result.Size != 1024 {
		t.Error("Size not set correctly")
	}
}

func TestFilePutResult(t *testing.T) {
	// Test struct can be created with all fields
	result := FilePutResult{
		Status:      "completed",
		RemotePath:  "/path/to/file",
		Size:        1024,
		Mode:        "0644",
		DirsCreated: true,
		Overwritten: false,
		Checksum:    "abc123",
		AtomicWrite: true,
	}

	if result.Status != "completed" {
		t.Error("Status not set correctly")
	}
	if !result.AtomicWrite {
		t.Error("AtomicWrite not set correctly")
	}
}

func TestDirTransferResult(t *testing.T) {
	result := DirTransferResult{
		Status:           "completed",
		FilesTransferred: 10,
		DirsCreated:      5,
		TotalBytes:       10240,
		SymlinksHandled:  2,
		Errors: []TransferError{
			{Path: "/path/to/error", Error: "permission denied"},
		},
	}

	if result.FilesTransferred != 10 {
		t.Error("FilesTransferred not set correctly")
	}
	if len(result.Errors) != 1 {
		t.Error("Errors not set correctly")
	}
}

func TestLocalFileOperations(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "file_transfer_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test file content
	content := []byte("Hello, World!")
	testFile := filepath.Join(tmpDir, "test.txt")

	// Write file
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Read file and verify
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(content))
	}

	// Calculate checksum
	hash := sha256.Sum256(content)
	checksum := hex.EncodeToString(hash[:])

	// Verify checksum format
	if len(checksum) != 64 {
		t.Errorf("checksum should be 64 characters, got %d", len(checksum))
	}
}

func TestAtomicWriteSimulation(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "atomic_write_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	finalPath := filepath.Join(tmpDir, "final.txt")
	tempPath := filepath.Join(tmpDir, ".final.txt.tmp."+randomSuffix())
	content := []byte("atomic write content")

	// Write to temp file
	if err := os.WriteFile(tempPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Verify temp file exists
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatal("temp file should exist")
	}

	// Rename to final
	if err := os.Rename(tempPath, finalPath); err != nil {
		t.Fatal(err)
	}

	// Verify final file exists
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != string(content) {
		t.Error("atomic write content mismatch")
	}

	// Verify temp file is gone
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after rename")
	}
}

func TestDirectoryCopySimulation(t *testing.T) {
	// Create source directory structure
	srcDir, err := os.MkdirTemp("", "src_dir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	// Create test files
	subDir := filepath.Join(srcDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		"file1.txt":        "content1",
		"file2.go":         "package main",
		"subdir/file3.txt": "content3",
		"subdir/file4.go":  "package sub",
	}

	for path, content := range files {
		fullPath := filepath.Join(srcDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create destination directory
	dstDir, err := os.MkdirTemp("", "dst_dir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	// Simulate copying with pattern "*.go"
	pattern := "**/*.go"
	filesCopied := 0

	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(srcDir, path)
		if !matchesPattern(relPath, pattern) {
			return nil
		}

		dstPath := filepath.Join(dstDir, relPath)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return err
		}

		filesCopied++
		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	// Should have copied 2 .go files
	if filesCopied != 2 {
		t.Errorf("expected 2 files copied with pattern %q, got %d", pattern, filesCopied)
	}

	// Verify .go files exist in destination
	for _, name := range []string{"file2.go", "subdir/file4.go"} {
		dstPath := filepath.Join(dstDir, name)
		if _, err := os.Stat(dstPath); err != nil {
			t.Errorf("expected %s to exist in destination", name)
		}
	}

	// Verify .txt files do NOT exist in destination
	for _, name := range []string{"file1.txt", "subdir/file3.txt"} {
		dstPath := filepath.Join(dstDir, name)
		if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
			t.Errorf("expected %s to NOT exist in destination", name)
		}
	}
}

func TestDefaultExclusions(t *testing.T) {
	// Verify default exclusions include common patterns
	expected := []string{".git", "node_modules", "__pycache__", ".DS_Store"}

	for _, pattern := range expected {
		found := false
		for _, exclusion := range defaultExclusions {
			if exclusion == pattern {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to be in defaultExclusions", pattern)
		}
	}
}

func TestBuildRelPath(t *testing.T) {
	tests := []struct {
		name     string
		parent   string
		filename string
		want     string
	}{
		{"empty parent", "", "file.txt", "file.txt"},
		{"with parent", "dir", "file.txt", "dir/file.txt"},
		{"nested parent", "dir/subdir", "file.txt", "dir/subdir/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRelPath(tt.parent, tt.filename)
			if got != tt.want {
				t.Errorf("buildRelPath(%q, %q) = %q, want %q", tt.parent, tt.filename, got, tt.want)
			}
		})
	}
}

func TestDirTransferResultAddError(t *testing.T) {
	result := DirTransferResult{Status: "completed"}

	result.addError("/path/to/file", "permission denied")
	result.addError("/other/file", "file not found")

	if len(result.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(result.Errors))
	}

	if result.Errors[0].Path != "/path/to/file" {
		t.Errorf("first error path mismatch")
	}
	if result.Errors[1].Error != "file not found" {
		t.Errorf("second error message mismatch")
	}
}

func TestFinalizeTransferResult(t *testing.T) {
	result := DirTransferResult{
		Status:           "completed",
		FilesTransferred: 5,
		TotalBytes:       1024,
	}

	startTime := time.Now().Add(-2 * time.Second)
	finalizeTransferResult(&result, startTime)

	if result.DurationMs < 2000 {
		t.Errorf("expected duration >= 2000ms, got %d", result.DurationMs)
	}

	if result.BytesPerSecond == 0 {
		t.Error("expected non-zero bytes per second")
	}

	// Test with errors
	result2 := DirTransferResult{
		Status: "completed",
		Errors: []TransferError{{Path: "/test", Error: "err"}},
	}
	finalizeTransferResult(&result2, time.Now())

	if result2.Status != "completed_with_errors" {
		t.Errorf("expected status 'completed_with_errors', got %q", result2.Status)
	}
}

func TestSymlinkAction(t *testing.T) {
	// Test symlinkAction constants exist and are distinct
	actions := []symlinkAction{symlinkSkip, symlinkHandled, symlinkFollow, symlinkError}
	seen := make(map[symlinkAction]bool)

	for _, action := range actions {
		if seen[action] {
			t.Errorf("duplicate symlinkAction value")
		}
		seen[action] = true
	}
}
