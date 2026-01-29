package security

import (
	"testing"
)

func TestCommandFilter_Blocklist(t *testing.T) {
	tests := []struct {
		name        string
		blocklist   []string
		command     string
		wantAllowed bool
	}{
		{
			name:        "allow normal command",
			blocklist:   []string{`rm\s+-rf\s+/\s*$`},
			command:     "ls -la",
			wantAllowed: true,
		},
		{
			name:        "block rm -rf /",
			blocklist:   []string{`rm\s+-rf\s+/\s*$`},
			command:     "rm -rf /",
			wantAllowed: false,
		},
		{
			name:        "allow rm with safe path",
			blocklist:   []string{`rm\s+-rf\s+/\s*$`},
			command:     "rm -rf /tmp/test",
			wantAllowed: true,
		},
		{
			name:        "block fork bomb",
			blocklist:   []string{`:\s*\(\s*\)\s*\{\s*:\s*\|`},
			command:     ":(){ :|:& };:",
			wantAllowed: false,
		},
		{
			name:        "empty blocklist allows all",
			blocklist:   []string{},
			command:     "rm -rf /",
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf, err := NewCommandFilter(tt.blocklist, nil)
			if err != nil {
				t.Fatalf("NewCommandFilter() error = %v", err)
			}

			allowed, _ := cf.IsAllowed(tt.command)
			if allowed != tt.wantAllowed {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.command, allowed, tt.wantAllowed)
			}
		})
	}
}

func TestCommandFilter_Allowlist(t *testing.T) {
	tests := []struct {
		name        string
		allowlist   []string
		command     string
		wantAllowed bool
	}{
		{
			name:        "allow matching command",
			allowlist:   []string{`^ls`, `^cat`, `^pwd`},
			command:     "ls -la",
			wantAllowed: true,
		},
		{
			name:        "block non-matching command",
			allowlist:   []string{`^ls`, `^cat`, `^pwd`},
			command:     "rm -rf /tmp/test",
			wantAllowed: false,
		},
		{
			name:        "allow git commands",
			allowlist:   []string{`^git\s`},
			command:     "git status",
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf, err := NewCommandFilter(nil, tt.allowlist)
			if err != nil {
				t.Fatalf("NewCommandFilter() error = %v", err)
			}

			allowed, _ := cf.IsAllowed(tt.command)
			if allowed != tt.wantAllowed {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.command, allowed, tt.wantAllowed)
			}
		})
	}
}

func TestCommandFilter_InvalidRegex(t *testing.T) {
	_, err := NewCommandFilter([]string{`[invalid`}, nil)
	if err == nil {
		t.Error("expected error for invalid regex, got nil")
	}
}

func TestDefaultBlocklist(t *testing.T) {
	blocklist := DefaultBlocklist()
	if len(blocklist) == 0 {
		t.Error("DefaultBlocklist() returned empty list")
	}

	// Verify patterns are valid regex
	_, err := NewCommandFilter(blocklist, nil)
	if err != nil {
		t.Errorf("DefaultBlocklist() contains invalid regex: %v", err)
	}
}
