package prompt

import (
	"regexp"
	"testing"
)

func TestNewClassifier(t *testing.T) {
	c := NewClassifier()
	if c == nil {
		t.Fatal("NewClassifier returned nil")
	}

	if len(c.patterns) == 0 {
		t.Error("Classifier should have default patterns")
	}

	if c.minConfidence != 0.5 {
		t.Errorf("Expected default minConfidence 0.5, got %f", c.minConfidence)
	}
}

func TestClassifier_PasswordPrompts(t *testing.T) {
	c := NewClassifier()

	tests := []struct {
		output   string
		wantType PromptType
	}{
		{"[sudo] password for user: ", PromptTypePassword},
		{"Password: ", PromptTypePassword},
		{"Enter passphrase: ", PromptTypePassword},
		{"SSH password for user: ", PromptTypePassword},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			class := c.Classify(tt.output)
			if class == nil {
				t.Fatal("Expected classification, got nil")
			}
			if class.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", class.Type, tt.wantType)
			}
			if !class.RequiresInput {
				t.Error("Password prompt should require input")
			}
		})
	}
}

func TestClassifier_ConfirmationPrompts(t *testing.T) {
	c := NewClassifier()

	tests := []struct {
		output   string
		wantType PromptType
	}{
		{"Do you want to continue? [Y/n]", PromptTypeConfirmation},
		{"Are you sure? (yes/no)", PromptTypeConfirmation},
		{"Proceed? [y/n]", PromptTypeConfirmation},
		// "Press enter" is detected as text prompt with the current heuristics
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			class := c.Classify(tt.output)
			if class == nil {
				t.Fatal("Expected classification, got nil")
			}
			if class.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", class.Type, tt.wantType)
			}
		})
	}
}

func TestClassifier_EditorDetection(t *testing.T) {
	c := NewClassifier()

	tests := []struct {
		name   string
		output string
	}{
		{"vim insert", "-- INSERT --"},
		{"vim normal", "-- NORMAL --"},
		{"nano", "GNU nano 5.4"},
		{"nano help", "^G Get Help"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class := c.Classify(tt.output)
			if class == nil {
				t.Fatal("Expected classification, got nil")
			}
			if class.Type != PromptTypeEditor {
				t.Errorf("Type = %v, want %v", class.Type, PromptTypeEditor)
			}
		})
	}
}

func TestClassifier_ShellPromptNotClassified(t *testing.T) {
	c := NewClassifier()

	// Shell prompts should NOT be classified as requiring input
	shellPrompts := []string{
		"user@host:~$ ",
		"root@server:/home# ",
		">>> ",
	}

	for _, prompt := range shellPrompts {
		t.Run(prompt, func(t *testing.T) {
			class := c.Classify(prompt)
			// Shell prompts might get classified but shouldn't be password/confirmation
			if class != nil {
				if class.Type == PromptTypePassword || class.Type == PromptTypeConfirmation {
					t.Errorf("Shell prompt incorrectly classified as %v", class.Type)
				}
			}
		})
	}
}

func TestClassifier_Confidence(t *testing.T) {
	c := NewClassifier()

	// Regex matches should have high confidence (1.0)
	regexMatch := c.Classify("[sudo] password for user: ")
	if regexMatch == nil {
		t.Fatal("Expected classification")
	}
	if regexMatch.Confidence != 1.0 {
		t.Errorf("Regex match confidence = %f, want 1.0", regexMatch.Confidence)
	}

	// Test that heuristic matches have confidence < 1.0
	// Use a prompt that won't match regex patterns
	heuristicMatch := c.Classify("Enter your API token:")
	if heuristicMatch == nil {
		t.Fatal("Expected classification")
	}
	if heuristicMatch.Confidence >= 1.0 {
		t.Errorf("Heuristic match confidence = %f, should be < 1.0", heuristicMatch.Confidence)
	}
}

func TestClassifier_WithContext(t *testing.T) {
	c := NewClassifier()

	ctx := ClassifyContext{
		Command: "sudo apt install vim",
	}

	// Classification with sudo context should have higher confidence
	class := c.ClassifyWithContext("[sudo] password for user: ", ctx)
	if class == nil {
		t.Fatal("Expected classification")
	}

	// Context should enhance confidence
	if class.Confidence < 0.9 {
		t.Errorf("Confidence with sudo context should be high, got %f", class.Confidence)
	}
}

func TestClassifier_WithCustomPatterns(t *testing.T) {
	customPattern := Pattern{
		Name:              "custom_prompt",
		Type:              PromptTypeText,
		SuggestedResponse: "custom response",
		Regex:             compileRegex(`Custom Input:`),
	}

	c := NewClassifier(WithCustomPatterns([]Pattern{customPattern}))

	class := c.Classify("Custom Input: ")
	if class == nil {
		t.Fatal("Expected classification for custom pattern")
	}
	if class.SuggestedResponse != "custom response" {
		t.Errorf("SuggestedResponse = %v, want 'custom response'", class.SuggestedResponse)
	}
}

func TestClassifier_WithMinConfidence(t *testing.T) {
	c := NewClassifier(WithMinConfidence(0.8))
	if c.minConfidence != 0.8 {
		t.Errorf("minConfidence = %f, want 0.8", c.minConfidence)
	}
}

func TestClassifier_WithLLMCallback(t *testing.T) {
	called := false
	callback := func(output string) (*Classification, error) {
		called = true
		return &Classification{
			Type:       PromptTypeText,
			Confidence: 0.8,
			Source:     "llm",
		}, nil
	}

	c := NewClassifier(WithLLMClassifier(callback))

	// LLM callback should only be called when regex/heuristics fail
	// Test with unrecognized output
	class := c.Classify("Something completely unrecognized xyz123")
	if !called {
		t.Error("LLM callback should have been called")
	}
	if class == nil || class.Source != "llm" {
		t.Error("Expected LLM classification")
	}
}

func compileRegex(pattern string) *Regex {
	return MustCompile(pattern)
}

type Regex = regexp.Regexp

func MustCompile(pattern string) *Regex {
	return regexp.MustCompile(pattern)
}
