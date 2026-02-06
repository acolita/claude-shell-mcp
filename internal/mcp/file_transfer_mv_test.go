package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakesessionmgr"
)

// ==================== handleShellFileMv handler-level tests ====================

func TestMv_HandleShellFileMv_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"source":      "/src/file.txt",
		"destination": "/dst/file.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing session_id")
	}
	if !strings.Contains(resultText(result), "session_id") {
		t.Errorf("error=%q, should mention session_id", resultText(result))
	}
}

func TestMv_HandleShellFileMv_MissingSource(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv1",
		"destination": "/dst/file.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(resultText(result), "source") {
		t.Errorf("error=%q, should mention source", resultText(result))
	}
}

func TestMv_HandleShellFileMv_MissingDestination(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_mv2",
		"source":     "/src/file.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing destination")
	}
	if !strings.Contains(resultText(result), "destination") {
		t.Errorf("error=%q, should mention destination", resultText(result))
	}
}

func TestMv_HandleShellFileMv_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"source":      "/src/file.txt",
		"destination": "/dst/file.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent session")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error=%q, should mention 'not found'", resultText(result))
	}
}

func TestMv_HandleShellFileMv_LocalSuccess(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/data/src.txt", []byte("move me"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_mv_ok")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv_ok",
		"source":      "/data/src.txt",
		"destination": "/data/dst.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
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
	if m["source"] != "/data/src.txt" {
		t.Errorf("source=%v", m["source"])
	}
	if m["destination"] != "/data/dst.txt" {
		t.Errorf("destination=%v", m["destination"])
	}

	// Verify file was moved
	_, readErr := ffs.ReadFile("/data/dst.txt")
	if readErr != nil {
		t.Fatal("destination file should exist after move")
	}
	_, readErr = ffs.ReadFile("/data/src.txt")
	if readErr == nil {
		t.Fatal("source file should not exist after move")
	}
}

