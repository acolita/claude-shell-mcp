// Package config handles configuration parsing for claude-shell-mcp.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level configuration.
type Config struct {
	Servers         []ServerConfig  `yaml:"servers"`
	Security        SecurityConfig  `yaml:"security"`
	Logging         LoggingConfig   `yaml:"logging"`
	Recording       RecordingConfig `yaml:"recording"`
	Shell           ShellConfig     `yaml:"shell"`
	PromptDetection PromptConfig    `yaml:"prompt_detection"`
	Mode            string          `yaml:"mode"` // "ssh" or "local"
}

// ServerConfig defines an SSH server connection.
type ServerConfig struct {
	Name    string     `yaml:"name"`
	Host    string     `yaml:"host"`
	Port    int        `yaml:"port"`
	User    string     `yaml:"user"`
	KeyPath string     `yaml:"key_path"`
	Auth    AuthConfig `yaml:"auth"`
}

// AuthConfig defines authentication settings.
type AuthConfig struct {
	Type          string `yaml:"type"`           // "key" or "password"
	Path          string `yaml:"path"`           // path to key file
	PassphraseEnv string `yaml:"passphrase_env"` // env var containing passphrase
}

// SecurityConfig defines security settings.
type SecurityConfig struct {
	SudoCacheTTL        time.Duration `yaml:"sudo_cache_ttl"`
	IdleTimeout         time.Duration `yaml:"idle_timeout"`
	MaxSessionsPerUser  int           `yaml:"max_sessions_per_user"`
	CommandBlocklist    []string      `yaml:"command_blocklist"`     // Regex patterns for blocked commands
	CommandAllowlist    []string      `yaml:"command_allowlist"`     // If set, only these patterns allowed
	MaxAuthFailures     int           `yaml:"max_auth_failures"`     // Max failed auth attempts before lockout
	AuthLockoutDuration time.Duration `yaml:"auth_lockout_duration"` // Duration of auth lockout
	UseKeyring          bool          `yaml:"use_keyring"`           // Use OS keyring for credential storage
}

// LoggingConfig defines logging settings.
type LoggingConfig struct {
	Level    string `yaml:"level"`    // "debug", "info", "warn", "error"
	Sanitize bool   `yaml:"sanitize"` // sanitize sensitive data from logs
}

// RecordingConfig defines session recording settings.
type RecordingConfig struct {
	Enabled bool   `yaml:"enabled"` // enable session recording
	Path    string `yaml:"path"`    // directory to store recordings
}

// ShellConfig defines shell behavior settings.
type ShellConfig struct {
	SourceRC bool   `yaml:"source_rc"` // source .bashrc/.zshrc (default: true)
	Path     string `yaml:"path"`      // custom shell path (overrides detection)
}

// PromptConfig defines prompt detection settings.
type PromptConfig struct {
	CustomPatterns []PatternConfig `yaml:"custom_patterns"`
}

// PatternConfig defines a custom prompt pattern.
type PatternConfig struct {
	Name      string `yaml:"name"`
	Regex     string `yaml:"regex"`
	Type      string `yaml:"type"`       // "password", "confirmation", "text"
	MaskInput bool   `yaml:"mask_input"` // mask input in logs
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Mode: "local",
		Security: SecurityConfig{
			SudoCacheTTL:       5 * time.Minute,
			IdleTimeout:        30 * time.Minute,
			MaxSessionsPerUser: 10,
		},
		Logging: LoggingConfig{
			Level:    "info",
			Sanitize: true,
		},
		Shell: ShellConfig{
			SourceRC: true, // Source shell rc files by default
		},
	}
}

// Load loads configuration from a YAML file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Mode != "local" && c.Mode != "ssh" && c.Mode != "" {
		return fmt.Errorf("invalid mode: %s (must be 'local' or 'ssh')", c.Mode)
	}

	if c.Security.MaxSessionsPerUser <= 0 {
		c.Security.MaxSessionsPerUser = 10
	}

	return nil
}
