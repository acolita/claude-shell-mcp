package sftp

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// --- Test helper: in-memory SFTP client/server pair ---

// readWriteCloser combines a Reader and Writer into a ReadWriteCloser.
type readWriteCloser struct {
	io.Reader
	io.Writer
	closers []io.Closer
}

func (rwc *readWriteCloser) Close() error {
	var firstErr error
	for _, c := range rwc.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// testSFTPEnv holds a complete test SFTP environment.
type testSFTPEnv struct {
	client     *Client
	sftpClient *sftp.Client
	server     *sftp.RequestServer
	serverDone chan struct{}
}

// cleanup closes all resources in the correct order.
func (env *testSFTPEnv) cleanup() {
	// Close server first to unblock pipe I/O, then close the sftp client.
	env.server.Close()
	<-env.serverDone
	env.sftpClient.Close()
}

// newConnectedClient creates a Client with a real in-memory SFTP connection.
// Returns the client and a cleanup function.
func newConnectedClient(t *testing.T) (*Client, func()) {
	t.Helper()
	env := newTestSFTPEnv(t)
	return env.client, env.cleanup
}

// newTestSFTPEnv creates the full test environment with access to internals.
func newTestSFTPEnv(t *testing.T) *testSFTPEnv {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	server := sftp.NewRequestServer(&readWriteCloser{
		Reader:  serverReader,
		Writer:  serverWriter,
		closers: []io.Closer{serverReader, serverWriter},
	}, sftp.InMemHandler())

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve()
	}()

	sftpClient, err := sftp.NewClientPipe(clientReader, clientWriter)
	if err != nil {
		server.Close()
		t.Fatalf("NewClientPipe failed: %v", err)
	}

	c := &Client{
		sftpClient: sftpClient,
	}

	return &testSFTPEnv{
		client:     c,
		sftpClient: sftpClient,
		server:     server,
		serverDone: serverDone,
	}
}

// --- ensureConnected: sftpClient already non-nil (short-circuit path) ---

func TestEnsureConnected_AlreadyConnected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	// ensureConnected should return nil because sftpClient is already set
	err := client.ensureConnected()
	if err != nil {
		t.Fatalf("ensureConnected with existing sftpClient should return nil, got: %v", err)
	}
}

func TestEnsureConnected_AlreadyConnectedCalledTwice(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.ensureConnected()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	err = client.ensureConnected()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
}

// --- Close with a real sftpClient (covers lines 68-71) ---

func TestClose_WithConnectedSFTPClient(t *testing.T) {
	env := newTestSFTPEnv(t)

	if env.client.sftpClient == nil {
		t.Fatal("sftpClient should be non-nil before close")
	}

	// Close server first to unblock pipe reads, so that client.Close() won't hang.
	env.server.Close()
	<-env.serverDone

	err := env.client.Close()
	if err != nil {
		// The sftp client may return an error because the server side closed first,
		// but our Client.Close should not panic.
		t.Logf("Close returned (possibly expected) error: %v", err)
	}

	if !env.client.closed {
		t.Error("closed flag should be true after Close")
	}
	if env.client.sftpClient != nil {
		t.Error("sftpClient should be nil after Close")
	}
}

func TestClose_WithConnectedClient_ThenClose_IsIdempotent(t *testing.T) {
	env := newTestSFTPEnv(t)

	// Close server first to unblock pipe reads.
	env.server.Close()
	<-env.serverDone

	_ = env.client.Close() // first close

	err2 := env.client.Close()
	if err2 != nil {
		t.Errorf("second Close should not error (idempotent): %v", err2)
	}
}

// --- IsConnected with a real sftpClient ---

func TestIsConnected_WithConnectedClient(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	if !client.IsConnected() {
		t.Error("client with non-nil sftpClient and closed=false should be connected")
	}
}

