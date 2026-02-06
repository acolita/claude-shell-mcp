package security

import (
	"errors"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

// setupMockKeyring initializes the go-keyring mock provider and returns
// a KeyringStore with enabled=true. This bypasses the real OS keyring.
func setupMockKeyring(t *testing.T) *KeyringStore {
	t.Helper()
	keyring.MockInit()
	return &KeyringStore{enabled: true}
}

// setupMockKeyringWithError initializes the go-keyring mock provider that
// returns the given error on all operations, and returns a KeyringStore
// with enabled=true.
func setupMockKeyringWithError(t *testing.T, err error) *KeyringStore {
	t.Helper()
	keyring.MockInitWithError(err)
	return &KeyringStore{enabled: true}
}

// --- NewKeyringStore tests ---

func TestNewKeyringStore_WithMockKeyring(t *testing.T) {
	keyring.MockInit()
	ks := NewKeyringStore()
	if ks == nil {
		t.Fatal("NewKeyringStore returned nil")
	}
	if !ks.IsEnabled() {
		t.Error("expected keyring to be enabled with mock provider")
	}
}

func TestNewKeyringStore_WithFailingKeyring(t *testing.T) {
	keyring.MockInitWithError(errors.New("mock keyring failure"))
	ks := NewKeyringStore()
	if ks == nil {
		t.Fatal("NewKeyringStore returned nil")
	}
	if ks.IsEnabled() {
		t.Error("expected keyring to be disabled when keyring returns error")
	}
}

// --- IsEnabled / SetEnabled tests ---

func TestKeyringStore_IsEnabled_Default(t *testing.T) {
	ks := &KeyringStore{enabled: false}
	if ks.IsEnabled() {
		t.Error("expected IsEnabled to be false")
	}
}

func TestKeyringStore_SetEnabled_Toggle(t *testing.T) {
	ks := &KeyringStore{enabled: false}
	ks.SetEnabled(true)
	if !ks.IsEnabled() {
		t.Error("expected IsEnabled to be true after SetEnabled(true)")
	}
	ks.SetEnabled(false)
	if ks.IsEnabled() {
		t.Error("expected IsEnabled to be false after SetEnabled(false)")
	}
}

func TestKeyringStore_IsEnabled_ConcurrentAccess(t *testing.T) {
	ks := &KeyringStore{enabled: true}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ks.SetEnabled(true)
		}()
		go func() {
			defer wg.Done()
			_ = ks.IsEnabled()
		}()
	}
	wg.Wait()
}

// --- SSH Passphrase tests ---

func TestKeyringStore_StoreAndGetSSHPassphrase(t *testing.T) {
	ks := setupMockKeyring(t)
	keyPath := "/home/user/.ssh/id_ed25519"
	passphrase := []byte("my-secret-passphrase")

	if err := ks.StoreSSHPassphrase(keyPath, passphrase); err != nil {
		t.Fatalf("StoreSSHPassphrase failed: %v", err)
	}

	got, err := ks.GetSSHPassphrase(keyPath)
	if err != nil {
		t.Fatalf("GetSSHPassphrase failed: %v", err)
	}
	if string(got) != string(passphrase) {
		t.Errorf("got %q, want %q", got, passphrase)
	}
}

func TestKeyringStore_GetSSHPassphrase_NotFound(t *testing.T) {
	ks := setupMockKeyring(t)

	got, err := ks.GetSSHPassphrase("/nonexistent/key")
	if err != nil {
		t.Fatalf("expected no error for missing key, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing key, got %q", got)
	}
}

func TestKeyringStore_DeleteSSHPassphrase(t *testing.T) {
	ks := setupMockKeyring(t)
	keyPath := "/home/user/.ssh/id_rsa"
	passphrase := []byte("to-be-deleted")

	if err := ks.StoreSSHPassphrase(keyPath, passphrase); err != nil {
		t.Fatalf("StoreSSHPassphrase failed: %v", err)
	}

	if err := ks.DeleteSSHPassphrase(keyPath); err != nil {
		t.Fatalf("DeleteSSHPassphrase failed: %v", err)
	}

	got, err := ks.GetSSHPassphrase(keyPath)
	if err != nil {
		t.Fatalf("GetSSHPassphrase after delete failed: %v", err)
	}
	if got != nil {
		t.Error("expected nil after deletion")
	}
}

