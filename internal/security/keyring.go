// Package security provides secure credential handling for claude-shell-mcp.
package security

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"sync"

	"github.com/zalando/go-keyring"
)

const (
	// KeyringService is the service name used for keyring entries.
	KeyringService = "claude-shell-mcp"
)

// KeyringStore provides OS keyring integration for credential storage.
// It uses the system keyring (macOS Keychain, Linux Secret Service, Windows Credential Manager).
type KeyringStore struct {
	enabled bool
	mu      sync.RWMutex
}

// NewKeyringStore creates a new keyring store.
// If the system keyring is not available, the store will be disabled.
func NewKeyringStore() *KeyringStore {
	ks := &KeyringStore{
		enabled: true,
	}

	// Test if keyring is available by trying a dummy operation
	testKey := "__claude_shell_mcp_test__"
	err := keyring.Set(KeyringService, testKey, "test")
	if err != nil {
		slog.Debug("keyring not available, using memory-only storage",
			slog.String("error", err.Error()),
		)
		ks.enabled = false
		return ks
	}

	// Clean up test entry
	_ = keyring.Delete(KeyringService, testKey)

	slog.Debug("keyring storage enabled")
	return ks
}

// IsEnabled returns true if the keyring is available and enabled.
func (ks *KeyringStore) IsEnabled() bool {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.enabled
}

// SetEnabled allows enabling/disabling keyring usage.
func (ks *KeyringStore) SetEnabled(enabled bool) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.enabled = enabled
}

// StoreSSHPassphrase stores an SSH key passphrase in the keyring.
func (ks *KeyringStore) StoreSSHPassphrase(keyPath string, passphrase []byte) error {
	if !ks.IsEnabled() {
		return fmt.Errorf("keyring not available")
	}

	// Base64 encode to safely store binary data
	encoded := base64.StdEncoding.EncodeToString(passphrase)
	key := fmt.Sprintf("ssh-passphrase:%s", keyPath)

	if err := keyring.Set(KeyringService, key, encoded); err != nil {
		return fmt.Errorf("failed to store SSH passphrase: %w", err)
	}

	slog.Debug("stored SSH passphrase in keyring",
		slog.String("key_path", keyPath),
	)
	return nil
}

// GetSSHPassphrase retrieves an SSH key passphrase from the keyring.
func (ks *KeyringStore) GetSSHPassphrase(keyPath string) ([]byte, error) {
	if !ks.IsEnabled() {
		return nil, fmt.Errorf("keyring not available")
	}

	key := fmt.Sprintf("ssh-passphrase:%s", keyPath)
	encoded, err := keyring.Get(KeyringService, key)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, nil // Not found is not an error
		}
		return nil, fmt.Errorf("failed to get SSH passphrase: %w", err)
	}

	passphrase, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode SSH passphrase: %w", err)
	}

	return passphrase, nil
}

// DeleteSSHPassphrase removes an SSH key passphrase from the keyring.
func (ks *KeyringStore) DeleteSSHPassphrase(keyPath string) error {
	if !ks.IsEnabled() {
		return fmt.Errorf("keyring not available")
	}

	key := fmt.Sprintf("ssh-passphrase:%s", keyPath)
	if err := keyring.Delete(KeyringService, key); err != nil {
		if err == keyring.ErrNotFound {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete SSH passphrase: %w", err)
	}

	return nil
}

// StoreSudoPassword stores a sudo password in the keyring.
func (ks *KeyringStore) StoreSudoPassword(host, user string, password []byte) error {
	if !ks.IsEnabled() {
		return fmt.Errorf("keyring not available")
	}

	encoded := base64.StdEncoding.EncodeToString(password)
	key := fmt.Sprintf("sudo:%s@%s", user, host)

	if err := keyring.Set(KeyringService, key, encoded); err != nil {
		return fmt.Errorf("failed to store sudo password: %w", err)
	}

	slog.Debug("stored sudo password in keyring",
		slog.String("user", user),
		slog.String("host", host),
	)
	return nil
}

// GetSudoPassword retrieves a sudo password from the keyring.
func (ks *KeyringStore) GetSudoPassword(host, user string) ([]byte, error) {
	if !ks.IsEnabled() {
		return nil, fmt.Errorf("keyring not available")
	}

	key := fmt.Sprintf("sudo:%s@%s", user, host)
	encoded, err := keyring.Get(KeyringService, key)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get sudo password: %w", err)
	}

	password, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode sudo password: %w", err)
	}

	return password, nil
}

// DeleteSudoPassword removes a sudo password from the keyring.
func (ks *KeyringStore) DeleteSudoPassword(host, user string) error {
	if !ks.IsEnabled() {
		return fmt.Errorf("keyring not available")
	}

	key := fmt.Sprintf("sudo:%s@%s", user, host)
	if err := keyring.Delete(KeyringService, key); err != nil {
		if err == keyring.ErrNotFound {
			return nil
		}
		return fmt.Errorf("failed to delete sudo password: %w", err)
	}

	return nil
}

// StoreServerPassword stores a server password in the keyring (for password-based SSH auth).
func (ks *KeyringStore) StoreServerPassword(host, user string, password []byte) error {
	if !ks.IsEnabled() {
		return fmt.Errorf("keyring not available")
	}

	encoded := base64.StdEncoding.EncodeToString(password)
	key := fmt.Sprintf("server:%s@%s", user, host)

	if err := keyring.Set(KeyringService, key, encoded); err != nil {
		return fmt.Errorf("failed to store server password: %w", err)
	}

	slog.Debug("stored server password in keyring",
		slog.String("user", user),
		slog.String("host", host),
	)
	return nil
}

// GetServerPassword retrieves a server password from the keyring.
func (ks *KeyringStore) GetServerPassword(host, user string) ([]byte, error) {
	if !ks.IsEnabled() {
		return nil, fmt.Errorf("keyring not available")
	}

	key := fmt.Sprintf("server:%s@%s", user, host)
	encoded, err := keyring.Get(KeyringService, key)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get server password: %w", err)
	}

	password, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode server password: %w", err)
	}

	return password, nil
}

// DeleteServerPassword removes a server password from the keyring.
func (ks *KeyringStore) DeleteServerPassword(host, user string) error {
	if !ks.IsEnabled() {
		return fmt.Errorf("keyring not available")
	}

	key := fmt.Sprintf("server:%s@%s", user, host)
	if err := keyring.Delete(KeyringService, key); err != nil {
		if err == keyring.ErrNotFound {
			return nil
		}
		return fmt.Errorf("failed to delete server password: %w", err)
	}

	return nil
}

// ClearAll removes all claude-shell-mcp entries from the keyring.
// Note: This is a best-effort operation as we can't enumerate keyring entries.
func (ks *KeyringStore) ClearAll(hosts []string, users []string, keyPaths []string) {
	if !ks.IsEnabled() {
		return
	}

	// Clear known entries
	for _, host := range hosts {
		for _, user := range users {
			_ = ks.DeleteSudoPassword(host, user)
			_ = ks.DeleteServerPassword(host, user)
		}
	}

	for _, keyPath := range keyPaths {
		_ = ks.DeleteSSHPassphrase(keyPath)
	}

	slog.Debug("cleared keyring entries")
}
