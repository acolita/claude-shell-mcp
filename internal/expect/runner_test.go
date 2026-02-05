package expect

import (
	"context"
	"testing"
	"time"
)

// --- Runner.AddScript tests ---

func TestRunner_AddScript_Valid(t *testing.T) {
	runner := NewRunner(nil)

	script := &Script{
		Name:           "custom_script",
		CommandPattern: `^my-command`,
		Steps: []Step{
			{
				Name:    "step1",
				Pattern: `prompt:`,
			},
		},
	}

	err := runner.AddScript(script)
	if err != nil {
		t.Fatalf("AddScript() returned unexpected error: %v", err)
	}

	// Verify the script is findable
	found := runner.FindScript("my-command --flag")
	if found == nil {
		t.Fatal("Expected to find added script via FindScript")
	}
	if found.Name != "custom_script" {
		t.Errorf("FindScript returned script %q, want %q", found.Name, "custom_script")
	}

	// Verify patterns were compiled
	if script.CompiledCommandPattern == nil {
		t.Error("Expected CompiledCommandPattern to be set after AddScript")
	}
	if script.Steps[0].CompiledPattern == nil {
		t.Error("Expected step CompiledPattern to be set after AddScript")
	}
}

func TestRunner_AddScript_InvalidRegex(t *testing.T) {
	runner := NewRunner(nil)

	script := &Script{
		Name:           "bad_script",
		CommandPattern: `^valid-command`,
		Steps: []Step{
			{
				Name:    "bad_step",
				Pattern: `[invalid(regex`,
			},
		},
	}

	err := runner.AddScript(script)
	if err == nil {
		t.Fatal("AddScript() expected error for invalid regex, got nil")
	}

	// Verify the script was NOT added
	found := runner.FindScript("valid-command")
	if found != nil {
		t.Error("Script with invalid regex should not have been added to runner")
	}
}

func TestRunner_AddScript_InvalidCommandPattern(t *testing.T) {
	runner := NewRunner(nil)

	script := &Script{
		Name:           "bad_cmd_pattern",
		CommandPattern: `[unclosed`,
		Steps: []Step{
			{
				Name:    "step1",
				Pattern: `prompt:`,
			},
		},
	}

	err := runner.AddScript(script)
	if err == nil {
		t.Fatal("AddScript() expected error for invalid command pattern regex, got nil")
	}
}

// --- MatchOutput tests ---

func TestRunner_MatchOutput_NilState(t *testing.T) {
	runner := NewRunner(nil)

	match := runner.MatchOutput(nil, "some output")
	if match != nil {
		t.Error("MatchOutput(nil state) should return nil")
	}
}

func TestRunner_MatchOutput_NilScript(t *testing.T) {
	runner := NewRunner(nil)

	state := &RunState{
		Script: nil,
	}

	match := runner.MatchOutput(state, "some output")
	if match != nil {
		t.Error("MatchOutput(nil script) should return nil")
	}
}

func TestRunner_MatchOutput_CompletedState(t *testing.T) {
	script := &Script{
		Name: "test",
		Steps: []Step{
			{Name: "s1", Pattern: `hello`},
		},
	}
	_ = script.Compile()
	runner := NewRunner([]*Script{script})

	state := &RunState{
		Script:    script,
		Completed: true,
	}

	match := runner.MatchOutput(state, "hello")
	if match != nil {
		t.Error("MatchOutput on completed state should return nil")
	}
}

func TestRunner_MatchOutput_AbortedState(t *testing.T) {
	script := &Script{
		Name: "test",
		Steps: []Step{
			{Name: "s1", Pattern: `hello`},
		},
	}
	_ = script.Compile()
	runner := NewRunner([]*Script{script})

	state := &RunState{
		Script:  script,
		Aborted: true,
	}

	match := runner.MatchOutput(state, "hello")
	if match != nil {
		t.Error("MatchOutput on aborted state should return nil")
	}
}

