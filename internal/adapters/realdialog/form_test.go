package realdialog

import (
	"bufio"
	"strings"
	"testing"
)

func TestPromptWithInput(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("custom-value\n"))
	result := prompt(scanner, "Name", "default")
	if result != "custom-value" {
		t.Errorf("prompt() = %q, want %q", result, "custom-value")
	}
}

func TestPromptEmptyReturnsDefault(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("\n"))
	result := prompt(scanner, "Name", "fallback")
	if result != "fallback" {
		t.Errorf("prompt() = %q, want %q", result, "fallback")
	}
}

func TestPromptWhitespaceReturnsDefault(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("   \n"))
	result := prompt(scanner, "Name", "fallback")
	if result != "fallback" {
		t.Errorf("prompt() = %q, want %q", result, "fallback")
	}
}

func TestPromptNoDefaultEmptyInput(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("\n"))
	result := prompt(scanner, "Name", "")
	if result != "" {
		t.Errorf("prompt() = %q, want empty", result)
	}
}

func TestPromptEOFReturnsDefault(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	result := prompt(scanner, "Name", "safe")
	if result != "safe" {
		t.Errorf("prompt() = %q, want %q", result, "safe")
	}
}
