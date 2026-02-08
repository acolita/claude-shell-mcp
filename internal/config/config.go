// Package config handles configuration parsing for claude-shell-mcp.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
	"gopkg.in/yaml.v3"
)

// DefaultConfigPath returns the default config file path:
// $XDG_CONFIG_HOME/claude-shell-mcp/config.yaml or ~/.config/claude-shell-mcp/config.yaml
func DefaultConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "claude-shell-mcp", "config.yaml")
}

// Config represents the top-level configuration.
type Config struct {
	Servers         []ServerConfig  `yaml:"servers"`
	Security        SecurityConfig  `yaml:"security"`
	Logging         LoggingConfig   `yaml:"logging"`
	Recording       RecordingConfig `yaml:"recording"`
	Shell           ShellConfig     `yaml:"shell"`
	PromptDetection PromptConfig    `yaml:"prompt_detection"`
}

// ServerConfig defines an SSH server connection.
type ServerConfig struct {
	Name            string     `yaml:"name"`
	Host            string     `yaml:"host"`
	Port            int        `yaml:"port"`
	User            string     `yaml:"user"`
	KeyPath         string     `yaml:"key_path"`
	Auth            AuthConfig `yaml:"auth"`
	SudoPasswordEnv string     `yaml:"sudo_password_env"` // env var containing sudo password
}

// AuthConfig defines authentication settings.
type AuthConfig struct {
	Type          string `yaml:"type"`           // "key" or "password"
	Path          string `yaml:"path"`           // path to key file
	PassphraseEnv string `yaml:"passphrase_env"` // env var containing key passphrase
	PasswordEnv   string `yaml:"password_env"`   // env var containing SSH password
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
// An optional FileSystem can be passed for testing; if omitted, the real OS is used.
func Load(path string, fsys ...ports.FileSystem) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	var data []byte
	var err error
	if len(fsys) > 0 && fsys[0] != nil {
		data, err = fsys[0].ReadFile(path)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet — return defaults (config_add will create it)
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Security.MaxSessionsPerUser <= 0 {
		c.Security.MaxSessionsPerUser = 10
	}

	return nil
}

// AddServer adds a server to the configuration.
// Returns an error if a server with the same name already exists.
func (c *Config) AddServer(server ServerConfig) error {
	for _, s := range c.Servers {
		if s.Name == server.Name {
			return fmt.Errorf("server %q already exists", server.Name)
		}
	}
	c.Servers = append(c.Servers, server)
	return nil
}

// Save writes the configuration to a YAML file.
// An optional FileSystem can be passed for testing; if omitted, the real OS is used.
func Save(cfg *Config, path string, fsys ...ports.FileSystem) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if len(fsys) > 0 && fsys[0] != nil {
		return fsys[0].WriteFile(path, data, 0644)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
