// Package prompt provides prompt detection and classification for interactive sessions.
package prompt

import (
	"regexp"
	"strings"
)

// Classification represents the result of prompt classification.
type Classification struct {
	Type              PromptType
	Confidence        float64 // 0.0 to 1.0
	RequiresInput     bool
	SuggestedResponse string
	Reason            string
	Source            string // "regex", "heuristic", or "llm"
}

// Classifier provides intelligent prompt classification beyond regex.
type Classifier struct {
	patterns      []Pattern
	enableLLM     bool
	llmCallback   LLMClassifyFunc
	minConfidence float64
}

// LLMClassifyFunc is a callback for LLM-based classification.
// It receives the output text and returns a classification.
type LLMClassifyFunc func(output string) (*Classification, error)

// ClassifierOption configures the classifier.
type ClassifierOption func(*Classifier)

// WithLLMClassifier enables LLM-based classification.
func WithLLMClassifier(callback LLMClassifyFunc) ClassifierOption {
	return func(c *Classifier) {
		c.enableLLM = true
		c.llmCallback = callback
	}
}

// WithMinConfidence sets the minimum confidence threshold.
func WithMinConfidence(confidence float64) ClassifierOption {
	return func(c *Classifier) {
		c.minConfidence = confidence
	}
}

// WithCustomPatterns adds custom regex patterns.
func WithCustomPatterns(patterns []Pattern) ClassifierOption {
	return func(c *Classifier) {
		c.patterns = append(c.patterns, patterns...)
	}
}

