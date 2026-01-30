package expect

import (
	"testing"
)

func TestScript_Compile(t *testing.T) {
	script := &Script{
		Name:           "test",
		CommandPattern: `^npm init`,
		Steps: []Step{
			{
				Name:    "step1",
				Pattern: `package name:`,
			},
		},
	}

	if err := script.Compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if script.CompiledCommandPattern == nil {
		t.Error("CompiledCommandPattern should not be nil")
	}

	if script.Steps[0].CompiledPattern == nil {
		t.Error("Step CompiledPattern should not be nil")
	}
}

func TestScript_MatchesCommand(t *testing.T) {
	script := &Script{
		Name:           "npm_init",
		Command:        "npm init",
		CommandPattern: `^npm init(\s|$)`,
	}
	_ = script.Compile()

	tests := []struct {
		cmd      string
		expected bool
	}{
		{"npm init", true},
		{"npm init --yes", true},
		{"npm install", false},
		{"npm", false},
		{"npm init-something", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := script.MatchesCommand(tt.cmd); got != tt.expected {
				t.Errorf("MatchesCommand(%q) = %v, want %v", tt.cmd, got, tt.expected)
			}
		})
	}
}

func TestDefaultScripts(t *testing.T) {
	scripts := DefaultScripts()

	if len(scripts) == 0 {
		t.Fatal("DefaultScripts() returned empty list")
	}

	expectedScripts := []string{
		"npm_init",
		"git_rebase_continue",
		"apt_upgrade",
		"ssh_host_key",
		"sudo_password",
	}

	for _, name := range expectedScripts {
		found := false
		for _, s := range scripts {
			if s.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected script %q not found", name)
		}
	}
}

func TestRunner_FindScript(t *testing.T) {
	runner := NewDefaultRunner()

	tests := []struct {
		command      string
		expectedName string
	}{
		{"npm init", "npm_init"},
		{"npm init --yes", "npm_init"},
		{"git rebase main", "git_rebase_continue"},
		{"sudo apt upgrade", "apt_upgrade"},
		{"ssh user@host", "ssh_host_key"},
		{"sudo su", "sudo_password"},
		{"ls -la", ""},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			script := runner.FindScript(tt.command)
			if tt.expectedName == "" {
				if script != nil {
					t.Errorf("FindScript(%q) = %v, want nil", tt.command, script.Name)
				}
			} else {
				if script == nil {
					t.Errorf("FindScript(%q) = nil, want %q", tt.command, tt.expectedName)
				} else if script.Name != tt.expectedName {
					t.Errorf("FindScript(%q) = %q, want %q", tt.command, script.Name, tt.expectedName)
				}
			}
		})
	}
}

func TestRunner_MatchOutput(t *testing.T) {
	runner := NewDefaultRunner()
	script := runner.FindScript("npm init")
	state := runner.StartScript(script)

	// Test matching first step
	match := runner.MatchOutput(state, "package name: (my-package)")
	if match == nil {
		t.Fatal("Expected match for package name prompt")
	}
	if match.Step.Name != "package_name" {
		t.Errorf("Expected step 'package_name', got %q", match.Step.Name)
	}

	// Advance state
	runner.AdvanceState(state, match)

	// Test matching second step
	match = runner.MatchOutput(state, "version: (1.0.0)")
	if match == nil {
		t.Fatal("Expected match for version prompt")
	}
	if match.Step.Name != "version" {
		t.Errorf("Expected step 'version', got %q", match.Step.Name)
	}
}

func TestRunner_RepeatStep(t *testing.T) {
	script := &Script{
		Name: "test_repeat",
		Steps: []Step{
			{
				Name:       "repeat_step",
				Pattern:    `Do you want to continue\?`,
				Response:   "y",
				Repeat:     true,
				MaxRepeats: 3,
			},
		},
	}
	_ = script.Compile()

	runner := NewRunner([]*Script{script})
	state := runner.StartScript(script)

	// Match three times
	for i := 0; i < 3; i++ {
		match := runner.MatchOutput(state, "Do you want to continue?")
		if match == nil {
			t.Fatalf("Expected match on repeat %d", i+1)
		}
		runner.AdvanceState(state, match)
	}

	// Should be completed after max repeats
	if !state.Completed {
		t.Error("Expected script to be completed after max repeats")
	}
}

func TestManager_SessionWorkflow(t *testing.T) {
	manager := NewManager()

	sessionID := "test_session"
	command := "npm init"

	// Start script detection
	state := manager.StartForSession(sessionID, command)
	if state == nil {
		t.Fatal("Expected state for npm init command")
	}

	// Process output
	match := manager.ProcessOutput(sessionID, "package name: (test)")
	if match == nil {
		t.Fatal("Expected match for package name prompt")
	}

	// Verify state was retrieved
	state = manager.GetState(sessionID)
	if state == nil {
		t.Fatal("Expected state to persist")
	}

	// End session
	manager.EndSession(sessionID)

	// Verify state is gone
	state = manager.GetState(sessionID)
	if state != nil {
		t.Error("Expected state to be removed after EndSession")
	}
}
