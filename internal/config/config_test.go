package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Mode != "local" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "local")
	}
	if cfg.Security.SudoCacheTTL != 5*time.Minute {
		t.Errorf("SudoCacheTTL = %v, want %v", cfg.Security.SudoCacheTTL, 5*time.Minute)
	}
	if cfg.Security.IdleTimeout != 30*time.Minute {
		t.Errorf("IdleTimeout = %v, want %v", cfg.Security.IdleTimeout, 30*time.Minute)
	}
	if cfg.Security.MaxSessionsPerUser != 10 {
		t.Errorf("MaxSessionsPerUser = %d, want %d", cfg.Security.MaxSessionsPerUser, 10)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "info")
	}
	if !cfg.Logging.Sanitize {
		t.Error("Logging.Sanitize = false, want true")
	}
	if !cfg.Shell.SourceRC {
		t.Error("Shell.SourceRC = false, want true")
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error: %v", err)
	}
	if cfg.Mode != "local" {
		t.Errorf("Mode = %q, want %q (default)", cfg.Mode, "local")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load(nonexistent) expected error, got nil")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.yaml")
	if err := os.WriteFile(path, []byte(":::invalid:::yaml{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load(invalid YAML) expected error, got nil")
	}
}

func TestLoadValidConfig(t *testing.T) {
	yaml := `
mode: ssh
servers:
  - name: prod
    host: 10.0.0.1
    port: 2222
    user: deploy
    key_path: ~/.ssh/id_rsa
    auth:
      type: key
      path: ~/.ssh/id_rsa
      passphrase_env: SSH_PASS
    sudo_password_env: PROD_SUDO
  - name: staging
    host: 10.0.0.2
    port: 22
    user: admin
    auth:
      type: password
      password_env: STAGING_SSH_PASS
security:
  sudo_cache_ttl: 10m
  idle_timeout: 1h
  max_sessions_per_user: 5
  command_blocklist:
    - "rm -rf /"
  max_auth_failures: 3
  auth_lockout_duration: 15m
  use_keyring: true
logging:
  level: debug
  sanitize: false
recording:
  enabled: true
  path: /var/log/recordings
shell:
  source_rc: false
  path: /bin/zsh
prompt_detection:
  custom_patterns:
    - name: vault
      regex: "Vault password:"
      type: password
      mask_input: true
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Mode
	if cfg.Mode != "ssh" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "ssh")
	}

	// Servers
	if len(cfg.Servers) != 2 {
		t.Fatalf("len(Servers) = %d, want 2", len(cfg.Servers))
	}
	s := cfg.Servers[0]
	if s.Name != "prod" {
		t.Errorf("Servers[0].Name = %q, want %q", s.Name, "prod")
	}
	if s.Host != "10.0.0.1" {
		t.Errorf("Servers[0].Host = %q, want %q", s.Host, "10.0.0.1")
	}
	if s.Port != 2222 {
		t.Errorf("Servers[0].Port = %d, want %d", s.Port, 2222)
	}
	if s.User != "deploy" {
		t.Errorf("Servers[0].User = %q, want %q", s.User, "deploy")
	}
	if s.KeyPath != "~/.ssh/id_rsa" {
		t.Errorf("Servers[0].KeyPath = %q, want %q", s.KeyPath, "~/.ssh/id_rsa")
	}
	if s.Auth.Type != "key" {
		t.Errorf("Servers[0].Auth.Type = %q, want %q", s.Auth.Type, "key")
	}
	if s.Auth.PassphraseEnv != "SSH_PASS" {
		t.Errorf("Servers[0].Auth.PassphraseEnv = %q, want %q", s.Auth.PassphraseEnv, "SSH_PASS")
	}
	if s.SudoPasswordEnv != "PROD_SUDO" {
		t.Errorf("Servers[0].SudoPasswordEnv = %q, want %q", s.SudoPasswordEnv, "PROD_SUDO")
	}

	s2 := cfg.Servers[1]
	if s2.Auth.Type != "password" {
		t.Errorf("Servers[1].Auth.Type = %q, want %q", s2.Auth.Type, "password")
	}
	if s2.Auth.PasswordEnv != "STAGING_SSH_PASS" {
		t.Errorf("Servers[1].Auth.PasswordEnv = %q, want %q", s2.Auth.PasswordEnv, "STAGING_SSH_PASS")
	}

	// Security
	if cfg.Security.SudoCacheTTL != 10*time.Minute {
		t.Errorf("SudoCacheTTL = %v, want %v", cfg.Security.SudoCacheTTL, 10*time.Minute)
	}
	if cfg.Security.IdleTimeout != time.Hour {
		t.Errorf("IdleTimeout = %v, want %v", cfg.Security.IdleTimeout, time.Hour)
	}
	if cfg.Security.MaxSessionsPerUser != 5 {
		t.Errorf("MaxSessionsPerUser = %d, want 5", cfg.Security.MaxSessionsPerUser)
	}
	if len(cfg.Security.CommandBlocklist) != 1 {
		t.Errorf("CommandBlocklist len = %d, want 1", len(cfg.Security.CommandBlocklist))
	}
	if cfg.Security.MaxAuthFailures != 3 {
		t.Errorf("MaxAuthFailures = %d, want 3", cfg.Security.MaxAuthFailures)
	}
	if cfg.Security.AuthLockoutDuration != 15*time.Minute {
		t.Errorf("AuthLockoutDuration = %v, want 15m", cfg.Security.AuthLockoutDuration)
	}
	if !cfg.Security.UseKeyring {
		t.Error("UseKeyring = false, want true")
	}

	// Logging
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Logging.Sanitize {
		t.Error("Logging.Sanitize = true, want false")
	}

	// Recording
	if !cfg.Recording.Enabled {
		t.Error("Recording.Enabled = false, want true")
	}
	if cfg.Recording.Path != "/var/log/recordings" {
		t.Errorf("Recording.Path = %q, want %q", cfg.Recording.Path, "/var/log/recordings")
	}

	// Shell
	if cfg.Shell.SourceRC {
		t.Error("Shell.SourceRC = true, want false")
	}
	if cfg.Shell.Path != "/bin/zsh" {
		t.Errorf("Shell.Path = %q, want %q", cfg.Shell.Path, "/bin/zsh")
	}

	// Prompt detection
	if len(cfg.PromptDetection.CustomPatterns) != 1 {
		t.Fatalf("CustomPatterns len = %d, want 1", len(cfg.PromptDetection.CustomPatterns))
	}
	p := cfg.PromptDetection.CustomPatterns[0]
	if p.Name != "vault" {
		t.Errorf("Pattern.Name = %q, want %q", p.Name, "vault")
	}
	if p.Type != "password" {
		t.Errorf("Pattern.Type = %q, want %q", p.Type, "password")
	}
	if !p.MaskInput {
		t.Error("Pattern.MaskInput = false, want true")
	}
}

func TestLoadPartialConfig(t *testing.T) {
	yaml := `
mode: ssh
servers:
  - name: dev
    host: localhost
`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "partial.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Overridden values
	if cfg.Mode != "ssh" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "ssh")
	}

	// Defaults preserved for unset fields
	if cfg.Security.SudoCacheTTL != 5*time.Minute {
		t.Errorf("SudoCacheTTL = %v, want default %v", cfg.Security.SudoCacheTTL, 5*time.Minute)
	}
	if cfg.Security.MaxSessionsPerUser != 10 {
		t.Errorf("MaxSessionsPerUser = %d, want default 10", cfg.Security.MaxSessionsPerUser)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{"local mode", "local", false},
		{"ssh mode", "ssh", false},
		{"empty mode", "", false},
		{"invalid mode", "telnet", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Mode = tt.mode
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFixesMaxSessions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 0

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	if cfg.Security.MaxSessionsPerUser != 10 {
		t.Errorf("MaxSessionsPerUser = %d, want 10 (corrected)", cfg.Security.MaxSessionsPerUser)
	}
}

func TestValidateFixesNegativeMaxSessions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Security.MaxSessionsPerUser = -5

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	if cfg.Security.MaxSessionsPerUser != 10 {
		t.Errorf("MaxSessionsPerUser = %d, want 10 (corrected)", cfg.Security.MaxSessionsPerUser)
	}
}

// --- Watcher tests ---

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNewWatcher(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, "mode: local\n")

	w, err := NewWatcher(path, nil)
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer w.Close()

	cfg := w.Config()
	if cfg.Mode != "local" {
		t.Errorf("Config().Mode = %q, want %q", cfg.Mode, "local")
	}
}

func TestNewWatcherMissingFile(t *testing.T) {
	_, err := NewWatcher("/nonexistent/config.yaml", nil)
	if err == nil {
		t.Fatal("NewWatcher(missing) expected error, got nil")
	}
}

func TestWatcherReloadsOnFileChange(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, "mode: local\n")

	var mu sync.Mutex
	var changed *Config

	w, err := NewWatcher(path, func(cfg *Config) {
		mu.Lock()
		changed = cfg
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer w.Close()

	// Modify the config file
	writeConfigFile(t, path, "mode: ssh\n")

	// Wait for the watcher to pick up the change
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := changed
		mu.Unlock()
		if c != nil && c.Mode == "ssh" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify the config was reloaded
	cfg := w.Config()
	if cfg.Mode != "ssh" {
		t.Errorf("Config().Mode = %q after reload, want %q", cfg.Mode, "ssh")
	}

	mu.Lock()
	if changed == nil {
		t.Error("onChange callback was never called")
	} else if changed.Mode != "ssh" {
		t.Errorf("onChange received Mode = %q, want %q", changed.Mode, "ssh")
	}
	mu.Unlock()
}

func TestWatcherReloadInvalidConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, "mode: local\n")

	callCount := 0
	var mu sync.Mutex

	w, err := NewWatcher(path, func(cfg *Config) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer w.Close()

	// Write invalid YAML - reload should fail silently (log error)
	writeConfigFile(t, path, ":::invalid{{{")

	time.Sleep(500 * time.Millisecond)

	// Original config should be preserved
	cfg := w.Config()
	if cfg.Mode != "local" {
		t.Errorf("Config().Mode = %q, want %q (preserved after bad reload)", cfg.Mode, "local")
	}

	mu.Lock()
	if callCount > 0 {
		t.Errorf("onChange was called %d times, want 0 (invalid config should not trigger)", callCount)
	}
	mu.Unlock()
}

func TestWatcherReloadInvalidMode(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, "mode: local\n")

	var mu sync.Mutex
	var lastMode string

	w, err := NewWatcher(path, func(cfg *Config) {
		mu.Lock()
		lastMode = cfg.Mode
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}
	defer w.Close()

	// Write valid YAML but invalid mode - should fail validation
	writeConfigFile(t, path, "mode: telnet\n")

	time.Sleep(500 * time.Millisecond)

	// Config should never be set to "telnet" (invalid mode fails validation)
	cfg := w.Config()
	if cfg.Mode == "telnet" {
		t.Errorf("Config().Mode = %q, invalid mode should have been rejected by validation", cfg.Mode)
	}

	mu.Lock()
	if lastMode == "telnet" {
		t.Errorf("onChange received Mode = %q, invalid mode should not trigger onChange", lastMode)
	}
	mu.Unlock()
}

func TestWatcherClose(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, "mode: local\n")

	w, err := NewWatcher(path, nil)
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}
