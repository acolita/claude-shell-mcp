package sftp

import (
	"os"
	"sync"
	"testing"
	"time"
)

// --- ToFileInfo tests (pure helper function) ---

// mockFileInfo implements os.FileInfo for testing ToFileInfo.
type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (m mockFileInfo) Name() string      { return m.name }
func (m mockFileInfo) Size() int64       { return m.size }
func (m mockFileInfo) Mode() os.FileMode { return m.mode }
func (m mockFileInfo) ModTime() time.Time { return m.modTime }
func (m mockFileInfo) IsDir() bool       { return m.isDir }
func (m mockFileInfo) Sys() interface{}  { return nil }

func TestToFileInfo_RegularFile(t *testing.T) {
	modTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	info := mockFileInfo{
		name:    "test.txt",
		size:    1024,
		mode:    0644,
		modTime: modTime,
		isDir:   false,
	}

	result := ToFileInfo(info)

	if result.Name != "test.txt" {
		t.Errorf("Name: got %q, want %q", result.Name, "test.txt")
	}
	if result.Size != 1024 {
		t.Errorf("Size: got %d, want %d", result.Size, 1024)
	}
	if result.Mode != 0644 {
		t.Errorf("Mode: got %v, want %v", result.Mode, os.FileMode(0644))
	}
	if result.ModTime != modTime.Unix() {
		t.Errorf("ModTime: got %d, want %d", result.ModTime, modTime.Unix())
	}
	if result.IsDir {
		t.Error("IsDir: got true, want false")
	}
	if result.IsLink {
		t.Error("IsLink: got true, want false for regular file")
	}
}

func TestToFileInfo_Directory(t *testing.T) {
	modTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	info := mockFileInfo{
		name:    "mydir",
		size:    4096,
		mode:    os.ModeDir | 0755,
		modTime: modTime,
		isDir:   true,
	}

	result := ToFileInfo(info)

	if result.Name != "mydir" {
		t.Errorf("Name: got %q, want %q", result.Name, "mydir")
	}
	if result.Size != 4096 {
		t.Errorf("Size: got %d, want %d", result.Size, 4096)
	}
	if !result.IsDir {
		t.Error("IsDir: got false, want true")
	}
	if result.IsLink {
		t.Error("IsLink: got true, want false for directory")
	}
	// Mode should include the ModeDir bit
	if result.Mode&os.ModeDir == 0 {
		t.Error("Mode should include ModeDir bit")
	}
}

func TestToFileInfo_Symlink(t *testing.T) {
	modTime := time.Date(2025, 3, 20, 12, 0, 0, 0, time.UTC)
	info := mockFileInfo{
		name:    "link.txt",
		size:    0,
		mode:    os.ModeSymlink | 0777,
		modTime: modTime,
		isDir:   false,
	}

	result := ToFileInfo(info)

	if result.Name != "link.txt" {
		t.Errorf("Name: got %q, want %q", result.Name, "link.txt")
	}
	if !result.IsLink {
		t.Error("IsLink: got false, want true for symlink")
	}
	if result.IsDir {
		t.Error("IsDir: got true, want false for symlink")
	}
	if result.Mode&os.ModeSymlink == 0 {
		t.Error("Mode should include ModeSymlink bit")
	}
}

func TestToFileInfo_ZeroSize(t *testing.T) {
	info := mockFileInfo{
		name:    "empty.txt",
		size:    0,
		mode:    0644,
		modTime: time.Unix(0, 0),
		isDir:   false,
	}

	result := ToFileInfo(info)

	if result.Size != 0 {
		t.Errorf("Size: got %d, want 0", result.Size)
	}
	if result.ModTime != 0 {
		t.Errorf("ModTime: got %d, want 0 for Unix epoch", result.ModTime)
	}
}

func TestToFileInfo_LargeFile(t *testing.T) {
	info := mockFileInfo{
		name:    "large.bin",
		size:    1<<40 + 512, // ~1 TiB + 512 bytes
		mode:    0600,
		modTime: time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
		isDir:   false,
	}

	result := ToFileInfo(info)

	if result.Size != 1<<40+512 {
		t.Errorf("Size: got %d, want %d", result.Size, int64(1<<40+512))
	}
}

