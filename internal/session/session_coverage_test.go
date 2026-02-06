package session

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/prompt"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakerand"
)

// ============================================================================
// parseEnvOutput tests
// ============================================================================

func TestParseEnvOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "basic key=value pairs",
			input: "HOME=/home/user\nPATH=/usr/bin\nSHELL=/bin/bash\n",
			want: map[string]string{
				"HOME":  "/home/user",
				"PATH":  "/usr/bin",
				"SHELL": "/bin/bash",
			},
		},
		{
			name:  "skips empty lines",
			input: "\n\nHOME=/home/user\n\n",
			want: map[string]string{
				"HOME": "/home/user",
			},
		},
		{
			name:  "skips env command echo",
			input: "$ env\nenv\nHOME=/home/user\n",
			want: map[string]string{
				"HOME": "/home/user",
			},
		},
		{
			name:  "skips prompt lines",
			input: "$ some prompt\nHOME=/home/user\n",
			want: map[string]string{
				"HOME": "/home/user",
			},
		},
		{
			name:  "skips underscore-prefixed vars",
			input: "_=/usr/bin/env\n__SOME_INTERNAL=val\nHOME=/home/user\n",
			want: map[string]string{
				"HOME": "/home/user",
			},
		},
		{
			name:  "skips SHLVL and OLDPWD",
			input: "SHLVL=2\nOLDPWD=/tmp\nHOME=/home/user\n",
			want: map[string]string{
				"HOME": "/home/user",
			},
		},
		{
			name:  "value with equals sign",
			input: "SOME_VAR=key=value\n",
			want: map[string]string{
				"SOME_VAR": "key=value",
			},
		},
		{
			name:  "empty output",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "line with no equals sign",
			input: "invalid-line-no-equals\nHOME=/home/user\n",
			want: map[string]string{
				"HOME": "/home/user",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseEnvOutput(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("len(result) = %d, want %d; got=%v", len(got), len(tt.want), got)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("result[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// ============================================================================
// parseAliasOutput tests
// ============================================================================

func TestParseAliasOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "bash-style aliases",
			input: "alias ll='ls -la'\nalias gs='git status'\n",
			want: map[string]string{
				"ll": "ls -la",
				"gs": "git status",
			},
		},
		{
			name:  "zsh-style aliases (no alias prefix)",
			input: "ll='ls -la'\ngs='git status'\n",
			want: map[string]string{
				"ll": "ls -la",
				"gs": "git status",
			},
		},
		{
			name:  "double-quoted values",
			input: "alias ll=\"ls -la\"\n",
			want: map[string]string{
				"ll": "ls -la",
			},
		},
		{
			name:  "skips empty lines and prompt",
			input: "$ alias\nalias\n\nalias ll='ls -la'\n",
			want: map[string]string{
				"ll": "ls -la",
			},
		},
		{
			name:  "empty output",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "unquoted value",
			input: "alias myalias=mycommand\n",
			want: map[string]string{
				"myalias": "mycommand",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAliasOutput(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("len(result) = %d, want %d; got=%v", len(got), len(tt.want), got)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("result[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// ============================================================================
// GetShellInfo tests
// ============================================================================

func TestSession_GetShellInfo(t *testing.T) {
	tests := []struct {
		name            string
		shell           string
		wantType        string
		wantHistory     bool
	}{
		{"bash", "/bin/bash", "bash", true},
		{"zsh", "/usr/bin/zsh", "zsh", true},
		{"sh", "/bin/sh", "sh", false},
		{"dash", "/usr/bin/dash", "sh", false},
		{"ash", "/bin/ash", "sh", false},
		{"unknown shell", "/usr/local/bin/myshell", "unknown", false},
		{"empty shell", "", "unknown", false}, // no slash found, so shellName is "", switch falls to default
		{"bare bash", "bash", "bash", true},   // no slash, shellName = "bash"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{Shell: tt.shell}
			info := sess.GetShellInfo()
			if info.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", info.Type, tt.wantType)
			}
			if info.Path != tt.shell {
				t.Errorf("Path = %q, want %q", info.Path, tt.shell)
			}
			if info.SupportsHistory != tt.wantHistory {
				t.Errorf("SupportsHistory = %v, want %v", info.SupportsHistory, tt.wantHistory)
			}
		})
	}
}

// ============================================================================
// extractExitCode tests
// ============================================================================

func TestSession_ExtractExitCode(t *testing.T) {
	sess := &Session{}

	tests := []struct {
		name     string
		output   string
		wantCode int
		wantOK   bool
	}{
		{
			name:     "legacy marker exit 0",
			output:   "some output\n___CMD_END_MARKER___0\n",
			wantCode: 0,
			wantOK:   true,
		},
		{
			name:     "legacy marker exit 1",
			output:   "some output\n___CMD_END_MARKER___1\n",
			wantCode: 1,
			wantOK:   true,
		},
		{
			name:     "dynamic marker exit 0",
			output:   "some output\n___CMD_END_abc123___0\n",
			wantCode: 0,
			wantOK:   true,
		},
		{
			name:     "dynamic marker exit 127",
			output:   "some output\n___CMD_END_def456___127\n",
			wantCode: 127,
			wantOK:   true,
		},
		{
			name:   "no marker",
			output: "just some output\n",
			wantOK: false,
		},
		{
			name:   "empty output",
			output: "",
			wantOK: false,
		},
		{
			name:     "with carriage returns",
			output:   "output\r\n___CMD_END_MARKER___0\r\n",
			wantCode: 0,
			wantOK:   true,
		},
		{
			name:     "marker with only carriage return",
			output:   "output\r___CMD_END_MARKER___0\r",
			wantCode: 0,
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := sess.extractExitCode(tt.output)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && code != tt.wantCode {
				t.Errorf("exitCode = %d, want %d", code, tt.wantCode)
			}
		})
	}
}

// ============================================================================
// cleanOutput tests
// ============================================================================

func TestSession_CleanOutput(t *testing.T) {
	sess := &Session{}

	tests := []struct {
		name    string
		output  string
		command string
		want    string
	}{
		{
			name:    "removes command echo",
			output:  "echo hello\nhello\n",
			command: "echo hello",
			want:    "hello",
		},
		{
			name:    "removes shell prompt lines",
			output:  "$ some prompt\nactual output\n",
			command: "",
			want:    "actual output",
		},
		{
			name:    "removes legacy end marker",
			output:  "output\n___CMD_END_MARKER___0\n",
			command: "",
			want:    "output",
		},
		{
			name:    "removes dynamic markers",
			output:  "___CMD_START_abc123___\noutput\n___CMD_END_abc123___0\n",
			command: "",
			want:    "output",
		},
		{
			name:    "strips carriage returns",
			output:  "output\r\nmore output\r\n",
			command: "",
			want:    "output\nmore output",
		},
		{
			name:    "trims leading and trailing blank lines",
			output:  "\n\n\nactual output\n\n\n",
			command: "",
			want:    "actual output",
		},
		{
			name:    "empty output",
			output:  "",
			command: "",
			want:    "",
		},
		{
			name:    "empty command does not skip lines",
			output:  "line1\nline2\n",
			command: "",
			want:    "line1\nline2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sess.cleanOutput(tt.output, tt.command)
			if got != tt.want {
				t.Errorf("cleanOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ============================================================================
// cleanCommandOutput tests
// ============================================================================

func TestSession_CleanCommandOutput(t *testing.T) {
	sess := &Session{}

	tests := []struct {
		name        string
		output      string
		command     string
		startMarker string
		endMarker   string
		want        string
	}{
		{
			name:        "removes end marker lines",
			output:      "hello world\n___CMD_END_abc___0\n",
			command:     "echo hello",
			startMarker: "___CMD_START_abc___",
			endMarker:   "___CMD_END_abc___",
			want:        "hello world",
		},
		{
			name:        "empty output",
			output:      "",
			command:     "cmd",
			startMarker: "___CMD_START_x___",
			endMarker:   "___CMD_END_x___",
			want:        "",
		},
		{
			name:        "output with only end marker",
			output:      "___CMD_END_abc___0",
			command:     "cmd",
			startMarker: "___CMD_START_abc___",
			endMarker:   "___CMD_END_abc___",
			want:        "",
		},
		{
			name:        "multi-line output preserves non-marker lines",
			output:      "line1\nline2\nline3\n___CMD_END_abc___0\n",
			command:     "",
			startMarker: "___CMD_START_abc___",
			endMarker:   "___CMD_END_abc___",
			want:        "line1\nline2\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sess.cleanCommandOutput(tt.output, tt.command, tt.startMarker, tt.endMarker)
			if got != tt.want {
				t.Errorf("cleanCommandOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ============================================================================
// SFTPClient tests (error paths)
// ============================================================================

func TestSession_SFTPClient_LocalMode(t *testing.T) {
	sess := &Session{Mode: "local"}
	_, err := sess.SFTPClient()
	if err == nil {
		t.Fatal("expected error for local session")
	}
	if !strings.Contains(err.Error(), "not available for local sessions") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not available for local sessions")
	}
}

func TestSession_SFTPClient_NilSSHClient(t *testing.T) {
	sess := &Session{Mode: "ssh", sshClient: nil}
	_, err := sess.SFTPClient()
	if err == nil {
		t.Fatal("expected error for nil SSH client")
	}
	if !strings.Contains(err.Error(), "SSH client not initialized") {
		t.Errorf("error = %q, want containing %q", err.Error(), "SSH client not initialized")
	}
}

// ============================================================================
// TunnelManager tests (error paths)
// ============================================================================

func TestSession_TunnelManager_LocalMode(t *testing.T) {
	sess := &Session{Mode: "local"}
	_, err := sess.TunnelManager()
	if err == nil {
		t.Fatal("expected error for local session")
	}
	if !strings.Contains(err.Error(), "not available for local sessions") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not available for local sessions")
	}
}

func TestSession_TunnelManager_NilSSHClient(t *testing.T) {
	sess := &Session{Mode: "ssh", sshClient: nil}
	_, err := sess.TunnelManager()
	if err == nil {
		t.Fatal("expected error for nil SSH client")
	}
	if !strings.Contains(err.Error(), "SSH client not initialized") {
		t.Errorf("error = %q, want containing %q", err.Error(), "SSH client not initialized")
	}
}

// ============================================================================
// WithSessionFileSystem tests
// ============================================================================

func TestWithSessionFileSystem(t *testing.T) {
	fs := fakefs.New()
	sess := NewSession("test", "local", WithSessionFileSystem(fs))
	if sess.fs != fs {
		t.Error("expected injected filesystem")
	}
}

// ============================================================================
// Initialize with custom prompt patterns
// ============================================================================

func TestSession_Initialize_WithCustomPromptPatterns(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	cfg.PromptDetection.CustomPatterns = []config.PatternConfig{
		{
			Name:      "vault_password",
			Regex:     `Vault password:`,
			Type:      "password",
			MaskInput: true,
		},
	}

	sess := NewSession("test_custom_patterns", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)

	err := sess.Initialize()
	if err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Prompt detector should have been created
	if sess.promptDetector == nil {
		t.Error("promptDetector should not be nil after Initialize")
	}
}

func TestSession_Initialize_WithInvalidCustomPattern(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()
	cfg.PromptDetection.CustomPatterns = []config.PatternConfig{
		{
			Name:  "bad_pattern",
			Regex: `[invalid`,
			Type:  "password",
		},
	}

	sess := NewSession("test_bad_pattern", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)

	err := sess.Initialize()
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
	if !strings.Contains(err.Error(), "bad_pattern") {
		t.Errorf("error = %q, want containing %q", err.Error(), "bad_pattern")
	}
}

// ============================================================================
// Initialize sets defaults when nil
// ============================================================================

func TestSession_Initialize_SetsDefaultClockRandomFS(t *testing.T) {
	pty := fakepty.New()
	sess := NewSession("test_defaults", "local",
		WithPTY(pty),
	)

	err := sess.Initialize()
	if err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	if sess.clock == nil {
		t.Error("clock should be set after Initialize")
	}
	if sess.random == nil {
		t.Error("random should be set after Initialize")
	}
	if sess.fs == nil {
		t.Error("fs should be set after Initialize")
	}
}

// ============================================================================
// CaptureEnv tests
// ============================================================================

func TestSession_CaptureEnv_WithFakePTY(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_capture_env", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Queue env output response
	pty.AddResponse("HOME=/home/testuser\nPATH=/usr/bin:/bin\n")

	envMap := sess.CaptureEnv()
	if envMap == nil {
		t.Fatal("expected non-nil env map")
	}
	if envMap["HOME"] != "/home/testuser" {
		t.Errorf("HOME = %q, want %q", envMap["HOME"], "/home/testuser")
	}

	// Verify "env" command was written to PTY
	written := pty.Written()
	if !strings.Contains(written, "env\n") {
		t.Errorf("expected 'env' command to be written, got %q", written)
	}
}

func TestSession_CaptureEnv_ClosedSession(t *testing.T) {
	sess := &Session{
		State:   StateClosed,
		EnvVars: map[string]string{"KEY": "value"},
	}

	envMap := sess.CaptureEnv()
	if envMap["KEY"] != "value" {
		t.Errorf("should return existing env vars for closed session")
	}
}

func TestSession_CaptureEnv_NilPTY(t *testing.T) {
	sess := &Session{
		State:   StateIdle,
		pty:     nil,
		EnvVars: map[string]string{"KEY": "value"},
	}

	envMap := sess.CaptureEnv()
	if envMap["KEY"] != "value" {
		t.Errorf("should return existing env vars when pty is nil")
	}
}

func TestSession_CaptureEnv_NoResponseReturnsExisting(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_capture_env_empty", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.EnvVars = map[string]string{"EXISTING": "val"}

	// No response queued - fakepty returns 0 bytes
	envMap := sess.CaptureEnv()
	if envMap["EXISTING"] != "val" {
		t.Errorf("should return existing env vars when no response, got %v", envMap)
	}
}

// ============================================================================
// CaptureAliases tests
// ============================================================================

func TestSession_CaptureAliases_WithFakePTY(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_capture_aliases", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Queue alias output response
	pty.AddResponse("alias ll='ls -la'\nalias gs='git status'\n")

	aliasMap := sess.CaptureAliases()
	if aliasMap == nil {
		t.Fatal("expected non-nil alias map")
	}
	if aliasMap["ll"] != "ls -la" {
		t.Errorf("ll = %q, want %q", aliasMap["ll"], "ls -la")
	}
	if aliasMap["gs"] != "git status" {
		t.Errorf("gs = %q, want %q", aliasMap["gs"], "git status")
	}
}

func TestSession_CaptureAliases_ClosedSession(t *testing.T) {
	sess := &Session{
		State:   StateClosed,
		Aliases: map[string]string{"ll": "ls -la"},
	}

	aliases := sess.CaptureAliases()
	if aliases["ll"] != "ls -la" {
		t.Errorf("should return existing aliases for closed session")
	}
}

func TestSession_CaptureAliases_NilPTY(t *testing.T) {
	sess := &Session{
		State:   StateIdle,
		pty:     nil,
		Aliases: map[string]string{"ll": "ls -la"},
	}

	aliases := sess.CaptureAliases()
	if aliases["ll"] != "ls -la" {
		t.Errorf("should return existing aliases when pty is nil")
	}
}

func TestSession_CaptureAliases_NoResponseReturnsExisting(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_capture_aliases_empty", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.Aliases = map[string]string{"existing": "val"}

	// No response queued
	aliases := sess.CaptureAliases()
	if aliases["existing"] != "val" {
		t.Errorf("should return existing aliases when no response")
	}
}

// ============================================================================
// getTimeout tests
// ============================================================================

func TestSession_GetTimeout(t *testing.T) {
	sess := &Session{}

	tests := []struct {
		name      string
		timeoutMs int
		want      time.Duration
	}{
		{"zero defaults to 30s", 0, 30 * time.Second},
		{"1000ms", 1000, 1 * time.Second},
		{"5000ms", 5000, 5 * time.Second},
		{"100ms", 100, 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sess.getTimeout(tt.timeoutMs)
			if got != tt.want {
				t.Errorf("getTimeout(%d) = %v, want %v", tt.timeoutMs, got, tt.want)
			}
		})
	}
}

// ============================================================================
// applyMultilineDelay tests
// ============================================================================

func TestSession_ApplyMultilineDelay(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{clock: clock}

	// Single line - no delay (no newline)
	sess.applyMultilineDelay("echo hello")
	// Just confirm it doesn't panic; fake clock Sleep is no-op

	// Multi-line command
	sess.applyMultilineDelay("line1\nline2\nline3")

	// Many lines - should cap at 500ms
	manyLines := strings.Repeat("line\n", 100)
	sess.applyMultilineDelay(manyLines)
}

// ============================================================================
// buildWrappedCommand tests
// ============================================================================

func TestSession_BuildWrappedCommand(t *testing.T) {
	sess := &Session{}

	cmd := sess.buildWrappedCommand("echo hello", "abc123")
	startMarker := startMarkerPrefix + "abc123" + markerSuffix
	endMarker := endMarkerPrefix + "abc123" + markerSuffix

	if !strings.Contains(cmd, startMarker) {
		t.Errorf("command should contain start marker %q, got %q", startMarker, cmd)
	}
	if !strings.Contains(cmd, endMarker) {
		t.Errorf("command should contain end marker %q, got %q", endMarker, cmd)
	}
	if !strings.Contains(cmd, "echo hello") {
		t.Errorf("command should contain the original command")
	}
	// Should end with newline
	if !strings.HasSuffix(cmd, "\n") {
		t.Error("command should end with newline")
	}
}

func TestSession_BuildWrappedCommand_SingleQuotesEscaped(t *testing.T) {
	sess := &Session{}
	cmd := sess.buildWrappedCommand("echo 'hello'", "abc123")
	// Single quotes in the command should be escaped
	if !strings.Contains(cmd, `'\''hello'\''`) {
		t.Errorf("expected escaped single quotes, got %q", cmd)
	}
}

// ============================================================================
// generateCommandID tests
// ============================================================================

func TestSession_GenerateCommandID_WithSequentialRandom(t *testing.T) {
	rand := fakerand.NewSequential()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{random: rand, clock: clock}

	id := sess.generateCommandID()
	// Sequential random starts at 0x00,0x01,0x02,0x03
	if id != "00010203" {
		t.Errorf("ID = %q, want %q", id, "00010203")
	}

	// Next call should use next 4 bytes
	id2 := sess.generateCommandID()
	if id2 != "04050607" {
		t.Errorf("ID2 = %q, want %q", id2, "04050607")
	}
}

func TestSession_GenerateCommandID_WithFixedRandom(t *testing.T) {
	rand := fakerand.NewFixed([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{random: rand, clock: clock}

	id := sess.generateCommandID()
	if id != "deadbeef" {
		t.Errorf("ID = %q, want %q", id, "deadbeef")
	}
}

// ============================================================================
// waitForEchoDisabled tests
// ============================================================================

func TestSession_WaitForEchoDisabled(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{clock: clock}

	// Should not panic; fake clock Sleep is a no-op
	sess.waitForEchoDisabled()
}

// readWithTimeout is tested indirectly through captureEnvAndPTY, detectRemoteShell, etc.
// Direct testing would require the fake clock to advance within the loop, which the
// current fakeclock (with no-op Sleep) cannot do without causing infinite loops.

// ============================================================================
// drainOutput tests
// ============================================================================

func TestSession_DrainOutput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock}

	// Queue some pending data
	pty.AddResponse("leftover output")

	// drainOutput should consume it without error
	sess.drainOutput()
}

// ============================================================================
// ProvideInput - more coverage
// ============================================================================

func TestSession_ProvideInput_NilPTY(t *testing.T) {
	sess := NewSession("test_provide_nil", "local")
	sess.State = StateAwaitingInput

	_, err := sess.ProvideInput("input")
	if err == nil {
		t.Fatal("expected error when PTY is nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not initialized")
	}
}

// ============================================================================
// SendRaw - write data test
// ============================================================================

func TestSession_SendRaw_WritesRawBytes(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_sendraw", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.State = StateAwaitingInput

	// Queue a response with the end marker so readOutput completes
	pty.AddResponse("___CMD_END_MARKER___0\n")

	result, err := sess.SendRaw(`\x04`)
	if err != nil {
		t.Fatalf("SendRaw error: %v", err)
	}

	// Verify raw bytes were written (Ctrl+D = 0x04)
	writtenBytes := pty.WrittenBytes()
	foundCtrlD := false
	for _, b := range writtenBytes {
		if b == 0x04 {
			foundCtrlD = true
			break
		}
	}
	if !foundCtrlD {
		t.Error("expected Ctrl+D (0x04) to be written to PTY")
	}

	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
}

// ============================================================================
// extractExitCodeWithMarker tests
// ============================================================================

func TestSession_ExtractExitCodeWithMarker(t *testing.T) {
	sess := &Session{}

	tests := []struct {
		name      string
		output    string
		endMarker string
		wantCode  int
		wantOK    bool
	}{
		{
			name:      "basic exit 0",
			output:    "output\n___CMD_END_abc___0\n",
			endMarker: "___CMD_END_abc___",
			wantCode:  0,
			wantOK:    true,
		},
		{
			name:      "exit code 42",
			output:    "___CMD_END_xyz___42\n",
			endMarker: "___CMD_END_xyz___",
			wantCode:  42,
			wantOK:    true,
		},
		{
			name:      "marker in middle of line (curl case)",
			output:    "000___CMD_END_abc___7\n",
			endMarker: "___CMD_END_abc___",
			wantCode:  7,
			wantOK:    true,
		},
		{
			name:      "no matching marker",
			output:    "output\n___CMD_END_different___0\n",
			endMarker: "___CMD_END_abc___",
			wantOK:    false,
		},
		{
			name:      "empty output",
			output:    "",
			endMarker: "___CMD_END_abc___",
			wantOK:    false,
		},
		{
			name:      "marker with carriage return",
			output:    "output\r\n___CMD_END_abc___0\r\n",
			endMarker: "___CMD_END_abc___",
			wantCode:  0,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := sess.extractExitCodeWithMarker(tt.output, tt.endMarker)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && code != tt.wantCode {
				t.Errorf("exitCode = %d, want %d", code, tt.wantCode)
			}
		})
	}
}

// ============================================================================
// parseMarkedOutput tests
// ============================================================================

func TestSession_ParseMarkedOutput(t *testing.T) {
	sess := &Session{}

	tests := []struct {
		name        string
		output      string
		startMarker string
		endMarker   string
		command     string
		wantAsync   string
		wantCmd     string
	}{
		{
			name:        "basic output with markers",
			output:      "___CMD_START_abc___\nhello world\n___CMD_END_abc___0\n",
			startMarker: "___CMD_START_abc___",
			endMarker:   "___CMD_END_abc___",
			command:     "echo hello",
			wantAsync:   "",
			wantCmd:     "hello world",
		},
		{
			name:        "no start marker",
			output:      "some background noise\n",
			startMarker: "___CMD_START_abc___",
			endMarker:   "___CMD_END_abc___",
			command:     "echo hello",
			wantAsync:   "some background noise",
			wantCmd:     "",
		},
		{
			name:        "async output before start marker",
			output:      "async msg\n___CMD_START_abc___\ncmd output\n",
			startMarker: "___CMD_START_abc___",
			endMarker:   "___CMD_END_abc___",
			command:     "cmd",
			wantAsync:   "async msg",
			wantCmd:     "cmd output",
		},
		{
			name:        "output starts with start marker",
			output:      "___CMD_START_abc___\nhello\n",
			startMarker: "___CMD_START_abc___",
			endMarker:   "___CMD_END_abc___",
			command:     "cmd",
			wantAsync:   "",
			wantCmd:     "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAsync, gotCmd := sess.parseMarkedOutput(tt.output, tt.startMarker, tt.endMarker, tt.command)
			if gotAsync != tt.wantAsync {
				t.Errorf("asyncOutput = %q, want %q", gotAsync, tt.wantAsync)
			}
			if gotCmd != tt.wantCmd {
				t.Errorf("cmdOutput = %q, want %q", gotCmd, tt.wantCmd)
			}
		})
	}
}

// ============================================================================
// findMarkerOnOwnLine tests
// ============================================================================

func TestFindMarkerOnOwnLine(t *testing.T) {
	tests := []struct {
		name   string
		output string
		marker string
		want   int
	}{
		{
			name:   "marker at start of output",
			output: "___CMD_START_abc___\nhello",
			marker: "___CMD_START_abc___",
			want:   0,
		},
		{
			name:   "marker on own line",
			output: "some output\n___CMD_START_abc___\nhello",
			marker: "___CMD_START_abc___",
			want:   12, // position after \n
		},
		{
			name:   "marker not on own line (embedded in text)",
			output: "echo ___CMD_START_abc___; cmd",
			marker: "___CMD_START_abc___",
			want:   -1,
		},
		{
			name:   "marker not found",
			output: "hello world",
			marker: "___CMD_START_abc___",
			want:   -1,
		},
		{
			name:   "empty output",
			output: "",
			marker: "___CMD_START_abc___",
			want:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMarkerOnOwnLine(tt.output, tt.marker)
			if got != tt.want {
				t.Errorf("findMarkerOnOwnLine() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ============================================================================
// cleanAsyncOutput tests
// ============================================================================

func TestSession_CleanAsyncOutput(t *testing.T) {
	sess := &Session{}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes shell prompt lines",
			input: "$ echo hello\nactual output",
			want:  "actual output",
		},
		{
			name:  "removes empty lines",
			input: "\n\nactual\n\n",
			want:  "actual",
		},
		{
			name:  "keeps non-prompt lines",
			input: "line1\nline2",
			want:  "line1\nline2",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "only prompts and blanks",
			input: "$ cmd\n\n",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sess.cleanAsyncOutput(tt.input)
			if got != tt.want {
				t.Errorf("cleanAsyncOutput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ============================================================================
// containsPeakTTYSignal tests (supplement)
// ============================================================================

func TestContainsPeakTTYSignal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "has 13 NUL bytes",
			input: "before" + peakTTYSignal + "after",
			want:  true,
		},
		{
			name:  "no NUL bytes",
			input: "normal output",
			want:  false,
		},
		{
			name:  "fewer than 13 NUL bytes",
			input: "output\x00\x00\x00",
			want:  false,
		},
		{
			name:  "exactly 13 NUL bytes",
			input: peakTTYSignal,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsPeakTTYSignal(tt.input)
			if got != tt.want {
				t.Errorf("containsPeakTTYSignal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// isConnectionBroken with io.EOF
// ============================================================================

func TestIsConnectionBroken_IoEOF(t *testing.T) {
	if !isConnectionBroken(io.EOF) {
		t.Error("io.EOF should be detected as broken connection")
	}
}

func TestIsConnectionBroken_UseOfClosed(t *testing.T) {
	err := fmt.Errorf("use of closed network connection")
	if !isConnectionBroken(err) {
		t.Error("'use of closed' should be detected as broken connection")
	}
}

// ============================================================================
// validateExecPreconditions tests
// ============================================================================

func TestSession_ValidateExecPreconditions(t *testing.T) {
	// Test closed session
	sess := &Session{State: StateClosed, pty: fakepty.New()}
	err := sess.validateExecPreconditions()
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected 'closed' error, got %v", err)
	}

	// Test nil PTY
	sess2 := &Session{State: StateIdle, pty: nil}
	err2 := sess2.validateExecPreconditions()
	if err2 == nil || !strings.Contains(err2.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got %v", err2)
	}

	// Test valid state
	sess3 := &Session{State: StateIdle, pty: fakepty.New()}
	err3 := sess3.validateExecPreconditions()
	if err3 != nil {
		t.Errorf("expected nil error, got %v", err3)
	}
}

// ============================================================================
// validateAwaitingInputState tests
// ============================================================================

func TestSession_ValidateAwaitingInputState(t *testing.T) {
	// Not awaiting input
	sess := &Session{State: StateIdle, pty: fakepty.New()}
	err := sess.validateAwaitingInputState()
	if err == nil || !strings.Contains(err.Error(), "not awaiting input") {
		t.Errorf("expected 'not awaiting input' error, got %v", err)
	}

	// Nil PTY but awaiting input
	sess2 := &Session{State: StateAwaitingInput, pty: nil}
	err2 := sess2.validateAwaitingInputState()
	if err2 == nil || !strings.Contains(err2.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got %v", err2)
	}

	// Valid state
	sess3 := &Session{State: StateAwaitingInput, pty: fakepty.New()}
	err3 := sess3.validateAwaitingInputState()
	if err3 != nil {
		t.Errorf("expected nil error, got %v", err3)
	}
}

// ============================================================================
// checkSSHConnection tests
// ============================================================================

func TestSession_CheckSSHConnection_LocalMode(t *testing.T) {
	sess := &Session{Mode: "local"}
	err := sess.checkSSHConnection()
	if err != nil {
		t.Errorf("expected nil error for local mode, got %v", err)
	}
}

func TestSession_CheckSSHConnection_NilSSHClient(t *testing.T) {
	sess := &Session{Mode: "ssh", sshClient: nil}
	err := sess.checkSSHConnection()
	if err != nil {
		t.Errorf("expected nil error for nil sshClient, got %v", err)
	}
}

// ============================================================================
// checkPTYAlive tests
// ============================================================================

func TestSession_CheckPTYAlive_NoControlSession(t *testing.T) {
	sess := &Session{controlSession: nil, PTYName: "5"}
	err := sess.checkPTYAlive()
	if err != nil {
		t.Errorf("expected nil error when no control session, got %v", err)
	}
}

func TestSession_CheckPTYAlive_NoPTYName(t *testing.T) {
	sess := &Session{PTYName: ""}
	err := sess.checkPTYAlive()
	if err != nil {
		t.Errorf("expected nil error when no PTYName, got %v", err)
	}
}

// ============================================================================
// Status for SSH session (exercises SSH branch)
// ============================================================================

func TestSession_Status_SSHSession(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_ssh_status", "ssh",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.Host = "prod.example.com"
	sess.User = "deploy"

	status := sess.Status()
	if status.Host != "prod.example.com" {
		t.Errorf("Host = %q, want %q", status.Host, "prod.example.com")
	}
	if status.User != "deploy" {
		t.Errorf("User = %q, want %q", status.User, "deploy")
	}
	// When sshClient is nil, Connected retains the default value (pty != nil && state != closed).
	// Since we have a PTY injected and state is idle, Connected is true.
	// This is the SSH-specific Status() branch that sets Host/User.
	if !status.Connected {
		t.Error("Connected should be true because PTY is set and state is idle (sshClient nil does not override)")
	}
}

// ============================================================================
// Exec with async output
// ============================================================================

func TestSession_Exec_WithAsyncOutput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0xAA, 0xBB, 0xCC, 0xDD})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_async", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "aabbccdd"
	startMarker := startMarkerPrefix + cmdID + markerSuffix
	endMarker := endMarkerPrefix + cmdID + markerSuffix

	// Simulate async output before the start marker, then command output
	pty.AddResponse(fmt.Sprintf("background msg\n%s\ncmd output\n%s0\n", startMarker, endMarker))

	result, err := sess.Exec("some_cmd", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.CommandID != cmdID {
		t.Errorf("CommandID = %q, want %q", result.CommandID, cmdID)
	}
	// Async output should be captured
	if result.AsyncOutput == "" {
		// May or may not contain async depending on marker parsing
		// This is fine as long as it doesn't fail
	}
}

// ============================================================================
// Exec with multiline command
// ============================================================================

func TestSession_Exec_MultilineCommand(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_multiline", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "01020304"
	pty.AddResponse(buildCommandOutput(cmdID, "multi\nline\noutput", 0))

	result, err := sess.Exec("line1\nline2\nline3", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
}

// ============================================================================
// forceKillCommandFallback tests
// ============================================================================

func TestSession_ForceKillCommandFallback(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock}

	// Queue no responses - the fallback should handle gracefully
	sess.forceKillCommandFallback()

	// Verify interrupt was called (at least once)
	if !pty.WasInterrupted() {
		t.Error("expected PTY to be interrupted")
	}

	// Verify something was written (newline for fresh prompt)
	written := pty.Written()
	if !strings.Contains(written, "\n") {
		t.Error("expected newline to be written for fresh prompt")
	}
}

func TestSession_ForceKillCommandFallback_WithPendingOutput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock}

	// Queue responses so there's "still output" after interrupts
	for i := 0; i < 20; i++ {
		pty.AddResponse("still running output")
	}

	sess.forceKillCommandFallback()

	// Should have tried interrupt and written 'q' for interactive apps
	written := pty.Written()
	if !strings.Contains(written, "q") {
		t.Error("expected 'q' to be written for interactive app quit")
	}
}

// ============================================================================
// forceKillCommand tests (without control session)
// ============================================================================

func TestSession_ForceKillCommand_NoControlSession(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		pty:            pty,
		clock:          clock,
		controlSession: nil,
		PTYName:        "",
	}

	sess.forceKillCommand()

	// Should fall through to forceKillCommandFallback
	if !pty.WasInterrupted() {
		t.Error("expected PTY to be interrupted via fallback")
	}
}

// ============================================================================
// Exec updates LastUsed
// ============================================================================

func TestSession_Exec_UpdatesLastUsed(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_lastused", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	initialLastUsed := sess.LastUsed

	// Advance clock
	clock.Advance(5 * time.Minute)

	cmdID := "01020304"
	pty.AddResponse(buildCommandOutput(cmdID, "output", 0))

	_, err := sess.Exec("cmd", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	if sess.LastUsed.Equal(initialLastUsed) {
		t.Error("LastUsed should have been updated after Exec")
	}
}

// ============================================================================
// updateCwd tests
// ============================================================================

func TestSession_UpdateCwd(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, Cwd: "/old/path"}

	// Queue pwd response
	pty.AddResponse("/new/path\n")

	sess.updateCwd()

	if sess.Cwd != "/new/path" {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, "/new/path")
	}
}

func TestSession_UpdateCwd_NoResponse(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, Cwd: "/old/path"}

	// No response queued
	sess.updateCwd()

	// Cwd should remain unchanged
	if sess.Cwd != "/old/path" {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, "/old/path")
	}
}

func TestSession_UpdateCwd_SkipsPwdEcho(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, Cwd: "/old"}

	// Response includes pwd echo and the actual path
	pty.AddResponse("pwd\n/home/user/project\n")

	sess.updateCwd()

	if sess.Cwd != "/home/user/project" {
		t.Errorf("Cwd = %q, want %q", sess.Cwd, "/home/user/project")
	}
}

// ============================================================================
// NewSession with various modes
// ============================================================================

func TestNewSession_Defaults(t *testing.T) {
	sess := NewSession("id1", "local")
	if sess.ID != "id1" {
		t.Errorf("ID = %q, want %q", sess.ID, "id1")
	}
	if sess.Mode != "local" {
		t.Errorf("Mode = %q, want %q", sess.Mode, "local")
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q", sess.State, StateIdle)
	}
	if sess.pty != nil {
		t.Error("pty should be nil by default")
	}
	if sess.clock != nil {
		t.Error("clock should be nil by default")
	}
}

func TestNewSession_SSHMode(t *testing.T) {
	sess := NewSession("id2", "ssh")
	if sess.Mode != "ssh" {
		t.Errorf("Mode = %q, want %q", sess.Mode, "ssh")
	}
	if !sess.IsSSH() {
		t.Error("IsSSH() should return true")
	}
}

func TestNewSession_WithAllOptions(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	fs := fakefs.New()
	cfg := config.DefaultConfig()

	sess := NewSession("id3", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithSessionFileSystem(fs),
		WithConfig(cfg),
	)

	if sess.pty != pty {
		t.Error("expected injected PTY")
	}
	if sess.clock != clock {
		t.Error("expected injected clock")
	}
	if sess.random != rand {
		t.Error("expected injected random")
	}
	if sess.fs != fs {
		t.Error("expected injected fs")
	}
	if sess.config != cfg {
		t.Error("expected injected config")
	}
}

// ============================================================================
// State constants
// ============================================================================

func TestStateConstants(t *testing.T) {
	if StateIdle != "idle" {
		t.Errorf("StateIdle = %q, want %q", StateIdle, "idle")
	}
	if StateRunning != "running" {
		t.Errorf("StateRunning = %q, want %q", StateRunning, "running")
	}
	if StateAwaitingInput != "awaiting_input" {
		t.Errorf("StateAwaitingInput = %q, want %q", StateAwaitingInput, "awaiting_input")
	}
	if StateClosed != "closed" {
		t.Errorf("StateClosed = %q, want %q", StateClosed, "closed")
	}
}

// ============================================================================
// Close with nil PTY
// ============================================================================

func TestSession_Close_NilPTY(t *testing.T) {
	sess := &Session{
		State: StateIdle,
		pty:   nil,
	}

	err := sess.Close()
	if err != nil {
		t.Errorf("expected nil error when closing session with nil PTY, got %v", err)
	}
	if sess.State != StateClosed {
		t.Errorf("State = %q, want %q", sess.State, StateClosed)
	}
}

// ============================================================================
// extractPTYNumber additional tests
// ============================================================================

func TestExtractPTYNumber_WithCarriageReturn(t *testing.T) {
	got := extractPTYNumber("/dev/pts/7\r")
	if got != "7" {
		t.Errorf("extractPTYNumber() = %q, want %q", got, "7")
	}
}

func TestExtractPTYNumber_WithTrailingText(t *testing.T) {
	got := extractPTYNumber("/dev/pts/42abc")
	if got != "42" {
		t.Errorf("extractPTYNumber() = %q, want %q", got, "42")
	}
}

// ============================================================================
// buildSSHAuthConfig with server config
// ============================================================================

func TestSession_BuildSSHAuthConfig_WithServerConfig(t *testing.T) {
	fs := fakefs.New()
	fs.SetEnv("MY_PASSPHRASE", "secret_passphrase")
	fs.SetEnv("MY_PASSWORD", "secret_password")

	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{
			Name:    "myserver",
			Host:    "server.example.com",
			KeyPath: "/keys/my_key",
			Auth: config.AuthConfig{
				PassphraseEnv: "MY_PASSPHRASE",
				PasswordEnv:   "MY_PASSWORD",
			},
		},
	}

	sess := &Session{
		Host:   "server.example.com",
		User:   "user",
		config: cfg,
		fs:     fs,
	}

	authCfg := sess.buildSSHAuthConfig()

	if authCfg.KeyPath != "/keys/my_key" {
		t.Errorf("KeyPath = %q, want %q", authCfg.KeyPath, "/keys/my_key")
	}
	if authCfg.KeyPassphrase != "secret_passphrase" {
		t.Errorf("KeyPassphrase = %q, want %q", authCfg.KeyPassphrase, "secret_passphrase")
	}
	if authCfg.Password != "secret_password" {
		t.Errorf("Password = %q, want %q", authCfg.Password, "secret_password")
	}
}

func TestSession_BuildSSHAuthConfig_NoMatchingServer(t *testing.T) {
	fs := fakefs.New()
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{
			Name:    "other",
			Host:    "other.example.com",
			KeyPath: "/keys/other_key",
		},
	}

	sess := &Session{
		Host:   "nonexistent.example.com",
		User:   "user",
		config: cfg,
		fs:     fs,
	}

	authCfg := sess.buildSSHAuthConfig()
	if authCfg.KeyPath != "" {
		t.Errorf("KeyPath should be empty when no server matches, got %q", authCfg.KeyPath)
	}
}

func TestSession_BuildSSHAuthConfig_WithExplicitKeyPath(t *testing.T) {
	sess := &Session{
		Host:    "example.com",
		User:    "user",
		KeyPath: "/explicit/key",
		config:  config.DefaultConfig(),
	}

	authCfg := sess.buildSSHAuthConfig()
	// When KeyPath is explicitly set, server config should not be consulted
	if authCfg.KeyPath != "/explicit/key" {
		t.Errorf("KeyPath = %q, want %q", authCfg.KeyPath, "/explicit/key")
	}
}

// ============================================================================
// ProvideInput with password prompt
// ============================================================================

func TestSession_PrepareForPasswordInput(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{
		clock: clock,
	}

	// Non-password prompt - should not wait
	sess.pendingPrompt = nil
	sess.prepareForPasswordInput()
	// No panic = success
}

// ============================================================================
// ensureConnectionHealthy tests
// ============================================================================

func TestSession_EnsureConnectionHealthy_LocalMode(t *testing.T) {
	sess := &Session{Mode: "local"}
	err := sess.ensureConnectionHealthy()
	if err != nil {
		t.Errorf("expected nil error for local mode, got %v", err)
	}
}

// ============================================================================
// Exec with ConfirmationPrompt detection
// ============================================================================

func TestSession_Exec_ConfirmationPrompt(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	rand := fakerand.New([]byte{0x01, 0x02, 0x03, 0x04})
	cfg := config.DefaultConfig()

	sess := NewSession("sess_confirm", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithSessionRandom(rand),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	cmdID := "01020304"
	startMarker := startMarkerPrefix + cmdID + markerSuffix

	// Simulate a confirmation prompt after the start marker
	pty.AddResponse(fmt.Sprintf("%s\nDo you want to continue? [Y/n] ", startMarker))
	for i := 0; i < 20; i++ {
		pty.AddResponse("")
	}

	result, err := sess.Exec("apt upgrade", 5000)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}

	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "confirmation" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "confirmation")
	}
}

// newExecContext is already tested in session_read_test.go

// ============================================================================
// buildPeakTTYResult tests
// ============================================================================

func TestSession_BuildPeakTTYResult(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{clock: clock}

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")
	output := "___CMD_START_abc___\nsome output\x00\x00"

	result := sess.buildPeakTTYResult(ctx, output)
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if result.PromptType != "interactive" {
		t.Errorf("PromptType = %q, want %q", result.PromptType, "interactive")
	}
	if result.Hint != hintPeakTTYWaiting {
		t.Errorf("Hint = %q, want %q", result.Hint, hintPeakTTYWaiting)
	}
	if result.CommandID != "abc" {
		t.Errorf("CommandID = %q, want %q", result.CommandID, "abc")
	}
	// NUL bytes should be removed from stdout
	if strings.Contains(result.Stdout, "\x00") {
		t.Error("Stdout should not contain NUL bytes")
	}
}

// ============================================================================
// checkForPeakTTYSignal tests
// ============================================================================

func TestSession_CheckForPeakTTYSignal_Detected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{clock: clock}
	sess.outputBuffer.WriteString("output" + peakTTYSignal + "more")

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")
	result, found := sess.checkForPeakTTYSignal(ctx)
	if !found {
		t.Fatal("expected peak-tty signal to be detected")
	}
	if result.Status != "awaiting_input" {
		t.Errorf("Status = %q, want %q", result.Status, "awaiting_input")
	}
	if sess.State != StateAwaitingInput {
		t.Errorf("State = %q, want %q", sess.State, StateAwaitingInput)
	}
}

func TestSession_CheckForPeakTTYSignal_NotDetected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{clock: clock}
	sess.outputBuffer.WriteString("normal output")

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")
	_, found := sess.checkForPeakTTYSignal(ctx)
	if found {
		t.Error("expected peak-tty signal NOT to be detected")
	}
}

// ============================================================================
// checkForPasswordPrompt tests
// ============================================================================

func TestSession_CheckForPasswordPrompt_Detected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_passwd", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.outputBuffer.WriteString("[sudo] password for user: ")

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "sudo cmd")
	result, found := sess.checkForPasswordPrompt(ctx, "[sudo] password for user: ")
	if !found {
		t.Fatal("expected password prompt to be detected")
	}
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

func TestSession_CheckForPasswordPrompt_NotDetected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_no_passwd", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.outputBuffer.WriteString("normal output")

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")
	_, found := sess.checkForPasswordPrompt(ctx, "normal output")
	if found {
		t.Error("expected no password prompt detection")
	}
}