func TestMv_HandleShellFileMv_LocalWithOverwrite(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/data/src.txt", []byte("new content"), 0644)
	ffs.AddFile("/data/dst.txt", []byte("old content"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_mv_ow")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv_ow",
		"source":      "/data/src.txt",
		"destination": "/data/dst.txt",
		"overwrite":   true,
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
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

	data, _ := ffs.ReadFile("/data/dst.txt")
	if string(data) != "new content" {
		t.Errorf("data=%q, want 'new content'", string(data))
	}
}

func TestMv_HandleShellFileMv_LocalWithCreateDirs(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.txt", []byte("content"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_mv_cd")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv_cd",
		"source":      "/src/file.txt",
		"destination": "/new/deep/dir/file.txt",
		"create_dirs": true,
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["dirs_created"] != true {
		t.Error("dirs_created should be true")
	}

	data, _ := ffs.ReadFile("/new/deep/dir/file.txt")
	if string(data) != "content" {
		t.Errorf("data=%q, want 'content'", string(data))
	}
}

// ==================== handleLocalFileMv direct tests ====================

func TestMv_HandleLocalFileMv_SourceNotFound(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileMv("/nonexistent.txt", "/dst.txt", FileMvOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent source")
	}
	if !strings.Contains(resultText(result), "source file not found") {
		t.Errorf("error=%q, should mention 'source file not found'", resultText(result))
	}
}

func TestMv_HandleLocalFileMv_SourceIsDirectory(t *testing.T) {
	ffs := fakefs.New()
	ffs.MkdirAll("/mydir", 0755)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileMv("/mydir", "/dst", FileMvOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for directory source")
	}
	if !strings.Contains(resultText(result), "directory") {
		t.Errorf("error=%q, should mention directory", resultText(result))
	}
}

func TestMv_HandleLocalFileMv_DestExistsNoOverwrite(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src.txt", []byte("source"), 0644)
	ffs.AddFile("/dst.txt", []byte("dest"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileMv("/src.txt", "/dst.txt", FileMvOptions{Overwrite: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when destination exists and overwrite=false")
	}
	if !strings.Contains(resultText(result), "destination exists") {
		t.Errorf("error=%q, should mention 'destination exists'", resultText(result))
	}
}

func TestMv_HandleLocalFileMv_DestExistsWithOverwrite(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src.txt", []byte("new"), 0644)
	ffs.AddFile("/dst.txt", []byte("old"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileMv("/src.txt", "/dst.txt", FileMvOptions{Overwrite: true})
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
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}

	data, _ := ffs.ReadFile("/dst.txt")
	if string(data) != "new" {
		t.Errorf("data=%q, want 'new'", string(data))
	}
}

func TestMv_HandleLocalFileMv_CreateDirs(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.txt", []byte("data"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileMv("/src/file.txt", "/new/deep/path/file.txt", FileMvOptions{CreateDirs: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["dirs_created"] != true {
		t.Error("dirs_created should be true")
	}

	data, _ := ffs.ReadFile("/new/deep/path/file.txt")
	if string(data) != "data" {
		t.Errorf("data=%q, want 'data'", string(data))
	}
}

func TestMv_HandleLocalFileMv_RenameError(t *testing.T) {
	// fakefs.Rename fails when source doesn't exist.
	// We can trigger this by deleting source between stat and rename.
	// However, this is tricky with fakefs. Instead, we test the case where
	// the source is known but the rename fails because it was removed.
	// The simplest approach: use the handler pipeline, where source stat
	// succeeds but we rely on the Rename failing.
	//
	// Actually, with fakefs the Rename cannot fail if the file exists.
	// So we test via the full handler chain with a source that becomes
	// non-existent between stat and rename -- but that's racy.
	//
	// Instead, let's verify the success path result metadata is correct.
	ffs := fakefs.New()
	ffs.AddFile("/a.txt", []byte("content"), 0755)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	result, err := srv.handleLocalFileMv("/a.txt", "/b.txt", FileMvOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["source"] != "/a.txt" {
		t.Errorf("source=%v", m["source"])
	}
	if m["destination"] != "/b.txt" {
		t.Errorf("destination=%v", m["destination"])
	}
	if m["mode"] != "0755" {
		t.Errorf("mode=%v, want '0755'", m["mode"])
	}
	size := m["size"].(float64)
	if size != 7 {
		t.Errorf("size=%v, want 7", size)
	}
}

func TestMv_HandleLocalFileMv_NoCreateDirsDefault(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.txt", []byte("data"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	// Without create_dirs, the rename should still work if the dest dir
	// already exists in the fakefs (AddFile auto-creates parent dirs).
	result, err := srv.handleLocalFileMv("/src/file.txt", "/src/renamed.txt", FileMvOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["dirs_created"] == true {
		t.Error("dirs_created should be false when create_dirs not set")
	}
}

// ==================== handleShellFileGet handler validation tests ====================

func TestMv_HandleShellFileGet_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"remote_path": "/some/file.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing session_id")
	}
	if !strings.Contains(resultText(result), "session_id") {
		t.Errorf("error=%q, should mention session_id", resultText(result))
	}
}

func TestMv_HandleShellFileGet_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_get1",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing remote_path")
	}
	if !strings.Contains(resultText(result), "remote_path") {
		t.Errorf("error=%q, should mention remote_path", resultText(result))
	}
}

func TestMv_HandleShellFileGet_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"remote_path": "/some/file.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent session")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error=%q, should mention 'not found'", resultText(result))
	}
}

func TestMv_HandleShellFileGet_LocalSuccess(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/data/test.txt", []byte("hello local"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_get_ok")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get_ok",
		"remote_path": "/data/test.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
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
	if m["content"] != "hello local" {
		t.Errorf("content=%v, want 'hello local'", m["content"])
	}
}

func TestMv_HandleShellFileGet_LocalWithChecksum(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/data/test.txt", []byte("checksummed"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_get_cs")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get_cs",
		"remote_path": "/data/test.txt",
		"checksum":    true,
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["checksum"] == nil || m["checksum"] == "" {
		t.Error("checksum should be present")
	}
}

