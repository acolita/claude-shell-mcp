package prompt

import (
	"regexp"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 1. NewDetector
// ---------------------------------------------------------------------------

func TestNewDetector(t *testing.T) {
	d := NewDetector()
	if d == nil {
		t.Fatal("NewDetector returned nil")
	}
	if len(d.patterns) == 0 {
		t.Error("NewDetector should populate default patterns")
	}
	if len(d.customPatterns) != 0 {
		t.Error("NewDetector should start with no custom patterns")
	}
}

// ---------------------------------------------------------------------------
// 2. AddPattern
// ---------------------------------------------------------------------------

func TestAddPattern(t *testing.T) {
	d := NewDetector()

	custom := Pattern{
		Name:      "custom_token",
		Regex:     regexp.MustCompile(`Enter API token:\s*$`),
		Type:      PromptTypeText,
		MaskInput: false,
	}
	d.AddPattern(custom)

	if len(d.customPatterns) != 1 {
		t.Fatalf("expected 1 custom pattern, got %d", len(d.customPatterns))
	}
	if d.customPatterns[0].Name != "custom_token" {
		t.Errorf("custom pattern name = %q, want %q", d.customPatterns[0].Name, "custom_token")
	}

	// Verify the custom pattern actually fires during detection.
	det := d.Detect("Please authenticate.\nEnter API token: ")
	if det == nil {
		t.Fatal("expected detection for custom pattern, got nil")
	}
	if det.Pattern.Name != "custom_token" {
		t.Errorf("detected pattern = %q, want %q", det.Pattern.Name, "custom_token")
	}
}

// ---------------------------------------------------------------------------
// 3. AddPatternFromConfig
// ---------------------------------------------------------------------------

func TestAddPatternFromConfig_ValidRegex(t *testing.T) {
	d := NewDetector()

	err := d.AddPatternFromConfig("vault_pw", `Vault password:\s*$`, "password", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	det := d.Detect("Vault password: ")
	if det == nil {
		t.Fatal("expected detection, got nil")
	}
	if det.Pattern.Name != "vault_pw" {
		t.Errorf("pattern name = %q, want %q", det.Pattern.Name, "vault_pw")
	}
	if det.Pattern.Type != PromptTypePassword {
		t.Errorf("type = %q, want %q", det.Pattern.Type, PromptTypePassword)
	}
	if !det.Pattern.MaskInput {
		t.Error("expected MaskInput to be true")
	}
}

func TestAddPatternFromConfig_InvalidRegex(t *testing.T) {
	d := NewDetector()

	err := d.AddPatternFromConfig("bad", `[invalid(`, "text", false)
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
}

func TestAddPatternFromConfig_PromptTypes(t *testing.T) {
	tests := []struct {
		inputType string
		wantType  PromptType
	}{
		{"password", PromptTypePassword},
		{"confirmation", PromptTypeConfirmation},
		{"editor", PromptTypeEditor},
		{"pager", PromptTypePager},
		{"something_unknown", PromptTypeText},
		{"", PromptTypeText},
	}

	for _, tt := range tests {
		t.Run("type_"+tt.inputType, func(t *testing.T) {
			d := NewDetector()
			err := d.AddPatternFromConfig("test", `test_prompt`, tt.inputType, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(d.customPatterns) != 1 {
				t.Fatalf("expected 1 custom pattern, got %d", len(d.customPatterns))
			}
			if d.customPatterns[0].Type != tt.wantType {
				t.Errorf("type = %q, want %q", d.customPatterns[0].Type, tt.wantType)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Detect
// ---------------------------------------------------------------------------

func TestDetect_SudoPassword(t *testing.T) {
	d := NewDetector()

	det := d.Detect("[sudo] password for deploy: ")
	if det == nil {
		t.Fatal("expected detection for sudo password prompt, got nil")
	}
	if det.Pattern.Type != PromptTypePassword {
		t.Errorf("type = %q, want %q", det.Pattern.Type, PromptTypePassword)
	}
	if !det.Pattern.MaskInput {
		t.Error("sudo password should have MaskInput true")
	}
}

func TestDetect_Confirmation(t *testing.T) {
	d := NewDetector()

	det := d.Detect("Do you want to continue? [Y/n] ")
	if det == nil {
		t.Fatal("expected detection for confirmation prompt, got nil")
	}
	if det.Pattern.Type != PromptTypeConfirmation {
		t.Errorf("type = %q, want %q", det.Pattern.Type, PromptTypeConfirmation)
	}
}

func TestDetect_NoMatch(t *testing.T) {
	d := NewDetector()

	det := d.Detect("total 42\ndrwxr-xr-x 2 user user 4096 Jan 1 00:00 .\n")
	if det != nil {
		t.Errorf("expected nil for non-prompt output, got detection %q", det.Pattern.Name)
	}
}

func TestDetect_CustomPatternsHavePriority(t *testing.T) {
	d := NewDetector()

	// Add a custom pattern that also matches "password" text but with a
	// different Name so we can distinguish which one fired.
	custom := Pattern{
		Name:      "custom_sudo_override",
		Regex:     regexp.MustCompile(`(?i)\[sudo\]\s+password\s+for\s+\w+:\s*$`),
		Type:      PromptTypePassword,
		MaskInput: true,
	}
	d.AddPattern(custom)

	det := d.Detect("[sudo] password for user: ")
	if det == nil {
		t.Fatal("expected detection, got nil")
	}
	if det.Pattern.Name != "custom_sudo_override" {
		t.Errorf("expected custom pattern to win, got %q", det.Pattern.Name)
	}
}

// ---------------------------------------------------------------------------
// 5. Detect with long buffer (>10 lines)
// ---------------------------------------------------------------------------

func TestDetect_LongBufferOnlyChecksLastTenLines(t *testing.T) {
	d := NewDetector()

	// Build a buffer with the prompt on line 1, followed by 15 lines of filler.
	// The prompt should NOT be detected since it is outside the last-10 window.
	var lines []string
	lines = append(lines, "[sudo] password for user: ")
	for i := 0; i < 15; i++ {
		lines = append(lines, "some regular output line")
	}
	buffer := strings.Join(lines, "\n")

	det := d.Detect(buffer)
	if det != nil {
		t.Errorf("prompt outside last 10 lines should not match, got %q", det.Pattern.Name)
	}
}

func TestDetect_LongBufferMatchesInLastTenLines(t *testing.T) {
	d := NewDetector()

	// Build a buffer where the prompt is in the last 10 lines.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "filler line")
	}
	lines = append(lines, "[sudo] password for deploy: ")
	buffer := strings.Join(lines, "\n")

	det := d.Detect(buffer)
	if det == nil {
		t.Fatal("prompt within last 10 lines should match, got nil")
	}
}

// ---------------------------------------------------------------------------
// 6. DetectAll
// ---------------------------------------------------------------------------

func TestDetectAll_MultipleMatches(t *testing.T) {
	d := NewDetector()

	// Use a buffer that triggers multiple distinct pattern types.
	// The SSH host key confirmation and the less pager pattern can coexist
	// because neither requires end-of-string anchoring.
	buffer := "Are you sure you want to continue connecting (yes/no)?\nGNU nano 7.2  /tmp/test.txt"

	detections := d.DetectAll(buffer)
	if len(detections) < 2 {
		t.Fatalf("expected at least 2 detections, got %d", len(detections))
	}

	foundConfirmation := false
	foundEditor := false
	for _, det := range detections {
		switch det.Pattern.Type {
		case PromptTypeConfirmation:
			foundConfirmation = true
		case PromptTypeEditor:
			foundEditor = true
		}
	}
	if !foundConfirmation {
		t.Error("expected a confirmation detection")
	}
	if !foundEditor {
		t.Error("expected an editor detection")
	}
}

func TestDetectAll_NoMatches(t *testing.T) {
	d := NewDetector()

	detections := d.DetectAll("just some boring output\nno prompts here\n")
	if len(detections) != 0 {
		t.Errorf("expected 0 detections, got %d", len(detections))
	}
}

func TestDetectAll_IncludesCustomAndDefault(t *testing.T) {
	d := NewDetector()

	custom := Pattern{
		Name:  "my_custom",
		Regex: regexp.MustCompile(`CUSTOM_PROMPT>`),
		Type:  PromptTypeText,
	}
	d.AddPattern(custom)

	buffer := "CUSTOM_PROMPT>\nPassword: "

	detections := d.DetectAll(buffer)

	foundCustom := false
	foundDefault := false
	for _, det := range detections {
		if det.Pattern.Name == "my_custom" {
			foundCustom = true
		}
		if det.Pattern.Name == "sudo_password_generic" {
			foundDefault = true
		}
	}
	if !foundCustom {
		t.Error("expected custom pattern in DetectAll results")
	}
	if !foundDefault {
		t.Error("expected default pattern in DetectAll results")
	}
}

// ---------------------------------------------------------------------------
// 7. matchPattern - context buffer and matched text
// ---------------------------------------------------------------------------

func TestMatchPattern_CapturesContextAndMatchedText(t *testing.T) {
	d := NewDetector()

	buffer := "Updating packages...\nReading database...\n[sudo] password for admin: "

	det := d.Detect(buffer)
	if det == nil {
		t.Fatal("expected detection, got nil")
	}

	if det.MatchedText != "[sudo] password for admin: " {
		t.Errorf("MatchedText = %q, want %q", det.MatchedText, "[sudo] password for admin: ")
	}

	if det.ContextBuffer == "" {
		t.Error("ContextBuffer should not be empty when there is text before the match")
	}
	if !strings.Contains(det.ContextBuffer, "Updating packages") {
		t.Errorf("ContextBuffer should contain preceding text, got %q", det.ContextBuffer)
	}
}

func TestMatchPattern_EmptyContextWhenMatchAtStart(t *testing.T) {
	d := NewDetector()

	buffer := "[sudo] password for root: "

	det := d.Detect(buffer)
	if det == nil {
		t.Fatal("expected detection, got nil")
	}
	if det.ContextBuffer != "" {
		t.Errorf("ContextBuffer should be empty when match is at start, got %q", det.ContextBuffer)
	}
}

func TestMatchPattern_SuggestedResponsePropagated(t *testing.T) {
	d := NewDetector()

	buffer := "Do you want to continue? [Y/n] "
	det := d.Detect(buffer)
	if det == nil {
		t.Fatal("expected detection, got nil")
	}
	// The apt_confirmation default pattern has SuggestedResponse "Y".
	if det.SuggestedResponse != "Y" {
		t.Errorf("SuggestedResponse = %q, want %q", det.SuggestedResponse, "Y")
	}
}

// ---------------------------------------------------------------------------
// 8. IsPasswordPrompt, IsConfirmation, IsEditor, IsPager
// ---------------------------------------------------------------------------

func TestDetection_IsPasswordPrompt(t *testing.T) {
	pwDet := &Detection{Pattern: Pattern{Type: PromptTypePassword}}
	if !pwDet.IsPasswordPrompt() {
		t.Error("IsPasswordPrompt should return true for password type")
	}

	confDet := &Detection{Pattern: Pattern{Type: PromptTypeConfirmation}}
	if confDet.IsPasswordPrompt() {
		t.Error("IsPasswordPrompt should return false for confirmation type")
	}
}

func TestDetection_IsConfirmation(t *testing.T) {
	confDet := &Detection{Pattern: Pattern{Type: PromptTypeConfirmation}}
	if !confDet.IsConfirmation() {
		t.Error("IsConfirmation should return true for confirmation type")
	}

	pwDet := &Detection{Pattern: Pattern{Type: PromptTypePassword}}
	if pwDet.IsConfirmation() {
		t.Error("IsConfirmation should return false for password type")
	}
}

func TestDetection_IsEditor(t *testing.T) {
	edDet := &Detection{Pattern: Pattern{Type: PromptTypeEditor}}
	if !edDet.IsEditor() {
		t.Error("IsEditor should return true for editor type")
	}

	textDet := &Detection{Pattern: Pattern{Type: PromptTypeText}}
	if textDet.IsEditor() {
		t.Error("IsEditor should return false for text type")
	}
}

func TestDetection_IsPager(t *testing.T) {
	pgDet := &Detection{Pattern: Pattern{Type: PromptTypePager}}
	if !pgDet.IsPager() {
		t.Error("IsPager should return true for pager type")
	}

	edDet := &Detection{Pattern: Pattern{Type: PromptTypeEditor}}
	if edDet.IsPager() {
		t.Error("IsPager should return false for editor type")
	}
}

// ---------------------------------------------------------------------------
// 9. Hint - all 6 branches
// ---------------------------------------------------------------------------

func TestDetection_Hint_Password(t *testing.T) {
	det := &Detection{Pattern: Pattern{Type: PromptTypePassword}}
	hint := det.Hint()
	expected := "Password required. Provide the password to continue."
	if hint != expected {
		t.Errorf("Hint = %q, want %q", hint, expected)
	}
}

func TestDetection_Hint_ConfirmationWithSuggested(t *testing.T) {
	det := &Detection{
		Pattern:           Pattern{Type: PromptTypeConfirmation},
		SuggestedResponse: "yes",
	}
	hint := det.Hint()
	expected := "Confirmation required. Suggested response: yes"
	if hint != expected {
		t.Errorf("Hint = %q, want %q", hint, expected)
	}
}

func TestDetection_Hint_ConfirmationWithoutSuggested(t *testing.T) {
	det := &Detection{
		Pattern:           Pattern{Type: PromptTypeConfirmation},
		SuggestedResponse: "",
	}
	hint := det.Hint()
	expected := "Confirmation required."
	if hint != expected {
		t.Errorf("Hint = %q, want %q", hint, expected)
	}
}

func TestDetection_Hint_Editor(t *testing.T) {
	det := &Detection{Pattern: Pattern{Type: PromptTypeEditor}}
	hint := det.Hint()
	expected := "Interactive editor detected. Use shell_interrupt to exit, or provide exit command (e.g., ':q!' for vim, 'Ctrl+X' for nano)."
	if hint != expected {
		t.Errorf("Hint = %q, want %q", hint, expected)
	}
}

func TestDetection_Hint_PagerWithSuggested(t *testing.T) {
	det := &Detection{
		Pattern:           Pattern{Type: PromptTypePager},
		SuggestedResponse: "q",
	}
	hint := det.Hint()
	expected := "Pager detected. Press 'q' to quit, or use shell_interrupt."
	if hint != expected {
		t.Errorf("Hint = %q, want %q", hint, expected)
	}
}

func TestDetection_Hint_PagerWithoutSuggested(t *testing.T) {
	det := &Detection{
		Pattern:           Pattern{Type: PromptTypePager},
		SuggestedResponse: "",
	}
	hint := det.Hint()
	expected := "Pager detected. Use 'q' to quit or shell_interrupt."
	if hint != expected {
		t.Errorf("Hint = %q, want %q", hint, expected)
	}
}

func TestDetection_Hint_DefaultText(t *testing.T) {
	det := &Detection{Pattern: Pattern{Type: PromptTypeText}}
	hint := det.Hint()
	expected := "Input required."
	if hint != expected {
		t.Errorf("Hint = %q, want %q", hint, expected)
	}
}

// ---------------------------------------------------------------------------
// Additional edge cases
// ---------------------------------------------------------------------------

func TestDetect_GenericPasswordPrompt(t *testing.T) {
	d := NewDetector()

	det := d.Detect("Password: ")
	if det == nil {
		t.Fatal("expected detection for generic password prompt, got nil")
	}
	if det.Pattern.Type != PromptTypePassword {
		t.Errorf("type = %q, want %q", det.Pattern.Type, PromptTypePassword)
	}
}

func TestDetect_SSHHostKeyConfirmation(t *testing.T) {
	d := NewDetector()

	buffer := "The authenticity of host 'example.com (1.2.3.4)' can't be established.\nRSA key fingerprint is SHA256:abc123.\nAre you sure you want to continue connecting (yes/no)?"
	det := d.Detect(buffer)
	if det == nil {
		t.Fatal("expected detection for SSH host key prompt, got nil")
	}
	if det.Pattern.Type != PromptTypeConfirmation {
		t.Errorf("type = %q, want %q", det.Pattern.Type, PromptTypeConfirmation)
	}
	if det.SuggestedResponse != "yes" {
		t.Errorf("SuggestedResponse = %q, want %q", det.SuggestedResponse, "yes")
	}
}

func TestDetect_LessPager(t *testing.T) {
	d := NewDetector()

	det := d.Detect("some content here\n(END) ")
	if det == nil {
		t.Fatal("expected detection for less pager, got nil")
	}
	if det.Pattern.Type != PromptTypePager {
		t.Errorf("type = %q, want %q", det.Pattern.Type, PromptTypePager)
	}
}

func TestDetect_NanoEditor(t *testing.T) {
	d := NewDetector()

	det := d.Detect("  GNU nano 7.2   /tmp/test.txt")
	if det == nil {
		t.Fatal("expected detection for nano editor, got nil")
	}
	if det.Pattern.Type != PromptTypeEditor {
		t.Errorf("type = %q, want %q", det.Pattern.Type, PromptTypeEditor)
	}
}

func TestDetect_EmptyBuffer(t *testing.T) {
	d := NewDetector()

	det := d.Detect("")
	if det != nil {
		t.Errorf("expected nil for empty buffer, got %q", det.Pattern.Name)
	}
}

func TestDetectAll_EmptyBuffer(t *testing.T) {
	d := NewDetector()

	detections := d.DetectAll("")
	if len(detections) != 0 {
		t.Errorf("expected 0 detections for empty buffer, got %d", len(detections))
	}
}

func TestAddPattern_MultipleCustom(t *testing.T) {
	d := NewDetector()

	d.AddPattern(Pattern{
		Name:  "first",
		Regex: regexp.MustCompile(`FIRST>`),
		Type:  PromptTypeText,
	})
	d.AddPattern(Pattern{
		Name:  "second",
		Regex: regexp.MustCompile(`SECOND>`),
		Type:  PromptTypeText,
	})

	if len(d.customPatterns) != 2 {
		t.Fatalf("expected 2 custom patterns, got %d", len(d.customPatterns))
	}

	// First custom pattern should match first.
	det := d.Detect("FIRST> SECOND>")
	if det == nil {
		t.Fatal("expected detection, got nil")
	}
	if det.Pattern.Name != "first" {
		t.Errorf("expected first custom pattern to match first, got %q", det.Pattern.Name)
	}
}

func TestDetect_BufferExactlyTenLines(t *testing.T) {
	d := NewDetector()

	// Build a buffer with exactly 10 lines, prompt on the last line.
	var lines []string
	for i := 0; i < 9; i++ {
		lines = append(lines, "filler")
	}
	lines = append(lines, "Password: ")
	buffer := strings.Join(lines, "\n")

	det := d.Detect(buffer)
	if det == nil {
		t.Fatal("expected detection within 10-line buffer, got nil")
	}
}

func TestDetect_BufferElevenLinesPromptOnFirstLine(t *testing.T) {
	d := NewDetector()

	// 11 lines total, prompt on line 1 (index 0). This should NOT match
	// because only the last 10 lines are checked.
	var lines []string
	lines = append(lines, "Password: ")
	for i := 0; i < 10; i++ {
		lines = append(lines, "filler")
	}
	buffer := strings.Join(lines, "\n")

	det := d.Detect(buffer)
	if det != nil {
		t.Errorf("prompt on line 1 of 11 should not be detected, got %q", det.Pattern.Name)
	}
}
