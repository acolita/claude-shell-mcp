package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// --- validateExecParams ---

func TestHelperValidateExecParams(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		command   string
		tailLines int
		headLines int
		wantErr   string
	}{
		{
			name:      "valid params",
			sessionID: "sess_123",
			command:   "ls -la",
			wantErr:   "",
		},
		{
			name:    "missing session_id",
			command: "ls",
			wantErr: "session_id is required",
		},
		{
			name:      "missing command",
			sessionID: "sess_123",
			wantErr:   "command is required",
		},
		{
			name:      "both tail and head set",
			sessionID: "sess_123",
			command:   "cat file",
			tailLines: 10,
			headLines: 5,
			wantErr:   "cannot use both tail_lines and head_lines",
		},
		{
			name:      "heredoc <<EOF",
			sessionID: "sess_123",
			command:   "cat <<EOF\nhello\nEOF",
			wantErr:   "Heredocs",
		},
		{
			name:      "heredoc <<'EOF'",
			sessionID: "sess_123",
			command:   "cat <<'EOF'\nhello\nEOF",
			wantErr:   "Heredocs",
		},
		{
			name:      `heredoc <<"EOF"`,
			sessionID: "sess_123",
			command:   `cat <<"EOF"` + "\nhello\nEOF",
			wantErr:   "Heredocs",
		},
		{
			name:      "heredoc <<-EOF",
			sessionID: "sess_123",
			command:   "cat <<-EOF\nhello\nEOF",
			wantErr:   "Heredocs",
		},
		{
			name:      "tail_lines only is valid",
			sessionID: "sess_123",
			command:   "cat bigfile",
			tailLines: 50,
			wantErr:   "",
		},
		{
			name:      "head_lines only is valid",
			sessionID: "sess_123",
			command:   "cat bigfile",
			headLines: 50,
			wantErr:   "",
		},
		{
			name:      "command with << followed by word is detected as heredoc",
			sessionID: "sess_123",
			command:   "echo 'use << for bitshift'",
			wantErr:   "Heredocs", // regex matches << followed by any word
		},
		{
			name:      "heredoc with spaces",
			sessionID: "sess_123",
			command:   "cat << MARKER\nhello\nMARKER",
			wantErr:   "Heredocs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateExecParams(tt.sessionID, tt.command, tt.tailLines, tt.headLines)
			if tt.wantErr == "" {
				if result != nil {
					t.Errorf("expected nil, got error result")
				}
			} else {
				if result == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !result.IsError {
					t.Errorf("expected IsError=true")
				}
				// Extract text content from the result
				resultJSON, _ := json.Marshal(result)
				if !strings.Contains(string(resultJSON), tt.wantErr) {
					t.Errorf("error result should contain %q, got %s", tt.wantErr, string(resultJSON))
				}
			}
		})
	}
}

// --- heredocPattern ---

func TestHelperHeredocPattern(t *testing.T) {
	tests := []struct {
		input string
		match bool
	}{
		{"<<EOF", true},
		{"<<'EOF'", true},
		{`<<"EOF"`, true},
		{"<<-EOF", true},
		{"<< EOF", true},
		{"<<MARKER", true},
		{"<<-  INDENT", true},
		{"echo hello", false},
		{"cat < file.txt", false},
		{"python script.py", false},
		{"x << 2", true}, // \w+ matches digits too, so this matches
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := heredocPattern.MatchString(tt.input)
			if got != tt.match {
				t.Errorf("heredocPattern.MatchString(%q) = %v, want %v", tt.input, got, tt.match)
			}
		})
	}
}

// --- jsonResult ---

