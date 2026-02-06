package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesessionmgr"
)

// --- helpers for local session creation ---

func newLocalSession(id string) *session.Session {
	pty := fakepty.New()
	clk := fakeclock.New(time.Now())
	sess := session.NewSession(id, "local",
		session.WithPTY(pty),
		session.WithSessionClock(clk),
	)
	_ = sess.Initialize()
	return sess
}

// ==================== copyToLocalPath ====================

func TestLocal_CopyToLocalPath_Success(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	info := &fakeFileInfo{
		name:    "source.txt",
		size:    5,
		mode:    0644,
		modTime: time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC),
	}

	errResult := srv.copyToLocalPath([]byte("hello"), "/dest/file.txt", info, false)
	if errResult != nil {
		t.Fatalf("unexpected error")
	}

	data, err := ffs.ReadFile("/dest/file.txt")
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("data=%q, want 'hello'", string(data))
	}
}

func TestLocal_CopyToLocalPath_WithPreserve(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	modTime := time.Date(2023, 1, 15, 12, 0, 0, 0, time.UTC)
	info := &fakeFileInfo{
		name:    "source.txt",
		size:    5,
		mode:    0644,
		modTime: modTime,
	}

	errResult := srv.copyToLocalPath([]byte("hello"), "/dest/preserved.txt", info, true)
	if errResult != nil {
		t.Fatalf("unexpected error")
	}

	stat, err := ffs.Stat("/dest/preserved.txt")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !stat.ModTime().Equal(modTime) {
		t.Errorf("ModTime=%v, want %v", stat.ModTime(), modTime)
	}
}

// ==================== preserveLocalTimestamp ====================

func TestLocal_PreserveLocalTimestamp_ChtimesError(t *testing.T) {
	ffs := fakefs.New()
	// Don't create the file -- Chtimes will fail on non-existent file
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	// This should not panic -- it logs a warning
	srv.preserveLocalTimestamp("/nonexistent/file.txt", true, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
}

func TestLocal_PreserveLocalTimestamp_PreserveFalse(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/file.txt", []byte("data"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	origInfo, _ := ffs.Stat("/file.txt")
	origMod := origInfo.ModTime()

	srv.preserveLocalTimestamp("/file.txt", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))

	info, _ := ffs.Stat("/file.txt")
	if !info.ModTime().Equal(origMod) {
		t.Error("modtime should not change when preserve=false")
	}
}

func TestLocal_PreserveLocalTimestamp_ZeroTime(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/file.txt", []byte("data"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	origInfo, _ := ffs.Stat("/file.txt")
	origMod := origInfo.ModTime()

	srv.preserveLocalTimestamp("/file.txt", true, time.Time{})

	info, _ := ffs.Stat("/file.txt")
	if !info.ModTime().Equal(origMod) {
		t.Error("modtime should not change when modTime is zero")
	}
}

// ==================== writeLocalFile ====================

func TestLocal_WriteLocalFile_NonAtomicPath(t *testing.T) {
	ffs := fakefs.New()
	ffs.MkdirAll("/dir", 0755)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	var result FilePutResult
	errResult := srv.writeLocalFile("/dir/file.txt", "/dir", []byte("non-atomic"), FilePutOptions{Mode: 0644, Atomic: false}, &result)
	if errResult != nil {
		t.Fatalf("unexpected error: %v", errResult.Content)
	}
	if result.AtomicWrite {
		t.Error("AtomicWrite should be false for non-atomic writes")
	}

	data, err := ffs.ReadFile("/dir/file.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "non-atomic" {
		t.Errorf("data=%q, want 'non-atomic'", string(data))
	}
}

func TestLocal_WriteLocalFile_AtomicWriteSuccess(t *testing.T) {
	ffs := fakefs.New()
	ffs.MkdirAll("/dir", 0755)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	var result FilePutResult
	errResult := srv.writeLocalFile("/dir/file.txt", "/dir", []byte("atomic content"), FilePutOptions{Mode: 0644, Atomic: true}, &result)
	if errResult != nil {
		t.Fatalf("unexpected error: %v", errResult.Content)
	}
	if !result.AtomicWrite {
		t.Error("AtomicWrite should be true")
	}

	data, err := ffs.ReadFile("/dir/file.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "atomic content" {
		t.Errorf("data=%q, want 'atomic content'", string(data))
	}
}

