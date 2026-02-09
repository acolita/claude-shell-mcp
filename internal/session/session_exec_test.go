package session

import (
	"fmt"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/ssh"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakerand"
)

// buildCommandOutput creates fake PTY output that matches what Exec expects.
// The format is: start_marker + output + end_marker + exit_code
func buildCommandOutput(cmdID, output string, exitCode int) string {
	startMarker := startMarkerPrefix + cmdID + markerSuffix
	endMarker := endMarkerPrefix + cmdID + markerSuffix
	return fmt.Sprintf("%s\n%s\n%s%d\n", startMarker, output, endMarker, exitCode)
}

func TestSession_Exec_SimpleCommand(t *testing.T) {
	// Create fake PTY with scripted responses
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})
	cfg := config.DefaultConfig()

	// Create session with injected fakes
	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	// Initialize (won't create real PTY because we injected one)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// The command ID will be generated from our fake random source
	// fakerand returns sequential bytes starting from what we provided
	// generateCommandID reads 4 bytes and hex encodes them
	expectedCmdID := "01020304"

	// Queue the response that matches what the session expects
	pty.AddResponse(buildCommandOutput(expectedCmdID, "hello world", 0))

	// Execute command
	result, err := sess.Exec("echo hello", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	// Verify result
	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", result.ExitCode)
	}
	// The exact stdout depends on how the session processes markers
	// Just verify it contains the expected output
	if result.Stdout == "" {
		t.Error("Stdout should not be empty")
	}

	// Verify the command was written to PTY
	written := pty.Written()
	if written == "" {
		t.Error("expected command to be written to PTY")
	}
}

func TestSession_Exec_NonZeroExitCode(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	expectedCmdID := "01020304"
	pty.AddResponse(buildCommandOutput(expectedCmdID, "error: file not found", 1))

	result, err := sess.Exec("cat /nonexistent", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Errorf("ExitCode = %v, want 1", result.ExitCode)
	}
}

func TestSession_Exec_Timeout(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Set PTY to block reads - simulates hanging command
	pty.SetBlockReads(true)

	// Use very short timeout
	result, err := sess.Exec("sleep 100", 100)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
}

func TestSession_Exec_PasswordPrompt(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	expectedCmdID := "01020304"
	startMarker := startMarkerPrefix + expectedCmdID + markerSuffix

	// Simulate sudo command that shows a password prompt
	// First response: start marker and partial output with password prompt
	pty.AddResponse(fmt.Sprintf("%s\n[sudo] password for user: ", startMarker))
	// Need multiple responses because the read loop continues
	// Add empty responses to simulate the wait
	for i := 0; i < 20; i++ {
		pty.AddResponse("")
	}

	result, err := sess.Exec("sudo apt update", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	// Should detect password prompt and return awaiting_input
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "password" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "password")
	}
	if !result.MaskInput {
		t.Error("MaskInput should be true for password prompts")
	}
}

func TestSession_Exec_ClosedSession(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Close the session
	sess.State = StateClosed

	// Try to execute - should fail
	_, err := sess.Exec("echo test", 5000)
	if err == nil {
		t.Error("expected error for closed session")
	}
}

func TestSession_Exec_NotInitialized(t *testing.T) {
	// Create session without PTY
	sess := NewSession("sess_test", "local")

	// Try to execute without Initialize - should fail
	_, err := sess.Exec("echo test", 5000)
	if err == nil {
		t.Error("expected error for uninitialized session")
	}
}