func TestHelperJsonResult(t *testing.T) {
	t.Run("valid map", func(t *testing.T) {
		result, err := jsonResult(map[string]any{
			"status": "ok",
			"count":  42,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Error("expected non-error result")
		}
		if len(result.Content) == 0 {
			t.Fatal("expected non-empty content")
		}
		// Extract text from the first content element
		tc, ok := mcpgo.AsTextContent(result.Content[0])
		if !ok {
			t.Fatal("expected TextContent in result")
		}
		if !strings.Contains(tc.Text, `"status"`) {
			t.Errorf("result text should contain 'status' field, got: %s", tc.Text)
		}
		if !strings.Contains(tc.Text, `"ok"`) {
			t.Errorf("result text should contain 'ok' value, got: %s", tc.Text)
		}
		// Verify it's valid JSON
		var parsed map[string]any
		if jsonErr := json.Unmarshal([]byte(tc.Text), &parsed); jsonErr != nil {
			t.Errorf("result content is not valid JSON: %v", jsonErr)
		}
	})

	t.Run("valid struct", func(t *testing.T) {
		input := FileGetResult{
			Status:     "completed",
			RemotePath: "/test/file.txt",
			Size:       1024,
		}
		result, err := jsonResult(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Error("expected non-error result")
		}
	})

	t.Run("unmarshalable value returns error result", func(t *testing.T) {
		// A channel cannot be marshaled to JSON
		ch := make(chan int)
		result, err := jsonResult(ch)
		if err != nil {
			t.Fatalf("unexpected Go error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result for unmarshalable value")
		}
	})
}

// --- parseFilePutMode ---

func TestHelperParseFilePutMode(t *testing.T) {
	tests := []struct {
		name     string
		modeStr  string
		wantMode os.FileMode
		wantErr  bool
	}{
		{
			name:     "empty string (no-op)",
			modeStr:  "",
			wantMode: 0,
			wantErr:  false,
		},
		{
			name:     "standard 0644",
			modeStr:  "0644",
			wantMode: 0644,
		},
		{
			name:     "executable 0755",
			modeStr:  "0755",
			wantMode: 0755,
		},
		{
			name:     "restrictive 0600",
			modeStr:  "0600",
			wantMode: 0600,
		},
		{
			name:    "invalid octal",
			modeStr: "not-octal",
			wantErr: true,
		},
		{
			name:    "invalid with special chars",
			modeStr: "rwxr-xr-x",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &FilePutOptions{}
			errResult := parseFilePutMode(tt.modeStr, opts)

			if tt.wantErr {
				if errResult == nil {
					t.Error("expected error result, got nil")
				} else if !errResult.IsError {
					t.Error("expected IsError=true")
				}
				return
			}

			if errResult != nil {
				t.Errorf("unexpected error: result is error")
				return
			}

			if tt.modeStr != "" && opts.Mode != tt.wantMode {
				t.Errorf("Mode = %04o, want %04o", opts.Mode, tt.wantMode)
			}
		})
	}
}

// --- validateFilePutInputs ---

func TestHelperValidateFilePutInputs(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		remotePath string
		opts       FilePutOptions
		wantErr    string
	}{
		{
			name:       "valid with content",
			sessionID:  "sess_123",
			remotePath: "/remote/file.txt",
			opts:       FilePutOptions{Content: "hello"},
			wantErr:    "",
		},
		{
			name:       "valid with local_path",
			sessionID:  "sess_123",
			remotePath: "/remote/file.txt",
			opts:       FilePutOptions{LocalPath: "/local/file.txt"},
			wantErr:    "",
		},
		{
			name:       "missing session_id",
			remotePath: "/remote/file.txt",
			opts:       FilePutOptions{Content: "hello"},
			wantErr:    "session_id is required",
		},
		{
			name:      "missing remote_path",
			sessionID: "sess_123",
			opts:      FilePutOptions{Content: "hello"},
			wantErr:   "remote_path is required",
		},
		{
			name:       "no content and no local_path",
			sessionID:  "sess_123",
			remotePath: "/remote/file.txt",
			opts:       FilePutOptions{},
			wantErr:    "either content or local_path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateFilePutInputs(tt.sessionID, tt.remotePath, tt.opts)
			if tt.wantErr == "" {
				if result != nil {
					t.Errorf("expected nil, got error result")
				}
			} else {
				if result == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !result.IsError {
					t.Error("expected IsError=true")
				}
			}
		})
	}
}

// --- processFileChecksum ---

