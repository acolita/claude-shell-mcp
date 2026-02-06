// Package fakesessionmgr provides a fake session manager for testing MCP handlers.
package fakesessionmgr

import (
	"fmt"
	"sync"

	"github.com/acolita/claude-shell-mcp/internal/session"
)

// defaultListDetailedFunc provides a simple ListDetailed using session fields directly.
func defaultListDetailedFunc(sessions map[string]*session.Session) []session.SessionInfo {
	var infos []session.SessionInfo
	for _, sess := range sessions {
		infos = append(infos, session.SessionInfo{
			ID:   sess.ID,
			Mode: sess.Mode,
			Host: sess.Host,
			User: sess.User,
		})
	}
	return infos
}

// Manager is a fake session manager that stores pre-configured sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*session.Session
	closed   map[string]bool
	nextID   int

	// Hooks for customizing behavior
	CreateFunc func(opts session.CreateOptions) (*session.Session, error)
}

// New creates a new fake Manager.
func New() *Manager {
	return &Manager{
		sessions: make(map[string]*session.Session),
		closed:   make(map[string]bool),
	}
}

// AddSession adds a pre-configured session to the manager.
func (m *Manager) AddSession(sess *session.Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sess.ID] = sess
}

// Create creates a new session. If CreateFunc is set, it delegates to it.
// Otherwise, returns an error (override for testing).
func (m *Manager) Create(opts session.CreateOptions) (*session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CreateFunc != nil {
		sess, err := m.CreateFunc(opts)
		if err != nil {
			return nil, err
		}
		m.sessions[sess.ID] = sess
		return sess, nil
	}

	return nil, fmt.Errorf("fakesessionmgr: Create not configured")
}

// Get returns a session by ID.
func (m *Manager) Get(id string) (*session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed[id] {
		return nil, fmt.Errorf("session not found: %s", id)
	}

	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return sess, nil
}

// Close closes a session by ID.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sessions[id]; !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	m.closed[id] = true
	delete(m.sessions, id)
	return nil
}

// ListDetailed returns info for all active sessions.
func (m *Manager) ListDetailed() []session.SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	return defaultListDetailedFunc(m.sessions)
}