// ==================== handleLocalFileGet ====================

func TestLocal_HandleLocalFileGet_DirectoryError(t *testing.T) {
	ffs := fakefs.New()
	ffs.MkdirAll("/mydir", 0755)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/mydir", FileGetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for directory path")
	}
	if !strings.Contains(resultText(result), "directory") {
		t.Errorf("error=%q, should mention directory", resultText(result))
	}
}

func TestLocal_HandleLocalFileGet_LargeFileNoLocalPath(t *testing.T) {
	ffs := fakefs.New()
	largeData := make([]byte, maxContentSize+1)
	for i := range largeData {
		largeData[i] = 'A'
	}
	ffs.AddFile("/bigfile.bin", largeData, 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/bigfile.bin", FileGetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for large file without local_path")
	}
	if !strings.Contains(resultText(result), "exceeds limit") {
		t.Errorf("error=%q, should mention exceeds limit", resultText(result))
	}
}

func TestLocal_HandleLocalFileGet_LargeFileWithLocalPath(t *testing.T) {
	ffs := fakefs.New()
	largeData := make([]byte, maxContentSize+1)
	for i := range largeData {
		largeData[i] = 'B'
	}
	ffs.AddFile("/bigfile.bin", largeData, 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/bigfile.bin", FileGetOptions{
		LocalPath: "/local/copy.bin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	copied, readErr := ffs.ReadFile("/local/copy.bin")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if len(copied) != maxContentSize+1 {
		t.Errorf("copied size=%d, want %d", len(copied), maxContentSize+1)
	}
}

