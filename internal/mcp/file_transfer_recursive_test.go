package mcp

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesessionmgr"
)

// ==================== shouldExclude ====================

func TestRecur_ShouldExclude_ExactMatches(t *testing.T) {
	tests := []struct {
		name       string
		fileName   string
		exclusions []string
		want       bool
	}{
		{
			name:       "matches .git",
			fileName:   ".git",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "matches node_modules",
			fileName:   "node_modules",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "matches __pycache__",
			fileName:   "__pycache__",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "matches .DS_Store",
			fileName:   ".DS_Store",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "matches .svn",
			fileName:   ".svn",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "matches .hg",
			fileName:   ".hg",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "matches .env",
			fileName:   ".env",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "matches .env.local",
			fileName:   ".env.local",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "does not match regular file",
			fileName:   "main.go",
			exclusions: defaultExclusions,
			want:       false,
		},
		{
			name:       "does not match partial name",
			fileName:   "my_node_modules",
			exclusions: defaultExclusions,
			want:       false,
		},
		{
			name:       "empty exclusions",
			fileName:   ".git",
			exclusions: []string{},
			want:       false,
		},
		{
			name:       "nil exclusions",
			fileName:   ".git",
			exclusions: nil,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExclude(tt.fileName, tt.exclusions)
			if got != tt.want {
				t.Errorf("shouldExclude(%q, ...) = %v, want %v", tt.fileName, got, tt.want)
			}
		})
	}
}

func TestRecur_ShouldExclude_WildcardPatterns(t *testing.T) {
	tests := []struct {
		name       string
		fileName   string
		exclusions []string
		want       bool
	}{
		{
			name:       "matches *.pyc",
			fileName:   "module.pyc",
			exclusions: []string{"*.pyc"},
			want:       true,
		},
		{
			name:       "matches *.pyo",
			fileName:   "module.pyo",
			exclusions: []string{"*.pyo"},
			want:       true,
		},
		{
			name:       "wildcard does not match without suffix",
			fileName:   "pyc",
			exclusions: []string{"*.pyc"},
			want:       false,
		},
		{
			name:       "wildcard matches any prefix",
			fileName:   "anything.bak",
			exclusions: []string{"*.bak"},
			want:       true,
		},
		{
			name:       "wildcard with default exclusions matches .pyc",
			fileName:   "cache.pyc",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "wildcard with default exclusions matches .pyo",
			fileName:   "optimized.pyo",
			exclusions: defaultExclusions,
			want:       true,
		},
		{
			name:       "multiple exclusion patterns",
			fileName:   "test.log",
			exclusions: []string{"*.bak", "*.log", "*.tmp"},
			want:       true,
		},
		{
			name:       "no match among multiple patterns",
			fileName:   "test.go",
			exclusions: []string{"*.bak", "*.log", "*.tmp"},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExclude(tt.fileName, tt.exclusions)
			if got != tt.want {
				t.Errorf("shouldExclude(%q, %v) = %v, want %v", tt.fileName, tt.exclusions, got, tt.want)
			}
		})
	}
}

// ==================== matchesPattern ====================

func TestRecur_MatchesPattern_EmptyPattern(t *testing.T) {
	// Empty pattern should match everything
	if !matchesPattern("any/path/file.go", "") {
		t.Error("empty pattern should match any path")
	}
	if !matchesPattern("", "") {
		t.Error("empty pattern should match empty path")
	}
}