func TestHelperProcessFileChecksum(t *testing.T) {
	data := []byte("hello world")
	expectedHash := sha256.Sum256(data)
	expectedChecksum := hex.EncodeToString(expectedHash[:])

	t.Run("checksum disabled", func(t *testing.T) {
		result := &FileGetResult{}
		errResult := processFileChecksum(data, FileGetOptions{Checksum: false}, result)
		if errResult != nil {
			t.Error("expected nil error result when checksum disabled")
		}
		if result.Checksum != "" {
			t.Error("checksum should not be set when disabled")
		}
	})

	t.Run("checksum enabled", func(t *testing.T) {
		result := &FileGetResult{}
		errResult := processFileChecksum(data, FileGetOptions{Checksum: true}, result)
		if errResult != nil {
			t.Error("unexpected error result")
		}
		if result.Checksum != expectedChecksum {
			t.Errorf("checksum = %q, want %q", result.Checksum, expectedChecksum)
		}
	})

	t.Run("checksum verification passes", func(t *testing.T) {
		result := &FileGetResult{}
		errResult := processFileChecksum(data, FileGetOptions{
			Checksum:         true,
			ExpectedChecksum: expectedChecksum,
		}, result)
		if errResult != nil {
			t.Error("expected nil error result for matching checksum")
		}
		if !result.ChecksumVerified {
			t.Error("ChecksumVerified should be true")
		}
	})

	t.Run("checksum verification fails", func(t *testing.T) {
		result := &FileGetResult{}
		errResult := processFileChecksum(data, FileGetOptions{
			Checksum:         true,
			ExpectedChecksum: "0000000000000000000000000000000000000000000000000000000000000000",
		}, result)
		if errResult == nil {
			t.Fatal("expected error result for mismatched checksum")
		}
		if !errResult.IsError {
			t.Error("expected IsError=true")
		}
	})

	t.Run("case insensitive checksum comparison", func(t *testing.T) {
		result := &FileGetResult{}
		errResult := processFileChecksum(data, FileGetOptions{
			Checksum:         true,
			ExpectedChecksum: strings.ToUpper(expectedChecksum),
		}, result)
		if errResult != nil {
			t.Error("expected nil error result for case-insensitive matching checksum")
		}
		if !result.ChecksumVerified {
			t.Error("ChecksumVerified should be true")
		}
	})
}

// --- setContentWithEncoding ---

func TestHelperSetContentWithEncoding(t *testing.T) {
	t.Run("text encoding", func(t *testing.T) {
		result := &FileGetResult{}
		data := []byte("hello world")
		setContentWithEncoding(data, "file.txt", FileGetOptions{Encoding: "text"}, result)
		if result.Encoding != "text" {
			t.Errorf("Encoding = %q, want 'text'", result.Encoding)
		}
		if result.Content != "hello world" {
			t.Errorf("Content = %q, want 'hello world'", result.Content)
		}
		if result.ContentSize != len(data) {
			t.Errorf("ContentSize = %d, want %d", result.ContentSize, len(data))
		}
	})

	t.Run("base64 encoding", func(t *testing.T) {
		result := &FileGetResult{}
		data := []byte{0x00, 0x01, 0x02, 0xFF}
		setContentWithEncoding(data, "file.bin", FileGetOptions{Encoding: "base64"}, result)
		if result.Encoding != "base64" {
			t.Errorf("Encoding = %q, want 'base64'", result.Encoding)
		}
		expected := base64.StdEncoding.EncodeToString(data)
		if result.Content != expected {
			t.Errorf("Content = %q, want %q", result.Content, expected)
		}
	})

	t.Run("compress compressible file", func(t *testing.T) {
		result := &FileGetResult{}
		// Highly compressible data
		data := []byte(strings.Repeat("AAAAAAAAAA", 1000))
		setContentWithEncoding(data, "file.txt", FileGetOptions{
			Encoding: "text",
			Compress: true,
		}, result)
		// When compressed, encoding should be base64
		if !result.Compressed {
			t.Error("expected Compressed=true for compressible text data")
		}
		if result.Encoding != "base64" {
			t.Errorf("Encoding = %q, want 'base64' when compressed", result.Encoding)
		}
		if result.CompressionRatio <= 0 || result.CompressionRatio >= 1 {
			t.Errorf("CompressionRatio = %f, expected between 0 and 1", result.CompressionRatio)
		}
	})

	t.Run("compress non-compressible file type", func(t *testing.T) {
		result := &FileGetResult{}
		data := []byte("some data")
		// .png is not in the compressible list
		setContentWithEncoding(data, "image.png", FileGetOptions{
			Encoding: "text",
			Compress: true,
		}, result)
		if result.Compressed {
			t.Error("expected Compressed=false for non-compressible file type")
		}
		if result.Encoding != "text" {
			t.Errorf("Encoding = %q, want 'text' for non-compressed", result.Encoding)
		}
	})
}