func TestKeyringStore_DeleteSSHPassphrase_NotFound(t *testing.T) {
	ks := setupMockKeyring(t)

	err := ks.DeleteSSHPassphrase("/nonexistent/key")
	if err != nil {
		t.Errorf("DeleteSSHPassphrase for missing key should return nil, got: %v", err)
	}
}

func TestKeyringStore_StoreSSHPassphrase_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	err := ks.StoreSSHPassphrase("/test", []byte("test"))
	if err == nil {
		t.Error("expected error when disabled")
	}
	if err.Error() != errKeyringNotAvailable {
		t.Errorf("expected %q, got %q", errKeyringNotAvailable, err.Error())
	}
}

func TestKeyringStore_GetSSHPassphrase_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	got, err := ks.GetSSHPassphrase("/test")
	if err == nil {
		t.Error("expected error when disabled")
	}
	if got != nil {
		t.Error("expected nil result when disabled")
	}
}

func TestKeyringStore_DeleteSSHPassphrase_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	err := ks.DeleteSSHPassphrase("/test")
	if err == nil {
		t.Error("expected error when disabled")
	}
}

func TestKeyringStore_StoreSSHPassphrase_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring store failure")
	ks := setupMockKeyringWithError(t, mockErr)

	err := ks.StoreSSHPassphrase("/test", []byte("test"))
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
	if !errors.Is(err, mockErr) {
		t.Errorf("expected wrapped error containing %q, got %q", mockErr, err)
	}
}

func TestKeyringStore_GetSSHPassphrase_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring get failure")
	ks := setupMockKeyringWithError(t, mockErr)

	got, err := ks.GetSSHPassphrase("/test")
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
	if got != nil {
		t.Error("expected nil result on error")
	}
}

func TestKeyringStore_DeleteSSHPassphrase_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring delete failure")
	ks := setupMockKeyringWithError(t, mockErr)

	err := ks.DeleteSSHPassphrase("/test")
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
}

// --- Sudo Password tests ---

func TestKeyringStore_StoreAndGetSudoPassword(t *testing.T) {
	ks := setupMockKeyring(t)
	host := "prod.server.com"
	user := "deploy"
	password := []byte("sudo-secret-123")

	if err := ks.StoreSudoPassword(host, user, password); err != nil {
		t.Fatalf("StoreSudoPassword failed: %v", err)
	}

	got, err := ks.GetSudoPassword(host, user)
	if err != nil {
		t.Fatalf("GetSudoPassword failed: %v", err)
	}
	if string(got) != string(password) {
		t.Errorf("got %q, want %q", got, password)
	}
}

func TestKeyringStore_GetSudoPassword_NotFound(t *testing.T) {
	ks := setupMockKeyring(t)

	got, err := ks.GetSudoPassword("unknown-host", "unknown-user")
	if err != nil {
		t.Fatalf("expected no error for missing password, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing password, got %q", got)
	}
}

func TestKeyringStore_DeleteSudoPassword(t *testing.T) {
	ks := setupMockKeyring(t)
	host := "test.host"
	user := "admin"
	password := []byte("delete-me")

	if err := ks.StoreSudoPassword(host, user, password); err != nil {
		t.Fatalf("StoreSudoPassword failed: %v", err)
	}

	if err := ks.DeleteSudoPassword(host, user); err != nil {
		t.Fatalf("DeleteSudoPassword failed: %v", err)
	}

	got, err := ks.GetSudoPassword(host, user)
	if err != nil {
		t.Fatalf("GetSudoPassword after delete failed: %v", err)
	}
	if got != nil {
		t.Error("expected nil after deletion")
	}
}

func TestKeyringStore_DeleteSudoPassword_NotFound(t *testing.T) {
	ks := setupMockKeyring(t)

	err := ks.DeleteSudoPassword("no-host", "no-user")
	if err != nil {
		t.Errorf("DeleteSudoPassword for missing key should return nil, got: %v", err)
	}
}

func TestKeyringStore_StoreSudoPassword_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	err := ks.StoreSudoPassword("host", "user", []byte("pass"))
	if err == nil {
		t.Error("expected error when disabled")
	}
}

func TestKeyringStore_GetSudoPassword_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	got, err := ks.GetSudoPassword("host", "user")
	if err == nil {
		t.Error("expected error when disabled")
	}
	if got != nil {
		t.Error("expected nil result when disabled")
	}
}