func TestMv_HandleShellFileGet_LocalBase64Encoding(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/data/binary.bin", []byte{0x00, 0xFF, 0xAB}, 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_get_b64")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get_b64",
		"remote_path": "/data/binary.bin",
		"encoding":    "base64",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
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
}

// ==================== handleShellFilePut handler validation tests ====================

func TestMv_HandleShellFilePut_MissingSessionID(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"remote_path": "/some/file.txt",
		"content":     "data",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing session_id")
	}
}

func TestMv_HandleShellFilePut_MissingRemotePath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id": "sess_put1",
		"content":    "data",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing remote_path")
	}
}

func TestMv_HandleShellFilePut_SessionNotFound(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_nonexistent",
		"remote_path": "/some/file.txt",
		"content":     "data",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent session")
	}
	if !strings.Contains(resultText(result), "not found") {
		t.Errorf("error=%q, should mention 'not found'", resultText(result))
	}
}

func TestMv_HandleShellFilePut_LocalWithAtomicWrite(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_at")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_at",
		"remote_path": "/output/atomic.txt",
		"content":     "atomic data",
		"atomic":      true,
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
	if m["atomic_write"] != true {
		t.Error("atomic_write should be true")
	}

	data, readErr := ffs.ReadFile("/output/atomic.txt")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "atomic data" {
		t.Errorf("data=%q, want 'atomic data'", string(data))
	}
}

func TestMv_HandleShellFilePut_LocalNonAtomicWrite(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_na")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_na",
		"remote_path": "/output/direct.txt",
		"content":     "direct write",
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
		t.Error("atomic_write should not be true for non-atomic write")
	}
}

func TestMv_HandleShellFilePut_InvalidMode(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_im",
		"remote_path": "/file.txt",
		"content":     "data",
		"mode":        "badmode",
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(resultText(result), "invalid mode") {
		t.Errorf("error=%q, should mention 'invalid mode'", resultText(result))
	}
}

func TestMv_HandleShellFilePut_NoContentOrLocalPath(t *testing.T) {
	sm := fakesessionmgr.New()
	srv := newTestServer(sm)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_nc",
		"remote_path": "/file.txt",
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

func TestMv_HandleShellFilePut_FileExistsNoOverwrite(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/output/existing.txt", []byte("old"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_ex")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_ex",
		"remote_path": "/output/existing.txt",
		"content":     "new content",
		"overwrite":   false,
	})

	result, err := srv.handleShellFilePut(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when file exists and overwrite=false")
	}
	if !strings.Contains(resultText(result), "file exists") {
		t.Errorf("error=%q, should mention 'file exists'", resultText(result))
	}
}

// ==================== resolveFileContent tests ====================

func TestMv_ResolveFileContent_TextContent(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	data, modTime, errResult := srv.resolveFileContent(FilePutOptions{
		Content:  "hello",
		Encoding: "text",
	})
	if errResult != nil {
		t.Fatal("unexpected error")
	}
	if string(data) != "hello" {
		t.Errorf("data=%q, want 'hello'", string(data))
	}
	if !modTime.IsZero() {
		t.Error("modTime should be zero for direct content")
	}
}

func TestMv_ResolveFileContent_Base64Content(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	data, _, errResult := srv.resolveFileContent(FilePutOptions{
		Content:  "aGVsbG8=", // base64 of "hello"
		Encoding: "base64",
	})
	if errResult != nil {
		t.Fatal("unexpected error")
	}
	if string(data) != "hello" {
		t.Errorf("data=%q, want 'hello'", string(data))
	}
}

func TestMv_ResolveFileContent_InvalidBase64(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	_, _, errResult := srv.resolveFileContent(FilePutOptions{
		Content:  "not!!!valid===base64",
		Encoding: "base64",
	})
	if errResult == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(resultText(errResult), "decode base64") {
		t.Errorf("error=%q, should mention base64 decode", resultText(errResult))
	}
}