func TestLocal_HandleLocalFileGet_ChecksumVerification(t *testing.T) {
	ffs := fakefs.New()
	content := []byte("checksum test content")
	ffs.AddFile("/check.txt", content, 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	hash := sha256.Sum256(content)
	expectedChecksum := hex.EncodeToString(hash[:])

	result, err := srv.handleLocalFileGet("/check.txt", FileGetOptions{
		Checksum:         true,
		ExpectedChecksum: expectedChecksum,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["checksum_verified"] != true {
		t.Error("checksum_verified should be true")
	}
}

func TestLocal_HandleLocalFileGet_ChecksumMismatch(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/check.txt", []byte("some data"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/check.txt", FileGetOptions{
		Checksum:         true,
		ExpectedChecksum: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(resultText(result), "checksum mismatch") {
		t.Errorf("error=%q, should mention checksum mismatch", resultText(result))
	}
}

func TestLocal_HandleLocalFileGet_NoChecksum(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/file.txt", []byte("data"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/file.txt", FileGetOptions{
		Checksum: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["checksum"] != nil && m["checksum"] != "" {
		t.Error("checksum should not be present when disabled")
	}
}

func TestLocal_HandleLocalFileGet_Base64Encoding(t *testing.T) {
	ffs := fakefs.New()
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	ffs.AddFile("/binary.bin", binaryData, 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/binary.bin", FileGetOptions{
		Encoding: "base64",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["encoding"] != "base64" {
		t.Errorf("encoding=%v, want 'base64'", m["encoding"])
	}

	contentStr, ok := m["content"].(string)
	if !ok {
		t.Fatal("content should be a string")
	}
	decoded, decErr := base64.StdEncoding.DecodeString(contentStr)
	if decErr != nil {
		t.Fatalf("base64 decode: %v", decErr)
	}
	if len(decoded) != len(binaryData) {
		t.Errorf("decoded length=%d, want %d", len(decoded), len(binaryData))
	}
}

func TestLocal_HandleLocalFileGet_CompressedContent(t *testing.T) {
	ffs := fakefs.New()
	content := []byte(strings.Repeat("ABCDEFGH", 1000))
	ffs.AddFile("/data.json", content, 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/data.json", FileGetOptions{
		Compress: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["compressed"] != true {
		t.Error("compressed should be true")
	}
	if m["encoding"] != "base64" {
		t.Errorf("encoding=%v, want 'base64'", m["encoding"])
	}
	ratio, ok := m["compression_ratio"].(float64)
	if !ok || ratio <= 0 || ratio >= 1 {
		t.Errorf("compression_ratio=%v, should be between 0 and 1", m["compression_ratio"])
	}
}

func TestLocal_HandleLocalFileGet_CompressNonCompressibleType(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/image.png", []byte("not really a png"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/image.png", FileGetOptions{
		Compress: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["compressed"] == true {
		t.Error("compressed should be false for non-compressible file types")
	}
}

func TestLocal_HandleLocalFileGet_LocalPathSameAsSource(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/test.txt", []byte("same path"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/test.txt", FileGetOptions{
		LocalPath: "/test.txt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["local_path"] != nil {
		t.Error("local_path should not be set when source and dest are the same")
	}
	if m["content"] != "same path" {
		t.Errorf("content=%v, want 'same path'", m["content"])
	}
}

func TestLocal_HandleLocalFileGet_ReadFileError(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/nonexistent.txt", FileGetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error=%q, should mention 'not found'", resultText(result))
	}
}

func TestLocal_HandleLocalFileGet_WithLocalPathAndPreserve(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/data.txt", []byte("preserve me"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileGet("/src/data.txt", FileGetOptions{
		LocalPath: "/dst/data.txt",
		Preserve:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["local_path"] != "/dst/data.txt" {
		t.Errorf("local_path=%v, want '/dst/data.txt'", m["local_path"])
	}

	data, readErr := ffs.ReadFile("/dst/data.txt")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "preserve me" {
		t.Errorf("data=%q, want 'preserve me'", string(data))
	}
}

// ==================== handleLocalFilePut ====================

func TestLocal_HandleLocalFilePut_BasicTextContent(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put1")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put1",
		"remote_path": "/output/file.txt",
		"content":     "hello world",
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}
	if m["dirs_created"] != true {
		t.Error("dirs_created should be true")
	}
}

func TestLocal_HandleLocalFilePut_Base64Content(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put2")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	binaryData := []byte{0x00, 0x01, 0xFF}
	encoded := base64.StdEncoding.EncodeToString(binaryData)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put2",
		"remote_path": "/output/binary.bin",
		"content":     encoded,
		"encoding":    "base64",
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	data, readErr := ffs.ReadFile("/output/binary.bin")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if len(data) != 3 || data[0] != 0x00 || data[2] != 0xFF {
		t.Errorf("binary content mismatch: got %v", data)
	}
}

func TestLocal_HandleLocalFilePut_InvalidBase64(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put3")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put3",
		"remote_path": "/output/file.bin",
		"content":     "not!!!valid===base64",
		"encoding":    "base64",
		"create_dirs": true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(resultText(result), "decode base64") {
		t.Errorf("error=%q, should mention base64 decode", resultText(result))
	}
}

func TestLocal_HandleLocalFilePut_NonAtomicWrite(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put4")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put4",
		"remote_path": "/output/noatomic.txt",
		"content":     "non-atomic content",
		"atomic":      false,
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["atomic_write"] == true {
		t.Error("atomic_write should be false")
	}

	data, readErr := ffs.ReadFile("/output/noatomic.txt")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "non-atomic content" {
		t.Errorf("data=%q", string(data))
	}
}

func TestLocal_HandleLocalFilePut_FileExistsNoOverwrite(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/output/existing.txt", []byte("old"), 0644)
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put5")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put5",
		"remote_path": "/output/existing.txt",
		"content":     "new content",
		"overwrite":   false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for existing file without overwrite")
	}
	if !strings.Contains(resultText(result), "file exists") {
		t.Errorf("error=%q, should mention 'file exists'", resultText(result))
	}
}

func TestLocal_HandleLocalFilePut_FileExistsWithOverwrite(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/output/existing.txt", []byte("old"), 0644)
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put6")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put6",
		"remote_path": "/output/existing.txt",
		"content":     "new content",
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["overwritten"] != true {
		t.Error("overwritten should be true")
	}

	data, readErr := ffs.ReadFile("/output/existing.txt")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "new content" {
		t.Errorf("data=%q, want 'new content'", string(data))
	}
}

func TestLocal_HandleLocalFilePut_WithCustomMode(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put7")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put7",
		"remote_path": "/output/script.sh",
		"content":     "#!/bin/bash\necho hello",
		"mode":        "0755",
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["mode"] != "0755" {
		t.Errorf("mode=%v, want '0755'", m["mode"])
	}
}

func TestLocal_HandleLocalFilePut_InvalidMode(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put8")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put8",
		"remote_path": "/output/file.txt",
		"content":     "data",
		"mode":        "invalid",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(resultText(result), "invalid mode") {
		t.Errorf("error=%q, should mention invalid mode", resultText(result))
	}
}

func TestLocal_HandleLocalFilePut_NoContentOrLocalPath(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put9")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put9",
		"remote_path": "/output/file.txt",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when neither content nor local_path provided")
	}
	if !strings.Contains(resultText(result), "either content or local_path") {
		t.Errorf("error=%q, should mention content/local_path requirement", resultText(result))
	}
}

func TestLocal_HandleLocalFilePut_FromLocalFile(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/original.txt", []byte("source file content"), 0644)
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put10")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put10",
		"remote_path": "/dst/copy.txt",
		"local_path":  "/src/original.txt",
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	data, readErr := ffs.ReadFile("/dst/copy.txt")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "source file content" {
		t.Errorf("data=%q, want 'source file content'", string(data))
	}
}

func TestLocal_HandleLocalFilePut_FromLocalFileNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put11")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put11",
		"remote_path": "/dst/copy.txt",
		"local_path":  "/nonexistent/file.txt",
		"create_dirs": true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent local file")
	}
	if !strings.Contains(resultText(result), "stat local file") {
		t.Errorf("error=%q, should mention stat local file", resultText(result))
	}
}

func TestLocal_HandleLocalFilePut_WithPreserve(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/data.txt", []byte("content"), 0644)
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put12")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put12",
		"remote_path": "/dst/data.txt",
		"local_path":  "/src/data.txt",
		"preserve":    true,
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}
}

