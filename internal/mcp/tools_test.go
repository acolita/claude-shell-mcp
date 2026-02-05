package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
)

func TestTruncateOutput(t *testing.T) {
	// Create test output with 10 lines
	lines := make([]string, 10)
	for i := 0; i < 10; i++ {
		lines[i] = strings.Repeat("x", i+1) // "x", "xx", "xxx", etc.
	}
	output := strings.Join(lines, "\n")

	tests := []struct {
		name          string
		output        string
		tailLines     int
		headLines     int
		wantTruncated bool
		wantTotal     int
		wantShown     int
		wantFirstLine string
		wantLastLine  string
	}{
		{
			name:          "no truncation",
			output:        output,
			tailLines:     0,
			headLines:     0,
			wantTruncated: false,
			wantTotal:     10,
			wantShown:     10,
			wantFirstLine: "x",
			wantLastLine:  "xxxxxxxxxx",
		},
		{
			name:          "tail 3 lines",
			output:        output,
			tailLines:     3,
			headLines:     0,
			wantTruncated: true,
			wantTotal:     10,
			wantShown:     3,
			wantFirstLine: "xxxxxxxx",
			wantLastLine:  "xxxxxxxxxx",
		},
		{
			name:          "head 3 lines",
			output:        output,
			tailLines:     0,
			headLines:     3,
			wantTruncated: true,
			wantTotal:     10,
			wantShown:     3,
			wantFirstLine: "x",
			wantLastLine:  "xxx",
		},
		{
			name:          "tail more than total",
			output:        output,
			tailLines:     20,
			headLines:     0,
			wantTruncated: false,
			wantTotal:     10,
			wantShown:     10,
			wantFirstLine: "x",
			wantLastLine:  "xxxxxxxxxx",
		},
		{
			name:          "head more than total",
			output:        output,
			tailLines:     0,
			headLines:     20,
			wantTruncated: false,
			wantTotal:     10,
			wantShown:     10,
			wantFirstLine: "x",
			wantLastLine:  "xxxxxxxxxx",
		},
		{
			name:          "tail 1 line",
			output:        output,
			tailLines:     1,
			headLines:     0,
			wantTruncated: true,
			wantTotal:     10,
			wantShown:     1,
			wantFirstLine: "xxxxxxxxxx",
			wantLastLine:  "xxxxxxxxxx",
		},
		{
			name:          "head 1 line",
			output:        output,
			tailLines:     0,
			headLines:     1,
			wantTruncated: true,
			wantTotal:     10,
			wantShown:     1,
			wantFirstLine: "x",
			wantLastLine:  "x",
		},
		{
			name:          "empty output",
			output:        "",
			tailLines:     5,
			headLines:     0,
			wantTruncated: false,
			wantTotal:     0,
			wantShown:     0,
			wantFirstLine: "",
			wantLastLine:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, truncated, total, shown := truncateOutput(tt.output, tt.tailLines, tt.headLines)

			if truncated != tt.wantTruncated {
				t.Errorf("truncated = %v, want %v", truncated, tt.wantTruncated)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if shown != tt.wantShown {
				t.Errorf("shown = %d, want %d", shown, tt.wantShown)
			}

			if result == "" && tt.wantFirstLine == "" {
				return // Empty output case
			}

			resultLines := strings.Split(result, "\n")
			if len(resultLines) > 0 && resultLines[0] != tt.wantFirstLine {
				t.Errorf("first line = %q, want %q", resultLines[0], tt.wantFirstLine)
			}
			if len(resultLines) > 0 && resultLines[len(resultLines)-1] != tt.wantLastLine {
				t.Errorf("last line = %q, want %q", resultLines[len(resultLines)-1], tt.wantLastLine)
			}
		})
	}
}