// --- newFilePutResult ---

func TestHelperNewFilePutResult(t *testing.T) {
	data := []byte("test content")
	result := newFilePutResult("/remote/file.txt", data, 0644)

	if result.Status != "completed" {
		t.Errorf("Status = %q, want 'completed'", result.Status)
	}
	if result.RemotePath != "/remote/file.txt" {
		t.Errorf("RemotePath = %q, want '/remote/file.txt'", result.RemotePath)
	}
	if result.Size != int64(len(data)) {
		t.Errorf("Size = %d, want %d", result.Size, len(data))
	}
	if result.Mode != "0644" {
		t.Errorf("Mode = %q, want '0644'", result.Mode)
	}
}

// --- setPutChecksum ---

func TestHelperSetPutChecksum(t *testing.T) {
	data := []byte("test data")

	t.Run("enabled", func(t *testing.T) {
		result := &FilePutResult{}
		setPutChecksum(data, true, result)
		hash := sha256.Sum256(data)
		expected := hex.EncodeToString(hash[:])
		if result.Checksum != expected {
			t.Errorf("Checksum = %q, want %q", result.Checksum, expected)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		result := &FilePutResult{}
		setPutChecksum(data, false, result)
		if result.Checksum != "" {
			t.Errorf("Checksum = %q, want empty", result.Checksum)
		}
	})
}

// --- isCompressible ---

func TestHelperIsCompressible(t *testing.T) {
	compressibleExts := []string{
		".txt", ".log", ".json", ".xml", ".yaml", ".yml", ".md", ".csv",
		".html", ".htm", ".css", ".js", ".ts", ".tsx", ".jsx", ".go",
		".py", ".rb", ".java", ".c", ".cpp", ".h", ".hpp", ".rs",
		".sh", ".bash", ".zsh", ".sql", ".conf", ".cfg", ".ini", ".toml",
	}
	for _, ext := range compressibleExts {
		if !isCompressible("file" + ext) {
			t.Errorf("isCompressible('file%s') = false, want true", ext)
		}
	}

	nonCompressibleExts := []string{
		".png", ".jpg", ".jpeg", ".gif", ".zip", ".gz", ".tar",
		".mp3", ".mp4", ".avi", ".mov", ".pdf", ".exe", ".bin",
		".wasm", ".so", ".dll",
	}
	for _, ext := range nonCompressibleExts {
		if isCompressible("file" + ext) {
			t.Errorf("isCompressible('file%s') = true, want false", ext)
		}
	}

	// Case insensitivity test (filepath.Ext preserves case, but ToLower is applied)
	if !isCompressible("FILE.TXT") {
		t.Error("isCompressible('FILE.TXT') should handle uppercase")
	}
	if !isCompressible("data.JSON") {
		t.Error("isCompressible('data.JSON') should handle uppercase")
	}
}

// --- compressData / decompressData ---

func TestHelperCompressDecompressRoundtrip(t *testing.T) {
	original := []byte("Hello, this is some test data that should compress and decompress correctly!")

	compressed, err := compressData(original)
	if err != nil {
		t.Fatalf("compressData: %v", err)
	}

	decompressed, err := decompressData(compressed)
	if err != nil {
		t.Fatalf("decompressData: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("roundtrip mismatch: got %q, want %q", decompressed, original)
	}
}

func TestHelperCompressDataReducesSize(t *testing.T) {
	// Highly repetitive data should compress well
	original := []byte(strings.Repeat("AAAA", 10000))
	compressed, err := compressData(original)
	if err != nil {
		t.Fatalf("compressData: %v", err)
	}
	if len(compressed) >= len(original) {
		t.Errorf("compressed size (%d) should be less than original (%d)", len(compressed), len(original))
	}
}

func TestHelperDecompressDataInvalidInput(t *testing.T) {
	_, err := decompressData([]byte("not gzip data"))
	if err == nil {
		t.Error("expected error for invalid gzip data")
	}
}

// --- fileStatError ---

func TestHelperFileStatError(t *testing.T) {
	t.Run("file not found", func(t *testing.T) {
		result := fileStatError("/missing/file.txt", fs.ErrNotExist)
		if !result.IsError {
			t.Error("expected error result")
		}
		resultJSON, _ := json.Marshal(result)
		if !strings.Contains(string(resultJSON), "file not found") {
			t.Errorf("expected 'file not found' in error, got %s", string(resultJSON))
		}
	})

	t.Run("other error", func(t *testing.T) {
		result := fileStatError("/some/file.txt", errors.New("permission denied"))
		if !result.IsError {
			t.Error("expected error result")
		}
		resultJSON, _ := json.Marshal(result)
		if !strings.Contains(string(resultJSON), "stat file") {
			t.Errorf("expected 'stat file' in error, got %s", string(resultJSON))
		}
	})
}

// --- resolveFileContent (via Server with fakefs) ---

func TestHelperResolveFileContent(t *testing.T) {
	t.Run("from content string", func(t *testing.T) {
		fs := fakefs.New()
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fs))

		data, modTime, errResult := srv.resolveFileContent(FilePutOptions{
			Content:  "hello world",
			Encoding: "text",
		})
		if errResult != nil {
			t.Fatalf("unexpected error result")
		}
		if string(data) != "hello world" {
			t.Errorf("data = %q, want 'hello world'", data)
		}
		if !modTime.IsZero() {
			t.Error("modTime should be zero for direct content")
		}
	})

	t.Run("from base64 content", func(t *testing.T) {
		fs := fakefs.New()
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fs))

		encoded := base64.StdEncoding.EncodeToString([]byte("binary data"))
		data, _, errResult := srv.resolveFileContent(FilePutOptions{
			Content:  encoded,
			Encoding: "base64",
		})
		if errResult != nil {
			t.Fatalf("unexpected error result")
		}
		if string(data) != "binary data" {
			t.Errorf("data = %q, want 'binary data'", data)
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		fs := fakefs.New()
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fs))

		_, _, errResult := srv.resolveFileContent(FilePutOptions{
			Content:  "not-valid-base64!!!",
			Encoding: "base64",
		})
		if errResult == nil {
			t.Error("expected error result for invalid base64")
		}
	})

	t.Run("from local file", func(t *testing.T) {
		fakeFS := fakefs.New()
		fakeFS.AddFile("/local/data.txt", []byte("file content"), 0644)
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		data, modTime, errResult := srv.resolveFileContent(FilePutOptions{
			LocalPath: "/local/data.txt",
		})
		if errResult != nil {
			t.Fatalf("unexpected error result")
		}
		if string(data) != "file content" {
			t.Errorf("data = %q, want 'file content'", data)
		}
		if modTime.IsZero() {
			t.Error("modTime should not be zero for local file")
		}
	})

	t.Run("local file not found", func(t *testing.T) {
		fakeFS := fakefs.New()
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		_, _, errResult := srv.resolveFileContent(FilePutOptions{
			LocalPath: "/nonexistent/file.txt",
		})
		if errResult == nil {
			t.Error("expected error result for missing local file")
		}
	})
}