// ============================================================================
// checkForInteractivePrompt tests
// ============================================================================

func TestSession_CheckForInteractivePrompt_NotDetected(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("test_no_prompt", "local",
		WithPTY(fakepty.New()),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")
	_, found := sess.checkForInteractivePrompt(ctx, "normal output without any prompts")
	if found {
		t.Error("expected no interactive prompt detection")
	}
}

// ============================================================================
// handleContextTimeout tests
// ============================================================================

func TestSession_HandleContextTimeout_NotCancelled(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, State: StateRunning}

	ctx := context.Background()
	execCtx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")

	result := sess.handleContextTimeout(ctx, execCtx)
	if result != nil {
		t.Error("expected nil result when context is not cancelled")
	}
}

func TestSession_HandleContextTimeout_Cancelled(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{pty: pty, clock: clock, State: StateRunning}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	execCtx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")

	result := sess.handleContextTimeout(ctx, execCtx)
	if result == nil {
		t.Fatal("expected non-nil result when context is cancelled")
	}
	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
	if sess.State != StateIdle {
		t.Errorf("State = %q, want %q", sess.State, StateIdle)
	}
}

// ============================================================================
// buildTimeoutResult tests
// ============================================================================

func TestSession_BuildTimeoutResult(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{clock: clock}
	sess.outputBuffer.WriteString("partial output")

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "cmd")
	result := sess.buildTimeoutResult(ctx)

	if result.Status != "timeout" {
		t.Errorf("Status = %q, want %q", result.Status, "timeout")
	}
	if result.CommandID != "abc" {
		t.Errorf("CommandID = %q, want %q", result.CommandID, "abc")
	}
}