func TestRunner_MatchOutput_MatchesCurrentStep(t *testing.T) {
	script := &Script{
		Name: "test",
		Steps: []Step{
			{Name: "first", Pattern: `enter name:`, Response: "alice", Action: ActionSend},
			{Name: "second", Pattern: `enter age:`, Response: "30"},
		},
	}
	_ = script.Compile()
	runner := NewRunner([]*Script{script})

	state := runner.StartScript(script)

	match := runner.MatchOutput(state, "enter name:")
	if match == nil {
		t.Fatal("Expected match for current step")
	}
	if match.Step.Name != "first" {
		t.Errorf("Expected step 'first', got %q", match.Step.Name)
	}
	if match.Response != "alice" {
		t.Errorf("Expected response 'alice', got %q", match.Response)
	}
	if match.Action != ActionSend {
		t.Errorf("Expected ActionSend, got %v", match.Action)
	}
	if match.StepIdx != 0 {
		t.Errorf("Expected StepIdx 0, got %d", match.StepIdx)
	}
}

func TestRunner_MatchOutput_OptionalStepSkipping(t *testing.T) {
	script := &Script{
		Name: "test_optional",
		Steps: []Step{
			{Name: "optional_step", Pattern: `optional prompt:`, Optional: true, Response: "opt"},
			{Name: "required_step", Pattern: `required prompt:`, Response: "req"},
		},
	}
	_ = script.Compile()
	runner := NewRunner([]*Script{script})

	state := runner.StartScript(script)
	// CurrentStep is 0 (optional_step), but we send output matching step 1
	match := runner.MatchOutput(state, "required prompt:")
	if match == nil {
		t.Fatal("Expected match when skipping optional step to reach later step")
	}
	if match.Step.Name != "required_step" {
		t.Errorf("Expected step 'required_step', got %q", match.Step.Name)
	}
	if match.StepIdx != 1 {
		t.Errorf("Expected StepIdx 1, got %d", match.StepIdx)
	}
}

func TestRunner_MatchOutput_NonOptionalStepBlocks(t *testing.T) {
	script := &Script{
		Name: "test_blocking",
		Steps: []Step{
			{Name: "required_first", Pattern: `first prompt:`, Optional: false, Response: "first"},
			{Name: "second_step", Pattern: `second prompt:`, Response: "second"},
		},
	}
	_ = script.Compile()
	runner := NewRunner([]*Script{script})

	state := runner.StartScript(script)
	// CurrentStep is 0 (required_first), output matches step 1 but NOT step 0
	// Since step 0 is not optional, we should NOT skip ahead
	match := runner.MatchOutput(state, "second prompt:")
	if match != nil {
		t.Error("Expected nil when current non-optional step doesn't match and later step does")
	}
}

func TestRunner_MatchOutput_NoMatch(t *testing.T) {
	script := &Script{
		Name: "test",
		Steps: []Step{
			{Name: "step1", Pattern: `expected prompt:`, Response: "response"},
		},
	}
	_ = script.Compile()
	runner := NewRunner([]*Script{script})

	state := runner.StartScript(script)
	match := runner.MatchOutput(state, "completely unrelated output")
	if match != nil {
		t.Error("Expected nil when no step matches the output")
	}
}

func TestRunner_MatchOutput_PastAllSteps(t *testing.T) {
	script := &Script{
		Name: "test",
		Steps: []Step{
			{Name: "only_step", Pattern: `prompt:`, Response: "r"},
		},
	}
	_ = script.Compile()
	runner := NewRunner([]*Script{script})

	state := runner.StartScript(script)
	state.CurrentStep = 1 // past the last step

	match := runner.MatchOutput(state, "prompt:")
	if match != nil {
		t.Error("Expected nil when CurrentStep is past all steps")
	}
}

// --- Script.MatchesCommand tests ---

