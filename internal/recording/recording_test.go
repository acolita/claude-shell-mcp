package recording

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realclock"
	"github.com/acolita/claude-shell-mcp/internal/adapters/realfs"
	"github.com/acolita/claude-shell-mcp/internal/ports"
)

func testFS() ports.FileSystem { return realfs.New() }
func testClock() ports.Clock   { return realclock.New() }

// ---------- Event tests ----------

func TestEventMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		expected string
	}{
		{
			name:     "output event",
			event:    Event{Time: 1.5, Type: "o", Data: "hello"},
			expected: `[1.5,"o","hello"]`,
		},
		{
			name:     "input event",
			event:    Event{Time: 0.0, Type: "i", Data: "ls\r\n"},
			expected: `[0,"i","ls\r\n"]`,
		},
		{
			name:     "zero time event",
			event:    Event{Time: 0, Type: "o", Data: ""},
			expected: `[0,"o",""]`,
		},
		{
			name:     "large timestamp",
			event:    Event{Time: 3723.456789, Type: "o", Data: "data"},
			expected: `[3723.456789,"o","data"]`,
		},
		{
			name:     "special characters in data",
			event:    Event{Time: 1.0, Type: "o", Data: "line1\nline2\ttab"},
			expected: `[1,"o","line1\nline2\ttab"]`,
		},
		{
			name:     "unicode data",
			event:    Event{Time: 0.5, Type: "o", Data: "Hello, 世界! 🌍"},
			expected: `[0.5,"o","Hello, 世界! 🌍"]`,
		},
		{
			name:     "json special chars in data",
			event:    Event{Time: 1.0, Type: "o", Data: `"quoted" and \backslash`},
			expected: `[1,"o","\"quoted\" and \\backslash"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.expected {
				t.Errorf("MarshalJSON() = %s, want %s", string(got), tt.expected)
			}
		})
	}
}

// ---------- Header tests ----------

func TestHeaderMarshalJSON(t *testing.T) {
	h := Header{
		Version:   2,
		Width:     120,
		Height:    40,
		Timestamp: 1700000000,
		Title:     "test session",
		Env: map[string]string{
			"SHELL": "/bin/bash",
			"TERM":  "xterm-256color",
		},
	}

	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("Marshal header: %v", err)
	}

	var parsed Header
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal header: %v", err)
	}

	if parsed.Version != 2 {
		t.Errorf("Version = %d, want 2", parsed.Version)
	}
	if parsed.Width != 120 {
		t.Errorf("Width = %d, want 120", parsed.Width)
	}
	if parsed.Height != 40 {
		t.Errorf("Height = %d, want 40", parsed.Height)
	}
	if parsed.Timestamp != 1700000000 {
		t.Errorf("Timestamp = %d, want 1700000000", parsed.Timestamp)
	}
	if parsed.Title != "test session" {
		t.Errorf("Title = %q, want %q", parsed.Title, "test session")
	}
	if parsed.Env["SHELL"] != "/bin/bash" {
		t.Errorf("Env[SHELL] = %q, want /bin/bash", parsed.Env["SHELL"])
	}
	if parsed.Env["TERM"] != "xterm-256color" {
		t.Errorf("Env[TERM] = %q, want xterm-256color", parsed.Env["TERM"])
	}
}

func TestHeaderMarshalJSON_OmitEmpty(t *testing.T) {
	h := Header{
		Version:   2,
		Width:     80,
		Height:    24,
		Timestamp: 1700000000,
	}

	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("Marshal header: %v", err)
	}

	str := string(data)
	if strings.Contains(str, "title") {
		t.Error("Empty title should be omitted from JSON")
	}
	if strings.Contains(str, "env") {
		t.Error("Nil env should be omitted from JSON")
	}
}

// ---------- NewRecorder tests ----------

func TestNewRecorder(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "recordings")

	r, err := NewRecorder(basePath, "sess_test123", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	defer r.Close()

	// Verify directory was created
	info, err := os.Stat(basePath)
	if err != nil {
		t.Fatalf("recording directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("basePath is not a directory")
	}

	// Verify file was created
	path := r.Path()
	if path == "" {
		t.Fatal("Path() returned empty string")
	}
	if !strings.HasPrefix(filepath.Base(path), "sess_test123_") {
		t.Errorf("filename %q does not start with session ID prefix", filepath.Base(path))
	}
	if !strings.HasSuffix(path, ".cast") {
		t.Errorf("filename %q does not end with .cast", path)
	}

	// Verify file exists and has content (the header)
	finfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("recording file not found: %v", err)
	}
	if finfo.Size() == 0 {
		t.Error("recording file is empty, expected header")
	}
}

func TestNewRecorder_WritesValidHeader(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_hdr", 132, 43, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	r.Close()

	// Read the file and verify the header
	data, err := os.ReadFile(r.Path())
	if err != nil {
		t.Fatalf("read recording file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		t.Fatal("recording file has no lines")
	}

	var header Header
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}

	if header.Version != 2 {
		t.Errorf("header.Version = %d, want 2", header.Version)
	}
	if header.Width != 132 {
		t.Errorf("header.Width = %d, want 132", header.Width)
	}
	if header.Height != 43 {
		t.Errorf("header.Height = %d, want 43", header.Height)
	}
	if header.Timestamp == 0 {
		t.Error("header.Timestamp should not be 0")
	}
	if header.Env["SHELL"] != "/bin/bash" {
		t.Errorf("header.Env[SHELL] = %q, want /bin/bash", header.Env["SHELL"])
	}
	if header.Env["TERM"] != "dumb" {
		t.Errorf("header.Env[TERM] = %q, want dumb", header.Env["TERM"])
	}
}

func TestNewRecorder_CreatesNestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "a", "b", "c", "recordings")

	r, err := NewRecorder(basePath, "sess_nested", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	defer r.Close()

	if _, err := os.Stat(basePath); err != nil {
		t.Fatalf("nested directory not created: %v", err)
	}
}

func TestNewRecorder_InvalidPath(t *testing.T) {
	// Use /dev/null as a parent, which is not a directory
	_, err := NewRecorder("/dev/null/recordings", "sess_bad", 80, 24, testFS(), testClock())
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

// ---------- RecordOutput / RecordInput tests ----------

func TestRecorderRecordOutput(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_out", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	if err := r.RecordOutput("hello world\n"); err != nil {
		t.Fatalf("RecordOutput() error = %v", err)
	}
	if err := r.RecordOutput("second line\n"); err != nil {
		t.Fatalf("RecordOutput() error = %v", err)
	}
	r.Close()

	events := readEvents(t, r.Path())
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	for _, ev := range events {
		if ev.Type != "o" {
			t.Errorf("event type = %q, want %q", ev.Type, "o")
		}
	}
	if events[0].Data != "hello world\n" {
		t.Errorf("first event data = %q, want %q", events[0].Data, "hello world\n")
	}
	if events[1].Data != "second line\n" {
		t.Errorf("second event data = %q, want %q", events[1].Data, "second line\n")
	}
}

func TestRecorderRecordInput(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_in", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	if err := r.RecordInput("ls -la\r\n"); err != nil {
		t.Fatalf("RecordInput() error = %v", err)
	}
	r.Close()

	events := readEvents(t, r.Path())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "i" {
		t.Errorf("event type = %q, want %q", events[0].Type, "i")
	}
	if events[0].Data != "ls -la\r\n" {
		t.Errorf("event data = %q, want %q", events[0].Data, "ls -la\r\n")
	}
}

func TestRecorderRecordMaskedInput(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_mask", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	if err := r.RecordMaskedInput(8); err != nil {
		t.Fatalf("RecordMaskedInput() error = %v", err)
	}
	r.Close()

	events := readEvents(t, r.Path())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "i" {
		t.Errorf("event type = %q, want %q", events[0].Type, "i")
	}
	if events[0].Data != "********" {
		t.Errorf("masked data = %q, want %q", events[0].Data, "********")
	}
}

func TestRecorderRecordMaskedInput_ZeroLength(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_mask0", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	if err := r.RecordMaskedInput(0); err != nil {
		t.Fatalf("RecordMaskedInput(0) error = %v", err)
	}
	r.Close()

	events := readEvents(t, r.Path())
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "" {
		t.Errorf("masked data = %q, want empty string", events[0].Data)
	}
}

func TestRecorderEventTimestamps(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_time", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	// Record events with a small delay between them
	if err := r.RecordOutput("first"); err != nil {
		t.Fatalf("RecordOutput() error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := r.RecordOutput("second"); err != nil {
		t.Fatalf("RecordOutput() error = %v", err)
	}
	r.Close()

	events := readEvents(t, r.Path())
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Timestamps should be non-negative and monotonically increasing
	if events[0].Time < 0 {
		t.Errorf("first event time %f should be >= 0", events[0].Time)
	}
	if events[1].Time <= events[0].Time {
		t.Errorf("second event time %f should be > first event time %f", events[1].Time, events[0].Time)
	}
}

// ---------- Recorder Close tests ----------

func TestRecorderClose_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_close", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	// First close should succeed
	if err := r.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	// Second close should return nil (idempotent)
	if err := r.Close(); err != nil {
		t.Errorf("second Close() error = %v, want nil", err)
	}
}

func TestRecorderRecordAfterClose(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_rac", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	r.Close()

	// Recording after close should silently succeed (no error, just no-op)
	if err := r.RecordOutput("should be ignored"); err != nil {
		t.Errorf("RecordOutput after Close should return nil, got %v", err)
	}
	if err := r.RecordInput("should be ignored"); err != nil {
		t.Errorf("RecordInput after Close should return nil, got %v", err)
	}
	if err := r.RecordMaskedInput(5); err != nil {
		t.Errorf("RecordMaskedInput after Close should return nil, got %v", err)
	}
}

// ---------- Recorder Path tests ----------

func TestRecorderPath(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_path", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	defer r.Close()

	path := r.Path()
	if path == "" {
		t.Error("Path() returned empty string")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("Path() returned non-absolute path: %q", path)
	}
}

func TestRecorderPath_NilFile(t *testing.T) {
	// Manually create a Recorder with nil file
	r := &Recorder{}
	if path := r.Path(); path != "" {
		t.Errorf("Path() with nil file = %q, want empty string", path)
	}
}

// ---------- Recorder concurrency test ----------

func TestRecorderConcurrentRecording(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_conc", 80, 24, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	const goroutines = 20
	const eventsPerGoroutine = 10
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				if id%2 == 0 {
					r.RecordOutput("output data")
				} else {
					r.RecordInput("input data")
				}
			}
		}(i)
	}

	wg.Wait()
	r.Close()

	events := readEvents(t, r.Path())
	expectedCount := goroutines * eventsPerGoroutine
	if len(events) != expectedCount {
		t.Errorf("expected %d events, got %d", expectedCount, len(events))
	}
}

// ---------- Full asciicast v2 file validation ----------

func TestRecorderAsciicastV2Format(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := NewRecorder(tmpDir, "sess_v2", 100, 50, testFS(), testClock())
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}

	r.RecordOutput("$ ")
	r.RecordInput("echo hello\r\n")
	r.RecordOutput("hello\r\n")
	r.RecordOutput("$ ")
	r.Close()

	data, err := os.ReadFile(r.Path())
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// 1 header + 4 events = 5 lines
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}

	// Validate header (line 0) is a JSON object
	var header map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header is not valid JSON: %v", err)
	}
	if header["version"].(float64) != 2 {
		t.Errorf("header version = %v, want 2", header["version"])
	}

	// Validate each event line (lines 1-4) is a JSON array [time, type, data]
	for i, line := range lines[1:] {
		var arr []interface{}
		if err := json.Unmarshal([]byte(line), &arr); err != nil {
			t.Fatalf("event line %d is not valid JSON: %v", i+1, err)
		}
		if len(arr) != 3 {
			t.Fatalf("event line %d has %d elements, want 3", i+1, len(arr))
		}
		// Time should be a number
		if _, ok := arr[0].(float64); !ok {
			t.Errorf("event %d time is not a number: %T", i+1, arr[0])
		}
		// Type should be a string
		eventType, ok := arr[1].(string)
		if !ok {
			t.Errorf("event %d type is not a string: %T", i+1, arr[1])
		}
		if eventType != "o" && eventType != "i" {
			t.Errorf("event %d type = %q, want 'o' or 'i'", i+1, eventType)
		}
		// Data should be a string
		if _, ok := arr[2].(string); !ok {
			t.Errorf("event %d data is not a string: %T", i+1, arr[2])
		}
	}
}

// ========== Manager tests ==========

func TestNewManager(t *testing.T) {
	m := NewManager("/tmp/test-recordings", true)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if !m.IsEnabled() {
		t.Error("expected manager to be enabled")
	}
	if m.basePath != "/tmp/test-recordings" {
		t.Errorf("basePath = %q, want /tmp/test-recordings", m.basePath)
	}
}

func TestNewManager_Disabled(t *testing.T) {
	m := NewManager("/tmp/test-recordings", false)
	if m.IsEnabled() {
		t.Error("expected manager to be disabled")
	}
}

func TestManagerIsEnabled(t *testing.T) {
	mEnabled := NewManager("", true)
	mDisabled := NewManager("", false)

	if !mEnabled.IsEnabled() {
		t.Error("expected enabled=true")
	}
	if mDisabled.IsEnabled() {
		t.Error("expected enabled=false")
	}
}

// ---------- Manager StartRecording tests ----------

func TestManagerStartRecording(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	err := m.StartRecording("sess_mgr1", 80, 24)
	if err != nil {
		t.Fatalf("StartRecording() error = %v", err)
	}

	path := m.GetRecordingPath("sess_mgr1")
	if path == "" {
		t.Error("GetRecordingPath() returned empty after StartRecording")
	}
	if !strings.HasSuffix(path, ".cast") {
		t.Errorf("recording path %q does not end in .cast", path)
	}
}

func TestManagerStartRecording_Disabled(t *testing.T) {
	m := NewManager("/nonexistent", false)

	// Should be a no-op when disabled
	err := m.StartRecording("sess_disabled", 80, 24)
	if err != nil {
		t.Fatalf("StartRecording() on disabled manager error = %v", err)
	}

	path := m.GetRecordingPath("sess_disabled")
	if path != "" {
		t.Errorf("GetRecordingPath() on disabled manager = %q, want empty", path)
	}
}

func TestManagerStartRecording_ReplacesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	// Start recording twice for the same session
	if err := m.StartRecording("sess_replace", 80, 24); err != nil {
		t.Fatalf("first StartRecording() error = %v", err)
	}
	firstPath := m.GetRecordingPath("sess_replace")

	// Small delay so the file name changes (includes timestamp)
	time.Sleep(1100 * time.Millisecond)

	if err := m.StartRecording("sess_replace", 100, 50); err != nil {
		t.Fatalf("second StartRecording() error = %v", err)
	}
	secondPath := m.GetRecordingPath("sess_replace")

	if firstPath == secondPath {
		t.Error("expected different recording paths after restart")
	}

	// The first file should exist (it was closed, not deleted)
	if _, err := os.Stat(firstPath); err != nil {
		t.Errorf("first recording file should still exist: %v", err)
	}
}

// ---------- Manager RecordOutput tests ----------

func TestManagerRecordOutput(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	m.StartRecording("sess_mout", 80, 24)
	m.RecordOutput("sess_mout", "hello from manager\n")
	m.StopRecording("sess_mout")

	path := filepath.Join(tmpDir, findCastFile(t, tmpDir, "sess_mout"))
	events := readEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "hello from manager\n" {
		t.Errorf("event data = %q, want %q", events[0].Data, "hello from manager\n")
	}
}

func TestManagerRecordOutput_Disabled(t *testing.T) {
	m := NewManager("/nonexistent", false)

	// Should not panic or error when disabled
	m.RecordOutput("sess_no", "data")
}

func TestManagerRecordOutput_UnknownSession(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	// Should not panic when recording to unknown session
	m.RecordOutput("nonexistent_session", "data")
}

// ---------- Manager RecordInput tests ----------

func TestManagerRecordInput(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	m.StartRecording("sess_min", 80, 24)
	m.RecordInput("sess_min", "command\r\n", false)
	m.StopRecording("sess_min")

	path := filepath.Join(tmpDir, findCastFile(t, tmpDir, "sess_min"))
	events := readEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "i" {
		t.Errorf("event type = %q, want 'i'", events[0].Type)
	}
	if events[0].Data != "command\r\n" {
		t.Errorf("event data = %q, want %q", events[0].Data, "command\r\n")
	}
}

func TestManagerRecordInput_Masked(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	m.StartRecording("sess_mmask", 80, 24)
	m.RecordInput("sess_mmask", "secret123", true)
	m.StopRecording("sess_mmask")

	path := filepath.Join(tmpDir, findCastFile(t, tmpDir, "sess_mmask"))
	events := readEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	// "secret123" is 9 chars, so masked should be "*********"
	if events[0].Data != "*********" {
		t.Errorf("masked data = %q, want %q", events[0].Data, "*********")
	}
	// Ensure the actual password is NOT in the file
	fileData, _ := os.ReadFile(path)
	if strings.Contains(string(fileData), "secret123") {
		t.Error("password leaked into recording file!")
	}
}

func TestManagerRecordInput_Disabled(t *testing.T) {
	m := NewManager("/nonexistent", false)

	// Should not panic or error when disabled
	m.RecordInput("sess_no", "data", false)
	m.RecordInput("sess_no", "secret", true)
}

func TestManagerRecordInput_UnknownSession(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	// Should not panic when recording to unknown session
	m.RecordInput("nonexistent_session", "data", false)
	m.RecordInput("nonexistent_session", "secret", true)
}

// ---------- Manager StopRecording tests ----------

func TestManagerStopRecording(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	m.StartRecording("sess_stop", 80, 24)
	path := m.GetRecordingPath("sess_stop")

	err := m.StopRecording("sess_stop")
	if err != nil {
		t.Fatalf("StopRecording() error = %v", err)
	}

	// After stopping, GetRecordingPath should return empty
	if got := m.GetRecordingPath("sess_stop"); got != "" {
		t.Errorf("GetRecordingPath after stop = %q, want empty", got)
	}

	// But the file should still exist
	if _, err := os.Stat(path); err != nil {
		t.Errorf("recording file should still exist after stop: %v", err)
	}
}

func TestManagerStopRecording_Disabled(t *testing.T) {
	m := NewManager("/nonexistent", false)

	err := m.StopRecording("sess_any")
	if err != nil {
		t.Errorf("StopRecording() on disabled manager error = %v", err)
	}
}

func TestManagerStopRecording_UnknownSession(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	err := m.StopRecording("nonexistent")
	if err != nil {
		t.Errorf("StopRecording() unknown session error = %v", err)
	}
}

// ---------- Manager GetRecordingPath tests ----------

func TestManagerGetRecordingPath_NoSession(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	path := m.GetRecordingPath("nonexistent")
	if path != "" {
		t.Errorf("GetRecordingPath() for nonexistent = %q, want empty", path)
	}
}

// ---------- Manager CloseAll tests ----------

func TestManagerCloseAll(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	// Start multiple recordings
	m.StartRecording("sess_a", 80, 24)
	m.StartRecording("sess_b", 80, 24)
	m.StartRecording("sess_c", 80, 24)

	pathA := m.GetRecordingPath("sess_a")
	pathB := m.GetRecordingPath("sess_b")
	pathC := m.GetRecordingPath("sess_c")

	m.CloseAll()

	// All paths should be gone from the manager
	if got := m.GetRecordingPath("sess_a"); got != "" {
		t.Errorf("sess_a still has path after CloseAll: %q", got)
	}
	if got := m.GetRecordingPath("sess_b"); got != "" {
		t.Errorf("sess_b still has path after CloseAll: %q", got)
	}
	if got := m.GetRecordingPath("sess_c"); got != "" {
		t.Errorf("sess_c still has path after CloseAll: %q", got)
	}

	// But files should still exist on disk
	for _, p := range []string{pathA, pathB, pathC} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file %q should still exist: %v", p, err)
		}
	}
}

func TestManagerCloseAll_Empty(t *testing.T) {
	m := NewManager(t.TempDir(), true)
	// Should not panic on empty manager
	m.CloseAll()
}

// ---------- Manager concurrent access ----------

func TestManagerConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	const sessions = 5
	const eventsPerSession = 20
	var wg sync.WaitGroup

	// Start all recordings
	for i := 0; i < sessions; i++ {
		sid := sessionID(i)
		if err := m.StartRecording(sid, 80, 24); err != nil {
			t.Fatalf("StartRecording(%s) error = %v", sid, err)
		}
	}

	// Concurrently record events
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sid := sessionID(id)
			for j := 0; j < eventsPerSession; j++ {
				m.RecordOutput(sid, "output data\n")
				m.RecordInput(sid, "input data\n", false)
			}
		}(i)
	}

	wg.Wait()

	// Stop all recordings
	for i := 0; i < sessions; i++ {
		sid := sessionID(i)
		if err := m.StopRecording(sid); err != nil {
			t.Errorf("StopRecording(%s) error = %v", sid, err)
		}
	}
}

// ---------- Full integration: Manager lifecycle ----------

func TestManagerFullLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(tmpDir, true)

	sid := "sess_lifecycle"

	// Start
	if err := m.StartRecording(sid, 120, 40); err != nil {
		t.Fatalf("StartRecording error = %v", err)
	}

	// Verify enabled and path exists
	if !m.IsEnabled() {
		t.Error("manager should be enabled")
	}
	path := m.GetRecordingPath(sid)
	if path == "" {
		t.Fatal("recording path should not be empty")
	}

	// Record a sequence of events
	m.RecordOutput(sid, "Welcome to server\r\n")
	m.RecordOutput(sid, "$ ")
	m.RecordInput(sid, "sudo apt update\r\n", false)
	m.RecordOutput(sid, "[sudo] password for user: ")
	m.RecordInput(sid, "mypassword", true) // masked
	m.RecordOutput(sid, "\r\nHit:1 http://archive.ubuntu.com/ubuntu focal InRelease\r\n")

	// Stop
	if err := m.StopRecording(sid); err != nil {
		t.Fatalf("StopRecording error = %v", err)
	}

	// Validate the file
	events := readEvents(t, path)
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d", len(events))
	}

	// Verify the password is masked
	passwordEvent := events[4] // 5th event, 0-indexed
	if strings.Contains(passwordEvent.Data, "mypassword") {
		t.Error("password should be masked in recording")
	}
	if passwordEvent.Data != "**********" {
		t.Errorf("masked password = %q, want %q", passwordEvent.Data, "**********")
	}

	// Verify timestamps are monotonically non-decreasing
	for i := 1; i < len(events); i++ {
		if events[i].Time < events[i-1].Time {
			t.Errorf("event %d time (%f) < event %d time (%f)",
				i, events[i].Time, i-1, events[i-1].Time)
		}
	}
}

// ---------- Helpers ----------

// eventFromJSON parses an asciicast v2 event array [time, type, data].
type parsedEvent struct {
	Time float64
	Type string
	Data string
}

// readEvents reads all event lines from an asciicast v2 file (skipping the header).
func readEvents(t *testing.T, path string) []parsedEvent {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open recording file: %v", err)
	}
	defer f.Close()

	var events []parsedEvent
	scanner := bufio.NewScanner(f)

	// Skip first line (header)
	if !scanner.Scan() {
		t.Fatal("recording file has no header line")
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var arr []interface{}
		if err := json.Unmarshal([]byte(line), &arr); err != nil {
			t.Fatalf("unmarshal event line %q: %v", line, err)
		}
		if len(arr) != 3 {
			t.Fatalf("event has %d elements, want 3", len(arr))
		}

		events = append(events, parsedEvent{
			Time: arr[0].(float64),
			Type: arr[1].(string),
			Data: arr[2].(string),
		})
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning recording file: %v", err)
	}

	return events
}

// findCastFile finds a .cast file in the given directory matching the session ID prefix.
func findCastFile(t *testing.T, dir, sessionPrefix string) string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), sessionPrefix+"_") && strings.HasSuffix(entry.Name(), ".cast") {
			return entry.Name()
		}
	}

	t.Fatalf("no .cast file found for session %q in %s", sessionPrefix, dir)
	return ""
}

// sessionID generates a session ID from an integer index.
func sessionID(i int) string {
	return "sess_concurrent_" + string(rune('A'+i))
}