// --- copyToLocalPath (via Server with fakefs) ---

func TestHelperCopyToLocalPath(t *testing.T) {
	t.Run("copies data to path", func(t *testing.T) {
		fakeFS := fakefs.New()
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		info := &fakeFileInfo{
			name:    "source.txt",
			size:    13,
			mode:    0644,
			modTime: time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
		}

		errResult := srv.copyToLocalPath([]byte("hello content"), "/dest/file.txt", info, false)
		if errResult != nil {
			t.Fatalf("unexpected error result")
		}

		data, err := fakeFS.ReadFile("/dest/file.txt")
		if err != nil {
			t.Fatalf("failed to read copied file: %v", err)
		}
		if string(data) != "hello content" {
			t.Errorf("file content = %q, want 'hello content'", data)
		}
	})

	t.Run("preserves timestamps", func(t *testing.T) {
		fakeFS := fakefs.New()
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		modTime := time.Date(2023, 1, 15, 12, 0, 0, 0, time.UTC)
		info := &fakeFileInfo{
			name:    "source.txt",
			size:    5,
			mode:    0644,
			modTime: modTime,
		}

		errResult := srv.copyToLocalPath([]byte("hello"), "/dest/preserved.txt", info, true)
		if errResult != nil {
			t.Fatalf("unexpected error result")
		}

		stat, err := fakeFS.Stat("/dest/preserved.txt")
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}
		if !stat.ModTime().Equal(modTime) {
			t.Errorf("ModTime = %v, want %v", stat.ModTime(), modTime)
		}
	})
}