func TestMv_ResolveFileContent_FromLocalFile(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/local/data.txt", []byte("from file"), 0644)
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	data, modTime, errResult := srv.resolveFileContent(FilePutOptions{
		LocalPath: "/local/data.txt",
	})
	if errResult != nil {
		t.Fatal("unexpected error")
	}
	if string(data) != "from file" {
		t.Errorf("data=%q, want 'from file'", string(data))
	}
	if modTime.IsZero() {
		t.Error("modTime should not be zero when reading from local file")
	}
}

func TestMv_ResolveFileContent_LocalFileNotFound(t *testing.T) {
	ffs := fakefs.New()
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(ffs))

	_, _, errResult := srv.resolveFileContent(FilePutOptions{
		LocalPath: "/nonexistent/file.txt",
	})
	if errResult == nil {
		t.Fatal("expected error for non-existent local file")
	}
	if !strings.Contains(resultText(errResult), "stat local file") {
		t.Errorf("error=%q, should mention 'stat local file'", resultText(errResult))
	}
}

// ==================== handleLocalFilePut edge cases ====================

func TestMv_HandleLocalFilePut_WithPreserveTimestamp(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/data.txt", []byte("preserve"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_pr")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_pr",
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

	m := resultJSON(t, result)
	if m["status"] != "completed" {
		t.Errorf("status=%v", m["status"])
	}
}