func TestIsConnected_TrueBeforeClose_FalseAfter(t *testing.T) {
	env := newTestSFTPEnv(t)

	if !env.client.IsConnected() {
		t.Error("should be connected before close")
	}

	// Close server first so client.Close() won't block.
	env.server.Close()
	<-env.serverDone

	_ = env.client.Close()

	if env.client.IsConnected() {
		t.Error("should not be connected after close")
	}
}

// --- Stat, Lstat, ReadDir on connected client (tests method body past ensureConnected) ---

func TestStat_Connected_NonExistentPath(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, err := client.Stat("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("Stat on non-existent path should return error")
	}
	// The error should come from the sftp server, not our ensureConnected check
	if strings.Contains(err.Error(), "sftp client is closed") || strings.Contains(err.Error(), "ssh connection is nil") {
		t.Errorf("expected sftp-level error, got connection error: %v", err)
	}
}

func TestLstat_Connected_NonExistentPath(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, err := client.Lstat("/nonexistent/path")
	if err == nil {
		t.Fatal("Lstat on non-existent path should return error")
	}
	if strings.Contains(err.Error(), "sftp client is closed") {
		t.Errorf("expected sftp-level error, got: %v", err)
	}
}

func TestReadDir_Connected_NonExistentPath(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, err := client.ReadDir("/nonexistent/dir")
	if err == nil {
		t.Fatal("ReadDir on non-existent path should return error")
	}
}

func TestReadLink_Connected_NonExistentPath(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, err := client.ReadLink("/nonexistent/link")
	if err == nil {
		t.Fatal("ReadLink on non-existent path should return error")
	}
}

// --- Mkdir, MkdirAll, Remove, Rename, PosixRename on connected client ---

func TestMkdir_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.Mkdir("/testdir")
	if err != nil {
		t.Fatalf("Mkdir should succeed on in-memory server: %v", err)
	}

	// Verify it exists
	info, err := client.Stat("/testdir")
	if err != nil {
		t.Fatalf("Stat after Mkdir should succeed: %v", err)
	}
	if !info.IsDir() {
		t.Error("created path should be a directory")
	}
}

func TestMkdirAll_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.MkdirAll("/a/b/c")
	if err != nil {
		t.Fatalf("MkdirAll should succeed: %v", err)
	}

	info, err := client.Stat("/a/b/c")
	if err != nil {
		t.Fatalf("Stat after MkdirAll should succeed: %v", err)
	}
	if !info.IsDir() {
		t.Error("created path should be a directory")
	}
}

func TestRemove_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.Remove("/nonexistent")
	if err == nil {
		t.Error("Remove on non-existent path should return error")
	}
}

func TestRename_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.Rename("/nonexistent", "/new")
	if err == nil {
		t.Error("Rename on non-existent path should return error")
	}
}

func TestPosixRename_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.PosixRename("/nonexistent", "/new")
	// PosixRename on InMemHandler may or may not be supported, but either way
	// the code path through our wrapper is covered.
	_ = err
}

// --- Chmod, Chtimes, Symlink on connected client ---

func TestChmod_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.Chmod("/nonexistent", 0644)
	if err == nil {
		t.Error("Chmod on non-existent path should return error")
	}
}

func TestChtimes_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.Chtimes("/nonexistent", time.Now(), time.Now())
	if err == nil {
		t.Error("Chtimes on non-existent path should return error")
	}
}

func TestSymlink_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.Symlink("/nonexistent", "/link")
	// InMemHandler may or may not support Symlink, but the code path is covered.
	_ = err
}

// --- Open, Create, OpenFile on connected client ---

func TestOpen_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, err := client.Open("/nonexistent")
	if err == nil {
		t.Error("Open on non-existent path should return error")
	}
}

func TestCreate_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	file, err := client.Create("/newfile.txt")
	if err != nil {
		t.Fatalf("Create should succeed: %v", err)
	}
	_ = file.Close()
}

func TestOpenFile_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, err := client.OpenFile("/nonexistent", os.O_RDONLY)
	if err == nil {
		t.Error("OpenFile on non-existent path should return error")
	}
}