func TestApplyAutoTruncation(t *testing.T) {
	// Save original timeNow and restore after test
	originalTimeNow := timeNow
	defer func() { timeNow = originalTimeNow }()

	// Fixed time for deterministic file names
	fixedTime := time.Unix(1704067200, 0) // 2024-01-01 00:00:00 UTC
	timeNow = func() time.Time { return fixedTime }

	tests := []struct {
		name          string
		stdout        string
		wantTruncated bool
		wantFileEmpty bool
		wantStdout    string
	}{
		{
			name:          "small output - no truncation",
			stdout:        "hello world",
			wantTruncated: false,
			wantFileEmpty: true,
			wantStdout:    "hello world",
		},
		{
			name:          "large output - saved to file",
			stdout:        strings.Repeat("x", 60*1024), // 60KB > 50KB threshold
			wantTruncated: true,
			wantFileEmpty: false,
			wantStdout:    "", // stdout cleared, only file path returned
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := fakefs.New()
			fs.SetCwd("/test/project")

			s := &Server{fs: fs}

			result := &session.ExecResult{
				Stdout: tt.stdout,
			}

			s.applyAutoTruncation("sess_123", result)

			if result.Truncated != tt.wantTruncated {
				t.Errorf("Truncated = %v, want %v", result.Truncated, tt.wantTruncated)
			}

			if result.Stdout != tt.wantStdout {
				t.Errorf("Stdout = %q (len=%d), want %q (len=%d)",
					truncStr(result.Stdout, 50), len(result.Stdout),
					truncStr(tt.wantStdout, 50), len(tt.wantStdout))
			}

			if tt.wantFileEmpty {
				if result.OutputFile != "" {
					t.Errorf("OutputFile = %q, want empty", result.OutputFile)
				}
			} else {
				// Verify file was created
				if result.OutputFile == "" {
					t.Error("OutputFile is empty, expected file path")
				} else {
					// Verify file contains the original content
					data, err := fs.ReadFile(result.OutputFile)
					if err != nil {
						t.Errorf("Failed to read output file: %v", err)
					} else if string(data) != tt.stdout {
						t.Errorf("File content length = %d, want %d", len(data), len(tt.stdout))
					}

					// Verify file is in the working directory
					expectedPrefix := "/test/project/.claude-shell-mcp/"
					if !strings.HasPrefix(result.OutputFile, expectedPrefix) {
						t.Errorf("OutputFile = %q, should start with %q", result.OutputFile, expectedPrefix)
					}
				}

				// Verify TotalBytes is set
				if result.TotalBytes != len(tt.stdout) {
					t.Errorf("TotalBytes = %d, want %d", result.TotalBytes, len(tt.stdout))
				}
			}
		})
	}
}

func TestLookupSudoPasswordFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		servers  []config.ServerConfig
		envVars  map[string]string
		wantNil  bool
		wantPass string
	}{
		{
			name: "found by host",
			host: "prod.example.com",
			servers: []config.ServerConfig{
				{Name: "prod", Host: "prod.example.com", SudoPasswordEnv: "PROD_PASS"},
			},
			envVars:  map[string]string{"PROD_PASS": "secret123"},
			wantPass: "secret123",
		},
		{
			name: "found by name",
			host: "prod",
			servers: []config.ServerConfig{
				{Name: "prod", Host: "prod.example.com", SudoPasswordEnv: "PROD_PASS"},
			},
			envVars:  map[string]string{"PROD_PASS": "secret123"},
			wantPass: "secret123",
		},
		{
			name: "no matching server",
			host: "unknown.example.com",
			servers: []config.ServerConfig{
				{Name: "prod", Host: "prod.example.com", SudoPasswordEnv: "PROD_PASS"},
			},
			envVars: map[string]string{"PROD_PASS": "secret123"},
			wantNil: true,
		},
		{
			name: "no sudo_password_env configured",
			host: "prod.example.com",
			servers: []config.ServerConfig{
				{Name: "prod", Host: "prod.example.com"},
			},
			wantNil: true,
		},
		{
			name: "env var is empty",
			host: "prod.example.com",
			servers: []config.ServerConfig{
				{Name: "prod", Host: "prod.example.com", SudoPasswordEnv: "PROD_PASS"},
			},
			envVars: map[string]string{},
			wantNil: true,
		},
		{
			name:    "empty host",
			host:    "",
			wantNil: true,
		},
		{
			name:    "nil config",
			host:    "prod.example.com",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := fakefs.New()
			for k, v := range tt.envVars {
				fs.SetEnv(k, v)
			}

			var cfg *config.Config
			if tt.name != "nil config" {
				cfg = &config.Config{Servers: tt.servers}
			}

			s := &Server{fs: fs, config: cfg}

			result := s.lookupSudoPasswordFromConfig(tt.host)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %q", string(result))
				}
			} else {
				if result == nil {
					t.Error("expected password, got nil")
				} else if string(result) != tt.wantPass {
					t.Errorf("password = %q, want %q", string(result), tt.wantPass)
				}
			}
		})
	}
}

// truncStr truncates a string for display in test output
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