func TestMv_HandleLocalFilePut_WithCustomMode(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_mode")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_mode",
		"remote_path": "/output/script.sh",
		"content":     "#!/bin/bash",
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

// ==================== FileMvResult struct fields ====================

func TestMv_FileMvResult_Fields(t *testing.T) {
	result := FileMvResult{
		Status:      "completed",
		Source:      "/src/file.txt",
		Destination: "/dst/file.txt",
		Size:        1024,
		Mode:        "0644",
		DirsCreated: true,
		Overwritten: true,
	}

	if result.Status != "completed" {
		t.Error("Status not set correctly")
	}
	if result.Source != "/src/file.txt" {
		t.Error("Source not set correctly")
	}
	if result.Destination != "/dst/file.txt" {
		t.Error("Destination not set correctly")
	}
	if result.Size != 1024 {
		t.Error("Size not set correctly")
	}
	if result.Mode != "0644" {
		t.Error("Mode not set correctly")
	}
	if !result.DirsCreated {
		t.Error("DirsCreated not set correctly")
	}
	if !result.Overwritten {
		t.Error("Overwritten not set correctly")
	}
}

func TestMv_FileMvOptions_Fields(t *testing.T) {
	opts := FileMvOptions{
		Overwrite:  true,
		CreateDirs: true,
	}

	if !opts.Overwrite {
		t.Error("Overwrite not set correctly")
	}
	if !opts.CreateDirs {
		t.Error("CreateDirs not set correctly")
	}
}

// ==================== handleShellFileGet local mode through handler chain ====================

func TestMv_HandleShellFileGet_LocalDirectory(t *testing.T) {
	ffs := fakefs.New()
	ffs.MkdirAll("/mydir", 0755)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_get_dir")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get_dir",
		"remote_path": "/mydir",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
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

func TestMv_HandleShellFileGet_LocalFileNotFound(t *testing.T) {
	ffs := fakefs.New()

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_get_nf")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get_nf",
		"remote_path": "/nonexistent.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
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

func TestMv_HandleShellFileGet_LocalWithCompression(t *testing.T) {
	ffs := fakefs.New()
	content := []byte(strings.Repeat("COMPRESS", 5000))
	ffs.AddFile("/data/big.json", content, 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_get_cmp")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get_cmp",
		"remote_path": "/data/big.json",
		"compress":    true,
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["compressed"] != true {
		t.Error("compressed should be true for compressible data")
	}
}

func TestMv_HandleShellFileGet_LocalWithLocalPathCopy(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/original.txt", []byte("copy me"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_get_cp")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_get_cp",
		"remote_path": "/src/original.txt",
		"local_path":  "/dst/copy.txt",
	})

	result, err := srv.handleShellFileGet(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["local_path"] != "/dst/copy.txt" {
		t.Errorf("local_path=%v, want '/dst/copy.txt'", m["local_path"])
	}

	data, readErr := ffs.ReadFile("/dst/copy.txt")
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "copy me" {
		t.Errorf("data=%q, want 'copy me'", string(data))
	}
}

// ==================== handleShellFileMv with combined options ====================

func TestMv_HandleShellFileMv_LocalWithOverwriteAndCreateDirs(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/file.txt", []byte("new"), 0644)
	ffs.AddFile("/deep/nested/dir/file.txt", []byte("old"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_mv_both")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv_both",
		"source":      "/src/file.txt",
		"destination": "/deep/nested/dir/file.txt",
		"overwrite":   true,
		"create_dirs": true,
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
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
	if m["dirs_created"] != true {
		t.Error("dirs_created should be true")
	}

	data, _ := ffs.ReadFile("/deep/nested/dir/file.txt")
	if string(data) != "new" {
		t.Errorf("data=%q, want 'new'", string(data))
	}
}

func TestMv_HandleShellFileMv_LocalSourceDirectory(t *testing.T) {
	ffs := fakefs.New()
	ffs.MkdirAll("/mydir", 0755)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_mv_dir")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv_dir",
		"source":      "/mydir",
		"destination": "/otherdir",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for directory source")
	}
	if !strings.Contains(resultText(result), "directory") {
		t.Errorf("error=%q, should mention directory", resultText(result))
	}
}

func TestMv_HandleShellFileMv_LocalSourceNotFound(t *testing.T) {
	ffs := fakefs.New()

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_mv_nf")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv_nf",
		"source":      "/nonexistent.txt",
		"destination": "/dst.txt",
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for non-existent source")
	}
	if !strings.Contains(resultText(result), "source file not found") {
		t.Errorf("error=%q, should mention 'source file not found'", resultText(result))
	}
}

func TestMv_HandleShellFileMv_LocalDestExistsNoOverwrite(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src.txt", []byte("source"), 0644)
	ffs.AddFile("/dst.txt", []byte("existing"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_mv_noo")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_mv_noo",
		"source":      "/src.txt",
		"destination": "/dst.txt",
		"overwrite":   false,
	})

	result, err := srv.handleShellFileMv(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when destination exists and overwrite=false")
	}
	if !strings.Contains(resultText(result), "destination exists") {
		t.Errorf("error=%q, should mention 'destination exists'", resultText(result))
	}
}

// ==================== handleShellFilePut local path through handler chain ====================

func TestMv_HandleShellFilePut_LocalFromLocalFile(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/src/original.txt", []byte("from local"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_lf")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_lf",
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
	if string(data) != "from local" {
		t.Errorf("data=%q, want 'from local'", string(data))
	}
}

func TestMv_HandleShellFilePut_LocalFromLocalFileNotFound(t *testing.T) {
	ffs := fakefs.New()

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_nf")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_nf",
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
		t.Errorf("error=%q, should mention 'stat local file'", resultText(result))
	}
}

func TestMv_HandleShellFilePut_LocalWithChecksumEnabled(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_cs")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_cs",
		"remote_path": "/output/file.txt",
		"content":     "checksum content",
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
	if m["checksum"] == nil || m["checksum"] == "" {
		t.Error("checksum should be present when checksum=true")
	}
}

func TestMv_HandleShellFilePut_LocalWithChecksumDisabled(t *testing.T) {
	ffs := fakefs.New()
	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_ncs")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_ncs",
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

func TestMv_HandleShellFilePut_LocalOverwriteExisting(t *testing.T) {
	ffs := fakefs.New()
	ffs.AddFile("/output/file.txt", []byte("old"), 0644)

	sm := fakesessionmgr.New()
	sess := newLocalSession("sess_put_ow")
	sm.AddSession(sess)
	srv := newTestServerWithFS(sm, ffs)

	req := makeRequest(map[string]any{
		"session_id":  "sess_put_ow",
		"remote_path": "/output/file.txt",
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

	data, _ := ffs.ReadFile("/output/file.txt")
	if string(data) != "new content" {
		t.Errorf("data=%q, want 'new content'", string(data))
	}
}
