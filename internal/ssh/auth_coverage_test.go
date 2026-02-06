package ssh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/acolita/claude-shell-mcp/internal/ports"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	gossh "golang.org/x/crypto/ssh"
)

// --- Test helpers ---

// mockDialer implements ports.NetworkDialer for testing.
type mockDialer struct {
	conn net.Conn
	err  error
}

func (d *mockDialer) Dial(network, address string) (net.Conn, error) {
	return d.conn, d.err
}

// Ensure mockDialer implements the interface.
var _ ports.NetworkDialer = (*mockDialer)(nil)

// generateEd25519Key generates an unencrypted Ed25519 private key in PEM format.
func generateEd25519Key(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal ed25519 key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})
}

// generateRSAKey generates an unencrypted RSA private key in PEM format.
func generateRSAKey(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
}

// generateECDSAKey generates an unencrypted ECDSA private key in PEM format.
func generateECDSAKey(t *testing.T) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal ECDSA key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})
}

// generateEncryptedEd25519Key generates a passphrase-protected Ed25519 key using OpenSSH format.
func generateEncryptedEd25519Key(t *testing.T, passphrase string) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	block, err := gossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}
	return pem.EncodeToMemory(block)
}

// --- Tests for expandPathWithFS ---

func TestExpandPathWithFS_TildePrefix(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	result := expandPathWithFS("~/foo/bar", fakeFS)
	expected := "/home/testuser/foo/bar"
	if result != expected {
		t.Errorf("expandPathWithFS(\"~/foo/bar\") = %q, want %q", result, expected)
	}
}

func TestExpandPathWithFS_NoTilde(t *testing.T) {
	fakeFS := fakefs.New()

	result := expandPathWithFS("/absolute/path", fakeFS)
	if result != "/absolute/path" {
		t.Errorf("expandPathWithFS(\"/absolute/path\") = %q, want \"/absolute/path\"", result)
	}
}

func TestExpandPathWithFS_RelativePath(t *testing.T) {
	fakeFS := fakefs.New()

	result := expandPathWithFS("relative/path", fakeFS)
	if result != "relative/path" {
		t.Errorf("expandPathWithFS(\"relative/path\") = %q, want \"relative/path\"", result)
	}
}

// --- Tests for matchSinglePattern ---

func TestMatchSinglePattern_ExactMatch(t *testing.T) {
	if !matchSinglePattern("example.com", "example.com") {
		t.Error("exact match should return true")
	}
}

func TestMatchSinglePattern_WildcardStar(t *testing.T) {
	if !matchSinglePattern("example.com", "*") {
		t.Error("* should match any host")
	}
}

func TestMatchSinglePattern_StarPrefix(t *testing.T) {
	if !matchSinglePattern("sub.example.com", "*.example.com") {
		t.Error("*.example.com should match sub.example.com")
	}
}

func TestMatchSinglePattern_StarSuffix(t *testing.T) {
	if !matchSinglePattern("example.com", "example.*") {
		t.Error("example.* should match example.com")
	}
}

func TestMatchSinglePattern_QuestionMark(t *testing.T) {
	if !matchSinglePattern("host1", "host?") {
		t.Error("host? should match host1")
	}
}

func TestMatchSinglePattern_QuestionMarkNoMatch(t *testing.T) {
	if matchSinglePattern("host12", "host?") {
		t.Error("host? should not match host12")
	}
}

func TestMatchSinglePattern_NoMatch(t *testing.T) {
	if matchSinglePattern("foo.com", "bar.com") {
		t.Error("should not match different hosts")
	}
}

func TestMatchSinglePattern_StarMiddle(t *testing.T) {
	if !matchSinglePattern("dev-server-01", "dev*01") {
		t.Error("dev*01 should match dev-server-01")
	}
}

func TestMatchSinglePattern_EmptyPattern(t *testing.T) {
	if matchSinglePattern("host", "") {
		t.Error("empty pattern should not match non-empty host")
	}
}

func TestMatchSinglePattern_EmptyHost(t *testing.T) {
	if matchSinglePattern("", "host") {
		t.Error("non-empty pattern should not match empty host")
	}
}