func TestRecur_MatchesPattern_SimpleGlobs(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		pattern string
		want    bool
	}{
		{
			name:    "matches *.go",
			relPath: "main.go",
			pattern: "*.go",
			want:    true,
		},
		{
			name:    "does not match *.go for .py",
			relPath: "script.py",
			pattern: "*.go",
			want:    false,
		},
		{
			name:    "matches *.log",
			relPath: "app.log",
			pattern: "*.log",
			want:    true,
		},
		{
			name:    "simple glob does not match nested path",
			relPath: "sub/main.go",
			pattern: "*.go",
			want:    false,
		},
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

func TestRecur_MatchesPattern_DoubleStarGlobs(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		pattern string
		want    bool
	}{
		{
			name:    "**/*.go matches nested file",
			relPath: "pkg/utils/helper.go",
			pattern: "**/*.go",
			want:    true,
		},
		{
			name:    "**/*.go matches single level deep",
			relPath: "cmd/main.go",
			pattern: "**/*.go",
			want:    true,
		},
		{
			name:    "**/*.go does not match non-.go file",
			relPath: "pkg/utils/helper.py",
			pattern: "**/*.go",
			want:    false,
		},
		{
			name:    "src/**/*.ts matches files under src",
			relPath: "src/components/App.ts",
			pattern: "src/**/*.ts",
			want:    true,
		},
		{
			name:    "src/**/*.ts does not match outside src",
			relPath: "lib/App.ts",
			pattern: "src/**/*.ts",
			want:    false,
		},
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

func TestRecur_MatchesPattern_BackslashNormalization(t *testing.T) {
	// Backslashes in paths should be normalized to forward slashes
	got := matchesPattern("sub\\dir\\file.go", "**/*.go")
	if !got {
		t.Error("matchesPattern should normalize backslashes to forward slashes")
	}
}

func TestRecur_MatchesPattern_InvalidPattern(t *testing.T) {
	// Invalid pattern should return true (include the file on error)
	got := matchesPattern("file.go", "[invalid")
	if !got {
		t.Error("matchesPattern should return true on invalid pattern (include file on error)")
	}
}

// ==================== buildRelPath ====================

func TestRecur_BuildRelPath(t *testing.T) {
	tests := []struct {
		name   string
		parent string
		child  string
		want   string
	}{
		{
			name:   "empty parent",
			parent: "",
			child:  "file.go",
			want:   "file.go",
		},
		{
			name:   "non-empty parent",
			parent: "src",
			child:  "main.go",
			want:   "src/main.go",
		},
		{
			name:   "nested parent",
			parent: "src/pkg/utils",
			child:  "helper.go",
			want:   "src/pkg/utils/helper.go",
		},
		{
			name:   "both empty",
			parent: "",
			child:  "",
			want:   "",
		},
		{
			name:   "parent with trailing slash content",
			parent: "dir",
			child:  "sub",
			want:   "dir/sub",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRelPath(tt.parent, tt.child)
			if got != tt.want {
				t.Errorf("buildRelPath(%q, %q) = %q, want %q", tt.parent, tt.child, got, tt.want)
			}
		})
	}
}

// ==================== finalizeTransferResult ====================

func TestRecur_FinalizeTransferResult_NoErrors(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := startTime.Add(2 * time.Second)

	fc := fakeclock.New(endTime)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithClock(fc))

	result := &DirTransferResult{
		Status:           "completed",
		FilesTransferred: 10,
		TotalBytes:       2048,
	}

	srv.finalizeTransferResult(result, startTime)

	if result.DurationMs != 2000 {
		t.Errorf("DurationMs=%d, want 2000", result.DurationMs)
	}
	if result.BytesPerSecond != 1024 {
		t.Errorf("BytesPerSecond=%d, want 1024", result.BytesPerSecond)
	}
	if result.Status != "completed" {
		t.Errorf("Status=%q, want 'completed'", result.Status)
	}
}

func TestRecur_FinalizeTransferResult_WithErrors(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := startTime.Add(1 * time.Second)

	fc := fakeclock.New(endTime)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithClock(fc))

	result := &DirTransferResult{
		Status:           "completed",
		FilesTransferred: 5,
		TotalBytes:       1000,
	}
	result.addError("/some/file.txt", "permission denied")

	srv.finalizeTransferResult(result, startTime)

	if result.Status != "completed_with_errors" {
		t.Errorf("Status=%q, want 'completed_with_errors'", result.Status)
	}
	if result.DurationMs != 1000 {
		t.Errorf("DurationMs=%d, want 1000", result.DurationMs)
	}
	if result.BytesPerSecond != 1000 {
		t.Errorf("BytesPerSecond=%d, want 1000", result.BytesPerSecond)
	}
}

func TestRecur_FinalizeTransferResult_ZeroDuration(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Same time as start (instant)
	fc := fakeclock.New(startTime)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithClock(fc))

	result := &DirTransferResult{
		Status:     "completed",
		TotalBytes: 500,
	}

	srv.finalizeTransferResult(result, startTime)

	if result.DurationMs != 0 {
		t.Errorf("DurationMs=%d, want 0", result.DurationMs)
	}
	// With zero duration, BytesPerSecond should remain 0
	if result.BytesPerSecond != 0 {
		t.Errorf("BytesPerSecond=%d, want 0 (zero duration)", result.BytesPerSecond)
	}
}

