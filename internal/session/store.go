// Package session provides session persistence for recovery after MCP restart.
package session

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// TunnelConfig contains the configuration needed to recreate a tunnel.
type TunnelConfig struct {
	Type       string `json:"type"`        // "local" or "reverse"
	LocalHost  string `json:"local_host"`
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
}

// SessionMetadata contains the information needed to recreate a session.
type SessionMetadata struct {
	ID      string         `json:"id"`
	Mode    string         `json:"mode"`
	Host    string         `json:"host,omitempty"`
	Port    int            `json:"port,omitempty"`
	User    string         `json:"user,omitempty"`
	KeyPath string         `json:"key_path,omitempty"`
	Cwd     string         `json:"cwd,omitempty"`
	Tunnels []TunnelConfig `json:"tunnels,omitempty"`
}

// SessionStore persists session metadata to enable recovery after MCP restart.
type SessionStore struct {
	path     string
	sessions map[string]SessionMetadata
	mu       sync.RWMutex
}

// NewSessionStore creates a session store at the default path.
func NewSessionStore() *SessionStore {
	// Use ~/.cache/claude-shell-mcp/sessions.json
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}

	cacheDir := filepath.Join(home, ".cache", "claude-shell-mcp")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		slog.Warn("failed to create cache dir, using /tmp", slog.String("error", err.Error()))
		cacheDir = "/tmp"
	}

	store := &SessionStore{
		path:     filepath.Join(cacheDir, "sessions.json"),
		sessions: make(map[string]SessionMetadata),
	}

	// Load existing sessions from disk
	store.load()

	return store
}

// Save persists a session's metadata for later recovery.
func (s *SessionStore) Save(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta := SessionMetadata{
		ID:      sess.ID,
		Mode:    sess.Mode,
		Host:    sess.Host,
		Port:    sess.Port,
		User:    sess.User,
		KeyPath: sess.KeyPath,
		Cwd:     sess.Cwd,
		Tunnels: sess.GetTunnelConfigs(),
	}

	s.sessions[sess.ID] = meta
	s.persist()
}

// Get retrieves session metadata by ID.
func (s *SessionStore) Get(id string) (SessionMetadata, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meta, ok := s.sessions[id]
	return meta, ok
}

// Delete removes session metadata.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, id)
	s.persist()
}

// load reads sessions from disk.
func (s *SessionStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to load session store", slog.String("error", err.Error()))
		}
		return
	}

	if err := json.Unmarshal(data, &s.sessions); err != nil {
		slog.Warn("failed to parse session store", slog.String("error", err.Error()))
		s.sessions = make(map[string]SessionMetadata)
	}
}

// persist writes sessions to disk.
func (s *SessionStore) persist() {
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal session store", slog.String("error", err.Error()))
		return
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		slog.Warn("failed to write session store", slog.String("error", err.Error()))
	}
}