func TestKeyringStore_DeleteSudoPassword_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	err := ks.DeleteSudoPassword("host", "user")
	if err == nil {
		t.Error("expected error when disabled")
	}
}

func TestKeyringStore_StoreSudoPassword_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring sudo store failure")
	ks := setupMockKeyringWithError(t, mockErr)

	err := ks.StoreSudoPassword("host", "user", []byte("pass"))
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
}

func TestKeyringStore_GetSudoPassword_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring sudo get failure")
	ks := setupMockKeyringWithError(t, mockErr)

	got, err := ks.GetSudoPassword("host", "user")
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
	if got != nil {
		t.Error("expected nil on error")
	}
}

func TestKeyringStore_DeleteSudoPassword_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring sudo delete failure")
	ks := setupMockKeyringWithError(t, mockErr)

	err := ks.DeleteSudoPassword("host", "user")
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
}

// --- Server Password tests ---

func TestKeyringStore_StoreAndGetServerPassword(t *testing.T) {
	ks := setupMockKeyring(t)
	host := "server.example.com"
	user := "admin"
	password := []byte("server-pass-789")

	if err := ks.StoreServerPassword(host, user, password); err != nil {
		t.Fatalf("StoreServerPassword failed: %v", err)
	}

	got, err := ks.GetServerPassword(host, user)
	if err != nil {
		t.Fatalf("GetServerPassword failed: %v", err)
	}
	if string(got) != string(password) {
		t.Errorf("got %q, want %q", got, password)
	}
}

func TestKeyringStore_GetServerPassword_NotFound(t *testing.T) {
	ks := setupMockKeyring(t)

	got, err := ks.GetServerPassword("unknown", "unknown")
	if err != nil {
		t.Fatalf("expected no error for missing password, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing password, got %q", got)
	}
}

func TestKeyringStore_DeleteServerPassword(t *testing.T) {
	ks := setupMockKeyring(t)
	host := "del.host.com"
	user := "testuser"
	password := []byte("delete-this")

	if err := ks.StoreServerPassword(host, user, password); err != nil {
		t.Fatalf("StoreServerPassword failed: %v", err)
	}

	if err := ks.DeleteServerPassword(host, user); err != nil {
		t.Fatalf("DeleteServerPassword failed: %v", err)
	}

	got, err := ks.GetServerPassword(host, user)
	if err != nil {
		t.Fatalf("GetServerPassword after delete failed: %v", err)
	}
	if got != nil {
		t.Error("expected nil after deletion")
	}
}

func TestKeyringStore_DeleteServerPassword_NotFound(t *testing.T) {
	ks := setupMockKeyring(t)

	err := ks.DeleteServerPassword("no-host", "no-user")
	if err != nil {
		t.Errorf("DeleteServerPassword for missing key should return nil, got: %v", err)
	}
}

func TestKeyringStore_StoreServerPassword_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	err := ks.StoreServerPassword("host", "user", []byte("pass"))
	if err == nil {
		t.Error("expected error when disabled")
	}
}

func TestKeyringStore_GetServerPassword_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	got, err := ks.GetServerPassword("host", "user")
	if err == nil {
		t.Error("expected error when disabled")
	}
	if got != nil {
		t.Error("expected nil when disabled")
	}
}

func TestKeyringStore_DeleteServerPassword_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	err := ks.DeleteServerPassword("host", "user")
	if err == nil {
		t.Error("expected error when disabled")
	}
}

func TestKeyringStore_StoreServerPassword_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring server store failure")
	ks := setupMockKeyringWithError(t, mockErr)

	err := ks.StoreServerPassword("host", "user", []byte("pass"))
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
}

func TestKeyringStore_GetServerPassword_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring server get failure")
	ks := setupMockKeyringWithError(t, mockErr)

	got, err := ks.GetServerPassword("host", "user")
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
	if got != nil {
		t.Error("expected nil on error")
	}
}

func TestKeyringStore_DeleteServerPassword_KeyringError(t *testing.T) {
	mockErr := errors.New("keyring server delete failure")
	ks := setupMockKeyringWithError(t, mockErr)

	err := ks.DeleteServerPassword("host", "user")
	if err == nil {
		t.Fatal("expected error from failing keyring")
	}
}

// --- ClearAll tests ---

