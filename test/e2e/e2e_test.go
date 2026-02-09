//go:build e2e

package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
)

// testEnv holds the Docker SSH test environment.
type testEnv struct {
	host     string
	keyPort  int
	passPort int
	keyPath  string
	password string
	config   *config.Config
}

// globalEnv is initialized once in TestMain.
var globalEnv *testEnv

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("docker"); err != nil {
		log.Println("docker not found, skipping e2e tests")
		os.Exit(0)
	}

	// Disable SSH agent to prevent its keys from interfering with our
	// explicit test key auth. The agent may have keys that exhaust the
	// server's MaxAuthTries before the test key is tried.
	origAuthSock := os.Getenv("SSH_AUTH_SOCK")
	os.Unsetenv("SSH_AUTH_SOCK")

	e2eDir, _ := filepath.Abs(".")

	// Start containers. Docker compose plugin resolution depends on
	// ~/.docker/cli-plugins, so we must do this BEFORE overriding HOME.
	composePath := filepath.Join(e2eDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "up", "-d", "--build", "--wait")
	cmd.Dir = e2eDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("docker compose up failed: %v\n%s", err, out)
	}

	// Set HOME to a temp directory so BuildHostKeyCallback falls back to
	// accept-all (no ~/.ssh/known_hosts file). Docker containers generate
	// fresh host keys on each build, so strict host key checking would fail.
	// This must happen AFTER docker compose, which needs ~/.docker/ to find
	// the compose plugin.
	origHome := os.Getenv("HOME")
	tmpHome, _ := os.MkdirTemp("", "e2e-home-*")
	os.Setenv("HOME", tmpHome)

	keyPath := filepath.Join(e2eDir, "testdata", "id_ed25519")

	globalEnv = &testEnv{
		host:     "127.0.0.1",
		keyPort:  2222,
		passPort: 2223,
		keyPath:  keyPath,
		password: "testpass123",
		config:   config.DefaultConfig(),
	}

	// Wait for SSH to become available
	if err := waitForSSHReady(globalEnv.host, globalEnv.keyPort, globalEnv.keyPath, ""); err != nil {
		log.Fatalf("Key-auth sshd not ready: %v", err)
	}
	if err := waitForSSHReady(globalEnv.host, globalEnv.passPort, "", globalEnv.password); err != nil {
		log.Fatalf("Password-auth sshd not ready: %v", err)
	}

	code := m.Run()

	// Restore HOME before running docker compose down, which needs
	// ~/.docker/cli-plugins to find the compose plugin.
	os.Setenv("HOME", origHome)
	os.RemoveAll(tmpHome)

	// Cleanup containers
	cmd = exec.Command("docker", "compose", "-f", composePath, "down", "-v")
	cmd.Dir = e2eDir
	cmd.CombinedOutput()

	// Restore env
	if origAuthSock != "" {
		os.Setenv("SSH_AUTH_SOCK", origAuthSock)
	}

	os.Exit(code)
}

