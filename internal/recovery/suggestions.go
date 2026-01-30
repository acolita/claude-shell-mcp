// Package recovery provides automatic error recovery suggestions.
package recovery

import (
	"regexp"
	"strings"
)

// Suggestion represents a recovery suggestion for an error.
type Suggestion struct {
	Error       string   // Description of the detected error
	Category    string   // Error category (permission, package, network, etc.)
	Commands    []string // Suggested fix commands
	Explanation string   // Why this might fix the issue
	Confidence  float64  // Confidence that this suggestion will help
	Risky       bool     // If true, user should review before running
}

// Analyzer detects errors and suggests recovery actions.
type Analyzer struct {
	rules []recoveryRule
}

type recoveryRule struct {
	name      string
	pattern   *regexp.Regexp
	category  string
	suggest   func(matches []string) *Suggestion
	priority  int
}

// NewAnalyzer creates a new error analyzer with default rules.
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		rules: defaultRules(),
	}
}

// Analyze examines command output and returns recovery suggestions.
func (a *Analyzer) Analyze(command, output string, exitCode int) []*Suggestion {
	var suggestions []*Suggestion

	// Only analyze if there's an error indication
	if exitCode == 0 && !containsErrorIndicators(output) {
		return nil
	}

	for _, rule := range a.rules {
		if matches := rule.pattern.FindStringSubmatch(output); matches != nil {
			if suggestion := rule.suggest(matches); suggestion != nil {
				suggestions = append(suggestions, suggestion)
			}
		}
	}

	// Sort by confidence (highest first)
	sortByConfidence(suggestions)

	return suggestions
}

func containsErrorIndicators(output string) bool {
	lowered := strings.ToLower(output)
	indicators := []string{
		"error:", "error ", "failed", "failure",
		"not found", "permission denied", "access denied",
		"command not found", "no such file", "cannot",
		"unable to", "could not", "refused",
	}
	for _, ind := range indicators {
		if strings.Contains(lowered, ind) {
			return true
		}
	}
	return false
}

func sortByConfidence(suggestions []*Suggestion) {
	// Simple bubble sort (usually few suggestions)
	for i := 0; i < len(suggestions); i++ {
		for j := i + 1; j < len(suggestions); j++ {
			if suggestions[j].Confidence > suggestions[i].Confidence {
				suggestions[i], suggestions[j] = suggestions[j], suggestions[i]
			}
		}
	}
}