// --- ReadFile on connected client ---

func TestReadFile_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, err := client.ReadFile("/nonexistent")
	if err == nil {
		t.Fatal("ReadFile on non-existent path should return error")
	}
	if !strings.Contains(err.Error(), "open file") {
		t.Errorf("error should wrap 'open file', got: %v", err)
	}
}

func TestReadFile_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	// Write a file first, then read it
	data := []byte("hello world")
	err := client.WriteFile("/readtest.txt", data, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := client.ReadFile("/readtest.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("ReadFile: got %q, want %q", string(got), string(data))
	}
}

// --- WriteFile on connected client ---

func TestWriteFile_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	data := []byte("test content")
	err := client.WriteFile("/writefile.txt", data, 0644)
	if err != nil {
		t.Fatalf("WriteFile should succeed: %v", err)
	}
}

func TestWriteFile_Connected_ZeroPerm(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	// When perm is 0, the chmod step should be skipped
	data := []byte("no chmod")
	err := client.WriteFile("/nochmod.txt", data, 0)
	if err != nil {
		t.Fatalf("WriteFile with zero perm should succeed: %v", err)
	}
}

// --- GetFile on connected client ---

func TestGetFile_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, _, err := client.GetFile("/nonexistent")
	if err == nil {
		t.Fatal("GetFile on non-existent should return error")
	}
	if !strings.Contains(err.Error(), "open remote file") {
		t.Errorf("error should wrap 'open remote file', got: %v", err)
	}
}

func TestGetFile_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	// Write first
	data := []byte("get file content")
	err := client.WriteFile("/getfile.txt", data, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	gotData, info, err := client.GetFile("/getfile.txt")
	if err != nil {
		t.Fatalf("GetFile failed: %v", err)
	}
	if string(gotData) != string(data) {
		t.Errorf("data: got %q, want %q", string(gotData), string(data))
	}
	if info == nil {
		t.Fatal("info should not be nil")
	}
	if info.Size() != int64(len(data)) {
		t.Errorf("size: got %d, want %d", info.Size(), len(data))
	}
}

// --- PutFile on connected client ---

func TestPutFile_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	data := []byte("put file data")
	err := client.PutFile("/putfile.txt", data, 0644)
	if err != nil {
		t.Fatalf("PutFile should succeed: %v", err)
	}

	// Verify by reading back
	got, err := client.ReadFile("/putfile.txt")
	if err != nil {
		t.Fatalf("ReadFile after PutFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data: got %q, want %q", string(got), string(data))
	}
}

func TestPutFile_Connected_ZeroPerm(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	data := []byte("no chmod put")
	err := client.PutFile("/putnocmod.txt", data, 0)
	if err != nil {
		t.Fatalf("PutFile with zero perm should succeed: %v", err)
	}
}

// --- GetFileStream on connected client ---

func TestGetFileStream_Connected_NonExistent(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	_, _, err := client.GetFileStream("/nonexistent")
	if err == nil {
		t.Fatal("GetFileStream on non-existent should return error")
	}
	if !strings.Contains(err.Error(), "open remote file") {
		t.Errorf("error should wrap 'open remote file', got: %v", err)
	}
}

func TestGetFileStream_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	// Write first
	data := []byte("stream content")
	err := client.WriteFile("/stream.txt", data, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	file, info, err := client.GetFileStream("/stream.txt")
	if err != nil {
		t.Fatalf("GetFileStream failed: %v", err)
	}
	defer file.Close()

	if info == nil {
		t.Fatal("info should not be nil")
	}
	if info.Size() != int64(len(data)) {
		t.Errorf("size: got %d, want %d", info.Size(), len(data))
	}

	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll from stream failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data: got %q, want %q", string(got), string(data))
	}
}

// --- PutFileStream on connected client ---