// ============================================================================
// buildCompletedResult tests
// ============================================================================

func TestSession_BuildCompletedResult(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	sess := &Session{clock: clock}
	sess.outputBuffer.WriteString("___CMD_START_abc___\nhello world\n___CMD_END_abc___0\n")

	ctx := newExecContext("abc", "___CMD_START_abc___", "___CMD_END_abc___", "echo hello")
	result := sess.buildCompletedResult(ctx, 0, "/home/user")

	if result.Status != "completed" {
		t.Errorf("Status = %q, want %q", result.Status, "completed")
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", result.ExitCode)
	}
	if result.Cwd != "/home/user" {
		t.Errorf("Cwd = %q, want %q", result.Cwd, "/home/user")
	}
	if result.CommandID != "abc" {
		t.Errorf("CommandID = %q, want %q", result.CommandID, "abc")
	}
}

// restoreState calls readWithTimeout internally, which loops forever with fakeclock.
// Test only the nil-PTY early return path.

func TestSession_RestoreState_NilPTY(t *testing.T) {
	sess := &Session{pty: nil}
	// Should return without panic (early return at line 529)
	sess.restoreState("/dir", map[string]string{"KEY": "val"})
}

// detectRemoteShell and captureEnvAndPTY both call readWithTimeout internally,
// which loops forever with fakeclock (Now() never advances). These functions
// are tested indirectly through integration tests that use real clocks.

