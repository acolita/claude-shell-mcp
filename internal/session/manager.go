// Package session manages shell sessions for claude-shell-mcp.
package session

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realclock"
	"github.com/acolita/claude-shell-mcp/internal/adapters/realrand"
	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// Manager manages shell sessions.
type Manager struct {
	sessions        map[string]*Session
	controlSessions map[string]*ControlSession // key: "local" or hostname
	store           *SessionStore              // persists session metadata for recovery
	mu              sync.RWMutex
	config          *config.Config
	clock           ports.Clock
	random          ports.Random
}

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithManagerClock sets the clock used by Manager.
func WithManagerClock(clock ports.Clock) ManagerOption {
	return func(m *Manager) {
		m.clock = clock
	}
}

// WithManagerRandom sets the random source used by Manager.
func WithManagerRandom(random ports.Random) ManagerOption {
	return func(m *Manager) {
		m.random = random
	}
}

// WithManagerStore sets the session store used by Manager.
func WithManagerStore(store *SessionStore) ManagerOption {
	return func(m *Manager) {
		m.store = store
	}
}

// NewManager creates a new session manager.
func NewManager(cfg *config.Config, opts ...ManagerOption) *Manager {
	m := &Manager{
		sessions:        make(map[string]*Session),
		controlSessions: make(map[string]*ControlSession),
		config:          cfg,
		clock:           realclock.New(),
		random:          realrand.New(),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Create store after options are applied (so we can inject a fake store)
	if m.store == nil {
		m.store = NewSessionStore()
	}

	return m
}

// Create creates a new session and returns its ID.
func (m *Manager) Create(opts CreateOptions) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check session limit
	if len(m.sessions) >= m.config.Security.MaxSessionsPerUser {
		return nil, fmt.Errorf("max sessions reached (%d)", m.config.Security.MaxSessionsPerUser)
	}

	id := m.generateSessionID()
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
		clock:    m.clock,
		random:   m.random,
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

	// Persist session metadata for recovery after MCP restart
	m.store.Save(sess)

	return sess, nil
}

// Get retrieves a session by ID.
// If the session doesn't exist but we have stored metadata (e.g., after MCP restart),
// it attempts to automatically recover the session.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()

	if ok {
		return sess, nil
	}

	// Session not in memory - try to recover from stored metadata
	return m.recover(id)
}

// recover attempts to recreate a session from stored metadata.
func (m *Manager) recover(id string) (*Session, error) {
	meta, ok := m.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check in case another goroutine recovered it
	if sess, ok := m.sessions[id]; ok {
		return sess, nil
	}

	// Recreate the session with stored metadata
	sess := &Session{
		ID:           id, // Use the same ID!
		State:        StateIdle,
		Mode:         meta.Mode,
		Host:         meta.Host,
		Port:         meta.Port,
		User:         meta.User,
		KeyPath:      meta.KeyPath,
		Cwd:          meta.Cwd,
		SavedTunnels: meta.Tunnels, // Saved tunnels for user to restore
		config:       m.config,
		clock:        m.clock,
		random:       m.random,
	}

	// Initialize the session (creates PTY/SSH connection)
	if err := sess.Initialize(); err != nil {
		// Failed to recover - remove stale metadata
		m.store.Delete(id)
		return nil, fmt.Errorf("failed to recover session %s: %w", id, err)
	}

	// Get or create control session
	opts := CreateOptions{
		Mode:    meta.Mode,
		Host:    meta.Host,
		Port:    meta.Port,
		User:    meta.User,
		KeyPath: meta.KeyPath,
	}
	if cs, err := m.getOrCreateControlSessionLocked(opts); err == nil {
		sess.controlSession = cs
	}

	m.sessions[id] = sess

	// Update stored metadata (cwd may have changed)
	m.store.Save(sess)

	return sess, nil
}

// Close closes and removes a session.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[id]
	if !ok {
		// Session not in memory - also clean up any stale metadata
		m.store.Delete(id)
		return fmt.Errorf("session not found: %s", id)
	}

	if err := sess.Close(); err != nil {
		return err
	}

	delete(m.sessions, id)

	// Remove persisted metadata
	m.store.Delete(id)

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

// SessionInfo contains summary information about a session.
type SessionInfo struct {
	ID        string `json:"session_id"`
	Mode      string `json:"mode"`
	Host      string `json:"host,omitempty"`
	User      string `json:"user,omitempty"`
	State     string `json:"state"`
	Cwd       string `json:"cwd,omitempty"`
	CreatedAt string `json:"created_at"`
	LastUsed  string `json:"last_used"`
	IdleFor   string `json:"idle_for"`
}

// ListDetailed returns detailed information about all active sessions.
func (m *Manager) ListDetailed() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(m.sessions))
	now := m.clock.Now()

	for _, sess := range m.sessions {
		info := SessionInfo{
			ID:        sess.ID,
			Mode:      sess.Mode,
			Host:      sess.Host,
			User:      sess.User,
			State:     string(sess.State),
			Cwd:       sess.Cwd,
			CreatedAt: sess.CreatedAt.Format(time.RFC3339),
			LastUsed:  sess.LastUsed.Format(time.RFC3339),
			IdleFor:   now.Sub(sess.LastUsed).Round(time.Second).String(),
		}
		infos = append(infos, info)
	}
	return infos
}

// SessionCount returns the number of active sessions.
func (m *Manager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// generateSessionID generates a unique session ID.
func (m *Manager) generateSessionID() string {
	b := make([]byte, 8)
	m.random.Read(b)
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