// --- checkLocalFileOverwrite (via Server with fakefs) ---

func TestHelperCheckLocalFileOverwrite(t *testing.T) {
	t.Run("file does not exist", func(t *testing.T) {
		fakeFS := fakefs.New()
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		result := &FilePutResult{}
		errResult := srv.checkLocalFileOverwrite("/nonexistent", false, result)
		if errResult != nil {
			t.Error("expected nil for non-existent file")
		}
		if result.Overwritten {
			t.Error("Overwritten should be false")
		}
	})

	t.Run("file exists, no overwrite", func(t *testing.T) {
		fakeFS := fakefs.New()
		fakeFS.AddFile("/existing.txt", []byte("data"), 0644)
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		result := &FilePutResult{}
		errResult := srv.checkLocalFileOverwrite("/existing.txt", false, result)
		if errResult == nil {
			t.Fatal("expected error when file exists and overwrite=false")
		}
		if !errResult.IsError {
			t.Error("expected IsError=true")
		}
	})

	t.Run("file exists, with overwrite", func(t *testing.T) {
		fakeFS := fakefs.New()
		fakeFS.AddFile("/existing.txt", []byte("data"), 0644)
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		result := &FilePutResult{}
		errResult := srv.checkLocalFileOverwrite("/existing.txt", true, result)
		if errResult != nil {
			t.Error("expected nil when overwrite=true")
		}
		if !result.Overwritten {
			t.Error("Overwritten should be true")
		}
	})
}

// --- preserveLocalTimestamp (via Server with fakefs) ---

func TestHelperPreserveLocalTimestamp(t *testing.T) {
	t.Run("preserve=false does nothing", func(t *testing.T) {
		fakeFS := fakefs.New()
		fakeFS.AddFile("/file.txt", []byte("data"), 0644)
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		// Get original modtime
		origInfo, _ := fakeFS.Stat("/file.txt")
		origMod := origInfo.ModTime()

		srv.preserveLocalTimestamp("/file.txt", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))

		info, _ := fakeFS.Stat("/file.txt")
		if !info.ModTime().Equal(origMod) {
			t.Error("modtime should not change when preserve=false")
		}
	})

	t.Run("preserve=true with zero time does nothing", func(t *testing.T) {
		fakeFS := fakefs.New()
		fakeFS.AddFile("/file.txt", []byte("data"), 0644)
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		origInfo, _ := fakeFS.Stat("/file.txt")
		origMod := origInfo.ModTime()

		srv.preserveLocalTimestamp("/file.txt", true, time.Time{})

		info, _ := fakeFS.Stat("/file.txt")
		if !info.ModTime().Equal(origMod) {
			t.Error("modtime should not change when modTime is zero")
		}
	})

	t.Run("preserve=true with valid time updates", func(t *testing.T) {
		fakeFS := fakefs.New()
		fakeFS.AddFile("/file.txt", []byte("data"), 0644)
		cfg := config.DefaultConfig()
		srv := NewServer(cfg, WithFileSystem(fakeFS))

		newTime := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
		srv.preserveLocalTimestamp("/file.txt", true, newTime)

		info, _ := fakeFS.Stat("/file.txt")
		if !info.ModTime().Equal(newTime) {
			t.Errorf("ModTime = %v, want %v", info.ModTime(), newTime)
		}
	})
}

