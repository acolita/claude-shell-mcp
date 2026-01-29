package prompt

import (
	"regexp"
	"strings"
	"sync"
)

// Detection represents a detected prompt.
type Detection struct {
	Pattern           Pattern
	MatchedText       string
	ContextBuffer     string // Last N lines before the match
	SuggestedResponse string
}

// Detector detects interactive prompts in terminal output.
type Detector struct {
	patterns       []Pattern
	customPatterns []Pattern
	mu             sync.RWMutex
}

// NewDetector creates a new prompt detector with default patterns.
func NewDetector() *Detector {
	return &Detector{
		patterns: DefaultPatterns(),
	}
}

// AddPattern adds a custom pattern to the detector.
func (d *Detector) AddPattern(p Pattern) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.customPatterns = append(d.customPatterns, p)
}

// AddPatternFromConfig adds a pattern from configuration.
func (d *Detector) AddPatternFromConfig(name, regex, promptType string, maskInput bool) error {
	re, err := regexp.Compile(regex)
	if err != nil {
		return err
	}

	var pt PromptType
	switch promptType {
	case "password":
		pt = PromptTypePassword
	case "confirmation":
		pt = PromptTypeConfirmation
	case "editor":
		pt = PromptTypeEditor
	case "pager":
		pt = PromptTypePager
	default:
		pt = PromptTypeText
	}

	d.AddPattern(Pattern{
		Name:      name,
		Regex:     re,
		Type:      pt,
		MaskInput: maskInput,
	})

	return nil
}

// Detect checks if the buffer contains an interactive prompt.
// Returns the detection if found, nil otherwise.
func (d *Detector) Detect(buffer string) *Detection {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Check custom patterns first (higher priority)
	for _, p := range d.customPatterns {
		if match := d.matchPattern(buffer, p); match != nil {
			return match
		}
	}

	// Check default patterns
	for _, p := range d.patterns {
		if match := d.matchPattern(buffer, p); match != nil {
			return match
		}
	}

	return nil
}

// matchPattern checks if a pattern matches the buffer.
func (d *Detector) matchPattern(buffer string, p Pattern) *Detection {
	// For prompt detection, we only care about the last few lines
	lines := strings.Split(buffer, "\n")
	lastLines := lines
	if len(lines) > 10 {
		lastLines = lines[len(lines)-10:]
	}
	recentBuffer := strings.Join(lastLines, "\n")

	if loc := p.Regex.FindStringIndex(recentBuffer); loc != nil {
		matchedText := recentBuffer[loc[0]:loc[1]]

		// Get context (text before the match)
		context := ""
		if loc[0] > 0 {
			context = recentBuffer[:loc[0]]
		}

		return &Detection{
			Pattern:           p,
			MatchedText:       matchedText,
			ContextBuffer:     strings.TrimSpace(context),
			SuggestedResponse: p.SuggestedResponse,
		}
	}

	return nil
}

// DetectAll returns all matching prompts in the buffer.
func (d *Detector) DetectAll(buffer string) []Detection {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var detections []Detection

	allPatterns := append(d.customPatterns, d.patterns...)
	for _, p := range allPatterns {
		if match := d.matchPattern(buffer, p); match != nil {
			detections = append(detections, *match)
		}
	}

	return detections
}

// IsPasswordPrompt returns true if the detection is for a password prompt.
func (det *Detection) IsPasswordPrompt() bool {
	return det.Pattern.Type == PromptTypePassword
}

// IsConfirmation returns true if the detection is for a confirmation prompt.
func (det *Detection) IsConfirmation() bool {
	return det.Pattern.Type == PromptTypeConfirmation
}

// IsEditor returns true if the detection is for an interactive editor.
func (det *Detection) IsEditor() bool {
	return det.Pattern.Type == PromptTypeEditor
}

// IsPager returns true if the detection is for a pager (less, more).
func (det *Detection) IsPager() bool {
	return det.Pattern.Type == PromptTypePager
}

// Hint returns a human-readable hint for the prompt.
func (det *Detection) Hint() string {
	switch det.Pattern.Type {
	case PromptTypePassword:
		return "Password required. Provide the password to continue."
	case PromptTypeConfirmation:
		if det.SuggestedResponse != "" {
			return "Confirmation required. Suggested response: " + det.SuggestedResponse
		}
		return "Confirmation required."
	case PromptTypeEditor:
		return "Interactive editor detected. Use shell_interrupt to exit, or provide exit command (e.g., ':q!' for vim, 'Ctrl+X' for nano)."
	case PromptTypePager:
		if det.SuggestedResponse != "" {
			return "Pager detected. Press '" + det.SuggestedResponse + "' to quit, or use shell_interrupt."
		}
		return "Pager detected. Use 'q' to quit or shell_interrupt."
	default:
		return "Input required."
	}
}