// NewClassifier creates a new prompt classifier.
func NewClassifier(opts ...ClassifierOption) *Classifier {
	c := &Classifier{
		patterns:      DefaultPatterns(),
		minConfidence: 0.5,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Classify analyzes output and returns a classification.
func (c *Classifier) Classify(output string) *Classification {
	// Try regex patterns first (highest confidence)
	if class := c.classifyByRegex(output); class != nil {
		return class
	}

	// Try heuristic-based classification
	if class := c.classifyByHeuristics(output); class != nil {
		return class
	}

	// Try LLM-based classification if enabled
	if c.enableLLM && c.llmCallback != nil {
		if class, err := c.llmCallback(output); err == nil && class != nil {
			class.Source = "llm"
			return class
		}
	}

	return nil
}

func (c *Classifier) classifyByRegex(output string) *Classification {
	for _, pattern := range c.patterns {
		if pattern.Regex != nil && pattern.Regex.MatchString(output) {
			return &Classification{
				Type:              pattern.Type,
				Confidence:        1.0,
				RequiresInput:     pattern.Type != PromptTypeEditor && pattern.Type != PromptTypePager,
				SuggestedResponse: pattern.SuggestedResponse,
				Reason:            "Matched pattern: " + pattern.Name,
				Source:            "regex",
			}
		}
	}
	return nil
}

func (c *Classifier) classifyByHeuristics(output string) *Classification {
	lines := strings.Split(output, "\n")
	if len(lines) == 0 {
		return nil
	}

	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine == "" && len(lines) > 1 {
		lastLine = strings.TrimSpace(lines[len(lines)-2])
	}

	// Password prompt heuristics
	if class := c.detectPasswordPrompt(lastLine); class != nil {
		return class
	}

	// Confirmation prompt heuristics
	if class := c.detectConfirmationPrompt(lastLine); class != nil {
		return class
	}

	// Interactive prompt heuristics
	if class := c.detectInteractivePrompt(lastLine); class != nil {
		return class
	}

	// Editor detection
	if class := c.detectEditor(output); class != nil {
		return class
	}

	return nil
}

func (c *Classifier) detectPasswordPrompt(line string) *Classification {
	lineLower := strings.ToLower(line)

	// Strong indicators
	strongIndicators := []string{
		"password:",
		"passphrase:",
		"secret:",
		"enter password",
		"password for",
	}

	for _, indicator := range strongIndicators {
		if strings.Contains(lineLower, indicator) {
			return &Classification{
				Type:          PromptTypePassword,
				Confidence:    0.9,
				RequiresInput: true,
				Reason:        "Contains password indicator: " + indicator,
				Source:        "heuristic",
			}
		}
	}

	// Weak indicators (need additional context)
	weakIndicators := []string{
		"credential",
		"authenticate",
		"login",
	}

	for _, indicator := range weakIndicators {
		if strings.Contains(lineLower, indicator) && strings.HasSuffix(line, ":") {
			return &Classification{
				Type:          PromptTypePassword,
				Confidence:    0.6,
				RequiresInput: true,
				Reason:        "Possible authentication prompt: " + indicator,
				Source:        "heuristic",
			}
		}
	}

	return nil
}

func (c *Classifier) detectConfirmationPrompt(line string) *Classification {
	lineLower := strings.ToLower(line)

	// Y/N or Yes/No patterns
	ynPatterns := []struct {
		pattern  *regexp.Regexp
		response string
	}{
		{regexp.MustCompile(`\[y/n\]`), "y"},
		{regexp.MustCompile(`\[Y/n\]`), "Y"},
		{regexp.MustCompile(`\[y/N\]`), "n"},
		{regexp.MustCompile(`\(yes/no\)`), "yes"},
		{regexp.MustCompile(`\(y/n\)`), "y"},
		{regexp.MustCompile(`\[yes/no\]`), "yes"},
	}

	for _, p := range ynPatterns {
		if p.pattern.MatchString(lineLower) {
			return &Classification{
				Type:              PromptTypeConfirmation,
				Confidence:        0.95,
				RequiresInput:     true,
				SuggestedResponse: p.response,
				Reason:            "Contains confirmation pattern",
				Source:            "heuristic",
			}
		}
	}

	// Continue prompts
	continueIndicators := []string{
		"press enter",
		"press any key",
		"continue?",
		"proceed?",
		"are you sure",
	}

	for _, indicator := range continueIndicators {
		if strings.Contains(lineLower, indicator) {
			return &Classification{
				Type:              PromptTypeConfirmation,
				Confidence:        0.8,
				RequiresInput:     true,
				SuggestedResponse: "",
				Reason:            "Contains continue indicator: " + indicator,
				Source:            "heuristic",
			}
		}
	}

	return nil
}

func (c *Classifier) detectInteractivePrompt(line string) *Classification {
	// Shell prompts typically end with $ or # or >
	shellEndings := []string{"$ ", "# ", "> "}
	for _, ending := range shellEndings {
		if strings.HasSuffix(line, ending) {
			// This is a shell prompt, not an input prompt
			return nil
		}
	}

	// Input prompts typically end with : or ?
	if strings.HasSuffix(line, ":") || strings.HasSuffix(line, "?") {
		// Check if it looks like a prompt rather than output
		if len(line) < 100 && !strings.Contains(line, "\t") {
			return &Classification{
				Type:          PromptTypeText,
				Confidence:    0.6,
				RequiresInput: true,
				Reason:        "Line ends with input indicator",
				Source:        "heuristic",
			}
		}
	}

	return nil
}

func (c *Classifier) detectEditor(output string) *Classification {
	// Common editor patterns
	editorPatterns := []struct {
		pattern *regexp.Regexp
		name    string
		hint    string
	}{
		{
			regexp.MustCompile(`-- INSERT --`),
			"vim insert mode",
			":wq to save and exit, :q! to exit without saving",
		},
		{
			regexp.MustCompile(`-- (NORMAL|VISUAL|COMMAND) --`),
			"vim mode",
			":q! to exit without saving",
		},
		{
			regexp.MustCompile(`GNU nano`),
			"nano editor",
			"Ctrl+X to exit",
		},
		{
			regexp.MustCompile(`\^G Get Help`),
			"nano editor",
			"Ctrl+X to exit",
		},
		{
			regexp.MustCompile(`less \d+`),
			"less pager",
			"q to quit",
		},
	}

	for _, ep := range editorPatterns {
		if ep.pattern.MatchString(output) {
			return &Classification{
				Type:              PromptTypeEditor,
				Confidence:        0.9,
				RequiresInput:     false,
				SuggestedResponse: ep.hint,
				Reason:            "Detected " + ep.name,
				Source:            "heuristic",
			}
		}
	}

	return nil
}

// ClassifyWithContext provides classification with additional context.
type ClassifyContext struct {
	Command        string   // The command that was executed
	PreviousOutput []string // Previous output lines
	SessionType    string   // "ssh" or "local"
	CurrentDir     string   // Current working directory
}

// ClassifyWithContext performs classification with additional context.
func (c *Classifier) ClassifyWithContext(output string, ctx ClassifyContext) *Classification {
	// Start with basic classification
	class := c.Classify(output)

	// Enhance with context if available
	if class != nil && ctx.Command != "" {
		// Adjust confidence based on command
		class = c.adjustForCommand(class, ctx.Command)
	}

	return class
}

func (c *Classifier) adjustForCommand(class *Classification, command string) *Classification {
	cmdLower := strings.ToLower(command)

	// sudo commands expect password prompts
	if strings.HasPrefix(cmdLower, "sudo ") && class.Type == PromptTypePassword {
		class.Confidence = min(class.Confidence+0.1, 1.0)
	}

	// npm/apt commands expect confirmation prompts
	if (strings.Contains(cmdLower, "npm ") || strings.Contains(cmdLower, "apt ")) &&
		class.Type == PromptTypeConfirmation {
		class.Confidence = min(class.Confidence+0.1, 1.0)
	}

	// git commands may need various inputs
	if strings.HasPrefix(cmdLower, "git ") && class.Type == PromptTypeText {
		class.Confidence = min(class.Confidence+0.1, 1.0)
	}

	return class
}