func TestToFileInfo_ExecutableFile(t *testing.T) {
	info := mockFileInfo{
		name:    "run.sh",
		size:    256,
		mode:    0755,
		modTime: time.Now(),
		isDir:   false,
	}

	result := ToFileInfo(info)

	if result.Mode != 0755 {
		t.Errorf("Mode: got %v, want %v", result.Mode, os.FileMode(0755))
	}
}

func TestToFileInfo_SpecialModes(t *testing.T) {
	tests := []struct {
		name     string
		mode     os.FileMode
		wantLink bool
	}{
		{"regular", 0644, false},
		{"symlink", os.ModeSymlink | 0777, true},
		{"device", os.ModeDevice | 0660, false},
		{"named_pipe", os.ModeNamedPipe | 0644, false},
		{"socket", os.ModeSocket | 0755, false},
		{"setuid", os.ModeSetuid | 0755, false},
		{"setgid", os.ModeSetgid | 0755, false},
		{"sticky", os.ModeSticky | 0755, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := mockFileInfo{
				name:    "file",
				mode:    tt.mode,
				modTime: time.Now(),
			}
			result := ToFileInfo(info)
			if result.IsLink != tt.wantLink {
				t.Errorf("IsLink: got %v, want %v", result.IsLink, tt.wantLink)
			}
			if result.Mode != tt.mode {
				t.Errorf("Mode: got %v, want %v", result.Mode, tt.mode)
			}
		})
	}
}

// --- NewClient tests ---

func TestNewClient_NilSSHConn(t *testing.T) {
	client := NewClient(nil)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.sshConn != nil {
		t.Error("sshConn should be nil when created with nil")
	}
	if client.sftpClient != nil {
		t.Error("sftpClient should be nil (lazy initialization)")
	}
	if client.closed {
		t.Error("closed should be false on new client")
	}
}

// --- IsConnected tests ---

func TestIsConnected_NewClient(t *testing.T) {
	client := NewClient(nil)
	if client.IsConnected() {
		t.Error("new client should not be connected (sftpClient is nil)")
	}
}

func TestIsConnected_AfterClose(t *testing.T) {
	client := NewClient(nil)
	_ = client.Close()
	if client.IsConnected() {
		t.Error("closed client should not be connected")
	}
}

// --- Close tests ---

func TestClose_NewClient(t *testing.T) {
	client := NewClient(nil)
	err := client.Close()
	if err != nil {
		t.Errorf("Close on new client should not error, got: %v", err)
	}
	if !client.closed {
		t.Error("closed should be true after Close()")
	}
}

func TestClose_Idempotent(t *testing.T) {
	client := NewClient(nil)

	err1 := client.Close()
	if err1 != nil {
		t.Errorf("first Close should not error: %v", err1)
	}

	err2 := client.Close()
	if err2 != nil {
		t.Errorf("second Close should not error (idempotent): %v", err2)
	}
}

func TestClose_SetsClosedFlag(t *testing.T) {
	client := NewClient(nil)
	_ = client.Close()

	if !client.closed {
		t.Error("closed flag should be true after Close()")
	}
}

// --- ensureConnected error paths (tested via public methods) ---

func TestEnsureConnected_ClosedClient(t *testing.T) {
	client := NewClient(nil)
	_ = client.Close()

	// All operations should fail with "sftp client is closed"
	_, err := client.Stat("/test")
	if err == nil {
		t.Fatal("Stat on closed client should return error")
	}
	if got := err.Error(); got != "sftp client is closed" {
		t.Errorf("error message: got %q, want %q", got, "sftp client is closed")
	}
}

func TestEnsureConnected_NilSSHConn(t *testing.T) {
	client := NewClient(nil)

	// sshConn is nil, so ensureConnected should fail
	_, err := client.Stat("/test")
	if err == nil {
		t.Fatal("Stat with nil SSH conn should return error")
	}
	if got := err.Error(); got != "ssh connection is nil" {
		t.Errorf("error message: got %q, want %q", got, "ssh connection is nil")
	}
}

// --- All methods fail on closed client ---

