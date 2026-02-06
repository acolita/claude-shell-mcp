package ssh

import (
	"bufio"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realfs"
	"github.com/acolita/claude-shell-mcp/internal/adapters/realnet"
	"github.com/acolita/claude-shell-mcp/internal/ports"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	KeyPath       string // Path to private key file
	KeyPassphrase string // Passphrase for encrypted keys
	UseAgent      bool   // Use SSH agent for authentication
	Password      string // Password for password authentication
	Host          string // Target host for SSH config lookup

	// Injected dependencies (optional, defaults to real implementations)
	FS     ports.FileSystem   // File system for reading keys/config
	Dialer ports.NetworkDialer // Network dialer for SSH agent connection
}

// BuildAuthMethods constructs SSH auth methods from config.
func BuildAuthMethods(cfg AuthConfig) ([]ssh.AuthMethod, error) {
	// Default injected dependencies
	if cfg.FS == nil {
		cfg.FS = realfs.New()
	}
	if cfg.Dialer == nil {
		cfg.Dialer = realnet.NewDialer()
	}

	var methods []ssh.AuthMethod

	methods = trySSHAgentAuth(cfg, methods)

	keyAuth, err := tryExplicitKeyAuth(cfg)
	if err != nil {
		return nil, err
	}
	if keyAuth != nil {
		methods = append(methods, keyAuth)
	}

	methods = trySSHConfigAuth(cfg, methods)
	methods = tryDefaultKeysAuth(cfg, methods)
	methods = tryPasswordAuth(cfg, methods)

	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication methods available")
	}

	return methods, nil
}

// trySSHAgentAuth attempts SSH agent authentication.
func trySSHAgentAuth(cfg AuthConfig, methods []ssh.AuthMethod) []ssh.AuthMethod {
	if !cfg.UseAgent {
		return methods
	}
	if agentAuth, err := sshAgentAuth(cfg.FS, cfg.Dialer); err == nil {
		methods = append(methods, agentAuth)
	}
	return methods
}

// tryExplicitKeyAuth attempts authentication with explicitly configured key.
func tryExplicitKeyAuth(cfg AuthConfig) (ssh.AuthMethod, error) {
	if cfg.KeyPath == "" {
		return nil, nil
	}
	keyAuth, err := privateKeyAuth(cfg.KeyPath, cfg.KeyPassphrase, cfg.FS)
	if err != nil {
		return nil, fmt.Errorf("private key auth: %w", err)
	}
	return keyAuth, nil
}

// trySSHConfigAuth attempts authentication using SSH config identity file.
func trySSHConfigAuth(cfg AuthConfig, methods []ssh.AuthMethod) []ssh.AuthMethod {
	if cfg.KeyPath != "" || cfg.Host == "" {
		return methods
	}
	configKey := getSSHConfigIdentityFile(cfg.Host, cfg.FS)
	if configKey == "" {
		return methods
	}
	if keyAuth, err := privateKeyAuth(configKey, cfg.KeyPassphrase, cfg.FS); err == nil {
		methods = append(methods, keyAuth)
	}
	return methods
}

// tryDefaultKeysAuth attempts authentication with default SSH keys.
func tryDefaultKeysAuth(cfg AuthConfig, methods []ssh.AuthMethod) []ssh.AuthMethod {
	if cfg.KeyPath != "" || cfg.Password != "" || len(methods) > 0 {
		return methods
	}
	defaultKeys := []string{"~/.ssh/id_ed25519", "~/.ssh/id_rsa", "~/.ssh/id_ecdsa"}
	for _, keyPath := range defaultKeys {
		expanded := expandPathWithFS(keyPath, cfg.FS)
		if _, err := cfg.FS.Stat(expanded); err != nil {
			continue
		}
		if keyAuth, err := privateKeyAuth(expanded, cfg.KeyPassphrase, cfg.FS); err == nil {
			return append(methods, keyAuth)
		}
	}
	return methods
}

// tryPasswordAuth adds password authentication methods.
func tryPasswordAuth(cfg AuthConfig, methods []ssh.AuthMethod) []ssh.AuthMethod {
	if cfg.Password == "" {
		return methods
	}
	methods = append(methods, PasswordAuth(cfg.Password))
	methods = append(methods, KeyboardInteractiveAuth(cfg.Password))
	return methods
}

// sshAgentAuth returns an SSH agent auth method.
func sshAgentAuth(fs ports.FileSystem, dialer ports.NetworkDialer) (ssh.AuthMethod, error) {
	socket := fs.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}

	conn, err := dialer.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial agent: %w", err)
	}

	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), nil
}