func TestLocal_HandleLocalFilePut_WithChecksum(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put13")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	content := "checksum content"
	req := makeRequest(map[string]any{
		"session_id":  "sess_put13",
		"remote_path": "/output/file.txt",
		"content":     content,
		"checksum":    true,
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	hash := sha256.Sum256([]byte(content))
	expected := hex.EncodeToString(hash[:])
	if m["checksum"] != expected {
		t.Errorf("checksum=%v, want %s", m["checksum"], expected)
	}
}

func TestLocal_HandleLocalFilePut_NoChecksum(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put14")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put14",
		"remote_path": "/output/file.txt",
		"content":     "no checksum",
		"checksum":    false,
		"create_dirs": true,
		"overwrite":   true,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["checksum"] != nil && m["checksum"] != "" {
		t.Error("checksum should not be present when disabled")
	}
}

// ==================== compressData ====================

func TestLocal_CompressData_EmptyData(t *testing.T) {
	compressed, err := compressData([]byte{})
	if err != nil {
		t.Fatalf("compressData on empty data: %v", err)
	}
	if len(compressed) == 0 {
		t.Error("compressed output should not be empty (gzip header)")
	}

	decompressed, err := decompressData(compressed)
	if err != nil {
		t.Fatalf("decompressData: %v", err)
	}
	if len(decompressed) != 0 {
		t.Errorf("decompressed length=%d, want 0", len(decompressed))
	}
}

func TestLocal_CompressData_SmallData(t *testing.T) {
	data := []byte("tiny")
	compressed, err := compressData(data)
	if err != nil {
		t.Fatalf("compressData: %v", err)
	}
	decompressed, err := decompressData(compressed)
	if err != nil {
		t.Fatalf("decompressData: %v", err)
	}
	if string(decompressed) != "tiny" {
		t.Errorf("roundtrip mismatch: got %q", string(decompressed))
	}
}

func TestLocal_CompressData_LargeRepetitiveData(t *testing.T) {
	data := []byte(strings.Repeat("COMPRESS_ME_", 10000))
	compressed, err := compressData(data)
	if err != nil {
		t.Fatalf("compressData: %v", err)
	}
	if len(compressed) >= len(data) {
		t.Errorf("compressed=%d should be smaller than original=%d", len(compressed), len(data))
	}
}

// ==================== handleLocalDirCopy ====================

func TestLocal_HandleLocalDirCopy_SuccessWithRealFS(t *testing.T) {
	// handleLocalDirCopy uses filepath.WalkDir on the real OS filesystem,
	// so we create real temp directories for the walk. Reads/writes go
	// through the Server's fs (fakefs), so we register matching files.
	srcDir := t.TempDir()
	subDir := srcDir + "/sub"
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcDir+"/file1.txt", []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subDir+"/file2.txt", []byte("content2"), 0644); err != nil {
		t.Fatal(err)
	}

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.MkdirAll(subDir, 0755)
	ffs.AddFile(srcDir+"/file1.txt", []byte("content1"), 0644)
	ffs.AddFile(subDir+"/file2.txt", []byte("content2"), 0644)

	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	dstDir := "/fakefs/dst"
	opts := DirGetOptions{
		LocalPath: dstDir,
		Preserve:  false,
	}

	result, err := srv.handleLocalDirCopy(srcDir, dstDir, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	filesTransferred := m["files_transferred"].(float64)
	if filesTransferred != 2 {
		t.Errorf("files_transferred=%v, want 2", filesTransferred)
	}
}

func TestLocal_HandleLocalDirCopy_WithPattern(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(srcDir+"/main.go", []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcDir+"/readme.md", []byte("# README"), 0644); err != nil {
		t.Fatal(err)
	}

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.AddFile(srcDir+"/main.go", []byte("package main"), 0644)
	ffs.AddFile(srcDir+"/readme.md", []byte("# README"), 0644)

	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{
		LocalPath: "/fakefs/dst",
		Pattern:   "*.go",
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
	if filesTransferred != 1 {
		t.Errorf("files_transferred=%v, want 1 (only .go files)", filesTransferred)
	}
}

func TestLocal_HandleLocalDirCopy_EmptyDir(t *testing.T) {
	srcDir := t.TempDir()

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{LocalPath: "/fakefs/dst"}
	result, err := srv.handleLocalDirCopy(srcDir, "/fakefs/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	filesTransferred := m["files_transferred"].(float64)
	if filesTransferred != 0 {
		t.Errorf("files_transferred=%v, want 0 for empty dir", filesTransferred)
	}
}

func TestLocal_HandleLocalDirCopy_SourceNotFound(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{LocalPath: "/dst"}
	result, err := srv.handleLocalDirCopy("/nonexistent_path_xyz", "/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestLocal_HandleLocalDirCopy_SourceIsFile(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.txt", []byte("not a dir"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{LocalPath: "/dst"}
	result, err := srv.handleLocalDirCopy("/src/file.txt", "/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for file source")
	}
	if !strings.Contains(resultText(result), "not a directory") {
		t.Errorf("error=%q, should mention 'not a directory'", resultText(result))
	}
}

func TestLocal_HandleLocalDirCopy_WithExclusions(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(srcDir+"/main.go", []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcDir+"/cache.pyc", []byte("bytecode"), 0644); err != nil {
		t.Fatal(err)
	}

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.AddFile(srcDir+"/main.go", []byte("package main"), 0644)
	ffs.AddFile(srcDir+"/cache.pyc", []byte("bytecode"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirGetOptions{
		LocalPath:  "/fakefs/dst",
		Exclusions: []string{"*.pyc"},
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
	if filesTransferred != 1 {
		t.Errorf("files_transferred=%v, want 1 (.pyc excluded)", filesTransferred)
	}
}

// ==================== copyLocalFile ====================

func TestLocal_CopyLocalFile_SuccessNoPreserve(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.go", []byte("package main"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	entry := &fakeDirEntry{name: "file.go", mode: 0644, size: 12, mod: time.Now()}

	srv.copyLocalFile("/src/file.go", "/dst/file.go", entry, false, result)

	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
	if result.TotalBytes != 12 {
		t.Errorf("TotalBytes=%d, want 12", result.TotalBytes)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestLocal_CopyLocalFile_SuccessWithPreserve(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.go", []byte("code"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	result := &DirTransferResult{Status: "completed"}
	modTime := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	entry := &fakeDirEntry{name: "file.go", mode: 0644, size: 4, mod: modTime}

	srv.copyLocalFile("/src/file.go", "/dst/file.go", entry, true, result)

	if result.FilesTransferred != 1 {
		t.Errorf("FilesTransferred=%d, want 1", result.FilesTransferred)
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestLocal_CopyLocalFile_SourceReadError(t *testing.T) {
	ffs := fakefs.New()
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
		t.Errorf("error path=%q", result.Errors[0].Path)
	}
}

// ==================== Full integration: handleShellDirGet / DirPut local mode ====================

func TestLocal_HandleShellDirGet_LocalMode(t *testing.T) {
	srcDir := t.TempDir()
	subDir := srcDir + "/sub"
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcDir+"/a.txt", []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subDir+"/b.txt", []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.MkdirAll(subDir, 0755)
	ffs.AddFile(srcDir+"/a.txt", []byte("aaa"), 0644)
	ffs.AddFile(subDir+"/b.txt", []byte("bbb"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_dg1")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_dg1",
		"remote_path": srcDir,
		"local_path":  "/fakefs/dst",
	})

	result, err := srv.handleShellDirGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}
	filesTransferred := m["files_transferred"].(float64)
	if filesTransferred != 2 {
		t.Errorf("files_transferred=%v, want 2", filesTransferred)
	}
}

func TestLocal_HandleShellDirPut_LocalMode(t *testing.T) {
	srcDir := t.TempDir()
	subDir := srcDir + "/sub"
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcDir+"/x.go", []byte("xx"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subDir+"/y.go", []byte("yy"), 0644); err != nil {
		t.Fatal(err)
	}

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.MkdirAll(subDir, 0755)
	ffs.AddFile(srcDir+"/x.go", []byte("xx"), 0644)
	ffs.AddFile(subDir+"/y.go", []byte("yy"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_dp1")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_dp1",
		"local_path":  srcDir,
		"remote_path": "/fakefs/remote",
	})

	result, err := srv.handleShellDirPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}
}

// ==================== handleShellDirGet / DirPut validation ====================

func TestLocal_HandleShellDirGet_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
		"local_path": "/dst",
	})

	result, err := srv.handleShellDirGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing remote_path")
	}
}

func TestLocal_HandleShellDirGet_MissingLocalPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_1",
		"remote_path": "/src",
	})

	result, err := srv.handleShellDirGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing local_path")
	}
}

func TestLocal_HandleShellDirPut_MissingLocalPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_1",
		"remote_path": "/dst",
	})

	result, err := srv.handleShellDirPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing local_path")
	}
}

