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
	sessions        map[string]*Session
	controlSessions map[string]*ControlSession // key: "local" or hostname
	mu              sync.RWMutex
	config          *config.Config
}

// NewManager creates a new session manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		sessions:        make(map[string]*Session),
		controlSessions: make(map[string]*ControlSession),
		config:          cfg,
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
		KeyPath:  opts.KeyPath,
		config:   m.config,
	}

	// Initialize the session (creates PTY/SSH connection)
	if err := sess.Initialize(); err != nil {
		return nil, fmt.Errorf("initialize session: %w", err)
	}

	// Get or create control session for this host (without locking again)
	cs, err := m.getOrCreateControlSessionLocked(opts)
	if err != nil {
		// Non-fatal: control session is optional for enhanced process management
		// The session can still work with fallback interrupt handling
	} else {
		sess.controlSession = cs
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
	KeyPath  string // Path to SSH private key file
}

// GetControlSession returns the control session for a host, creating it if needed.
// For local sessions, use host="local".
func (m *Manager) GetControlSession(opts CreateOptions) (*ControlSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getOrCreateControlSessionLocked(opts)
}

// getOrCreateControlSessionLocked returns or creates a control session.
// Caller must hold m.mu lock.
func (m *Manager) getOrCreateControlSessionLocked(opts CreateOptions) (*ControlSession, error) {
	host := opts.Host
	if opts.Mode == "local" || host == "" {
		host = "local"
	}

	// Return existing control session if available
	if cs, ok := m.controlSessions[host]; ok {
		return cs, nil
	}

	// Create new control session
	csOpts := ControlSessionOptions{
		Mode:     opts.Mode,
		Host:     opts.Host,
		Port:     opts.Port,
		User:     opts.User,
		Password: opts.Password,
		KeyPath:  opts.KeyPath,
	}

	cs, err := NewControlSession(csOpts)
	if err != nil {
		return nil, fmt.Errorf("create control session for %s: %w", host, err)
	}

	m.controlSessions[host] = cs
	return cs, nil
}

// CloseControlSession closes a control session for a specific host.
func (m *Manager) CloseControlSession(host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cs, ok := m.controlSessions[host]
	if !ok {
		return nil // Not an error if it doesn't exist
	}

	if err := cs.Close(); err != nil {
		return err
	}

	delete(m.controlSessions, host)
	return nil
}

// CloseAll closes all sessions and control sessions.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	// Close all regular sessions
	for id, sess := range m.sessions {
		if err := sess.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close session %s: %w", id, err))
		}
		delete(m.sessions, id)
	}

	// Close all control sessions
	for host, cs := range m.controlSessions {
		if err := cs.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close control session %s: %w", host, err))
		}
		delete(m.controlSessions, host)
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