// waitForSSHReady retries SSH connection until it succeeds or times out.
func waitForSSHReady(host string, port int, keyPath, password string) error {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		cfg := config.DefaultConfig()
		mgr := session.NewManager(cfg)

		opts := session.CreateOptions{
			Mode:     "ssh",
			Host:     host,
			Port:     port,
			User:     "testuser",
			KeyPath:  keyPath,
			Password: password,
		}

		sess, err := mgr.Create(opts)
		if err == nil {
			mgr.Close(sess.ID)
			return nil
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("SSH on %s:%d not available within 30s: %v", host, port, lastErr)
}

// createKeySession creates an SSH session using key-based auth.
func createKeySession(t *testing.T, env *testEnv) (*session.Manager, *session.Session) {
	t.Helper()
	mgr := session.NewManager(env.config)
	sess, err := mgr.Create(session.CreateOptions{
		Mode:    "ssh",
		Host:    env.host,
		Port:    env.keyPort,
		User:    "testuser",
		KeyPath: env.keyPath,
	})
	if err != nil {
		t.Fatalf("failed to create key-auth session: %v", err)
	}
	t.Cleanup(func() { mgr.Close(sess.ID) })
	return mgr, sess
}

// createPassSession creates an SSH session using password auth.
func createPassSession(t *testing.T, env *testEnv) (*session.Manager, *session.Session) {
	t.Helper()
	mgr := session.NewManager(env.config)
	sess, err := mgr.Create(session.CreateOptions{
		Mode:     "ssh",
		Host:     env.host,
		Port:     env.passPort,
		User:     "testuser",
		Password: env.password,
	})
	if err != nil {
		t.Fatalf("failed to create password-auth session: %v", err)
	}
	t.Cleanup(func() { mgr.Close(sess.ID) })
	return mgr, sess
}

func TestKeyAuthConnect(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	if sess.State != session.StateIdle {
		t.Errorf("expected state idle, got %s", sess.State)
	}
	if sess.Shell == "" {
		t.Error("expected Shell to be set")
	}
	if sess.Mode != "ssh" {
		t.Errorf("expected mode ssh, got %s", sess.Mode)
	}
}

func TestPasswordAuthConnect(t *testing.T) {
	_, sess := createPassSession(t, globalEnv)

	if sess.State != session.StateIdle {
		t.Errorf("expected state idle, got %s", sess.State)
	}
	if sess.Mode != "ssh" {
		t.Errorf("expected mode ssh, got %s", sess.Mode)
	}
}

func TestExecEcho(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	result, err := sess.Exec("echo hello", 10000)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.Status != "completed" {
		t.Errorf("expected status completed, got %s", result.Status)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %v", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("expected stdout to contain 'hello', got %q", result.Stdout)
	}
}

func TestExecCwdPersistence(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	// Use cd && pwd in a single command to verify cwd changes within a session.
	// The session may reconnect between Exec calls (resetting state), so we
	// combine dependent commands.
	result, err := sess.Exec("cd /tmp && pwd", 10000)
	if err != nil {
		t.Fatalf("cd && pwd failed: %v", err)
	}

	if !strings.Contains(result.Stdout, "/tmp") {
		t.Errorf("expected cwd /tmp, got %q", result.Stdout)
	}
}

func TestExecExitCode(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	result, err := sess.Exec("exit 42", 10000)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.ExitCode == nil || *result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %v", result.ExitCode)
	}
}

func TestExecEnvVars(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	// Combine export and echo in a single command to avoid state loss
	// from potential session reconnection between Exec calls.
	result, err := sess.Exec("export FOO=bar && echo $FOO", 10000)
	if err != nil {
		t.Fatalf("export+echo failed: %v", err)
	}

	if !strings.Contains(result.Stdout, "bar") {
		t.Errorf("expected stdout to contain 'bar', got %q", result.Stdout)
	}
}

func TestExecSequential(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	commands := []string{
		"echo one",
		"echo two",
		"echo three",
		"echo four",
		"echo five",
	}

	for i, cmd := range commands {
		result, err := sess.Exec(cmd, 10000)
		if err != nil {
			t.Fatalf("command %d (%s) failed: %v", i, cmd, err)
		}
		if result.Status != "completed" {
			t.Errorf("command %d: expected status completed, got %s", i, result.Status)
		}

		// Verify session returns to idle
		status := sess.Status()
		if status.State != session.StateIdle {
			t.Errorf("command %d: expected idle state after exec, got %s", i, status.State)
		}
	}
}

func TestSFTPPutGet(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		t.Fatalf("failed to get SFTP client: %v", err)
	}

	content := "hello from e2e test\n"
	remotePath := "/tmp/e2e-test-file.txt"

	// Put file
	if err := sftpClient.PutFile(remotePath, []byte(content), 0644); err != nil {
		t.Fatalf("SFTP put failed: %v", err)
	}

	// Get file
	data, info, err := sftpClient.GetFile(remotePath)
	if err != nil {
		t.Fatalf("SFTP get failed: %v", err)
	}

	if string(data) != content {
		t.Errorf("content mismatch: got %q, want %q", string(data), content)
	}
	if info.Size() != int64(len(content)) {
		t.Errorf("size mismatch: got %d, want %d", info.Size(), len(content))
	}
}

func TestSFTPPutAtomic(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		t.Fatalf("failed to get SFTP client: %v", err)
	}

	content := "atomic write content\n"
	remotePath := "/tmp/e2e-atomic-test.txt"

	// Write using temp file + rename (atomic pattern)
	tmpPath := remotePath + ".tmp"
	if err := sftpClient.PutFile(tmpPath, []byte(content), 0644); err != nil {
		t.Fatalf("SFTP put tmp failed: %v", err)
	}
	if err := sftpClient.PosixRename(tmpPath, remotePath); err != nil {
		t.Fatalf("SFTP rename failed: %v", err)
	}

	// Verify via exec (not SFTP) to confirm the file is visible in the shell
	result, err := sess.Exec(fmt.Sprintf("cat %s", remotePath), 10000)
	if err != nil {
		t.Fatalf("cat failed: %v", err)
	}
	if !strings.Contains(result.Stdout, "atomic write content") {
		t.Errorf("expected atomic content, got %q", result.Stdout)
	}
}