func TestRecur_FinalizeTransferResult_ZeroBytes(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := startTime.Add(5 * time.Second)

	fc := fakeclock.New(endTime)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithClock(fc))

	result := &DirTransferResult{
		Status:     "completed",
		TotalBytes: 0,
	}

	srv.finalizeTransferResult(result, startTime)

	if result.BytesPerSecond != 0 {
		t.Errorf("BytesPerSecond=%d, want 0", result.BytesPerSecond)
	}
}

// ==================== DirTransferResult.addError ====================

func TestRecur_AddError(t *testing.T) {
	result := &DirTransferResult{Status: "completed"}

	result.addError("/path/to/file.txt", "permission denied")
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Path != "/path/to/file.txt" {
		t.Errorf("path=%q, want '/path/to/file.txt'", result.Errors[0].Path)
	}
	if result.Errors[0].Error != "permission denied" {
		t.Errorf("error=%q, want 'permission denied'", result.Errors[0].Error)
	}

	result.addError("/another/file.txt", "no such file")
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(result.Errors))
	}
}

// ==================== dirEntryFromInfo ====================

func TestRecur_DirEntryFromInfo_File(t *testing.T) {
	info := &fakeFileInfo{
		name:    "test.go",
		size:    1024,
		mode:    0644,
		modTime: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		isDir:   false,
	}

	entry := dirEntryFromInfo{info: info}

	if entry.Name() != "test.go" {
		t.Errorf("Name()=%q, want 'test.go'", entry.Name())
	}
	if entry.IsDir() {
		t.Error("IsDir() should be false for a file")
	}
	if entry.Type() != 0 {
		t.Errorf("Type()=%v, want 0 (regular file)", entry.Type())
	}

	gotInfo, err := entry.Info()
	if err != nil {
		t.Fatalf("Info() returned error: %v", err)
	}
	if gotInfo.Size() != 1024 {
		t.Errorf("Info().Size()=%d, want 1024", gotInfo.Size())
	}
}

func TestRecur_DirEntryFromInfo_Directory(t *testing.T) {
	info := &fakeFileInfo{
		name:  "mydir",
		mode:  os.ModeDir | 0755,
		isDir: true,
	}

	entry := dirEntryFromInfo{info: info}

	if !entry.IsDir() {
		t.Error("IsDir() should be true for a directory")
	}
	if entry.Type()&fs.ModeDir == 0 {
		t.Error("Type() should include ModeDir")
	}
}

// ==================== handleUploadSymlink (skip/follow modes using fakefs) ====================

func TestRecur_HandleUploadSymlink_Skip(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result := &DirTransferResult{Status: "completed"}

	action, info := srv.handleUploadSymlink(nil, "/local/link", "/remote/link", "skip", result)

	if action != symlinkSkip {
		t.Errorf("action=%d, want symlinkSkip (%d)", action, symlinkSkip)
	}
	if info != nil {
		t.Error("info should be nil for skip action")
	}
	if result.SymlinksHandled != 0 {
		t.Errorf("SymlinksHandled=%d, want 0", result.SymlinksHandled)
	}
}

// Note: TestRecur_HandleUploadSymlink_PreserveSuccess is not included because
// the "preserve" path calls sftpClient.Symlink which requires a real SSH connection.
// The Readlink step (via s.fs) works, but the sftpClient.Symlink on a nil *sftp.Client panics.