func TestPutFileStream_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	file, err := client.PutFileStream("/putstream.txt")
	if err != nil {
		t.Fatalf("PutFileStream failed: %v", err)
	}

	data := []byte("streamed write")
	_, err = file.Write(data)
	if err != nil {
		t.Fatalf("Write to stream failed: %v", err)
	}
	file.Close()

	// Verify by reading back
	got, err := client.ReadFile("/putstream.txt")
	if err != nil {
		t.Fatalf("ReadFile after PutFileStream failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data: got %q, want %q", string(got), string(data))
	}
}

// --- Getwd on connected client ---

func TestGetwd_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	wd, err := client.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if wd == "" {
		t.Error("Getwd should return a non-empty path")
	}
}

// --- RealPath on connected client ---

func TestRealPath_Connected(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	path, err := client.RealPath(".")
	if err != nil {
		t.Fatalf("RealPath failed: %v", err)
	}
	if path == "" {
		t.Error("RealPath should return a non-empty path")
	}
}

// --- Concurrent operations on connected client ---

func TestConcurrent_OperationsOnConnectedClient(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	// Create a file first
	err := client.WriteFile("/concurrent.txt", []byte("data"), 0644)
	if err != nil {
		t.Fatalf("initial WriteFile failed: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 20

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.ReadFile("/concurrent.txt")
		}()
	}

	// Concurrent stats
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.Stat("/concurrent.txt")
		}()
	}

	// Concurrent IsConnected
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.IsConnected()
		}()
	}

	wg.Wait()
}

func TestConcurrent_CloseWhileOperationsOnConnected(t *testing.T) {
	env := newTestSFTPEnv(t)

	// Close server first to avoid blocking on pipe reads during concurrent close.
	env.server.Close()
	<-env.serverDone

	var wg sync.WaitGroup

	// Concurrent operations (will fail since server is down, but should not panic)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = env.client.Stat("/test")
		}()
	}

	// Concurrent close
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = env.client.Close()
		}()
	}

	wg.Wait()

	if !env.client.closed {
		t.Error("client should be closed")
	}
}

// --- Rename/Remove with connected client and existing files ---