func TestScript_MatchesCommand_ExactCommand(t *testing.T) {
	script := &Script{
		Name:    "exact",
		Command: "deploy --prod",
	}
	// No pattern, no Compile needed

	if !script.MatchesCommand("deploy --prod") {
		t.Error("Expected exact Command match to return true")
	}
	if script.MatchesCommand("deploy --staging") {
		t.Error("Expected non-matching command to return false")
	}
}

func TestScript_MatchesCommand_PatternOnly(t *testing.T) {
	script := &Script{
		Name:           "pattern_only",
		CommandPattern: `^deploy\s+`,
	}
	_ = script.Compile()

	if !script.MatchesCommand("deploy --prod") {
		t.Error("Expected pattern match to return true")
	}
	if script.MatchesCommand("run deploy") {
		t.Error("Expected non-matching pattern to return false")
	}
}

func TestScript_MatchesCommand_NeitherMatches(t *testing.T) {
	script := &Script{
		Name:           "both",
		Command:        "exact-cmd",
		CommandPattern: `^pattern-cmd`,
	}
	_ = script.Compile()

	if script.MatchesCommand("unrelated") {
		t.Error("Expected false when neither command nor pattern matches")
	}
}

func TestScript_MatchesCommand_NoCommandNoPattern(t *testing.T) {
	script := &Script{
		Name: "empty",
	}

	if script.MatchesCommand("anything") {
		t.Error("Expected false when script has no Command and no CommandPattern")
	}
}

// --- Script.Compile tests ---

func TestScript_Compile_InvalidStepPattern(t *testing.T) {
	script := &Script{
		Name: "bad_step",
		Steps: []Step{
			{
				Name:    "step1",
				Pattern: `[invalid(`,
			},
		},
	}

	err := script.Compile()
	if err == nil {
		t.Fatal("Expected error for invalid step pattern regex")
	}
}

func TestScript_Compile_InvalidCommandPattern(t *testing.T) {
	script := &Script{
		Name:           "bad_cmd",
		CommandPattern: `[invalid(`,
	}

	err := script.Compile()
	if err == nil {
		t.Fatal("Expected error for invalid command pattern regex")
	}
}

func TestScript_Compile_EmptyPatterns(t *testing.T) {
	script := &Script{
		Name: "empty_patterns",
		Steps: []Step{
			{
				Name:    "step1",
				Pattern: "",
			},
		},
	}

	err := script.Compile()
	if err != nil {
		t.Fatalf("Compile() with empty patterns should not error, got: %v", err)
	}
	if script.Steps[0].CompiledPattern != nil {
		t.Error("Expected nil CompiledPattern for empty pattern string")
	}
}

// --- Manager.AddScript tests ---

func TestManager_AddScript_Valid(t *testing.T) {
	manager := NewManager()

	script := &Script{
		Name:           "custom_manager_script",
		CommandPattern: `^custom-cmd`,
		Steps: []Step{
			{Name: "s1", Pattern: `prompt>`, Response: "answer"},
		},
	}

	err := manager.AddScript(script)
	if err != nil {
		t.Fatalf("Manager.AddScript() returned unexpected error: %v", err)
	}

	// Verify the script is available for session matching
	state := manager.StartForSession("test-sess", "custom-cmd --flag")
	if state == nil {
		t.Fatal("Expected script to be findable after Manager.AddScript")
	}
	if state.Script.Name != "custom_manager_script" {
		t.Errorf("Expected script name 'custom_manager_script', got %q", state.Script.Name)
	}
}

func TestManager_AddScript_InvalidRegex(t *testing.T) {
	manager := NewManager()

	script := &Script{
		Name:           "bad_manager_script",
		CommandPattern: `^valid`,
		Steps: []Step{
			{Name: "s1", Pattern: `[broken(`},
		},
	}

	err := manager.AddScript(script)
	if err == nil {
		t.Fatal("Manager.AddScript() expected error for invalid regex, got nil")
	}
}

// --- Manager.WaitForStep tests ---