func TestLocal_HandleShellDirPut_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_1",
		"local_path": "/src",
	})

	result, err := srv.handleShellDirPut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing remote_path")
	}
}

// ==================== setContentWithEncoding ====================

func TestLocal_SetContentWithEncoding_TextMode(t *testing.T) {
	result := FileGetResult{}
	setContentWithEncoding([]byte("hello"), "/file.txt", FileGetOptions{Encoding: "text"}, &result)

	if result.Content != "hello" {
		t.Errorf("content=%q, want 'hello'", result.Content)
	}
	if result.Encoding != "text" {
		t.Errorf("encoding=%q, want 'text'", result.Encoding)
	}
	if result.ContentSize != 5 {
		t.Errorf("content_size=%d, want 5", result.ContentSize)
	}
}

func TestLocal_SetContentWithEncoding_Base64Mode(t *testing.T) {
	data := []byte{0x00, 0xFF}
	result := FileGetResult{}
	setContentWithEncoding(data, "/file.bin", FileGetOptions{Encoding: "base64"}, &result)

	if result.Encoding != "base64" {
		t.Errorf("encoding=%q, want 'base64'", result.Encoding)
	}

	decoded, err := base64.StdEncoding.DecodeString(result.Content)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if len(decoded) != 2 || decoded[0] != 0x00 || decoded[1] != 0xFF {
		t.Errorf("decoded=%v, want [0x00 0xFF]", decoded)
	}
}