func TestMatchSinglePattern_BothEmpty(t *testing.T) {
	if !matchSinglePattern("", "") {
		t.Error("empty pattern should match empty host")
	}
}

func TestMatchSinglePattern_ConsecutiveStars(t *testing.T) {
	if !matchSinglePattern("anything", "**") {
		t.Error("** should match anything")
	}
}

func TestMatchSinglePattern_TrailingStar(t *testing.T) {
	if !matchSinglePattern("host", "host*") {
		t.Error("host* should match host (trailing star matches empty)")
	}
}

func TestMatchSinglePattern_PatternLongerThanHost(t *testing.T) {
	if matchSinglePattern("ab", "abcd") {
		t.Error("shorter host should not match longer literal pattern")
	}
}

func TestMatchSinglePattern_HostLongerThanPattern(t *testing.T) {
	if matchSinglePattern("abcd", "ab") {
		t.Error("longer host should not match shorter literal pattern")
	}
}

func TestMatchSinglePattern_QuestionMarkAndStar(t *testing.T) {
	if !matchSinglePattern("ab", "?*") {
		t.Error("?* should match 'ab'")
	}
	if !matchSinglePattern("a", "?*") {
		t.Error("?* should match 'a'")
	}
}

// --- Tests for matchSSHHostPattern ---

func TestMatchSSHHostPattern_MultiplePatterns(t *testing.T) {
	if !matchSSHHostPattern("bar.com", "foo.com bar.com") {
		t.Error("should match when host is in multi-pattern string")
	}
}

func TestMatchSSHHostPattern_MultiplePatterns_NoMatch(t *testing.T) {
	if matchSSHHostPattern("baz.com", "foo.com bar.com") {
		t.Error("should not match when host is not in multi-pattern string")
	}
}

func TestMatchSSHHostPattern_WildcardInMultiple(t *testing.T) {
	if !matchSSHHostPattern("dev.example.com", "*.example.com prod.other.com") {
		t.Error("should match wildcard in multi-pattern string")
	}
}

// --- Tests for skipWildcards ---

func TestSkipWildcards(t *testing.T) {
	tests := []struct {
		pattern string
		start   int
		want    int
	}{
		{"***abc", 0, 3},
		{"abc", 0, 0},
		{"*", 0, 1},
		{"a*b", 1, 2},
	}
	for _, tc := range tests {
		got := skipWildcards(tc.pattern, tc.start)
		if got != tc.want {
			t.Errorf("skipWildcards(%q, %d) = %d, want %d", tc.pattern, tc.start, got, tc.want)
		}
	}
}

// --- Tests for matchWildcard ---

func TestMatchWildcard_TrailingStars(t *testing.T) {
	if !matchWildcard("anything.here", "**", 0, 0) {
		t.Error("trailing ** should match rest of host")
	}
}

func TestMatchWildcard_StarThenLiteral(t *testing.T) {
	if !matchWildcard("foobar", "*bar", 0, 0) {
		t.Error("*bar should match foobar")
	}
}

func TestMatchWildcard_NoMatchAfterStar(t *testing.T) {
	if matchWildcard("foo", "*z", 0, 0) {
		t.Error("*z should not match foo")
	}
}

// --- Tests for privateKeyAuth ---

func TestPrivateKeyAuth_Ed25519(t *testing.T) {
	fakeFS := fakefs.New()
	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/test/.ssh/id_ed25519", keyData, 0600)

	auth, err := privateKeyAuth("/home/test/.ssh/id_ed25519", "", fakeFS)
	if err != nil {
		t.Fatalf("privateKeyAuth: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth method")
	}
}

func TestPrivateKeyAuth_RSA(t *testing.T) {
	fakeFS := fakefs.New()
	keyData := generateRSAKey(t)
	fakeFS.AddFile("/home/test/.ssh/id_rsa", keyData, 0600)

	auth, err := privateKeyAuth("/home/test/.ssh/id_rsa", "", fakeFS)
	if err != nil {
		t.Fatalf("privateKeyAuth: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth method")
	}
}