func TestManager_WaitForStep_NoState(t *testing.T) {
	manager := NewManager()

	ctx := context.Background()
	err := manager.WaitForStep(ctx, "nonexistent-session", "some_step")
	if err == nil {
		t.Fatal("WaitForStep() expected error for session with no state, got nil")
	}
	if err.Error() != "no expect state for session nonexistent-session" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestManager_WaitForStep_ContextCancellation(t *testing.T) {
	manager := NewManager()

	// Create a session with a script that has multiple steps
	script := &Script{
		Name:           "wait_test",
		CommandPattern: `^wait-cmd`,
		Steps: []Step{
			{Name: "step_one", Pattern: `first:`, Response: "1"},
			{Name: "step_two", Pattern: `second:`, Response: "2"},
		},
	}
	_ = script.Compile()
	_ = manager.runner.AddScript(script)

	state := manager.StartForSession("wait-sess", "wait-cmd")
	if state == nil {
		t.Fatal("Expected state for wait-cmd")
	}

	// Create a context that will be cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	// Wait for step_two which hasn't been reached yet (CurrentStep is 0)
	err := manager.WaitForStep(ctx, "wait-sess", "step_two")
	if err == nil {
		t.Fatal("WaitForStep() expected context deadline error, got nil")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestManager_WaitForStep_StepAlreadyPassed(t *testing.T) {
	manager := NewManager()

	script := &Script{
		Name:           "passed_test",
		CommandPattern: `^passed-cmd`,
		Steps: []Step{
			{Name: "step_one", Pattern: `first:`, Response: "1"},
			{Name: "step_two", Pattern: `second:`, Response: "2"},
			{Name: "step_three", Pattern: `third:`, Response: "3"},
		},
	}
	_ = script.Compile()
	_ = manager.runner.AddScript(script)

	manager.StartForSession("passed-sess", "passed-cmd")

	// Process output to advance past step_one and step_two
	manager.ProcessOutput("passed-sess", "first:")
	manager.ProcessOutput("passed-sess", "second:")

	// Now wait for step_one which has already been passed
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := manager.WaitForStep(ctx, "passed-sess", "step_one")
	if err != nil {
		t.Fatalf("WaitForStep() for already-passed step should return nil, got: %v", err)
	}
}

func TestManager_WaitForStep_CompletedScript(t *testing.T) {
	manager := NewManager()

	script := &Script{
		Name:           "complete_test",
		CommandPattern: `^complete-cmd`,
		Steps: []Step{
			{Name: "only_step", Pattern: `prompt:`, Response: "done"},
		},
	}
	_ = script.Compile()
	_ = manager.runner.AddScript(script)

	manager.StartForSession("complete-sess", "complete-cmd")

	// Process output to complete the script
	manager.ProcessOutput("complete-sess", "prompt:")

	// WaitForStep should return nil when script is completed
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := manager.WaitForStep(ctx, "complete-sess", "only_step")
	if err != nil {
		t.Fatalf("WaitForStep() on completed script should return nil, got: %v", err)
	}
}

func TestManager_WaitForStep_StateRemovedMidWait(t *testing.T) {
	manager := NewManager()

	script := &Script{
		Name:           "removal_test",
		CommandPattern: `^removal-cmd`,
		Steps: []Step{
			{Name: "step_one", Pattern: `first:`, Response: "1"},
			{Name: "step_two", Pattern: `second:`, Response: "2"},
		},
	}
	_ = script.Compile()
	_ = manager.runner.AddScript(script)

	manager.StartForSession("removal-sess", "removal-cmd")

	// Remove the session state after a short delay
	go func() {
		time.Sleep(150 * time.Millisecond)
		manager.EndSession("removal-sess")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := manager.WaitForStep(ctx, "removal-sess", "step_two")
	if err == nil {
		t.Fatal("WaitForStep() expected error when state is removed mid-wait, got nil")
	}
	expected := "expect state removed for session removal-sess"
	if err.Error() != expected {
		t.Errorf("Expected error %q, got %q", expected, err.Error())
	}
}