func TestLocal_SetContentWithEncoding_CompressSmallData(t *testing.T) {
	data := []byte("hi")
	result := FileGetResult{}
	setContentWithEncoding(data, "/file.txt", FileGetOptions{Compress: true}, &result)

	// "hi" is tiny, gzip overhead makes it larger, so compression is skipped
	if result.Compressed {
		t.Error("compressed should be false for tiny data that doesn't compress well")
	}
}

func TestLocal_SetContentWithEncoding_CompressLargeData(t *testing.T) {
	data := []byte(strings.Repeat("AAAA", 5000))
	result := FileGetResult{}
	setContentWithEncoding(data, "/file.log", FileGetOptions{Compress: true}, &result)

	if !result.Compressed {
		t.Error("compressed should be true for highly compressible data")
	}
	if result.Encoding != "base64" {
		t.Errorf("encoding=%q, want 'base64'", result.Encoding)
	}
	if result.CompressionRatio <= 0 || result.CompressionRatio >= 1 {
		t.Errorf("compression_ratio=%f, should be between 0 and 1", result.CompressionRatio)
	}
}

// ==================== processFileChecksum ====================

func TestLocal_ProcessFileChecksum_Disabled(t *testing.T) {
	result := FileGetResult{}
	errResult := processFileChecksum([]byte("data"), FileGetOptions{Checksum: false}, &result)
	if errResult != nil {
		t.Error("should return nil when checksum disabled")
	}
	if result.Checksum != "" {
		t.Error("checksum should be empty when disabled")
	}
}