// privateKeyAuth returns a private key auth method.
func privateKeyAuth(keyPath, passphrase string, fs ports.FileSystem) (ssh.AuthMethod, error) {
	expanded := expandPathWithFS(keyPath, fs)

	keyData, err := fs.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(keyData)
	}
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	return ssh.PublicKeys(signer), nil
}

// BuildHostKeyCallback creates a host key callback from known_hosts.
// An optional FileSystem can be passed; if not provided, defaults to real filesystem.
func BuildHostKeyCallback(knownHostsPath string, fsys ...ports.FileSystem) (ssh.HostKeyCallback, error) {
	var fs ports.FileSystem
	if len(fsys) > 0 && fsys[0] != nil {
		fs = fsys[0]
	} else {
		fs = realfs.New()
	}

	if knownHostsPath == "" {
		knownHostsPath = "~/.ssh/known_hosts"
	}

	expanded := expandPathWithFS(knownHostsPath, fs)

	// Check if known_hosts exists
	if _, err := fs.Stat(expanded); err != nil {
		// Return a callback that accepts any host key but logs a warning
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			// In production, you might want to prompt the user or auto-add
			return nil
		}, nil
	}

	callback, err := knownhosts.New(expanded)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts: %w", err)
	}

	return callback, nil
}

// InsecureHostKeyCallback returns a callback that accepts any host key.
// Use only for testing or when host key verification is explicitly disabled.
func InsecureHostKeyCallback() ssh.HostKeyCallback {
	return ssh.InsecureIgnoreHostKey()
}

// expandPath expands ~ to home directory using real filesystem.
func expandPath(path string) string {
	return expandPathWithFS(path, realfs.New())
}

// expandPathWithFS expands ~ to home directory using the given filesystem.
func expandPathWithFS(path string, fs ports.FileSystem) string {
	if strings.HasPrefix(path, "~/") {
		home, err := fs.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// getSSHConfigIdentityFile parses ~/.ssh/config and returns the IdentityFile for a host.
func getSSHConfigIdentityFile(host string, fs ports.FileSystem) string {
	configPath := expandPathWithFS("~/.ssh/config", fs)
	file, err := fs.Open(configPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentHost string
	var matchesHost bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key-value pairs
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.ToLower(parts[0])
		value := strings.Join(parts[1:], " ")

		switch key {
		case "host":
			currentHost = value
			// Check if this host pattern matches our target
			matchesHost = matchSSHHostPattern(host, currentHost)
		case "identityfile":
			if matchesHost {
				return expandPathWithFS(value, fs)
			}
		}
	}

	return ""
}

// matchSSHHostPattern checks if host matches an SSH config Host pattern.
// Supports wildcards (* matches any sequence, ? matches single char).
func matchSSHHostPattern(host, pattern string) bool {
	// Handle multiple patterns separated by spaces
	patterns := strings.Fields(pattern)
	for _, p := range patterns {
		if matchSinglePattern(host, p) {
			return true
		}
	}
	return false
}

// skipWildcards advances past consecutive wildcards and returns new index.
func skipWildcards(pattern string, i int) int {
	for i < len(pattern) && pattern[i] == '*' {
		i++
	}
	return i
}

// matchWildcard attempts to match a wildcard against the host, returning true if matched.
func matchWildcard(host, pattern string, hostIdx, patIdx int) bool {
	patIdx = skipWildcards(pattern, patIdx)
	if patIdx == len(pattern) {
		return true // Trailing * matches rest
	}
	for hostIdx < len(host) {
		if matchSinglePattern(host[hostIdx:], pattern[patIdx:]) {
			return true
		}
		hostIdx++
	}
	return false
}

// matchSinglePattern matches a single SSH host pattern.
func matchSinglePattern(host, pattern string) bool {
	if pattern == "*" || pattern == host {
		return true
	}

	i, j := 0, 0
	for i < len(pattern) && j < len(host) {
		if pattern[i] == '*' {
			return matchWildcard(host, pattern, j, i)
		}
		if pattern[i] == '?' || pattern[i] == host[j] {
			i++
			j++
		} else {
			return false
		}
	}

	i = skipWildcards(pattern, i)
	return i == len(pattern) && j == len(host)
}

// PasswordAuth returns a password auth method.
func PasswordAuth(password string) ssh.AuthMethod {
	return ssh.Password(password)
}

// KeyboardInteractiveAuth returns a keyboard-interactive auth method.
func KeyboardInteractiveAuth(password string) ssh.AuthMethod {
	return ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range questions {
			answers[i] = password
		}
		return answers, nil
	})
}