func TestKeyringStore_ClearAll(t *testing.T) {
	ks := setupMockKeyring(t)

	// Populate various entries
	hosts := []string{"host1.com", "host2.com"}
	users := []string{"user1", "user2"}
	keyPaths := []string{"/keys/id_rsa", "/keys/id_ed25519"}

	for _, h := range hosts {
		for _, u := range users {
			if err := ks.StoreSudoPassword(h, u, []byte("sudo-"+u+"@"+h)); err != nil {
				t.Fatalf("StoreSudoPassword(%s, %s) failed: %v", h, u, err)
			}
			if err := ks.StoreServerPassword(h, u, []byte("server-"+u+"@"+h)); err != nil {
				t.Fatalf("StoreServerPassword(%s, %s) failed: %v", h, u, err)
			}
		}
	}
	for _, kp := range keyPaths {
		if err := ks.StoreSSHPassphrase(kp, []byte("passphrase-"+kp)); err != nil {
			t.Fatalf("StoreSSHPassphrase(%s) failed: %v", kp, err)
		}
	}

	// ClearAll should not panic
	ks.ClearAll(hosts, users, keyPaths)

	// Verify all entries are removed
	for _, h := range hosts {
		for _, u := range users {
			got, err := ks.GetSudoPassword(h, u)
			if err != nil {
				t.Errorf("GetSudoPassword(%s, %s) error: %v", h, u, err)
			}
			if got != nil {
				t.Errorf("expected nil for sudo password %s@%s after ClearAll", u, h)
			}

			got, err = ks.GetServerPassword(h, u)
			if err != nil {
				t.Errorf("GetServerPassword(%s, %s) error: %v", h, u, err)
			}
			if got != nil {
				t.Errorf("expected nil for server password %s@%s after ClearAll", u, h)
			}
		}
	}
	for _, kp := range keyPaths {
		got, err := ks.GetSSHPassphrase(kp)
		if err != nil {
			t.Errorf("GetSSHPassphrase(%s) error: %v", kp, err)
		}
		if got != nil {
			t.Errorf("expected nil for passphrase %s after ClearAll", kp)
		}
	}
}

func TestKeyringStore_ClearAll_Disabled(t *testing.T) {
	ks := &KeyringStore{enabled: false}

	// Should not panic even when disabled
	ks.ClearAll([]string{"host"}, []string{"user"}, []string{"/key"})
}

func TestKeyringStore_ClearAll_EmptyLists(t *testing.T) {
	ks := setupMockKeyring(t)

	// Should not panic with empty lists
	ks.ClearAll(nil, nil, nil)
	ks.ClearAll([]string{}, []string{}, []string{})
}

// --- Cross-credential isolation tests ---

func TestKeyringStore_CredentialIsolation(t *testing.T) {
	ks := setupMockKeyring(t)

	// Store different credential types for the same host/user
	host := "shared.host.com"
	user := "shared-user"

	sudoPass := []byte("sudo-password")
	serverPass := []byte("server-password")

	if err := ks.StoreSudoPassword(host, user, sudoPass); err != nil {
		t.Fatalf("StoreSudoPassword failed: %v", err)
	}
	if err := ks.StoreServerPassword(host, user, serverPass); err != nil {
		t.Fatalf("StoreServerPassword failed: %v", err)
	}

	// They should be different credentials
	gotSudo, err := ks.GetSudoPassword(host, user)
	if err != nil {
		t.Fatalf("GetSudoPassword failed: %v", err)
	}
	gotServer, err := ks.GetServerPassword(host, user)
	if err != nil {
		t.Fatalf("GetServerPassword failed: %v", err)
	}

	if string(gotSudo) != string(sudoPass) {
		t.Errorf("sudo password: got %q, want %q", gotSudo, sudoPass)
	}
	if string(gotServer) != string(serverPass) {
		t.Errorf("server password: got %q, want %q", gotServer, serverPass)
	}

	// Deleting one should not affect the other
	if err := ks.DeleteSudoPassword(host, user); err != nil {
		t.Fatalf("DeleteSudoPassword failed: %v", err)
	}

	gotServer, err = ks.GetServerPassword(host, user)
	if err != nil {
		t.Fatalf("GetServerPassword after sudo delete failed: %v", err)
	}
	if string(gotServer) != string(serverPass) {
		t.Errorf("server password should survive sudo deletion: got %q, want %q", gotServer, serverPass)
	}
}

// --- Binary data / special characters tests ---