func TestAllMethods_FailOnClosedClient(t *testing.T) {
	client := NewClient(nil)
	_ = client.Close()

	wantErr := "sftp client is closed"

	t.Run("Stat", func(t *testing.T) {
		_, err := client.Stat("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Lstat", func(t *testing.T) {
		_, err := client.Lstat("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("ReadDir", func(t *testing.T) {
		_, err := client.ReadDir("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("ReadLink", func(t *testing.T) {
		_, err := client.ReadLink("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Mkdir", func(t *testing.T) {
		err := client.Mkdir("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("MkdirAll", func(t *testing.T) {
		err := client.MkdirAll("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Remove", func(t *testing.T) {
		err := client.Remove("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Rename", func(t *testing.T) {
		err := client.Rename("/old", "/new")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("PosixRename", func(t *testing.T) {
		err := client.PosixRename("/old", "/new")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Chmod", func(t *testing.T) {
		err := client.Chmod("/path", 0644)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Chtimes", func(t *testing.T) {
		err := client.Chtimes("/path", time.Now(), time.Now())
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Symlink", func(t *testing.T) {
		err := client.Symlink("/old", "/new")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Open", func(t *testing.T) {
		_, err := client.Open("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Create", func(t *testing.T) {
		_, err := client.Create("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("OpenFile", func(t *testing.T) {
		_, err := client.OpenFile("/path", os.O_RDONLY)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("ReadFile", func(t *testing.T) {
		_, err := client.ReadFile("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("WriteFile", func(t *testing.T) {
		err := client.WriteFile("/path", []byte("data"), 0644)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("GetFile", func(t *testing.T) {
		_, _, err := client.GetFile("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("PutFile", func(t *testing.T) {
		err := client.PutFile("/path", []byte("data"), 0644)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("GetFileStream", func(t *testing.T) {
		_, _, err := client.GetFileStream("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("PutFileStream", func(t *testing.T) {
		_, err := client.PutFileStream("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Getwd", func(t *testing.T) {
		_, err := client.Getwd()
		assertErrorContains(t, err, wantErr)
	})

	t.Run("RealPath", func(t *testing.T) {
		_, err := client.RealPath("/path")
		assertErrorContains(t, err, wantErr)
	})
}

// --- All methods fail with nil SSH connection ---

func TestAllMethods_FailOnNilSSHConn(t *testing.T) {
	wantErr := "ssh connection is nil"

	t.Run("Stat", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.Stat("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Lstat", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.Lstat("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("ReadDir", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.ReadDir("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("ReadLink", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.ReadLink("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Mkdir", func(t *testing.T) {
		client := NewClient(nil)
		err := client.Mkdir("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("MkdirAll", func(t *testing.T) {
		client := NewClient(nil)
		err := client.MkdirAll("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Remove", func(t *testing.T) {
		client := NewClient(nil)
		err := client.Remove("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Rename", func(t *testing.T) {
		client := NewClient(nil)
		err := client.Rename("/old", "/new")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("PosixRename", func(t *testing.T) {
		client := NewClient(nil)
		err := client.PosixRename("/old", "/new")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Chmod", func(t *testing.T) {
		client := NewClient(nil)
		err := client.Chmod("/path", 0644)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Chtimes", func(t *testing.T) {
		client := NewClient(nil)
		err := client.Chtimes("/path", time.Now(), time.Now())
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Symlink", func(t *testing.T) {
		client := NewClient(nil)
		err := client.Symlink("/old", "/new")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Open", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.Open("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Create", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.Create("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("OpenFile", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.OpenFile("/path", os.O_RDONLY)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("ReadFile", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.ReadFile("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("WriteFile", func(t *testing.T) {
		client := NewClient(nil)
		err := client.WriteFile("/path", []byte("data"), 0644)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("GetFile", func(t *testing.T) {
		client := NewClient(nil)
		_, _, err := client.GetFile("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("PutFile", func(t *testing.T) {
		client := NewClient(nil)
		err := client.PutFile("/path", []byte("data"), 0644)
		assertErrorContains(t, err, wantErr)
	})

	t.Run("GetFileStream", func(t *testing.T) {
		client := NewClient(nil)
		_, _, err := client.GetFileStream("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("PutFileStream", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.PutFileStream("/path")
		assertErrorContains(t, err, wantErr)
	})

	t.Run("Getwd", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.Getwd()
		assertErrorContains(t, err, wantErr)
	})

	t.Run("RealPath", func(t *testing.T) {
		client := NewClient(nil)
		_, err := client.RealPath("/path")
		assertErrorContains(t, err, wantErr)
	})
}

// --- Concurrency safety tests ---

func TestClose_ConcurrentSafety(t *testing.T) {
	client := NewClient(nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.Close()
		}()
	}
	wg.Wait()

	if !client.closed {
		t.Error("client should be closed after concurrent Close() calls")
	}
}

func TestIsConnected_ConcurrentSafety(t *testing.T) {
	client := NewClient(nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.IsConnected()
		}()
	}
	wg.Wait()
}

func TestConcurrent_CloseAndOperations(t *testing.T) {
	client := NewClient(nil)

	var wg sync.WaitGroup

	// Concurrent Close calls
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.Close()
		}()
	}

	// Concurrent operation calls (should all fail gracefully)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.Stat("/test")
		}()
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.IsConnected()
		}()
	}

	wg.Wait()
}

// --- State transition tests ---

func TestStateTransition_NewToClosedToOperations(t *testing.T) {
	client := NewClient(nil)

	// Initial state: not connected, not closed
	if client.IsConnected() {
		t.Error("new client should not be connected")
	}

	// Close
	err := client.Close()
	if err != nil {
		t.Errorf("Close should not error: %v", err)
	}

	// After close: not connected, closed
	if client.IsConnected() {
		t.Error("closed client should not be connected")
	}

	// Operations fail with "closed" error, not "nil ssh" error
	_, err = client.Stat("/test")
	if err == nil {
		t.Fatal("expected error on closed client")
	}
	if got := err.Error(); got != "sftp client is closed" {
		t.Errorf("expected 'sftp client is closed', got %q", got)
	}
}

func TestNewClient_SetsSSHConn(t *testing.T) {
	// We can't create a real ssh.Client without a server, but we can verify
	// that NewClient properly stores whatever is passed (tested with nil here,
	// which is the only safe option without an SSH server).
	client := NewClient(nil)
	if client.sshConn != nil {
		t.Error("sshConn should be nil when NewClient is called with nil")
	}
}

// --- FileInfo struct field tests ---

func TestFileInfo_FieldDefaults(t *testing.T) {
	// Verify zero value of FileInfo
	var fi FileInfo
	if fi.Name != "" {
		t.Errorf("Name zero value: got %q, want empty", fi.Name)
	}
	if fi.Size != 0 {
		t.Errorf("Size zero value: got %d, want 0", fi.Size)
	}
	if fi.Mode != 0 {
		t.Errorf("Mode zero value: got %v, want 0", fi.Mode)
	}
	if fi.ModTime != 0 {
		t.Errorf("ModTime zero value: got %d, want 0", fi.ModTime)
	}
	if fi.IsDir {
		t.Error("IsDir zero value: got true, want false")
	}
	if fi.IsLink {
		t.Error("IsLink zero value: got true, want false")
	}
}

func TestToFileInfo_PreservesAllFields(t *testing.T) {
	modTime := time.Date(2024, 7, 4, 15, 30, 45, 0, time.UTC)
	info := mockFileInfo{
		name:    "document.pdf",
		size:    5242880, // 5 MiB
		mode:    0640,
		modTime: modTime,
		isDir:   false,
	}

	result := ToFileInfo(info)

	// Verify all fields are correctly mapped
	if result.Name != info.Name() {
		t.Errorf("Name mismatch: got %q, want %q", result.Name, info.Name())
	}
	if result.Size != info.Size() {
		t.Errorf("Size mismatch: got %d, want %d", result.Size, info.Size())
	}
	if result.Mode != info.Mode() {
		t.Errorf("Mode mismatch: got %v, want %v", result.Mode, info.Mode())
	}
	if result.ModTime != info.ModTime().Unix() {
		t.Errorf("ModTime mismatch: got %d, want %d", result.ModTime, info.ModTime().Unix())
	}
	if result.IsDir != info.IsDir() {
		t.Errorf("IsDir mismatch: got %v, want %v", result.IsDir, info.IsDir())
	}
}

func TestToFileInfo_NegativeModTime(t *testing.T) {
	// Dates before Unix epoch should produce negative timestamps
	modTime := time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC)
	info := mockFileInfo{
		name:    "old.dat",
		size:    100,
		mode:    0644,
		modTime: modTime,
	}

	result := ToFileInfo(info)

	if result.ModTime >= 0 {
		t.Errorf("ModTime for pre-epoch date should be negative, got %d", result.ModTime)
	}
}

// --- Helper ---

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if got := err.Error(); got != want {
		t.Errorf("error: got %q, want %q", got, want)
	}
}
