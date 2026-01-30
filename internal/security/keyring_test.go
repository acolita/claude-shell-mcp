package security

import (
	"testing"
)

func TestKeyringStore_NewKeyringStore(t *testing.T) {
	// This test may fail on systems without a keyring (headless servers, CI)
	ks := NewKeyringStore()

	// Just verify it doesn't panic and returns a valid object
	if ks == nil {
		t.Fatal("NewKeyringStore returned nil")
	}

	// Log whether keyring is available
	t.Logf("Keyring enabled: %v", ks.IsEnabled())
}

func TestKeyringStore_SetEnabled(t *testing.T) {
	ks := NewKeyringStore()

	// Test enabling/disabling
	originalState := ks.IsEnabled()

	ks.SetEnabled(false)
	if ks.IsEnabled() {
		t.Error("SetEnabled(false) did not disable keyring")
	}

	ks.SetEnabled(true)
	// Note: This may still be false if keyring was never available
	// We just test that SetEnabled doesn't panic

	// Restore original state
	ks.SetEnabled(originalState)
}

func TestKeyringStore_SSHPassphrase(t *testing.T) {
	ks := NewKeyringStore()
	if !ks.IsEnabled() {
		t.Skip("Keyring not available on this system")
	}

	testKeyPath := "/tmp/test_key_for_claude_shell_mcp"
	testPassphrase := []byte("test-passphrase-123")

	// Store passphrase
	err := ks.StoreSSHPassphrase(testKeyPath, testPassphrase)
	if err != nil {
		t.Fatalf("StoreSSHPassphrase failed: %v", err)
	}

	// Retrieve passphrase
	retrieved, err := ks.GetSSHPassphrase(testKeyPath)
	if err != nil {
		t.Fatalf("GetSSHPassphrase failed: %v", err)
	}

	if string(retrieved) != string(testPassphrase) {
		t.Errorf("Retrieved passphrase mismatch: got %q, want %q", retrieved, testPassphrase)
	}

	// Delete passphrase
	err = ks.DeleteSSHPassphrase(testKeyPath)
	if err != nil {
		t.Fatalf("DeleteSSHPassphrase failed: %v", err)
	}

	// Verify deletion
	retrieved, err = ks.GetSSHPassphrase(testKeyPath)
	if err != nil {
		t.Fatalf("GetSSHPassphrase after delete failed: %v", err)
	}
	if retrieved != nil {
		t.Error("Passphrase should be nil after deletion")
	}
}

func TestKeyringStore_SudoPassword(t *testing.T) {
	ks := NewKeyringStore()
	if !ks.IsEnabled() {
		t.Skip("Keyring not available on this system")
	}

	testHost := "test-host.local"
	testUser := "testuser"
	testPassword := []byte("sudo-password-456")

	// Store password
	err := ks.StoreSudoPassword(testHost, testUser, testPassword)
	if err != nil {
		t.Fatalf("StoreSudoPassword failed: %v", err)
	}

	// Retrieve password
	retrieved, err := ks.GetSudoPassword(testHost, testUser)
	if err != nil {
		t.Fatalf("GetSudoPassword failed: %v", err)
	}

	if string(retrieved) != string(testPassword) {
		t.Errorf("Retrieved password mismatch: got %q, want %q", retrieved, testPassword)
	}

	// Delete password
	err = ks.DeleteSudoPassword(testHost, testUser)
	if err != nil {
		t.Fatalf("DeleteSudoPassword failed: %v", err)
	}

	// Verify deletion
	retrieved, err = ks.GetSudoPassword(testHost, testUser)
	if err != nil {
		t.Fatalf("GetSudoPassword after delete failed: %v", err)
	}
	if retrieved != nil {
		t.Error("Password should be nil after deletion")
	}
}

func TestKeyringStore_ServerPassword(t *testing.T) {
	ks := NewKeyringStore()
	if !ks.IsEnabled() {
		t.Skip("Keyring not available on this system")
	}

	testHost := "server.example.com"
	testUser := "admin"
	testPassword := []byte("server-password-789")

	// Store password
	err := ks.StoreServerPassword(testHost, testUser, testPassword)
	if err != nil {
		t.Fatalf("StoreServerPassword failed: %v", err)
	}

	// Retrieve password
	retrieved, err := ks.GetServerPassword(testHost, testUser)
	if err != nil {
		t.Fatalf("GetServerPassword failed: %v", err)
	}

	if string(retrieved) != string(testPassword) {
		t.Errorf("Retrieved password mismatch: got %q, want %q", retrieved, testPassword)
	}

	// Delete password
	err = ks.DeleteServerPassword(testHost, testUser)
	if err != nil {
		t.Fatalf("DeleteServerPassword failed: %v", err)
	}

	// Verify deletion
	retrieved, err = ks.GetServerPassword(testHost, testUser)
	if err != nil {
		t.Fatalf("GetServerPassword after delete failed: %v", err)
	}
	if retrieved != nil {
		t.Error("Password should be nil after deletion")
	}
}

func TestKeyringStore_DisabledOperations(t *testing.T) {
	ks := NewKeyringStore()
	ks.SetEnabled(false)

	// All operations should return errors when disabled
	err := ks.StoreSSHPassphrase("/test", []byte("test"))
	if err == nil {
		t.Error("StoreSSHPassphrase should fail when disabled")
	}

	_, err = ks.GetSSHPassphrase("/test")
	if err == nil {
		t.Error("GetSSHPassphrase should fail when disabled")
	}

	err = ks.StoreSudoPassword("host", "user", []byte("test"))
	if err == nil {
		t.Error("StoreSudoPassword should fail when disabled")
	}

	_, err = ks.GetSudoPassword("host", "user")
	if err == nil {
		t.Error("GetSudoPassword should fail when disabled")
	}
}
