package recovery

import (
	"testing"
)

func TestNewAnalyzer(t *testing.T) {
	a := NewAnalyzer()
	if a == nil {
		t.Fatal("NewAnalyzer returned nil")
	}
	if len(a.rules) == 0 {
		t.Error("Analyzer should have default rules")
	}
}

func TestAnalyzer_PermissionDenied(t *testing.T) {
	a := NewAnalyzer()

	output := "cp: cannot create regular file '/usr/bin/test': Permission denied"
	suggestions := a.Analyze("cp test /usr/bin/", output, 1)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for permission denied")
	}

	found := false
	for _, s := range suggestions {
		if s.Category == "permission" {
			found = true
			if !s.Risky {
				t.Error("Permission fix should be marked as risky")
			}
		}
	}
	if !found {
		t.Error("Expected permission category suggestion")
	}
}

func TestAnalyzer_CommandNotFound(t *testing.T) {
	a := NewAnalyzer()

	output := "bash: htop: command not found"
	suggestions := a.Analyze("htop", output, 127)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for command not found")
	}

	found := false
	for _, s := range suggestions {
		if s.Category == "package" {
			found = true
			if len(s.Commands) == 0 {
				t.Error("Expected install commands")
			}
		}
	}
	if !found {
		t.Error("Expected package category suggestion")
	}
}

func TestAnalyzer_NpmEacces(t *testing.T) {
	a := NewAnalyzer()

	output := `npm ERR! code EACCES
npm ERR! syscall mkdir
npm ERR! path /usr/lib/node_modules/some-package`

	suggestions := a.Analyze("npm install -g some-package", output, 1)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for npm EACCES")
	}

	if suggestions[0].Category != "permission" {
		t.Errorf("Expected permission category, got %s", suggestions[0].Category)
	}
}

func TestAnalyzer_GitNotRepo(t *testing.T) {
	a := NewAnalyzer()

	output := "fatal: not a git repository (or any of the parent directories): .git"
	suggestions := a.Analyze("git status", output, 128)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for git not repo")
	}

	if suggestions[0].Category != "config" {
		t.Errorf("Expected config category, got %s", suggestions[0].Category)
	}
}

func TestAnalyzer_DiskFull(t *testing.T) {
	a := NewAnalyzer()

	output := "write failed: No space left on device"
	suggestions := a.Analyze("dd if=/dev/zero of=bigfile", output, 1)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for disk full")
	}

	if suggestions[0].Category != "disk" {
		t.Errorf("Expected disk category, got %s", suggestions[0].Category)
	}
}

func TestAnalyzer_PortInUse(t *testing.T) {
	a := NewAnalyzer()

	output := "Error: listen EADDRINUSE: address already in use :::3000"
	suggestions := a.Analyze("npm start", output, 1)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for port in use")
	}

	if suggestions[0].Category != "network" {
		t.Errorf("Expected network category, got %s", suggestions[0].Category)
	}
}

func TestAnalyzer_PythonModuleNotFound(t *testing.T) {
	a := NewAnalyzer()

	output := "ModuleNotFoundError: No module named 'requests'"
	suggestions := a.Analyze("python script.py", output, 1)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for Python module not found")
	}

	if suggestions[0].Category != "package" {
		t.Errorf("Expected package category, got %s", suggestions[0].Category)
	}

	hasInstall := false
	for _, cmd := range suggestions[0].Commands {
		if cmd == "pip install requests" || cmd == "pip3 install requests" {
			hasInstall = true
			break
		}
	}
	if !hasInstall {
		t.Error("Expected pip install command")
	}
}

func TestAnalyzer_NodeModuleNotFound(t *testing.T) {
	a := NewAnalyzer()

	output := "Error: Cannot find module 'express'"
	suggestions := a.Analyze("node app.js", output, 1)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for Node module not found")
	}

	if suggestions[0].Category != "package" {
		t.Errorf("Expected package category, got %s", suggestions[0].Category)
	}
}

func TestAnalyzer_DockerDaemon(t *testing.T) {
	a := NewAnalyzer()

	output := "Cannot connect to the Docker daemon at unix:///var/run/docker.sock"
	suggestions := a.Analyze("docker ps", output, 1)

	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for Docker daemon")
	}

	if suggestions[0].Category != "service" {
		t.Errorf("Expected service category, got %s", suggestions[0].Category)
	}
}

func TestAnalyzer_NoErrorsNoSuggestions(t *testing.T) {
	a := NewAnalyzer()

	output := "total 4\ndrwxr-xr-x 2 user user 4096 Jan 1 00:00 dir"
	suggestions := a.Analyze("ls -la", output, 0)

	if len(suggestions) != 0 {
		t.Error("Expected no suggestions for successful command")
	}
}

func TestAnalyzer_SortByConfidence(t *testing.T) {
	a := NewAnalyzer()

	// Create output that matches multiple rules
	output := `Permission denied
command not found: npm
No space left on device`

	suggestions := a.Analyze("test", output, 1)

	if len(suggestions) < 2 {
		t.Skip("Need multiple suggestions to test sorting")
	}

	// Check that suggestions are sorted by confidence
	for i := 0; i < len(suggestions)-1; i++ {
		if suggestions[i].Confidence < suggestions[i+1].Confidence {
			t.Error("Suggestions should be sorted by confidence descending")
		}
	}
}

func TestSuggestPackageInstall(t *testing.T) {
	tests := []struct {
		cmd           string
		wantContains  string
	}{
		{"node", "nodejs"},
		{"python", "python"},
		{"docker", "docker"},
		{"git", "git"},
		{"unknowncmd", "unknowncmd"},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			suggestions := suggestPackageInstall(tt.cmd)
			if len(suggestions) == 0 {
				t.Fatal("Expected at least one suggestion")
			}

			found := false
			for _, s := range suggestions {
				if containsSubstring(s, tt.wantContains) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected suggestion containing %q", tt.wantContains)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsWord(s, substr))
}

func containsWord(s, word string) bool {
	return len(s) > 0 && (s == word || (len(s) > len(word) && hasSubstring(s, word)))
}

func hasSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
