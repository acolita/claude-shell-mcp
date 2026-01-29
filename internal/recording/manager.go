// Package recording provides session recording in asciicast v2 format.
package recording

import (
	"sync"
)

// Manager manages recorders for multiple sessions.
type Manager struct {
	mu        sync.RWMutex
	recorders map[string]*Recorder
	basePath  string
	enabled   bool
}

// NewManager creates a new recording manager.
func NewManager(basePath string, enabled bool) *Manager {
	return &Manager{
		recorders: make(map[string]*Recorder),
		basePath:  basePath,
		enabled:   enabled,
	}
}

// StartRecording starts recording for a session.
func (m *Manager) StartRecording(sessionID string, width, height int) error {
	if !m.enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Close existing recorder if any
	if existing, ok := m.recorders[sessionID]; ok {
		existing.Close()
	}

	recorder, err := NewRecorder(m.basePath, sessionID, width, height)
	if err != nil {
		return err
	}

	m.recorders[sessionID] = recorder
	return nil
}

// RecordOutput records output for a session.
func (m *Manager) RecordOutput(sessionID, data string) {
	if !m.enabled {
		return
	}

	m.mu.RLock()
	recorder, ok := m.recorders[sessionID]
	m.mu.RUnlock()

	if ok {
		recorder.RecordOutput(data)
	}
}

// RecordInput records input for a session.
func (m *Manager) RecordInput(sessionID, data string, masked bool) {
	if !m.enabled {
		return
	}

	m.mu.RLock()
	recorder, ok := m.recorders[sessionID]
	m.mu.RUnlock()

	if ok {
		if masked {
			recorder.RecordMaskedInput(len(data))
		} else {
			recorder.RecordInput(data)
		}
	}
}

// StopRecording stops recording for a session.
func (m *Manager) StopRecording(sessionID string) error {
	if !m.enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if recorder, ok := m.recorders[sessionID]; ok {
		err := recorder.Close()
		delete(m.recorders, sessionID)
		return err
	}
	return nil
}

// GetRecordingPath returns the path of the recording file for a session.
func (m *Manager) GetRecordingPath(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if recorder, ok := m.recorders[sessionID]; ok {
		return recorder.Path()
	}
	return ""
}

// CloseAll closes all recorders.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, recorder := range m.recorders {
		recorder.Close()
		delete(m.recorders, id)
	}
}

// IsEnabled returns whether recording is enabled.
func (m *Manager) IsEnabled() bool {
	return m.enabled
}