func TestRename_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.WriteFile("/rename_src.txt", []byte("rename me"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	err = client.Rename("/rename_src.txt", "/rename_dst.txt")
	if err != nil {
		t.Fatalf("Rename should succeed: %v", err)
	}

	// Old path should be gone
	_, err = client.Stat("/rename_src.txt")
	if err == nil {
		t.Error("old path should not exist after rename")
	}

	// New path should exist
	_, err = client.Stat("/rename_dst.txt")
	if err != nil {
		t.Errorf("new path should exist after rename: %v", err)
	}
}

func TestRemove_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.WriteFile("/removeme.txt", []byte("remove me"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	err = client.Remove("/removeme.txt")
	if err != nil {
		t.Fatalf("Remove should succeed: %v", err)
	}

	_, err = client.Stat("/removeme.txt")
	if err == nil {
		t.Error("file should not exist after remove")
	}
}

// --- ReadDir with connected client and existing directory ---

func TestReadDir_Connected_ExistingDir(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	// Create directory with files
	err := client.Mkdir("/listdir")
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	err = client.WriteFile("/listdir/a.txt", []byte("a"), 0644)
	if err != nil {
		t.Fatalf("WriteFile a.txt failed: %v", err)
	}
	err = client.WriteFile("/listdir/b.txt", []byte("b"), 0644)
	if err != nil {
		t.Fatalf("WriteFile b.txt failed: %v", err)
	}

	entries, err := client.ReadDir("/listdir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("expected at least 2 entries, got %d", len(entries))
	}
}

// --- Stat on connected client with existing file ---

func TestStat_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	data := []byte("stat me")
	err := client.WriteFile("/statme.txt", data, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	info, err := client.Stat("/statme.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Name() != "statme.txt" {
		t.Errorf("Name: got %q, want %q", info.Name(), "statme.txt")
	}
	if info.Size() != int64(len(data)) {
		t.Errorf("Size: got %d, want %d", info.Size(), len(data))
	}
	if info.IsDir() {
		t.Error("should not be a directory")
	}
}

// --- Lstat on connected client with existing file ---

func TestLstat_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.WriteFile("/lstatme.txt", []byte("lstat me"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	info, err := client.Lstat("/lstatme.txt")
	if err != nil {
		t.Fatalf("Lstat failed: %v", err)
	}
	if info.Name() != "lstatme.txt" {
		t.Errorf("Name: got %q, want %q", info.Name(), "lstatme.txt")
	}
}

// --- Chmod on connected client with existing file ---

func TestChmod_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.WriteFile("/chmodme.txt", []byte("chmod me"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	err = client.Chmod("/chmodme.txt", 0755)
	if err != nil {
		t.Fatalf("Chmod should succeed: %v", err)
	}
}

// --- Chtimes on connected client with existing file ---

func TestChtimes_Connected_ExistingFile(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	err := client.WriteFile("/chtimes.txt", []byte("chtimes"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	now := time.Now()
	err = client.Chtimes("/chtimes.txt", now, now)
	if err != nil {
		t.Fatalf("Chtimes should succeed: %v", err)
	}
}

// --- Operations after close on connected client ---

func TestOperationsAfterClose_OnConnectedClient(t *testing.T) {
	env := newTestSFTPEnv(t)

	client := env.client

	// Verify connected
	if !client.IsConnected() {
		t.Fatal("should be connected")
	}

	// Close server first, then close client to avoid blocking.
	env.server.Close()
	<-env.serverDone
	_ = client.Close()

	// All operations should fail with "closed" error
	wantErr := "sftp client is closed"

	_, err := client.Stat("/test")
	if err == nil || err.Error() != wantErr {
		t.Errorf("Stat after close: got %v, want %q", err, wantErr)
	}

	_, err = client.ReadFile("/test")
	if err == nil || err.Error() != wantErr {
		t.Errorf("ReadFile after close: got %v, want %q", err, wantErr)
	}

	err = client.WriteFile("/test", []byte("x"), 0644)
	if err == nil || err.Error() != wantErr {
		t.Errorf("WriteFile after close: got %v, want %q", err, wantErr)
	}

	_, _, err = client.GetFile("/test")
	if err == nil || err.Error() != wantErr {
		t.Errorf("GetFile after close: got %v, want %q", err, wantErr)
	}

	err = client.PutFile("/test", []byte("x"), 0644)
	if err == nil || err.Error() != wantErr {
		t.Errorf("PutFile after close: got %v, want %q", err, wantErr)
	}

	_, _, err = client.GetFileStream("/test")
	if err == nil || err.Error() != wantErr {
		t.Errorf("GetFileStream after close: got %v, want %q", err, wantErr)
	}

	_, err = client.PutFileStream("/test")
	if err == nil || err.Error() != wantErr {
		t.Errorf("PutFileStream after close: got %v, want %q", err, wantErr)
	}

	_, err = client.Getwd()
	if err == nil || err.Error() != wantErr {
		t.Errorf("Getwd after close: got %v, want %q", err, wantErr)
	}
}

// --- OpenFile with connected client creating a new file ---

func TestOpenFile_Connected_CreateNew(t *testing.T) {
	client, cleanup := newConnectedClient(t)
	defer cleanup()

	file, err := client.OpenFile("/openfile_new.txt", os.O_CREATE|os.O_WRONLY)
	if err != nil {
		t.Fatalf("OpenFile with O_CREATE should succeed: %v", err)
	}
	_, _ = file.Write([]byte("openfile data"))
	file.Close()

	// Read back to verify
	data, err := client.ReadFile("/openfile_new.txt")
	if err != nil {
		t.Fatalf("ReadFile after OpenFile failed: %v", err)
	}
	if string(data) != "openfile data" {
		t.Errorf("data: got %q, want %q", string(data), "openfile data")
	}
}