func defaultRules() []recoveryRule {
	return []recoveryRule{
		// Permission denied
		{
			name:     "permission_denied",
			pattern:  regexp.MustCompile(`(?i)permission denied`),
			category: "permission",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "Permission denied",
					Category:    "permission",
					Commands:    []string{"sudo !!", "sudo <previous_command>"},
					Explanation: "The operation requires elevated privileges. Try running with sudo.",
					Confidence:  0.8,
					Risky:       true,
				}
			},
		},

		// Command not found
		{
			name:     "command_not_found",
			pattern:  regexp.MustCompile(`(?i)(\S+):\s*command not found`),
			category: "package",
			suggest: func(matches []string) *Suggestion {
				cmd := ""
				if len(matches) > 1 {
					cmd = matches[1]
				}
				return &Suggestion{
					Error:       "Command not found: " + cmd,
					Category:    "package",
					Commands:    suggestPackageInstall(cmd),
					Explanation: "The command is not installed. Try installing the package.",
					Confidence:  0.7,
				}
			},
		},

		// npm EACCES
		{
			name:     "npm_eacces",
			pattern:  regexp.MustCompile(`(?i)npm ERR! code EACCES`),
			category: "permission",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "npm permission error",
					Category:    "permission",
					Commands:    []string{"sudo npm install", "npm install --prefix ~/.local"},
					Explanation: "npm doesn't have permission to write. Use sudo or install locally.",
					Confidence:  0.85,
					Risky:       true,
				}
			},
		},

		// npm ENOENT - missing package.json
		{
			name:     "npm_enoent_package",
			pattern:  regexp.MustCompile(`(?i)npm ERR! code ENOENT.*package\.json`),
			category: "config",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "No package.json found",
					Category:    "config",
					Commands:    []string{"npm init -y", "npm init"},
					Explanation: "The directory doesn't have a package.json. Initialize npm first.",
					Confidence:  0.9,
				}
			},
		},

		// Git not a repository
		{
			name:     "git_not_repo",
			pattern:  regexp.MustCompile(`(?i)fatal: not a git repository`),
			category: "config",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "Not a git repository",
					Category:    "config",
					Commands:    []string{"git init", "cd <project_root>"},
					Explanation: "The current directory is not a git repository. Initialize one or navigate to the correct directory.",
					Confidence:  0.9,
				}
			},
		},

		// Git merge conflicts
		{
			name:     "git_conflict",
			pattern:  regexp.MustCompile(`(?i)CONFLICT \(content\):`),
			category: "git",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "Git merge conflict",
					Category:    "git",
					Commands:    []string{"git status", "git diff", "git merge --abort"},
					Explanation: "There are merge conflicts that need to be resolved manually.",
					Confidence:  0.95,
				}
			},
		},

		// SSH connection refused
		{
			name:     "ssh_refused",
			pattern:  regexp.MustCompile(`(?i)connection refused`),
			category: "network",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "Connection refused",
					Category:    "network",
					Commands:    []string{"ping <host>", "systemctl status sshd"},
					Explanation: "The remote host is refusing connections. Check if the service is running and the port is open.",
					Confidence:  0.7,
				}
			},
		},

		// SSH host key changed
		{
			name:     "ssh_host_key",
			pattern:  regexp.MustCompile(`(?i)REMOTE HOST IDENTIFICATION HAS CHANGED`),
			category: "security",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "SSH host key has changed",
					Category:    "security",
					Commands:    []string{"ssh-keygen -R <hostname>"},
					Explanation: "The remote host's key has changed. This could indicate a security issue. Only remove the old key if you trust the host.",
					Confidence:  0.8,
					Risky:       true,
				}
			},
		},

		// Disk full
		{
			name:     "disk_full",
			pattern:  regexp.MustCompile(`(?i)no space left on device`),
			category: "disk",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "Disk full",
					Category:    "disk",
					Commands:    []string{"df -h", "du -sh * | sort -h | tail -10", "sudo apt clean"},
					Explanation: "The disk is full. Check disk usage and clean up unnecessary files.",
					Confidence:  0.9,
				}
			},
		},

		// Port already in use
		{
			name:     "port_in_use",
			pattern:  regexp.MustCompile(`(?i)(?:address already in use|EADDRINUSE).*?:(\d+)?`),
			category: "network",
			suggest: func(matches []string) *Suggestion {
				port := ""
				if len(matches) > 1 {
					port = matches[1]
				}
				return &Suggestion{
					Error:       "Port already in use" + ifNotEmpty(port, ": "+port),
					Category:    "network",
					Commands:    []string{"lsof -i :" + port, "netstat -tlnp | grep " + port, "kill -9 <PID>"},
					Explanation: "Another process is using the port. Find and stop the process or use a different port.",
					Confidence:  0.85,
				}
			},
		},

		// Python module not found
		{
			name:     "python_module",
			pattern:  regexp.MustCompile(`(?i)ModuleNotFoundError: No module named '(\w+)'`),
			category: "package",
			suggest: func(matches []string) *Suggestion {
				module := ""
				if len(matches) > 1 {
					module = matches[1]
				}
				return &Suggestion{
					Error:       "Python module not found: " + module,
					Category:    "package",
					Commands:    []string{"pip install " + module, "pip3 install " + module},
					Explanation: "The Python module is not installed. Install it with pip.",
					Confidence:  0.9,
				}
			},
		},

		// Node module not found
		{
			name:     "node_module",
			pattern:  regexp.MustCompile(`(?i)Cannot find module '([@\w/-]+)'`),
			category: "package",
			suggest: func(matches []string) *Suggestion {
				module := ""
				if len(matches) > 1 {
					module = matches[1]
				}
				return &Suggestion{
					Error:       "Node module not found: " + module,
					Category:    "package",
					Commands:    []string{"npm install " + module, "npm install"},
					Explanation: "The Node.js module is not installed. Install it with npm or run npm install to restore all dependencies.",
					Confidence:  0.9,
				}
			},
		},

		// Docker daemon not running
		{
			name:     "docker_daemon",
			pattern:  regexp.MustCompile(`(?i)Cannot connect to the Docker daemon`),
			category: "service",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "Docker daemon not running",
					Category:    "service",
					Commands:    []string{"sudo systemctl start docker", "sudo service docker start"},
					Explanation: "The Docker daemon is not running. Start it with systemctl.",
					Confidence:  0.9,
				}
			},
		},

		// apt lock file
		{
			name:     "apt_lock",
			pattern:  regexp.MustCompile(`(?i)Could not get lock|Unable to acquire the dpkg frontend lock`),
			category: "package",
			suggest: func(_ []string) *Suggestion {
				return &Suggestion{
					Error:       "Package manager is locked",
					Category:    "package",
					Commands:    []string{"sudo rm /var/lib/dpkg/lock-frontend", "sudo rm /var/lib/dpkg/lock", "sudo dpkg --configure -a"},
					Explanation: "Another package manager process is running or crashed. Wait for it to finish or remove the lock file.",
					Confidence:  0.7,
					Risky:       true,
				}
			},
		},

		// File not found
		{
			name:     "file_not_found",
			pattern:  regexp.MustCompile(`(?i)No such file or directory:\s*'?([^']+)'?`),
			category: "filesystem",
			suggest: func(matches []string) *Suggestion {
				file := ""
				if len(matches) > 1 {
					file = matches[1]
				}
				return &Suggestion{
					Error:       "File not found: " + file,
					Category:    "filesystem",
					Commands:    []string{"ls -la", "find . -name '" + extractFilename(file) + "'"},
					Explanation: "The file or directory does not exist. Check the path and try again.",
					Confidence:  0.6,
				}
			},
		},
	}
}

func suggestPackageInstall(cmd string) []string {
	// Common command -> package mappings
	packageMap := map[string][]string{
		"node":    {"sudo apt install nodejs", "brew install node"},
		"npm":     {"sudo apt install npm", "brew install node"},
		"python":  {"sudo apt install python3", "brew install python"},
		"pip":     {"sudo apt install python3-pip", "brew install python"},
		"docker":  {"sudo apt install docker.io", "brew install docker"},
		"git":     {"sudo apt install git", "brew install git"},
		"curl":    {"sudo apt install curl", "brew install curl"},
		"wget":    {"sudo apt install wget", "brew install wget"},
		"vim":     {"sudo apt install vim", "brew install vim"},
		"htop":    {"sudo apt install htop", "brew install htop"},
		"make":    {"sudo apt install build-essential", "xcode-select --install"},
		"gcc":     {"sudo apt install build-essential", "xcode-select --install"},
		"go":      {"sudo apt install golang", "brew install go"},
		"cargo":   {"curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"},
		"rustc":   {"curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"},
	}

	if suggestions, ok := packageMap[cmd]; ok {
		return suggestions
	}

	// Generic suggestions
	return []string{
		"sudo apt install " + cmd,
		"brew install " + cmd,
		"which " + cmd,
	}
}

func extractFilename(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func ifNotEmpty(s, prefix string) string {
	if s != "" {
		return prefix
	}
	return ""
}