func TestLocal_ProcessFileChecksum_Enabled(t *testing.T) {
	data := []byte("test data")
	result := FileGetResult{}
	errResult := processFileChecksum(data, FileGetOptions{Checksum: true}, &result)
	if errResult != nil {
		t.Error("should return nil for successful checksum")
	}

	hash := sha256.Sum256(data)
	expected := hex.EncodeToString(hash[:])
	if result.Checksum != expected {
		t.Errorf("checksum=%q, want %q", result.Checksum, expected)
	}
}

func TestLocal_ProcessFileChecksum_VerifyMatch(t *testing.T) {
	data := []byte("verify me")
	hash := sha256.Sum256(data)
	expected := hex.EncodeToString(hash[:])

	result := FileGetResult{}
	errResult := processFileChecksum(data, FileGetOptions{
		Checksum:         true,
		ExpectedChecksum: expected,
	}, &result)
	if errResult != nil {
		t.Error("should return nil for matching checksum")
	}
	if !result.ChecksumVerified {
		t.Error("ChecksumVerified should be true")
	}
}

func TestLocal_ProcessFileChecksum_VerifyMismatch(t *testing.T) {
	data := []byte("verify me")
	result := FileGetResult{}
	errResult := processFileChecksum(data, FileGetOptions{
		Checksum:         true,
		ExpectedChecksum: "0000000000000000000000000000000000000000000000000000000000000000",
	}, &result)
	if errResult == nil {
		t.Fatal("expected error for checksum mismatch")
	}
	if !errResult.IsError {
		t.Error("should be an error result")
	}
}

// ==================== handleLocalDirCopyPut ====================