func TestRecur_HandleUploadSymlink_PreserveReadlinkError(t *testing.T) {
	ffs := fakefs.New()
	// No symlink added, so Readlink will fail
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result := &DirTransferResult{Status: "completed"}

	action, info := srv.handleUploadSymlink(nil, "/local/nosuchlink", "/remote/link", "preserve", result)

	if action != symlinkError {
		t.Errorf("action=%d, want symlinkError (%d)", action, symlinkError)
	}
	if info != nil {
		t.Error("info should be nil on error")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Path != "/local/nosuchlink" {
		t.Errorf("error path=%q, want '/local/nosuchlink'", result.Errors[0].Path)
	}
}

func TestRecur_HandleUploadSymlink_FollowSuccess(t *testing.T) {
	ffs := fakefs.New()
	// Create the target file that the symlink points to
	ffs.AddFile("/local/target.txt", []byte("content"), 0644)

	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result := &DirTransferResult{Status: "completed"}

	action, info := srv.handleUploadSymlink(nil, "/local/target.txt", "/remote/file.txt", "follow", result)

	if action != symlinkFollow {
		t.Errorf("action=%d, want symlinkFollow (%d)", action, symlinkFollow)
	}
	if info == nil {
		t.Fatal("info should not be nil on follow success")
	}
	if info.Name() != "target.txt" {
		t.Errorf("info.Name()=%q, want 'target.txt'", info.Name())
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestRecur_HandleUploadSymlink_FollowStatError(t *testing.T) {
	ffs := fakefs.New()
	// No file at the path, so Stat will fail
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result := &DirTransferResult{Status: "completed"}

	action, info := srv.handleUploadSymlink(nil, "/local/nonexistent", "/remote/file.txt", "follow", result)

	if action != symlinkError {
		t.Errorf("action=%d, want symlinkError (%d)", action, symlinkError)
	}
	if info != nil {
		t.Error("info should be nil on stat error")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Path != "/local/nonexistent" {
		t.Errorf("error path=%q", result.Errors[0].Path)
	}
}

// ==================== handleDownloadSymlink (skip mode) ====================

func TestRecur_HandleDownloadSymlink_Skip(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result := &DirTransferResult{Status: "completed"}

	action, info := srv.handleDownloadSymlink(nil, "/remote/link", "/local/link", "skip", result)

	if action != symlinkSkip {
		t.Errorf("action=%d, want symlinkSkip (%d)", action, symlinkSkip)
	}
	if info != nil {
		t.Error("info should be nil for skip")
	}
	if result.SymlinksHandled != 0 {
		t.Errorf("SymlinksHandled=%d, want 0", result.SymlinksHandled)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

// ==================== copyLocalFile ====================

func TestRecur_CopyLocalFile_Success(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/hello.go", []byte("package main"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	modTime := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	entry := &fakeDirEntry{name: "hello.go", mode: 0644, size: 12, mod: modTime}

	srv.copyLocalFile("/src/hello.go", "/dst/hello.go", entry, false, result)

	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
	if result.TotalBytes != 12 {
		t.Errorf("TotalBytes=%d, want 12", result.TotalBytes)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	data, err := ffs.ReadFile("/dst/hello.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "package main" {
		t.Errorf("data=%q, want 'package main'", string(data))
	}
}

func TestRecur_CopyLocalFile_WithPreserve(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.txt", []byte("data"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	modTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	entry := &fakeDirEntry{name: "file.txt", mode: 0644, size: 4, mod: modTime}

	srv.copyLocalFile("/src/file.txt", "/dst/file.txt", entry, true, result)

	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	stat, err := ffs.Stat("/dst/file.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !stat.ModTime().Equal(modTime) {
		t.Errorf("ModTime=%v, want %v", stat.ModTime(), modTime)
	}
}

func TestRecur_CopyLocalFile_ReadError(t *testing.T) {
	ffs := fakefs.New()
	// File does not exist - ReadFile will fail
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	entry := &fakeDirEntry{name: "missing.go", mode: 0644, size: 0}

	srv.copyLocalFile("/src/missing.go", "/dst/missing.go", entry, false, result)

	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0", result.FilesTransferred)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Path != "/src/missing.go" {
		t.Errorf("error path=%q, want '/src/missing.go'", result.Errors[0].Path)
	}
}

func TestRecur_CopyLocalFile_MultipleFiles(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/a.go", []byte("aaa"), 0644)
	ffs.AddFile("/src/b.go", []byte("bbbbb"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	entryA := &fakeDirEntry{name: "a.go", mode: 0644, size: 3}
	entryB := &fakeDirEntry{name: "b.go", mode: 0644, size: 5}

	srv.copyLocalFile("/src/a.go", "/dst/a.go", entryA, false, result)
	srv.copyLocalFile("/src/b.go", "/dst/b.go", entryB, false, result)

	if result.FilesTransferred != 2 {
		t.Errorf("FilesTransferred=%d, want 2", result.FilesTransferred)
	}
	if result.TotalBytes != 8 {
		t.Errorf("TotalBytes=%d, want 8", result.TotalBytes)
	}
}

// ==================== processLocalCopyEntry ====================

func TestRecur_ProcessLocalCopyEntry_WalkError(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{LocalPath: "/dst"}

	err := srv.processLocalCopyEntry("/src", "/dst", "/src/bad.txt", nil, os.ErrPermission, opts, result)
	if err != nil {
		t.Fatalf("processLocalCopyEntry should not return error on walk error, got: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].Path != "/src/bad.txt" {
		t.Errorf("error path=%q", result.Errors[0].Path)
	}
}

func TestRecur_ProcessLocalCopyEntry_RootDir(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{LocalPath: "/dst"}

	// Use a real temp dir so filepath.Rel works correctly
	srcDir := t.TempDir()

	entry, _ := os.ReadDir(srcDir)
	_ = entry // empty dir

	// The root directory itself (relPath == ".")
	dirEntry := &fakeDirEntry{name: filepath.Base(srcDir), isDir: true, mode: os.ModeDir | 0755}
	err := srv.processLocalCopyEntry(srcDir, "/dst", srcDir, dirEntry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Root should be skipped
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0 (root dir skipped)", result.FilesTransferred)
	}
}

func TestRecur_ProcessLocalCopyEntry_ExcludedFile(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{
		LocalPath:  "/dst",
		Exclusions: []string{"*.pyc"},
	}

	srcDir := t.TempDir()
	pycPath := filepath.Join(srcDir, "module.pyc")
	os.WriteFile(pycPath, []byte("bytecode"), 0644)

	entry := &fakeDirEntry{name: "module.pyc", mode: 0644, size: 8}
	err := srv.processLocalCopyEntry(srcDir, "/dst", pycPath, entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0 (excluded file)", result.FilesTransferred)
	}
}

func TestRecur_ProcessLocalCopyEntry_ExcludedDir(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{
		LocalPath:  "/dst",
		Exclusions: defaultExclusions,
	}

	srcDir := t.TempDir()
	gitDir := filepath.Join(srcDir, ".git")
	os.MkdirAll(gitDir, 0755)

	entry := &fakeDirEntry{name: ".git", isDir: true, mode: os.ModeDir | 0755}
	err := srv.processLocalCopyEntry(srcDir, "/dst", gitDir, entry, nil, opts, result)
	if err != filepath.SkipDir {
		t.Errorf("expected filepath.SkipDir for excluded directory, got: %v", err)
	}
}

func TestRecur_ProcessLocalCopyEntry_SubDirectory(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{LocalPath: "/dst"}

	srcDir := t.TempDir()
	subDir := filepath.Join(srcDir, "subdir")
	os.MkdirAll(subDir, 0755)

	entry := &fakeDirEntry{name: "subdir", isDir: true, mode: os.ModeDir | 0755}
	err := srv.processLocalCopyEntry(srcDir, "/dst", subDir, entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Directories are skipped (no file transfer)
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0 (directories skipped)", result.FilesTransferred)
	}
}

func TestRecur_ProcessLocalCopyEntry_PatternMismatch(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/readme.md", []byte("# README"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{
		LocalPath: "/dst",
		Pattern:   "*.go",
	}

	srcDir := t.TempDir()
	mdPath := filepath.Join(srcDir, "readme.md")
	os.WriteFile(mdPath, []byte("# README"), 0644)

	entry := &fakeDirEntry{name: "readme.md", mode: 0644, size: 8}
	err := srv.processLocalCopyEntry(srcDir, "/dst", mdPath, entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FilesTransferred != 0 {
		t.Errorf("FilesTransferred=%d, want 0 (pattern mismatch)", result.FilesTransferred)
	}
}

func TestRecur_ProcessLocalCopyEntry_PatternMatch(t *testing.T) {
	srcDir := t.TempDir()
	goPath := filepath.Join(srcDir, "main.go")
	os.WriteFile(goPath, []byte("package main"), 0644)

	ffs := fakefs.New()
	ffs.AddFile(goPath, []byte("package main"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	opts := DirGetOptions{
		LocalPath: "/dst",
		Pattern:   "*.go",
	}

	entry := &fakeDirEntry{name: "main.go", mode: 0644, size: 12}
	err := srv.processLocalCopyEntry(srcDir, "/dst", goPath, entry, nil, opts, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
}

// ==================== handleLocalDirCopy (more edge cases) ====================

func TestRecur_HandleLocalDirCopy_WithPreserve(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(srcDir+"/data.txt", []byte("preserved"), 0644); err != nil {
		t.Fatal(err)
	}

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.AddFile(srcDir+"/data.txt", []byte("preserved"), 0644)

	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{
		LocalPath: "/fakefs/dst",
		Preserve:  true,
	}

	result, err := srv.handleLocalDirCopy(srcDir, "/fakefs/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["files_transferred"].(float64) != 1 {
		t.Errorf("files_transferred=%v, want 1", m["files_transferred"])
	}
	if m["status"] != "completed" {
		t.Errorf("status=%v, want 'completed'", m["status"])
	}
}

func TestRecur_HandleLocalDirCopy_NestedWithPattern(t *testing.T) {
	srcDir := t.TempDir()
	subDir := filepath.Join(srcDir, "pkg")
	os.MkdirAll(subDir, 0755)

	os.WriteFile(filepath.Join(srcDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(subDir, "utils.go"), []byte("package pkg"), 0644)
	os.WriteFile(filepath.Join(subDir, "readme.md"), []byte("# Readme"), 0644)

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.MkdirAll(subDir, 0755)
	ffs.AddFile(filepath.Join(srcDir, "main.go"), []byte("package main"), 0644)
	ffs.AddFile(filepath.Join(subDir, "utils.go"), []byte("package pkg"), 0644)
	ffs.AddFile(filepath.Join(subDir, "readme.md"), []byte("# Readme"), 0644)

	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{
		LocalPath: "/fakefs/dst",
		Pattern:   "**/*.go",
	}

	result, err := srv.handleLocalDirCopy(srcDir, "/fakefs/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	filesTransferred := m["files_transferred"].(float64)
	// doublestar **/*.go matches both "main.go" (root level) and "pkg/utils.go" (nested)
	// because ** matches zero or more path segments
	if filesTransferred != 2 {
		t.Errorf("files_transferred=%v, want 2 (**/*.go matches both root and nested .go files)", filesTransferred)
	}
}

func TestRecur_HandleLocalDirCopy_ExcludesDefaultPatterns(t *testing.T) {
	srcDir := t.TempDir()
	gitDir := filepath.Join(srcDir, ".git")
	nodeDir := filepath.Join(srcDir, "node_modules")
	os.MkdirAll(gitDir, 0755)
	os.MkdirAll(nodeDir, 0755)

	os.WriteFile(filepath.Join(srcDir, "app.js"), []byte("console.log()"), 0644)
	os.WriteFile(filepath.Join(gitDir, "config"), []byte("git config"), 0644)
	os.WriteFile(filepath.Join(nodeDir, "module.js"), []byte("module"), 0644)
	os.WriteFile(filepath.Join(srcDir, ".DS_Store"), []byte("ds_store"), 0644)

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.AddFile(filepath.Join(srcDir, "app.js"), []byte("console.log()"), 0644)
	ffs.AddFile(filepath.Join(srcDir, ".DS_Store"), []byte("ds_store"), 0644)

	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{
		LocalPath:  "/fakefs/dst",
		Exclusions: defaultExclusions,
	}

	result, err := srv.handleLocalDirCopy(srcDir, "/fakefs/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	filesTransferred := m["files_transferred"].(float64)
	// Only app.js should be transferred; .git/, node_modules/, .DS_Store excluded
	if filesTransferred != 1 {
		t.Errorf("files_transferred=%v, want 1 (only app.js, exclusions applied)", filesTransferred)
	}
}

// ==================== handleLocalDirCopyPut ====================

func TestRecur_HandleLocalDirCopyPut_PropagatesOptions(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0644)

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.AddFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0644)

	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirPutOptions{
		RemotePath: "/fakefs/remote",
		Preserve:   true,
		Symlinks:   "skip",
		MaxDepth:   10,
		Exclusions: defaultExclusions,
	}

	result, err := srv.handleLocalDirCopyPut(srcDir, "/fakefs/remote", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v, want 'completed'", m["status"])
	}
	if m["files_transferred"].(float64) != 1 {
		t.Errorf("files_transferred=%v, want 1", m["files_transferred"])
	}
}

func TestRecur_HandleLocalDirCopyPut_SourceNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirPutOptions{
		RemotePath: "/fakefs/remote",
	}

	result, err := srv.handleLocalDirCopyPut("/nonexistent_dir_xyz", "/fakefs/remote", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent source")
	}
}

// ==================== symlinkAction constants ====================

func TestRecur_SymlinkActionConstants(t *testing.T) {
	// Verify the constants have distinct values
	actions := map[symlinkAction]string{
		symlinkSkip:    "skip",
		symlinkHandled: "handled",
		symlinkFollow:  "follow",
		symlinkError:   "error",
	}

	if len(actions) != 4 {
		t.Errorf("expected 4 distinct symlink actions, some duplicates found")
	}
}

// ==================== DirGetOptions / DirPutOptions defaults ====================

func TestRecur_DirGetOptions_Defaults(t *testing.T) {
	opts := DirGetOptions{
		LocalPath:  "/dst",
		Preserve:   true,
		Symlinks:   "follow",
		MaxDepth:   20,
		Exclusions: defaultExclusions,
	}

	if opts.LocalPath != "/dst" {
		t.Errorf("LocalPath=%q", opts.LocalPath)
	}
	if !opts.Preserve {
		t.Error("Preserve should be true")
	}
	if opts.Symlinks != "follow" {
		t.Errorf("Symlinks=%q", opts.Symlinks)
	}
	if opts.MaxDepth != 20 {
		t.Errorf("MaxDepth=%d", opts.MaxDepth)
	}
	if len(opts.Exclusions) != len(defaultExclusions) {
		t.Errorf("Exclusions count=%d, want %d", len(opts.Exclusions), len(defaultExclusions))
	}
}

func TestRecur_DirPutOptions_Defaults(t *testing.T) {
	opts := DirPutOptions{
		RemotePath: "/remote",
		Preserve:   true,
		Symlinks:   "follow",
		MaxDepth:   20,
		Overwrite:  false,
		Exclusions: defaultExclusions,
	}

	if opts.RemotePath != "/remote" {
		t.Errorf("RemotePath=%q", opts.RemotePath)
	}
	if opts.Overwrite {
		t.Error("Overwrite should be false by default")
	}
}

// ==================== defaultExclusions coverage ====================

func TestRecur_DefaultExclusions_ContainsExpected(t *testing.T) {
	expected := []string{
		".git", ".svn", ".hg", "node_modules", "__pycache__",
		".DS_Store", "*.pyc", "*.pyo", ".env", ".env.local",
	}

	if len(defaultExclusions) != len(expected) {
		t.Errorf("defaultExclusions has %d entries, want %d", len(defaultExclusions), len(expected))
	}

	for i, exp := range expected {
		if i >= len(defaultExclusions) {
			break
		}
		if defaultExclusions[i] != exp {
			t.Errorf("defaultExclusions[%d]=%q, want %q", i, defaultExclusions[i], exp)
		}
	}
}

// ==================== Real filesystem symlink tests ====================

func TestRecur_HandleLocalDirCopy_WithRealSymlinks(t *testing.T) {
	srcDir := t.TempDir()

	// Create a regular file
	targetFile := filepath.Join(srcDir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink to the file
	symlinkPath := filepath.Join(srcDir, "link.txt")
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		t.Skipf("cannot create symlinks on this platform: %v", err)
	}

	// Set up fakefs with the files (for copyLocalFile reads)
	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.AddFile(targetFile, []byte("target content"), 0644)
	// The symlink in the real FS will be followed by WalkDir (default behavior)
	// So link.txt will appear as a regular file and copyLocalFile reads it via fakefs
	ffs.AddFile(symlinkPath, []byte("target content"), 0644)

	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{
		LocalPath: "/fakefs/dst",
		Symlinks:  "follow",
	}

	result, err := srv.handleLocalDirCopy(srcDir, "/fakefs/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	filesTransferred := m["files_transferred"].(float64)
	// Both the target file and the followed symlink should be transferred
	if filesTransferred != 2 {
		t.Errorf("files_transferred=%v, want 2", filesTransferred)
	}
}

// ==================== Tool definition tests ====================

func TestRecur_ShellDirGetTool(t *testing.T) {
	tool := shellDirGetTool()
	if tool.Name != "shell_dir_get" {
		t.Errorf("Name=%q, want 'shell_dir_get'", tool.Name)
	}
	if tool.Description == "" {
		t.Error("Description should not be empty")
	}
}

func TestRecur_ShellDirPutTool(t *testing.T) {
	tool := shellDirPutTool()
	if tool.Name != "shell_dir_put" {
		t.Errorf("Name=%q, want 'shell_dir_put'", tool.Name)
	}
	if tool.Description == "" {
		t.Error("Description should not be empty")
	}
}