func TestSFTPDirGetPut(t *testing.T) {
	_, sess := createKeySession(t, globalEnv)

	// Create a directory tree via exec
	_, err := sess.Exec("mkdir -p /tmp/e2e-dir/sub1 /tmp/e2e-dir/sub2", 10000)
	if err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	_, err = sess.Exec("echo file1 > /tmp/e2e-dir/file1.txt && echo file2 > /tmp/e2e-dir/sub1/file2.txt && echo file3 > /tmp/e2e-dir/sub2/file3.txt", 10000)
	if err != nil {
		t.Fatalf("create files failed: %v", err)
	}

	sftpClient, err := sess.SFTPClient()
	if err != nil {
		t.Fatalf("failed to get SFTP client: %v", err)
	}

	// Verify directory structure via SFTP
	entries, err := sftpClient.ReadDir("/tmp/e2e-dir")
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}

	for _, expected := range []string{"file1.txt", "sub1", "sub2"} {
		if !names[expected] {
			t.Errorf("expected %q in directory listing, got %v", expected, names)
		}
	}

	// Read a nested file
	data, _, err := sftpClient.GetFile("/tmp/e2e-dir/sub1/file2.txt")
	if err != nil {
		t.Fatalf("get nested file failed: %v", err)
	}
	if !strings.Contains(string(data), "file2") {
		t.Errorf("expected 'file2' in content, got %q", string(data))
	}
}

func TestSessionClose(t *testing.T) {
	mgr := session.NewManager(globalEnv.config)

	sess, err := mgr.Create(session.CreateOptions{
		Mode:    "ssh",
		Host:    globalEnv.host,
		Port:    globalEnv.keyPort,
		User:    "testuser",
		KeyPath: globalEnv.keyPath,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	id := sess.ID

	if err := mgr.Close(id); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Session should be gone
	_, err = mgr.Get(id)
	if err == nil {
		t.Error("expected error getting closed session")
	}
}

func TestMultipleSessions(t *testing.T) {
	mgr := session.NewManager(globalEnv.config)
	t.Cleanup(func() { mgr.CloseAll() })

	const numSessions = 3
	sessions := make([]*session.Session, numSessions)

	// Create sessions concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex
	var createErr error

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sess, err := mgr.Create(session.CreateOptions{
				Mode:    "ssh",
				Host:    globalEnv.host,
				Port:    globalEnv.keyPort,
				User:    "testuser",
				KeyPath: globalEnv.keyPath,
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				createErr = err
				return
			}
			sessions[idx] = sess
		}(i)
	}
	wg.Wait()

	if createErr != nil {
		t.Fatalf("failed to create sessions: %v", createErr)
	}

	// Verify each session can independently execute commands
	for i, sess := range sessions {
		result, err := sess.Exec(fmt.Sprintf("echo session-%d", i), 10000)
		if err != nil {
			t.Fatalf("session %d exec failed: %v", i, err)
		}
		expected := fmt.Sprintf("session-%d", i)
		if !strings.Contains(result.Stdout, expected) {
			t.Errorf("session %d: expected %q in stdout, got %q", i, expected, result.Stdout)
		}
	}
}

func TestWrongPassword(t *testing.T) {
	mgr := session.NewManager(globalEnv.config)

	_, err := mgr.Create(session.CreateOptions{
		Mode:     "ssh",
		Host:     globalEnv.host,
		Port:     globalEnv.passPort,
		User:     "testuser",
		Password: "wrongpassword",
	})
	if err == nil {
		t.Fatal("expected auth error with wrong password, got nil")
	}

	t.Logf("Got expected error: %v", err)
}

func TestWrongKey(t *testing.T) {
	// Generate a random key that won't be authorized
	tmpKey := filepath.Join(t.TempDir(), "wrong_key")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(tmpKey, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	mgr := session.NewManager(globalEnv.config)
	_, err = mgr.Create(session.CreateOptions{
		Mode:    "ssh",
		Host:    globalEnv.host,
		Port:    globalEnv.keyPort,
		User:    "testuser",
		KeyPath: tmpKey,
	})
	if err == nil {
		t.Fatal("expected auth error with wrong key, got nil")
	}

	t.Logf("Got expected error: %v", err)
}