// --- saveOutputToFile (via Server with fakefs) ---

func TestHelperSaveOutputToFile(t *testing.T) {
	fixedTime := time.Unix(1700000000, 0)
	fc := fakeclock.New(fixedTime)

	fakeFS := fakefs.New()
	fakeFS.SetCwd("/workspace")
	cfg := config.DefaultConfig()
	srv := NewServer(cfg, WithFileSystem(fakeFS), WithClock(fc))

	path, err := srv.saveOutputToFile("sess_abc", "big output content")
	if err != nil {
		t.Fatalf("saveOutputToFile: %v", err)
	}

	// Verify path format
	expectedPrefix := "/workspace/.claude-shell-mcp/sess_abc_"
	if !strings.HasPrefix(path, expectedPrefix) {
		t.Errorf("path %q should start with %q", path, expectedPrefix)
	}
	if !strings.HasSuffix(path, ".txt") {
		t.Errorf("path %q should end with '.txt'", path)
	}

	// Verify file was written
	data, err := fakeFS.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if string(data) != "big output content" {
		t.Errorf("file content = %q, want 'big output content'", data)
	}
}

// --- Tool definition schema verification ---

func TestHelperToolDefinitions(t *testing.T) {
	// Verify all tool definition functions return non-empty tools with names and descriptions
	tools := []struct {
		name string
		fn   func() mcpgo.Tool
	}{
		{"shellSessionCreateTool", shellSessionCreateTool},
		{"shellSessionListTool", shellSessionListTool},
		{"shellExecTool", shellExecTool},
		{"shellProvideInputTool", shellProvideInputTool},
		{"shellSudoAuthTool", shellSudoAuthTool},
		{"shellSendRawTool", shellSendRawTool},
		{"shellInterruptTool", shellInterruptTool},
		{"shellSessionStatusTool", shellSessionStatusTool},
		{"shellSessionCloseTool", shellSessionCloseTool},
		{"shellDebugTool", shellDebugTool},
		{"shellFileGetTool", shellFileGetTool},
		{"shellFilePutTool", shellFilePutTool},
		{"shellFileMvTool", shellFileMvTool},
		{"shellDirGetTool", shellDirGetTool},
		{"shellDirPutTool", shellDirPutTool},
		{"shellFileGetChunkedTool", shellFileGetChunkedTool},
		{"shellFilePutChunkedTool", shellFilePutChunkedTool},
		{"shellTransferStatusTool", shellTransferStatusTool},
		{"shellTransferResumeTool", shellTransferResumeTool},
		{"shellTunnelCreateTool", shellTunnelCreateTool},
		{"shellTunnelListTool", shellTunnelListTool},
		{"shellTunnelCloseTool", shellTunnelCloseTool},
		{"shellTunnelRestoreTool", shellTunnelRestoreTool},
		{"peakTTYStatusTool", peakTTYStatusTool},
		{"peakTTYStartTool", peakTTYStartTool},
		{"peakTTYStopTool", peakTTYStopTool},
		{"peakTTYDeployTool", peakTTYDeployTool},
		{"shellConfigAddTool", shellConfigAddTool},
		{"shellServerListTool", shellServerListTool},
		{"shellServerTestTool", shellServerTestTool},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			tool := tt.fn()
			if tool.Name == "" {
				t.Errorf("%s: tool name is empty", tt.name)
			}
			if tool.Description == "" {
				t.Errorf("%s: tool description is empty", tt.name)
			}
		})
	}
}

// --- fakeFileInfo helper for tests ---

type fakeFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *fakeFileInfo) Name() string       { return fi.name }
func (fi *fakeFileInfo) Size() int64        { return fi.size }
func (fi *fakeFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *fakeFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *fakeFileInfo) IsDir() bool        { return fi.isDir }
func (fi *fakeFileInfo) Sys() any           { return nil }