func TestLocal_HandleLocalDirCopyPut_Success(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(srcDir+"/file.go", []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}

	ffs := fakefs.New()
	ffs.MkdirAll(srcDir, 0755)
	ffs.AddFile(srcDir+"/file.go", []byte("package x"), 0644)
	sm := fakesessionmgr.New()
	srv := newTestServerWithFS(sm, ffs)

	opts := DirPutOptions{
		RemotePath: "/fakefs/dst",
		Preserve:   true,
		Symlinks:   "follow",
		MaxDepth:   20,
	}

	result, err := srv.handleLocalDirCopyPut(srcDir, "/fakefs/dst", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	filesTransferred := m["files_transferred"].(float64)
	if filesTransferred != 1 {
		t.Errorf("files_transferred=%v, want 1", filesTransferred)
	}
}

// ==================== helper function tests ====================

func TestLocal_NewFilePutResult(t *testing.T) {
	result := newFilePutResult("/path/file.txt", []byte("hello"), 0755)
	if result.Status != "completed" {
		t.Errorf("status=%q", result.Status)
	}
	if result.RemotePath != "/path/file.txt" {
		t.Errorf("remote_path=%q", result.RemotePath)
	}
	if result.Size != 5 {
		t.Errorf("size=%d, want 5", result.Size)
	}
	if result.Mode != "0755" {
		t.Errorf("mode=%q, want '0755'", result.Mode)
	}
}

func TestLocal_SetPutChecksum_Enabled(t *testing.T) {
	data := []byte("checksum data")
	result := FilePutResult{}
	setPutChecksum(data, true, &result)

	hash := sha256.Sum256(data)
	expected := hex.EncodeToString(hash[:])
	if result.Checksum != expected {
		t.Errorf("checksum=%q, want %q", result.Checksum, expected)
	}
}

func TestLocal_SetPutChecksum_Disabled(t *testing.T) {
	result := FilePutResult{}
	setPutChecksum([]byte("data"), false, &result)
	if result.Checksum != "" {
		t.Error("checksum should be empty when disabled")
	}
}

func TestLocal_ParseFilePutMode_Empty(t *testing.T) {
	opts := FilePutOptions{Mode: 0644}
	errResult := parseFilePutMode("", &opts)
	if errResult != nil {
		t.Error("expected nil for empty mode string")
	}
	if opts.Mode != 0644 {
		t.Errorf("mode should remain unchanged: %o", opts.Mode)
	}
}

func TestLocal_ParseFilePutMode_Valid(t *testing.T) {
	opts := FilePutOptions{Mode: 0644}
	errResult := parseFilePutMode("0755", &opts)
	if errResult != nil {
		t.Error("expected nil for valid mode")
	}
	if opts.Mode != 0755 {
		t.Errorf("mode=%o, want 0755", opts.Mode)
	}
}

func TestLocal_ParseFilePutMode_Invalid(t *testing.T) {
	opts := FilePutOptions{}
	errResult := parseFilePutMode("notamode", &opts)
	if errResult == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestLocal_ValidateFilePutInputs_AllValid(t *testing.T) {
	errResult := validateFilePutInputs("sess_1", "/path", FilePutOptions{Content: "data"})
	if errResult != nil {
		t.Error("expected nil for valid inputs")
	}
}

func TestLocal_ValidateFilePutInputs_MissingSessionID(t *testing.T) {
	errResult := validateFilePutInputs("", "/path", FilePutOptions{Content: "data"})
	if errResult == nil {
		t.Fatal("expected error for empty session_id")
	}
}

func TestLocal_ValidateFilePutInputs_MissingRemotePath(t *testing.T) {
	errResult := validateFilePutInputs("sess_1", "", FilePutOptions{Content: "data"})
	if errResult == nil {
		t.Fatal("expected error for empty remote_path")
	}
}

func TestLocal_ValidateFilePutInputs_MissingContentAndLocalPath(t *testing.T) {
	errResult := validateFilePutInputs("sess_1", "/path", FilePutOptions{})
	if errResult == nil {
		t.Fatal("expected error when both content and local_path are empty")
	}
}

func TestLocal_IsCompressible(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"file.go", true},
		{"file.py", true},
		{"file.json", true},
		{"file.txt", true},
		{"file.log", true},
		{"file.yaml", true},
		{"file.toml", true},
		{"file.sql", true},
		{"file.png", false},
		{"file.jpg", false},
		{"file.zip", false},
		{"file.exe", false},
		{"file", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := isCompressible(tt.filename)
			if got != tt.want {
				t.Errorf("isCompressible(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestLocal_FileStatError_GenericError(t *testing.T) {
	result := fileStatError("/missing.txt", errors.New("permission denied"))
	if !result.IsError {
		t.Error("expected error result")
	}
	text := resultText(result)
	if !strings.Contains(text, "stat file") {
		t.Errorf("error=%q, should contain 'stat file'", text)
	}
}