func TestPrivateKeyAuth_ECDSA(t *testing.T) {
	fakeFS := fakefs.New()
	keyData := generateECDSAKey(t)
	fakeFS.AddFile("/home/test/.ssh/id_ecdsa", keyData, 0600)

	auth, err := privateKeyAuth("/home/test/.ssh/id_ecdsa", "", fakeFS)
	if err != nil {
		t.Fatalf("privateKeyAuth: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth method")
	}
}

func TestPrivateKeyAuth_WithPassphrase(t *testing.T) {
	fakeFS := fakefs.New()
	passphrase := "testpassword123"
	keyData := generateEncryptedEd25519Key(t, passphrase)
	fakeFS.AddFile("/home/test/.ssh/id_ed25519", keyData, 0600)

	auth, err := privateKeyAuth("/home/test/.ssh/id_ed25519", passphrase, fakeFS)
	if err != nil {
		t.Fatalf("privateKeyAuth with passphrase: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth method")
	}
}

func TestPrivateKeyAuth_WrongPassphrase(t *testing.T) {
	fakeFS := fakefs.New()
	passphrase := "correct"
	keyData := generateEncryptedEd25519Key(t, passphrase)
	fakeFS.AddFile("/home/test/.ssh/id_ed25519", keyData, 0600)

	_, err := privateKeyAuth("/home/test/.ssh/id_ed25519", "wrong", fakeFS)
	if err == nil {
		t.Fatal("expected error with wrong passphrase")
	}
}