func TestKeyringStore_SSHPassphrase_BinaryData(t *testing.T) {
	ks := setupMockKeyring(t)

	// Binary data with null bytes and high-value bytes
	binaryPass := []byte{0x00, 0x01, 0xFF, 0xFE, 0x80, 0x7F, 0x00, 0xAB}
	keyPath := "/keys/binary-key"

	if err := ks.StoreSSHPassphrase(keyPath, binaryPass); err != nil {
		t.Fatalf("StoreSSHPassphrase with binary data failed: %v", err)
	}

	got, err := ks.GetSSHPassphrase(keyPath)
	if err != nil {
		t.Fatalf("GetSSHPassphrase with binary data failed: %v", err)
	}

	if len(got) != len(binaryPass) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(binaryPass))
	}
	for i := range binaryPass {
		if got[i] != binaryPass[i] {
			t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], binaryPass[i])
		}
	}
}

func TestKeyringStore_SSHPassphrase_EmptyData(t *testing.T) {
	ks := setupMockKeyring(t)

	emptyPass := []byte{}
	keyPath := "/keys/empty-key"

	if err := ks.StoreSSHPassphrase(keyPath, emptyPass); err != nil {
		t.Fatalf("StoreSSHPassphrase with empty data failed: %v", err)
	}

	got, err := ks.GetSSHPassphrase(keyPath)
	if err != nil {
		t.Fatalf("GetSSHPassphrase with empty data failed: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// --- Overwrite tests ---

func TestKeyringStore_OverwriteSSHPassphrase(t *testing.T) {
	ks := setupMockKeyring(t)
	keyPath := "/keys/overwrite-key"

	if err := ks.StoreSSHPassphrase(keyPath, []byte("old-passphrase")); err != nil {
		t.Fatalf("StoreSSHPassphrase (old) failed: %v", err)
	}
	if err := ks.StoreSSHPassphrase(keyPath, []byte("new-passphrase")); err != nil {
		t.Fatalf("StoreSSHPassphrase (new) failed: %v", err)
	}

	got, err := ks.GetSSHPassphrase(keyPath)
	if err != nil {
		t.Fatalf("GetSSHPassphrase failed: %v", err)
	}
	if string(got) != "new-passphrase" {
		t.Errorf("got %q, want %q", got, "new-passphrase")
	}
}

func TestKeyringStore_OverwriteSudoPassword(t *testing.T) {
	ks := setupMockKeyring(t)
	host := "host.com"
	user := "admin"

	if err := ks.StoreSudoPassword(host, user, []byte("old-sudo")); err != nil {
		t.Fatalf("StoreSudoPassword (old) failed: %v", err)
	}
	if err := ks.StoreSudoPassword(host, user, []byte("new-sudo")); err != nil {
		t.Fatalf("StoreSudoPassword (new) failed: %v", err)
	}

	got, err := ks.GetSudoPassword(host, user)
	if err != nil {
		t.Fatalf("GetSudoPassword failed: %v", err)
	}
	if string(got) != "new-sudo" {
		t.Errorf("got %q, want %q", got, "new-sudo")
	}
}

func TestKeyringStore_OverwriteServerPassword(t *testing.T) {
	ks := setupMockKeyring(t)
	host := "host.com"
	user := "admin"

	if err := ks.StoreServerPassword(host, user, []byte("old-server")); err != nil {
		t.Fatalf("StoreServerPassword (old) failed: %v", err)
	}
	if err := ks.StoreServerPassword(host, user, []byte("new-server")); err != nil {
		t.Fatalf("StoreServerPassword (new) failed: %v", err)
	}

	got, err := ks.GetServerPassword(host, user)
	if err != nil {
		t.Fatalf("GetServerPassword failed: %v", err)
	}
	if string(got) != "new-server" {
		t.Errorf("got %q, want %q", got, "new-server")
	}
}

// --- Multiple distinct keys tests ---

func TestKeyringStore_MultipleDistinctSSHPassphrases(t *testing.T) {
	ks := setupMockKeyring(t)

	keys := map[string]string{
		"/keys/key1": "passphrase-1",
		"/keys/key2": "passphrase-2",
		"/keys/key3": "passphrase-3",
	}

	for kp, pp := range keys {
		if err := ks.StoreSSHPassphrase(kp, []byte(pp)); err != nil {
			t.Fatalf("StoreSSHPassphrase(%s) failed: %v", kp, err)
		}
	}

	for kp, pp := range keys {
		got, err := ks.GetSSHPassphrase(kp)
		if err != nil {
			t.Fatalf("GetSSHPassphrase(%s) failed: %v", kp, err)
		}
		if string(got) != pp {
			t.Errorf("key %s: got %q, want %q", kp, got, pp)
		}
	}
}

func TestKeyringStore_MultipleDistinctSudoPasswords(t *testing.T) {
	ks := setupMockKeyring(t)

	entries := []struct {
		host, user, pass string
	}{
		{"host1.com", "root", "pass1"},
		{"host1.com", "deploy", "pass2"},
		{"host2.com", "root", "pass3"},
	}

	for _, e := range entries {
		if err := ks.StoreSudoPassword(e.host, e.user, []byte(e.pass)); err != nil {
			t.Fatalf("StoreSudoPassword(%s, %s) failed: %v", e.host, e.user, err)
		}
	}

	for _, e := range entries {
		got, err := ks.GetSudoPassword(e.host, e.user)
		if err != nil {
			t.Fatalf("GetSudoPassword(%s, %s) failed: %v", e.host, e.user, err)
		}
		if string(got) != e.pass {
			t.Errorf("%s@%s: got %q, want %q", e.user, e.host, got, e.pass)
		}
	}
}

// --- KeyringService constant test ---

func TestKeyringServiceConstant(t *testing.T) {
	if KeyringService != "claude-shell-mcp" {
		t.Errorf("KeyringService = %q, want %q", KeyringService, "claude-shell-mcp")
	}
}

// --- ClearAll partial entries test ---

// --- Base64 decode failure tests ---
// These tests inject invalid base64 directly into the mock keyring
// to trigger the decode error path in GetSSHPassphrase/GetSudoPassword/GetServerPassword.

func TestKeyringStore_GetSSHPassphrase_InvalidBase64(t *testing.T) {
	keyring.MockInit()
	ks := &KeyringStore{enabled: true}

	// Directly set invalid base64 in the keyring using the same key format
	keyPath := "/keys/corrupt"
	key := "ssh-passphrase:" + keyPath
	if err := keyring.Set(KeyringService, key, "!!!not-valid-base64!!!"); err != nil {
		t.Fatalf("keyring.Set failed: %v", err)
	}

	got, err := ks.GetSSHPassphrase(keyPath)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if got != nil {
		t.Error("expected nil result on decode error")
	}
}

func TestKeyringStore_GetSudoPassword_InvalidBase64(t *testing.T) {
	keyring.MockInit()
	ks := &KeyringStore{enabled: true}

	host := "host.com"
	user := "user"
	key := "sudo:" + user + "@" + host
	if err := keyring.Set(KeyringService, key, "%%%corrupt%%%"); err != nil {
		t.Fatalf("keyring.Set failed: %v", err)
	}

	got, err := ks.GetSudoPassword(host, user)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if got != nil {
		t.Error("expected nil result on decode error")
	}
}

func TestKeyringStore_GetServerPassword_InvalidBase64(t *testing.T) {
	keyring.MockInit()
	ks := &KeyringStore{enabled: true}

	host := "host.com"
	user := "admin"
	key := "server:" + user + "@" + host
	if err := keyring.Set(KeyringService, key, "<<>>invalid<<>>"); err != nil {
		t.Fatalf("keyring.Set failed: %v", err)
	}

	got, err := ks.GetServerPassword(host, user)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if got != nil {
		t.Error("expected nil result on decode error")
	}
}

func TestKeyringStore_ClearAll_PartialEntries(t *testing.T) {
	ks := setupMockKeyring(t)

	// Only store a few entries, but ClearAll with broader lists
	if err := ks.StoreSudoPassword("host1", "user1", []byte("pass")); err != nil {
		t.Fatalf("StoreSudoPassword failed: %v", err)
	}

	// ClearAll with hosts/users that don't have entries should not error
	ks.ClearAll(
		[]string{"host1", "host2", "host3"},
		[]string{"user1", "user2"},
		[]string{"/key1", "/key2"},
	)

	// The entry we did store should be gone
	got, err := ks.GetSudoPassword("host1", "user1")
	if err != nil {
		t.Fatalf("GetSudoPassword after ClearAll failed: %v", err)
	}
	if got != nil {
		t.Error("expected nil after ClearAll")
	}
}
