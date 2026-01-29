// Package session manages shell sessions for claude-shell-mcp.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/acolita/claude-shell-mcp/internal/config"
)

// Manager manages shell sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	config   *config.Config
}

// NewManager creates a new session manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		config:   cfg,
	}
}

// Create creates a new session and returns its ID.
func (m *Manager) Create(opts CreateOptions) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check session limit
	if len(m.sessions) >= m.config.Security.MaxSessionsPerUser {
		return nil, fmt.Errorf("max sessions reached (%d)", m.config.Security.MaxSessionsPerUser)
	}

	id := generateSessionID()
	sess := &Session{
		ID:       id,
		State:    StateIdle,
		Mode:     opts.Mode,
		Host:     opts.Host,
		Port:     opts.Port,
		User:     opts.User,
		Password: opts.Password,
		config:   m.config,
	}

	// Initialize the session (creates PTY/SSH connection)
	if err := sess.Initialize(); err != nil {
		return nil, fmt.Errorf("initialize session: %w", err)
	}

	m.sessions[id] = sess
	return sess, nil
}

// Get retrieves a session by ID.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return sess, nil
}

// Close closes and removes a session.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	if err := sess.Close(); err != nil {
		return err
	}

	delete(m.sessions, id)
	return nil
}

// List returns all active session IDs.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// SessionCount returns the number of active sessions.
func (m *Manager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// generateSessionID generates a unique session ID.
func generateSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "sess_" + hex.EncodeToString(b)
}

// CreateOptions defines options for creating a session.
type CreateOptions struct {
	Mode     string // "local" or "ssh"
	Host     string
	Port     int
	User     string
	Password string // For password-based SSH authentication
}
