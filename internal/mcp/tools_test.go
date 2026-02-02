package mcp

import (
	"strings"
	"testing"
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