func TestPrivateKeyAuth_FileNotFound(t *testing.T) {
	fakeFS := fakefs.New()

	_, err := privateKeyAuth("/nonexistent/key", "", fakeFS)
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestPrivateKeyAuth_InvalidKeyData(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.AddFile("/home/test/.ssh/bad_key", []byte("not a real key"), 0600)

	_, err := privateKeyAuth("/home/test/.ssh/bad_key", "", fakeFS)
	if err == nil {
		t.Fatal("expected error for invalid key data")
	}
}

func TestPrivateKeyAuth_TildeExpansion(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/myuser")
	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/myuser/.ssh/id_ed25519", keyData, 0600)

	auth, err := privateKeyAuth("~/.ssh/id_ed25519", "", fakeFS)
	if err != nil {
		t.Fatalf("privateKeyAuth with tilde: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth method")
	}
}

// --- Tests for sshAgentAuth ---

func TestSSHAgentAuth_NoSocket(t *testing.T) {
	fakeFS := fakefs.New()
	dialer := &mockDialer{}

	_, err := sshAgentAuth(fakeFS, dialer)
	if err == nil {
		t.Fatal("expected error when SSH_AUTH_SOCK is not set")
	}
}

func TestSSHAgentAuth_DialFailure(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetEnv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")
	dialer := &mockDialer{err: fmt.Errorf("connection refused")}

	_, err := sshAgentAuth(fakeFS, dialer)
	if err == nil {
		t.Fatal("expected error when dial fails")
	}
}

func TestSSHAgentAuth_Success(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetEnv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	dialer := &mockDialer{conn: client}

	auth, err := sshAgentAuth(fakeFS, dialer)
	if err != nil {
		t.Fatalf("sshAgentAuth: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth method")
	}
}

// --- Tests for PasswordAuth and KeyboardInteractiveAuth ---

func TestPasswordAuth(t *testing.T) {
	auth := PasswordAuth("secret")
	if auth == nil {
		t.Fatal("PasswordAuth returned nil")
	}
}

func TestKeyboardInteractiveAuth(t *testing.T) {
	auth := KeyboardInteractiveAuth("secret")
	if auth == nil {
		t.Fatal("KeyboardInteractiveAuth returned nil")
	}
}

// --- Tests for InsecureHostKeyCallback ---

func TestInsecureHostKeyCallback(t *testing.T) {
	cb := InsecureHostKeyCallback()
	if cb == nil {
		t.Fatal("InsecureHostKeyCallback returned nil")
	}
	err := cb("host:22", nil, nil)
	if err != nil {
		t.Fatalf("insecure callback returned error: %v", err)
	}
}

// --- Tests for BuildHostKeyCallback ---

func TestBuildHostKeyCallback_MissingKnownHosts(t *testing.T) {
	fakeFS := fakefs.New()
	cb, err := BuildHostKeyCallback("", fakeFS)
	if err != nil {
		t.Fatalf("BuildHostKeyCallback: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
	err = cb("somehost:22", nil, nil)
	if err != nil {
		t.Fatalf("permissive callback returned error: %v", err)
	}
}

func TestBuildHostKeyCallback_CustomPath(t *testing.T) {
	fakeFS := fakefs.New()
	cb, err := BuildHostKeyCallback("/tmp/my_known_hosts", fakeFS)
	if err != nil {
		t.Fatalf("BuildHostKeyCallback: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestBuildHostKeyCallback_WithValidKnownHosts(t *testing.T) {
	tmpDir := t.TempDir()
	knownHostsPath := filepath.Join(tmpDir, "known_hosts")

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pub, err := gossh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatalf("new public key: %v", err)
	}
	entry := fmt.Sprintf("example.com %s", gossh.MarshalAuthorizedKey(pub))

	if err := os.WriteFile(knownHostsPath, []byte(entry), 0644); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	// Use default (real) filesystem since knownhosts.New requires real file access
	cb, err := BuildHostKeyCallback(knownHostsPath)
	if err != nil {
		t.Fatalf("BuildHostKeyCallback with real file: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestBuildHostKeyCallback_InvalidKnownHosts(t *testing.T) {
	tmpDir := t.TempDir()
	knownHostsPath := filepath.Join(tmpDir, "known_hosts")

	// Write invalid content that will fail to parse
	invalidContent := "this is not a valid known_hosts format @@@@\n"
	if err := os.WriteFile(knownHostsPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	// Use default (real) filesystem
	_, err := BuildHostKeyCallback(knownHostsPath)
	if err == nil {
		t.Fatal("expected error for invalid known_hosts file")
	}
}

// --- Tests for getSSHConfigIdentityFile ---

func TestGetSSHConfigIdentityFile_NoConfigFile(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	result := getSSHConfigIdentityFile("myhost", fakeFS)
	if result != "" {
		t.Errorf("expected empty string when config file missing, got %q", result)
	}
}

func TestGetSSHConfigIdentityFile_ExactHostMatch(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host myhost
  IdentityFile ~/.ssh/myhost_key
  User deploy

Host other
  IdentityFile ~/.ssh/other_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("myhost", fakeFS)
	expected := "/home/testuser/.ssh/myhost_key"
	if result != expected {
		t.Errorf("getSSHConfigIdentityFile(\"myhost\") = %q, want %q", result, expected)
	}
}

func TestGetSSHConfigIdentityFile_WildcardMatch(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host *.example.com
  IdentityFile ~/.ssh/example_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("dev.example.com", fakeFS)
	expected := "/home/testuser/.ssh/example_key"
	if result != expected {
		t.Errorf("getSSHConfigIdentityFile(\"dev.example.com\") = %q, want %q", result, expected)
	}
}

func TestGetSSHConfigIdentityFile_NoMatch(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host otherhost
  IdentityFile ~/.ssh/other_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("myhost", fakeFS)
	if result != "" {
		t.Errorf("expected empty string for no match, got %q", result)
	}
}

func TestGetSSHConfigIdentityFile_CommentsAndEmptyLines(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `# This is a comment

Host myhost
  # another comment
  IdentityFile ~/.ssh/myhost_key

# trailing comment
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("myhost", fakeFS)
	expected := "/home/testuser/.ssh/myhost_key"
	if result != expected {
		t.Errorf("getSSHConfigIdentityFile with comments = %q, want %q", result, expected)
	}
}

func TestGetSSHConfigIdentityFile_MultipleHostPatterns(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host foo bar baz
  IdentityFile ~/.ssh/multi_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("bar", fakeFS)
	expected := "/home/testuser/.ssh/multi_key"
	if result != expected {
		t.Errorf("getSSHConfigIdentityFile(\"bar\") = %q, want %q", result, expected)
	}
}

func TestGetSSHConfigIdentityFile_HostWithoutIdentityFile(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host myhost
  User deploy
  Port 2222

Host other
  IdentityFile ~/.ssh/other_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("myhost", fakeFS)
	if result != "" {
		t.Errorf("expected empty string when host has no IdentityFile, got %q", result)
	}
}

func TestGetSSHConfigIdentityFile_SingleFieldLine(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host myhost
  SingleFieldOnly
  IdentityFile ~/.ssh/myhost_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("myhost", fakeFS)
	expected := "/home/testuser/.ssh/myhost_key"
	if result != expected {
		t.Errorf("getSSHConfigIdentityFile = %q, want %q", result, expected)
	}
}

func TestGetSSHConfigIdentityFile_StarHost(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host *
  IdentityFile ~/.ssh/default_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	result := getSSHConfigIdentityFile("anything.example.com", fakeFS)
	expected := "/home/testuser/.ssh/default_key"
	if result != expected {
		t.Errorf("getSSHConfigIdentityFile for * host = %q, want %q", result, expected)
	}
}

// --- Tests for trySSHAgentAuth ---

func TestTrySSHAgentAuth_Disabled(t *testing.T) {
	cfg := AuthConfig{
		UseAgent: false,
	}
	methods := trySSHAgentAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when agent disabled, got %d", len(methods))
	}
}

func TestTrySSHAgentAuth_NoSocket(t *testing.T) {
	fakeFS := fakefs.New()
	cfg := AuthConfig{
		UseAgent: true,
		FS:       fakeFS,
		Dialer:   &mockDialer{err: fmt.Errorf("fail")},
	}
	methods := trySSHAgentAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when agent fails, got %d", len(methods))
	}
}

// --- Tests for tryExplicitKeyAuth ---

func TestTryExplicitKeyAuth_NoKeyPath(t *testing.T) {
	cfg := AuthConfig{}
	auth, err := tryExplicitKeyAuth(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != nil {
		t.Error("expected nil auth when no key path provided")
	}
}

func TestTryExplicitKeyAuth_ValidKey(t *testing.T) {
	fakeFS := fakefs.New()
	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/test/.ssh/id_ed25519", keyData, 0600)

	cfg := AuthConfig{
		KeyPath: "/home/test/.ssh/id_ed25519",
		FS:      fakeFS,
	}
	auth, err := tryExplicitKeyAuth(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth == nil {
		t.Error("expected non-nil auth method")
	}
}

func TestTryExplicitKeyAuth_InvalidKey(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.AddFile("/home/test/.ssh/bad_key", []byte("garbage"), 0600)

	cfg := AuthConfig{
		KeyPath: "/home/test/.ssh/bad_key",
		FS:      fakeFS,
	}
	_, err := tryExplicitKeyAuth(cfg)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

// --- Tests for trySSHConfigAuth ---

func TestTrySSHConfigAuth_SkippedWhenKeyPathSet(t *testing.T) {
	cfg := AuthConfig{
		KeyPath: "/some/key",
	}
	methods := trySSHConfigAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when KeyPath set, got %d", len(methods))
	}
}

func TestTrySSHConfigAuth_SkippedWhenNoHost(t *testing.T) {
	cfg := AuthConfig{
		Host: "",
	}
	methods := trySSHConfigAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when Host empty, got %d", len(methods))
	}
}

func TestTrySSHConfigAuth_WithValidConfig(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/testuser/.ssh/myhost_key", keyData, 0600)

	configContent := `Host myhost
  IdentityFile ~/.ssh/myhost_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	cfg := AuthConfig{
		Host: "myhost",
		FS:   fakeFS,
	}
	methods := trySSHConfigAuth(cfg, nil)
	if len(methods) != 1 {
		t.Errorf("expected 1 method from SSH config, got %d", len(methods))
	}
}

func TestTrySSHConfigAuth_ConfigKeyParseFails(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	fakeFS.AddFile("/home/testuser/.ssh/myhost_key", []byte("not a key"), 0600)

	configContent := `Host myhost
  IdentityFile ~/.ssh/myhost_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	cfg := AuthConfig{
		Host: "myhost",
		FS:   fakeFS,
	}
	methods := trySSHConfigAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when config key parse fails, got %d", len(methods))
	}
}

// --- Tests for tryDefaultKeysAuth ---

func TestTryDefaultKeysAuth_SkippedWhenKeyPathSet(t *testing.T) {
	cfg := AuthConfig{
		KeyPath: "/some/key",
	}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when KeyPath set, got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_SkippedWhenPasswordSet(t *testing.T) {
	fakeFS := fakefs.New()
	cfg := AuthConfig{
		Password: "secret",
		FS:       fakeFS,
	}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when password set, got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_SkippedWhenMethodsExist(t *testing.T) {
	fakeFS := fakefs.New()
	cfg := AuthConfig{FS: fakeFS}
	existing := []gossh.AuthMethod{PasswordAuth("x")}
	methods := tryDefaultKeysAuth(cfg, existing)
	if len(methods) != 1 {
		t.Errorf("expected 1 method (unchanged), got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_FindsEd25519(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/testuser/.ssh/id_ed25519", keyData, 0600)

	cfg := AuthConfig{FS: fakeFS}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 1 {
		t.Errorf("expected 1 method from default ed25519, got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_FindsRSA(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	keyData := generateRSAKey(t)
	fakeFS.AddFile("/home/testuser/.ssh/id_rsa", keyData, 0600)

	cfg := AuthConfig{FS: fakeFS}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 1 {
		t.Errorf("expected 1 method from default RSA, got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_FindsECDSA(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	keyData := generateECDSAKey(t)
	fakeFS.AddFile("/home/testuser/.ssh/id_ecdsa", keyData, 0600)

	cfg := AuthConfig{FS: fakeFS}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 1 {
		t.Errorf("expected 1 method from default ECDSA, got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_PrefersEd25519(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	ed25519Key := generateEd25519Key(t)
	rsaKey := generateRSAKey(t)
	fakeFS.AddFile("/home/testuser/.ssh/id_ed25519", ed25519Key, 0600)
	fakeFS.AddFile("/home/testuser/.ssh/id_rsa", rsaKey, 0600)

	cfg := AuthConfig{FS: fakeFS}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 1 {
		t.Errorf("expected 1 method (first default key found), got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_NoDefaultKeys(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	cfg := AuthConfig{FS: fakeFS}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when no default keys exist, got %d", len(methods))
	}
}

func TestTryDefaultKeysAuth_SkipsBadKeys(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	fakeFS.AddFile("/home/testuser/.ssh/id_ed25519", []byte("bad key"), 0600)
	rsaKey := generateRSAKey(t)
	fakeFS.AddFile("/home/testuser/.ssh/id_rsa", rsaKey, 0600)

	cfg := AuthConfig{FS: fakeFS}
	methods := tryDefaultKeysAuth(cfg, nil)
	if len(methods) != 1 {
		t.Errorf("expected 1 method (skip bad ed25519, use RSA), got %d", len(methods))
	}
}

// --- Tests for tryPasswordAuth ---

func TestTryPasswordAuth_NoPassword(t *testing.T) {
	cfg := AuthConfig{}
	methods := tryPasswordAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when no password, got %d", len(methods))
	}
}

func TestTryPasswordAuth_WithPassword(t *testing.T) {
	cfg := AuthConfig{Password: "secret"}
	methods := tryPasswordAuth(cfg, nil)
	if len(methods) != 2 {
		t.Errorf("expected 2 methods (password + keyboard-interactive), got %d", len(methods))
	}
}

// --- Tests for BuildAuthMethods ---

func TestBuildAuthMethods_NoMethodsAvailable(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	dialer := &mockDialer{err: fmt.Errorf("no agent")}

	cfg := AuthConfig{
		FS:     fakeFS,
		Dialer: dialer,
	}
	_, err := BuildAuthMethods(cfg)
	if err == nil {
		t.Fatal("expected error when no auth methods available")
	}
}

func TestBuildAuthMethods_WithPassword(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	dialer := &mockDialer{err: fmt.Errorf("no agent")}

	cfg := AuthConfig{
		Password: "mypassword",
		FS:       fakeFS,
		Dialer:   dialer,
	}
	methods, err := BuildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("BuildAuthMethods: %v", err)
	}
	if len(methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(methods))
	}
}

func TestBuildAuthMethods_WithExplicitKey(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	dialer := &mockDialer{err: fmt.Errorf("no agent")}

	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/testuser/.ssh/id_ed25519", keyData, 0600)

	cfg := AuthConfig{
		KeyPath: "/home/testuser/.ssh/id_ed25519",
		FS:      fakeFS,
		Dialer:  dialer,
	}
	methods, err := BuildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("BuildAuthMethods: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("expected 1 method, got %d", len(methods))
	}
}

func TestBuildAuthMethods_WithInvalidExplicitKey(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	dialer := &mockDialer{err: fmt.Errorf("no agent")}

	fakeFS.AddFile("/home/testuser/.ssh/bad_key", []byte("invalid"), 0600)

	cfg := AuthConfig{
		KeyPath: "/home/testuser/.ssh/bad_key",
		FS:      fakeFS,
		Dialer:  dialer,
	}
	_, err := BuildAuthMethods(cfg)
	if err == nil {
		t.Fatal("expected error for invalid explicit key")
	}
}

func TestBuildAuthMethods_WithDefaultKeys(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	dialer := &mockDialer{err: fmt.Errorf("no agent")}

	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/testuser/.ssh/id_ed25519", keyData, 0600)

	cfg := AuthConfig{
		FS:     fakeFS,
		Dialer: dialer,
	}
	methods, err := BuildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("BuildAuthMethods: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("expected 1 method from default key, got %d", len(methods))
	}
}

func TestBuildAuthMethods_WithSSHConfig(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	dialer := &mockDialer{err: fmt.Errorf("no agent")}

	keyData := generateEd25519Key(t)
	fakeFS.AddFile("/home/testuser/.ssh/myhost_key", keyData, 0600)

	configContent := `Host myhost
  IdentityFile ~/.ssh/myhost_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	cfg := AuthConfig{
		Host:   "myhost",
		FS:     fakeFS,
		Dialer: dialer,
	}
	methods, err := BuildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("BuildAuthMethods: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("expected 1 method from SSH config, got %d", len(methods))
	}
}

func TestBuildAuthMethods_DefaultDependencies(t *testing.T) {
	// Test that nil FS and Dialer get defaults. Should not panic.
	cfg := AuthConfig{
		Password: "test",
	}
	methods, err := BuildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("BuildAuthMethods with defaults: %v", err)
	}
	if len(methods) < 2 {
		t.Errorf("expected at least 2 methods, got %d", len(methods))
	}
}

func TestBuildAuthMethods_WithSSHAgent(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	fakeFS.SetEnv("SSH_AUTH_SOCK", "/tmp/agent.sock")

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	dialer := &mockDialer{conn: client}

	cfg := AuthConfig{
		UseAgent: true,
		Password: "backup",
		FS:       fakeFS,
		Dialer:   dialer,
	}
	methods, err := BuildAuthMethods(cfg)
	if err != nil {
		t.Fatalf("BuildAuthMethods: %v", err)
	}
	// Should have agent + password + keyboard-interactive
	if len(methods) < 3 {
		t.Errorf("expected at least 3 methods, got %d", len(methods))
	}
}

// --- Test trySSHConfigAuth when config has no match ---

func TestTrySSHConfigAuth_ConfigNoMatch(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")

	configContent := `Host otherhost
  IdentityFile ~/.ssh/other_key
`
	fakeFS.AddFile("/home/testuser/.ssh/config", []byte(configContent), 0644)

	cfg := AuthConfig{
		Host: "myhost", // does not match "otherhost"
		FS:   fakeFS,
	}
	methods := trySSHConfigAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when SSH config has no match, got %d", len(methods))
	}
}

func TestTrySSHConfigAuth_NoConfigFile(t *testing.T) {
	fakeFS := fakefs.New()
	fakeFS.SetHomeDir("/home/testuser")
	// No ~/.ssh/config file exists

	cfg := AuthConfig{
		Host: "myhost",
		FS:   fakeFS,
	}
	methods := trySSHConfigAuth(cfg, nil)
	if len(methods) != 0 {
		t.Errorf("expected 0 methods when no SSH config file, got %d", len(methods))
	}
}

// --- Tests for expandPath ---

func TestExpandPath(t *testing.T) {
	result := expandPath("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("expandPath(\"/absolute/path\") = %q, want \"/absolute/path\"", result)
	}
}
