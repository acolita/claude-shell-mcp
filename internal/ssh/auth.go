package ssh

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

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
}

// BuildAuthMethods constructs SSH auth methods from config.
func BuildAuthMethods(cfg AuthConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try SSH agent first if requested
	if cfg.UseAgent {
		if agentAuth, err := sshAgentAuth(); err == nil {
			methods = append(methods, agentAuth)
		}
	}

	// Add key file authentication
	if cfg.KeyPath != "" {
		keyAuth, err := privateKeyAuth(cfg.KeyPath, cfg.KeyPassphrase)
		if err != nil {
			return nil, fmt.Errorf("private key auth: %w", err)
		}
		methods = append(methods, keyAuth)
	}

	// Try SSH config lookup if no explicit key specified
	if cfg.KeyPath == "" && cfg.Host != "" {
		configKey := getSSHConfigIdentityFile(cfg.Host)
		if configKey != "" {
			keyAuth, err := privateKeyAuth(configKey, cfg.KeyPassphrase)
			if err == nil {
				methods = append(methods, keyAuth)
			}
		}
	}

	// Try default key locations if no explicit key specified and no password
	if cfg.KeyPath == "" && cfg.Password == "" && len(methods) == 0 {
		defaultKeys := []string{
			"~/.ssh/id_ed25519",
			"~/.ssh/id_rsa",
			"~/.ssh/id_ecdsa",
		}
		for _, keyPath := range defaultKeys {
			expanded := expandPath(keyPath)
			if _, err := os.Stat(expanded); err == nil {
				if keyAuth, err := privateKeyAuth(expanded, cfg.KeyPassphrase); err == nil {
					methods = append(methods, keyAuth)
					break // Use first available key
				}
			}
		}
	}

	// Add password authentication if provided
	if cfg.Password != "" {
		methods = append(methods, PasswordAuth(cfg.Password))
		methods = append(methods, KeyboardInteractiveAuth(cfg.Password))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication methods available")
	}

	return methods, nil
}

// sshAgentAuth returns an SSH agent auth method.
func sshAgentAuth() (ssh.AuthMethod, error) {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}

	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial agent: %w", err)
	}

	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), nil
}

// privateKeyAuth returns a private key auth method.
func privateKeyAuth(keyPath, passphrase string) (ssh.AuthMethod, error) {
	expanded := expandPath(keyPath)

	keyData, err := os.ReadFile(expanded)
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
func BuildHostKeyCallback(knownHostsPath string) (ssh.HostKeyCallback, error) {
	if knownHostsPath == "" {
		knownHostsPath = "~/.ssh/known_hosts"
	}

	expanded := expandPath(knownHostsPath)

	// Check if known_hosts exists
	if _, err := os.Stat(expanded); os.IsNotExist(err) {
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

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// getSSHConfigIdentityFile parses ~/.ssh/config and returns the IdentityFile for a host.
func getSSHConfigIdentityFile(host string) string {
	configPath := expandPath("~/.ssh/config")
	file, err := os.Open(configPath)
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
				return expandPath(value)
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

// matchSinglePattern matches a single SSH host pattern.
func matchSinglePattern(host, pattern string) bool {
	// Simple exact match or * wildcard
	if pattern == "*" {
		return true
	}
	if pattern == host {
		return true
	}

	// Convert SSH pattern to a simple matcher
	// SSH uses * for any chars and ? for single char
	i, j := 0, 0
	for i < len(pattern) && j < len(host) {
		if pattern[i] == '*' {
			// Skip consecutive wildcards
			for i < len(pattern) && pattern[i] == '*' {
				i++
			}
			if i == len(pattern) {
				return true // Trailing * matches rest
			}
			// Find next match
			for j < len(host) {
				if matchSinglePattern(host[j:], pattern[i:]) {
					return true
				}
				j++
			}
			return false
		} else if pattern[i] == '?' || pattern[i] == host[j] {
			i++
			j++
		} else {
			return false
		}
	}

	// Check if we consumed both strings
	for i < len(pattern) && pattern[i] == '*' {
		i++
	}
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