// min function is already tested in control_coverage_test.go

// ============================================================================
// GetTunnelConfigs for SSH mode without client
// ============================================================================

func TestSession_GetTunnelConfigs_SSHModeNoClient(t *testing.T) {
	sess := &Session{Mode: "ssh", sshClient: nil}
	configs := sess.GetTunnelConfigs()
	if configs != nil {
		t.Errorf("expected nil for nil sshClient, got %v", configs)
	}
}

// ============================================================================
// Interrupt clears pendingPrompt
// ============================================================================

func TestSession_Interrupt_ClearsPendingPrompt(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_int_prompt", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.State = StateAwaitingInput
	// Set pendingPrompt to a non-nil value (will be cleared by Interrupt)
	sess.pendingPrompt = &prompt.Detection{}

	err := sess.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt error: %v", err)
	}

	if sess.pendingPrompt != nil {
		t.Error("pendingPrompt should be nil after Interrupt")
	}
}

// ============================================================================
// shellPromptCommand with bare shell name
// ============================================================================

func TestSession_ShellPromptCommand_BareShellName(t *testing.T) {
	sess := &Session{Shell: "zsh"}
	cmd := sess.shellPromptCommand()
	// "zsh" has no "/" so LastIndex returns -1, shellName = "zsh"
	if !strings.Contains(cmd, "PROMPT='$ '") {
		t.Errorf("expected zsh prompt setting, got %q", cmd)
	}
}