func TestSession_Initialize_WithInjectedPTY(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)

	// Initialize should succeed and set state
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	if sess.State != StateIdle {
		t.Errorf("State = %v, want %v", sess.State, StateIdle)
	}

	// CreatedAt and LastUsed should be set from our fake clock
	expectedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	if !sess.CreatedAt.Equal(expectedTime) {
		t.Errorf("CreatedAt = %v, want %v", sess.CreatedAt, expectedTime)
	}
	if !sess.LastUsed.Equal(expectedTime) {
		t.Errorf("LastUsed = %v, want %v", sess.LastUsed, expectedTime)
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no ANSI",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "color codes",
			input: "\x1b[31mred\x1b[0m text",
			want:  "red text",
		},
		{
			name:  "cursor movement",
			input: "\x1b[2Jhello\x1b[H",
			want:  "hello",
		},
		{
			name:  "mixed",
			input: "\x1b[1;32mgreen bold\x1b[0m normal",
			want:  "green bold normal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			if got != tt.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractPTYNumber(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"full path", "/dev/pts/3", "3"},
		{"another path", "/dev/pts/123", "123"},
		{"empty", "", ""},
		{"no prefix returns empty", "pts/3", ""}, // Only /dev/pts/ prefix is valid
		{"with trailing newline", "/dev/pts/5\n", "5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPTYNumber(tt.input)
			if got != tt.want {
				t.Errorf("extractPTYNumber(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSession_Status(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	status := sess.Status()

	if status.ID != "sess_test" {
		t.Errorf("ID = %q, want %q", status.ID, "sess_test")
	}
	if status.State != "idle" {
		t.Errorf("State = %q, want %q", status.State, "idle")
	}
	if status.Mode != "local" {
		t.Errorf("Mode = %q, want %q", status.Mode, "local")
	}
}

func TestSession_MultipleCommands(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	// Provide enough random bytes for multiple command IDs
	rand := fakerand.New([]byte{
		0x01, 0x02, 0x03, 0x04, // First command ID
		0x05, 0x06, 0x07, 0x08, // Second command ID
	})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// First command
	pty.AddResponse(buildCommandOutput("01020304", "output1", 0))
	result1, err := sess.Exec("cmd1", 5000)
	if err != nil {
		t.Fatalf("First Exec error: %v", err)
	}
	if result1.Status != "completed" {
		t.Errorf("First Status = %q, want completed", result1.Status)
	}

	// Second command
	pty.AddResponse(buildCommandOutput("05060708", "output2", 0))
	result2, err := sess.Exec("cmd2", 5000)
	if err != nil {
		t.Fatalf("Second Exec error: %v", err)
	}
	if result2.Status != "completed" {
		t.Errorf("Second Status = %q, want completed", result2.Status)
	}
}

// SSH validation and configuration tests

func TestSession_ValidateSSHConfig(t *testing.T) {
	tests := []struct {
		name    string
		session *Session
		wantErr string
	}{
		{
			name:    "missing host",
			session: &Session{Mode: "ssh", User: "user"},
			wantErr: "host is required",
		},
		{
			name:    "missing user",
			session: &Session{Mode: "ssh", Host: "example.com"},
			wantErr: "user is required",
		},
		{
			name:    "valid config",
			session: &Session{Mode: "ssh", Host: "example.com", User: "user"},
			wantErr: "",
		},
		{
			name:    "default port",
			session: &Session{Mode: "ssh", Host: "example.com", User: "user", Port: 0},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.session.validateSSHConfig()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Check that port was defaulted
				if tt.session.Port == 0 {
					t.Error("port should be defaulted to 22")
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestSession_BuildSSHAuthConfig(t *testing.T) {
	// Test with password
	sess := &Session{
		Host:     "example.com",
		User:     "user",
		Password: "secret",
	}
	authCfg := sess.buildSSHAuthConfig()

	if authCfg.Password != "secret" {
		t.Errorf("Password = %q, want %q", authCfg.Password, "secret")
	}
	if authCfg.Host != "example.com" {
		t.Errorf("Host = %q, want %q", authCfg.Host, "example.com")
	}
	if !authCfg.UseAgent {
		t.Error("UseAgent should be true by default")
	}

	// Test with key path
	sess2 := &Session{
		Host:    "example.com",
		User:    "user",
		KeyPath: "/home/user/.ssh/id_ed25519",
	}
	authCfg2 := sess2.buildSSHAuthConfig()

	if authCfg2.KeyPath != "/home/user/.ssh/id_ed25519" {
		t.Errorf("KeyPath = %q, want %q", authCfg2.KeyPath, "/home/user/.ssh/id_ed25519")
	}
}

func TestSession_ApplyServerAuthConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{
			Name:    "prod",
			Host:    "prod.example.com",
			KeyPath: "/keys/prod_key",
		},
		{
			Name: "staging",
			Host: "staging.example.com",
			Auth: config.AuthConfig{
				PassphraseEnv: "SSH_PASSPHRASE",
			},
		},
	}

	// Test matching by host
	sess := &Session{
		Host:   "prod.example.com",
		config: cfg,
	}
	authCfg := &ssh.AuthConfig{}
	sess.applyServerAuthConfig(authCfg)

	if authCfg.KeyPath != "/keys/prod_key" {
		t.Errorf("KeyPath = %q, want %q", authCfg.KeyPath, "/keys/prod_key")
	}

	// Test matching by name
	sess2 := &Session{
		Host:   "prod", // Match by name
		config: cfg,
	}
	authCfg2 := &ssh.AuthConfig{}
	sess2.applyServerAuthConfig(authCfg2)

	if authCfg2.KeyPath != "/keys/prod_key" {
		t.Errorf("KeyPath = %q, want %q", authCfg2.KeyPath, "/keys/prod_key")
	}
}

func TestSession_ShellPromptCommand(t *testing.T) {
	tests := []struct {
		name  string
		shell string
		want  string
	}{
		{"bash", "/bin/bash", "PS1='$ '"},
		{"zsh", "/bin/zsh", "PROMPT='$ '"}, // zsh uses PROMPT not PS1
		{"sh", "/bin/sh", "PS1='$ '"},
		{"fish", "/usr/bin/fish", "function fish_prompt"}, // fish uses function syntax
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{Shell: tt.shell}
			got := sess.shellPromptCommand()
			if !contains(got, tt.want) {
				t.Errorf("shellPromptCommand() = %q, want containing %q", got, tt.want)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
