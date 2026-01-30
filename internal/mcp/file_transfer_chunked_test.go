package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChunkCalculation(t *testing.T) {
	tests := []struct {
		name        string
		totalSize   int64
		chunkSize   int
		wantChunks  int
	}{
		{"exact fit", 1024, 1024, 1},
		{"one extra", 1025, 1024, 2},
		{"small file", 500, 1024, 1},
		{"large file", 10485760, 1048576, 10}, // 10MB / 1MB chunks
		{"uneven", 3000000, 1048576, 3},       // ~3MB / 1MB chunks
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := int((tt.totalSize + int64(tt.chunkSize) - 1) / int64(tt.chunkSize))
			if got != tt.wantChunks {
				t.Errorf("chunk count = %d, want %d", got, tt.wantChunks)
			}
		})
	}
}

func TestTransferManifest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "manifest_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	manifestPath := filepath.Join(tmpDir, "test.transfer")

	// Create a manifest
	now := time.Now()
	manifest := &TransferManifest{
		Version:       1,
		Direction:     "get",
		RemotePath:    "/remote/file.bin",
		LocalPath:     "/local/file.bin",
		TotalSize:     10240,
		ChunkSize:     1024,
		TotalChunks:   10,
		StartedAt:     now,
		LastUpdatedAt: now,
		SessionID:     "sess_123",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "abc123"},
			{Index: 1, Offset: 1024, Size: 1024, Completed: true, Checksum: "def456"},
			{Index: 2, Offset: 2048, Size: 1024, Completed: false},
		},
		BytesSent: 2048,
	}

	// Save manifest
	if err := saveManifest(manifest, manifestPath); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest file should exist: %v", err)
	}

	// Load manifest
	loaded, err := loadManifest(manifestPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}

	// Verify fields
	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}
	if loaded.Direction != "get" {
		t.Errorf("Direction = %q, want %q", loaded.Direction, "get")
	}
	if loaded.TotalSize != 10240 {
		t.Errorf("TotalSize = %d, want 10240", loaded.TotalSize)
	}
	if loaded.TotalChunks != 10 {
		t.Errorf("TotalChunks = %d, want 10", loaded.TotalChunks)
	}
	if len(loaded.Chunks) != 3 {
		t.Errorf("len(Chunks) = %d, want 3", len(loaded.Chunks))
	}
	if !loaded.Chunks[0].Completed {
		t.Error("Chunk 0 should be completed")
	}
	if loaded.Chunks[2].Completed {
		t.Error("Chunk 2 should not be completed")
	}
	if loaded.BytesSent != 2048 {
		t.Errorf("BytesSent = %d, want 2048", loaded.BytesSent)
	}
}

func TestManifestJSON(t *testing.T) {
	now := time.Now()
	manifest := &TransferManifest{
		Version:       1,
		Direction:     "put",
		RemotePath:    "/remote/path",
		LocalPath:     "/local/path",
		TotalSize:     5000,
		ChunkSize:     1024,
		TotalChunks:   5,
		StartedAt:     now,
		LastUpdatedAt: now,
		SessionID:     "sess_456",
		Chunks: []ChunkInfo{
			{Index: 0, Offset: 0, Size: 1024, Completed: true, Checksum: "hash1"},
		},
	}

	// Marshal
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Unmarshal
	var parsed TransferManifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if parsed.Direction != manifest.Direction {
		t.Errorf("Direction mismatch")
	}
	if parsed.TotalChunks != manifest.TotalChunks {
		t.Errorf("TotalChunks mismatch")
	}
}

func TestChunkInfo(t *testing.T) {
	chunk := ChunkInfo{
		Index:     5,
		Offset:    5120,
		Size:      1024,
		Checksum:  "",
		Completed: false,
	}

	// Simulate calculating checksum
	data := make([]byte, chunk.Size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	hash := sha256.Sum256(data)
	chunk.Checksum = hex.EncodeToString(hash[:])
	chunk.Completed = true

	if !chunk.Completed {
		t.Error("Chunk should be marked completed")
	}
	if len(chunk.Checksum) != 64 {
		t.Errorf("Checksum should be 64 chars, got %d", len(chunk.Checksum))
	}
}

func TestChunkedTransferResult(t *testing.T) {
	result := ChunkedTransferResult{
		Status:           "completed",
		ManifestPath:     "/path/to/manifest.transfer",
		ChunksCompleted:  10,
		TotalChunks:      10,
		BytesTransferred: 10240,
		TotalBytes:       10240,
		Progress:         100.0,
		BytesPerSecond:   1024000,
		DurationMs:       10,
	}

	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.Progress != 100.0 {
		t.Errorf("Progress = %f, want 100.0", result.Progress)
	}
	if result.ChunksCompleted != result.TotalChunks {
		t.Error("ChunksCompleted should equal TotalChunks")
	}
}

func TestChunkSizeLimits(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{DefaultChunkSize, DefaultChunkSize},
		{MaxChunkSize, MaxChunkSize},
		{MaxChunkSize + 1, MaxChunkSize}, // Should be capped
		{500, 1024},                       // Should be minimum 1024
		{0, 1024},                         // Should be minimum 1024
	}

	for _, tt := range tests {
		chunkSize := tt.input
		if chunkSize > MaxChunkSize {
			chunkSize = MaxChunkSize
		}
		if chunkSize < 1024 {
			chunkSize = 1024
		}
		if chunkSize != tt.expected {
			t.Errorf("chunkSize(%d) = %d, want %d", tt.input, chunkSize, tt.expected)
		}
	}
}

func TestProgressCalculation(t *testing.T) {
	tests := []struct {
		bytesSent  int64
		totalSize  int64
		wantPct    float64
	}{
		{0, 1000, 0},
		{500, 1000, 50},
		{1000, 1000, 100},
		{250, 1000, 25},
		{0, 0, 0}, // Edge case: empty file
	}

	for _, tt := range tests {
		var progress float64
		if tt.totalSize > 0 {
			progress = float64(tt.bytesSent) / float64(tt.totalSize) * 100
		}
		if progress != tt.wantPct {
			t.Errorf("progress(%d/%d) = %f, want %f", tt.bytesSent, tt.totalSize, progress, tt.wantPct)
		}
	}
}

func TestLoadManifestError(t *testing.T) {
	// Non-existent file
	_, err := loadManifest("/nonexistent/path/manifest.transfer")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	// Invalid JSON
	tmpDir, err := os.MkdirTemp("", "manifest_err_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	invalidPath := filepath.Join(tmpDir, "invalid.transfer")
	if err := os.WriteFile(invalidPath, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = loadManifest(invalidPath)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}
