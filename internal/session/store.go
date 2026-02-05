// Package session provides session persistence for recovery after MCP restart.
package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realfs"
	"github.com/acolita/claude-shell-mcp/internal/ports"
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
	fs       ports.FileSystem
}

// SessionStoreOption configures a SessionStore.
type SessionStoreOption func(*SessionStore)

// WithFileSystem sets the filesystem used by SessionStore.
func WithFileSystem(fs ports.FileSystem) SessionStoreOption {
	return func(s *SessionStore) {
		s.fs = fs
	}
}

// WithStorePath sets a custom storage path (for testing).
func WithStorePath(path string) SessionStoreOption {
	return func(s *SessionStore) {
		s.path = path
	}
}

// NewSessionStore creates a session store at the default path.
func NewSessionStore(opts ...SessionStoreOption) *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]SessionMetadata),
		fs:       realfs.New(), // default to real filesystem
	}

	// Apply options first so we can use the configured filesystem
	for _, opt := range opts {
		opt(store)
	}

	// If no custom path was set, determine the default path
	if store.path == "" {
		store.path = store.defaultPath()
	}

	// Load existing sessions from disk
	store.load()

	return store
}

// defaultPath determines the default storage path using the configured filesystem.
func (s *SessionStore) defaultPath() string {
	home, err := s.fs.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}

	cacheDir := filepath.Join(home, ".cache", "claude-shell-mcp")
	if err := s.fs.MkdirAll(cacheDir, 0700); err != nil {
		slog.Warn("failed to create cache dir, using /tmp", slog.String("error", err.Error()))
		cacheDir = "/tmp"
	}

	return filepath.Join(cacheDir, "sessions.json")
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
	data, err := s.fs.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
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

	if err := s.fs.WriteFile(s.path, data, 0600); err != nil {
		slog.Warn("failed to write session store", slog.String("error", err.Error()))
	}
}
