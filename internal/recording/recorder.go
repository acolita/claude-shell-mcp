// Package recording provides session recording in asciicast v2 format.
package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// Recorder records terminal I/O in asciicast v2 format.
// See: https://docs.asciinema.org/manual/asciicast/v2/
type Recorder struct {
	mu        sync.Mutex
	file      ports.FileHandle
	startTime time.Time
	closed    bool
	clock     ports.Clock
}

// Header is the asciicast v2 header.
type Header struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Title     string            `json:"title,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// Event is an asciicast v2 event [time, type, data].
type Event struct {
	Time float64 `json:"-"`
	Type string  `json:"-"`
	Data string  `json:"-"`
}

// MarshalJSON implements custom JSON marshaling for Event.
func (e Event) MarshalJSON() ([]byte, error) {
	return json.Marshal([]interface{}{e.Time, e.Type, e.Data})
}

// NewRecorder creates a new recorder writing to the specified path.
func NewRecorder(basePath, sessionID string, width, height int, fs ports.FileSystem, clock ports.Clock) (*Recorder, error) {
	if err := fs.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("create recording directory: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.cast", sessionID, clock.Now().Format("20060102_150405"))
	fullPath := filepath.Join(basePath, filename)

	file, err := fs.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("create recording file: %w", err)
	}

	r := &Recorder{
		file:      file,
		startTime: clock.Now(),
		clock:     clock,
	}

	// Write header
	header := Header{
		Version:   2,
		Width:     width,
		Height:    height,
		Timestamp: r.startTime.Unix(),
		Env: map[string]string{
			"SHELL": "/bin/bash",
			"TERM":  "dumb",
		},
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("marshal header: %w", err)
	}

	if _, err := file.Write(append(headerJSON, '\n')); err != nil {
		file.Close()
		return nil, fmt.Errorf("write header: %w", err)
	}

	return r, nil
}

// RecordOutput records output data (terminal -> user).
func (r *Recorder) RecordOutput(data string) error {
	return r.record("o", data)
}

// RecordInput records input data (user -> terminal).
// Note: Use RecordMaskedInput for password inputs.
func (r *Recorder) RecordInput(data string) error {
	return r.record("i", data)
}

// RecordMaskedInput records input as masked (for passwords).
func (r *Recorder) RecordMaskedInput(length int) error {
	masked := ""
	for i := 0; i < length; i++ {
		masked += "*"
	}
	return r.record("i", masked)
}

// record writes an event to the recording file.
func (r *Recorder) record(eventType, data string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	elapsed := r.clock.Now().Sub(r.startTime).Seconds()
	event := Event{
		Time: elapsed,
		Type: eventType,
		Data: data,
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	if _, err := r.file.Write(append(eventJSON, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}

	return nil
}

// Close closes the recorder and flushes any buffered data.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	return r.file.Close()
}

// Path returns the path to the recording file.
func (r *Recorder) Path() string {
	if r.file == nil {
		return ""
	}
	return r.file.Name()
}
