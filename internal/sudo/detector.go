// Package sudo provides smart sudo detection and handling.
package sudo

import (
	"regexp"
	"strings"
)

// Prediction represents a sudo requirement prediction.
type Prediction struct {
	NeedsSudo   bool
	Confidence  float64
	Reason      string
	Alternative string // Alternative command that doesn't need sudo
}

// Detector analyzes commands to predict sudo requirements.
type Detector struct {
	sudoPaths       []string // Paths that typically require sudo
	sudoCommands    []string // Commands that typically require sudo
	noSudoCommands  []string // Commands that never need sudo
	systemdPatterns []*regexp.Regexp
}

// NewDetector creates a new sudo detector with default rules.
func NewDetector() *Detector {
	return &Detector{
		sudoPaths: []string{
			"/etc/", "/usr/", "/var/", "/opt/", "/root/",
			"/sys/", "/proc/", "/boot/", "/lib/", "/sbin/",
		},
		sudoCommands: []string{
			"apt", "apt-get", "dpkg", "yum", "dnf", "pacman",
			"systemctl", "service", "journalctl",
			"mount", "umount", "fdisk", "parted",
			"useradd", "userdel", "usermod", "groupadd", "passwd",
			"chown", "chmod", "chgrp",
			"iptables", "ufw", "firewall-cmd",
			"docker", "podman",
			"snap", "flatpak",
		},
		noSudoCommands: []string{
			"ls", "cat", "less", "more", "head", "tail",
			"grep", "find", "which", "whereis", "type",
			"echo", "printf", "env", "printenv",
			"pwd", "cd", "pushd", "popd",
			"date", "cal", "uptime", "whoami", "id",
			"ps", "top", "htop", "free", "df", "du",
			"git", "npm", "node", "python", "pip",
			"curl", "wget", "ssh", "scp",
			"vim", "nano", "emacs", "code",
			"man", "help", "info",
		},
		systemdPatterns: []*regexp.Regexp{
			regexp.MustCompile(`systemctl\s+(start|stop|restart|enable|disable|reload)`),
			regexp.MustCompile(`service\s+\S+\s+(start|stop|restart)`),
		},
	}
}

// Predict analyzes a command and predicts if sudo is needed.
func (d *Detector) Predict(command string) *Prediction {
	command = strings.TrimSpace(command)

	// Already has sudo
	if strings.HasPrefix(command, "sudo ") {
		return &Prediction{
			NeedsSudo:  false,
			Confidence: 1.0,
			Reason:     "Command already uses sudo",
		}
	}

	// Parse command
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return &Prediction{
			NeedsSudo:  false,
			Confidence: 1.0,
			Reason:     "Empty command",
		}
	}

	baseCmd := extractBaseCommand(parts[0])

	// Check systemd patterns first (most specific)
	for _, pattern := range d.systemdPatterns {
		if pattern.MatchString(command) {
			return &Prediction{
				NeedsSudo:   true,
				Confidence:  0.95,
				Reason:      "Systemd service management requires sudo",
				Alternative: "",
			}
		}
	}

	// Check for package management patterns (before general sudo commands)
	if pred := d.checkPackageManagement(command); pred != nil {
		return pred
	}

	// Check if writing to sudo paths
	if pred := d.checkPathAccess(command, parts); pred != nil {
		return pred
	}

	// Check if command typically requires sudo
	for _, sudoCmd := range d.sudoCommands {
		if baseCmd == sudoCmd {
			pred := &Prediction{
				NeedsSudo:  true,
				Confidence: 0.8,
				Reason:     "Command '" + baseCmd + "' typically requires sudo",
			}

			// Check for alternatives
			pred.Alternative = d.suggestAlternative(baseCmd, parts)
			return pred
		}
	}

	// Check if command never needs sudo
	for _, noSudo := range d.noSudoCommands {
		if baseCmd == noSudo {
			return &Prediction{
				NeedsSudo:  false,
				Confidence: 0.9,
				Reason:     "Command typically doesn't require sudo",
			}
		}
	}

	// Default: probably doesn't need sudo
	return &Prediction{
		NeedsSudo:  false,
		Confidence: 0.6,
		Reason:     "No sudo indicators detected",
	}
}

func extractBaseCommand(cmd string) string {
	// Remove path prefix
	if strings.Contains(cmd, "/") {
		parts := strings.Split(cmd, "/")
		cmd = parts[len(parts)-1]
	}
	return cmd
}

func (d *Detector) checkPathAccess(command string, parts []string) *Prediction {
	// Check if any argument is a sudo-requiring path
	for _, part := range parts[1:] {
		for _, sudoPath := range d.sudoPaths {
			if strings.HasPrefix(part, sudoPath) {
				// Check if it's a write operation
				if d.isWriteOperation(parts[0]) {
					return &Prediction{
						NeedsSudo:  true,
						Confidence: 0.85,
						Reason:     "Writing to system path: " + sudoPath,
					}
				}
			}
		}
	}
	return nil
}

func (d *Detector) isWriteOperation(cmd string) bool {
	writeCommands := []string{
		"cp", "mv", "rm", "mkdir", "rmdir", "touch",
		"tee", "dd",
		"chmod", "chown", "chgrp",
		"ln", "install",
	}
	baseCmd := extractBaseCommand(cmd)
	for _, wc := range writeCommands {
		if baseCmd == wc {
			return true
		}
	}
	return false
}

func (d *Detector) checkPackageManagement(command string) *Prediction {
	patterns := []struct {
		pattern *regexp.Regexp
		reason  string
	}{
		{regexp.MustCompile(`apt(-get)?\s+(install|remove|purge|update|upgrade|dist-upgrade)`), "Package installation/removal"},
		{regexp.MustCompile(`yum\s+(install|remove|update)`), "Package installation/removal"},
		{regexp.MustCompile(`dnf\s+(install|remove|update)`), "Package installation/removal"},
		{regexp.MustCompile(`pacman\s+-S`), "Package installation"},
		{regexp.MustCompile(`npm\s+install\s+-g`), "Global npm package installation"},
	}

	// Special check for pip - needs sudo unless --user is present
	if strings.Contains(command, "pip install") && !strings.Contains(command, "--user") {
		return &Prediction{
			NeedsSudo:   true,
			Confidence:  0.7,
			Reason:      "System-wide pip installation typically requires sudo",
			Alternative: "Use pip install --user or virtualenv",
		}
	}

	// Special check for npm -g
	if strings.Contains(command, "npm install -g") || strings.Contains(command, "npm install --global") {
		return &Prediction{
			NeedsSudo:   true,
			Confidence:  0.8,
			Reason:      "Global npm package installation typically requires sudo",
			Alternative: "Use npm --prefix ~/.local or npx",
		}
	}

	for _, p := range patterns {
		if p.pattern.MatchString(command) {
			return &Prediction{
				NeedsSudo:  true,
				Confidence: 0.9,
				Reason:     p.reason + " typically requires sudo",
			}
		}
	}

	return nil
}

func (d *Detector) suggestAlternative(cmd string, parts []string) string {
	alternatives := map[string]string{
		"docker": "Add user to docker group: sudo usermod -aG docker $USER",
		"npm":    "Use npm --prefix ~/.local or npx",
		"pip":    "Use pip install --user or virtualenv",
	}

	if alt, ok := alternatives[cmd]; ok {
		return alt
	}

	return ""
}

// IsSudoPrompt checks if the output indicates a sudo password prompt.
func IsSudoPrompt(output string) bool {
	patterns := []string{
		`\[sudo\]\s+password\s+for\s+\w+:`,
		`^Password:\s*$`,                // Just "Password:" alone
		`password\s+for\s+\w+:\s*$`,     // "password for user:"
	}

	for _, p := range patterns {
		if matched, _ := regexp.MatchString(`(?im)`+p, output); matched {
			return true
		}
	}

	return false
}

// IsSudoError checks if the output indicates a sudo-related error.
func IsSudoError(output string) bool {
	errorPatterns := []string{
		`sudo:\s+\d+ incorrect password attempts`,
		`Sorry, try again`,
		`sudo:\s+a password is required`,
		`is not in the sudoers file`,
		`user .+ is not allowed to execute`,
	}

	for _, p := range errorPatterns {
		if matched, _ := regexp.MatchString(`(?i)`+p, output); matched {
			return true
		}
	}

	return false
}

// SudoErrorType represents the type of sudo error.
type SudoErrorType int

const (
	SudoErrorNone SudoErrorType = iota
	SudoErrorWrongPassword
	SudoErrorNotInSudoers
	SudoErrorNotAllowed
	SudoErrorTimeout
)

// ParseSudoError identifies the specific sudo error type.
func ParseSudoError(output string) SudoErrorType {
	lowerOutput := strings.ToLower(output)

	if strings.Contains(lowerOutput, "incorrect password") ||
		strings.Contains(lowerOutput, "sorry, try again") {
		return SudoErrorWrongPassword
	}

	if strings.Contains(lowerOutput, "is not in the sudoers file") {
		return SudoErrorNotInSudoers
	}

	if strings.Contains(lowerOutput, "is not allowed to execute") {
		return SudoErrorNotAllowed
	}

	if strings.Contains(lowerOutput, "timestamp timeout") {
		return SudoErrorTimeout
	}

	return SudoErrorNone
}

// SuggestFix provides suggestions for sudo errors.
func SuggestFix(errorType SudoErrorType) string {
	switch errorType {
	case SudoErrorWrongPassword:
		return "Check that you're entering the correct password. The password is for your user account, not root."
	case SudoErrorNotInSudoers:
		return "Your user is not in the sudoers file. Contact your system administrator or use: usermod -aG sudo <username>"
	case SudoErrorNotAllowed:
		return "Your user is not allowed to run this specific command with sudo. Check /etc/sudoers configuration."
	case SudoErrorTimeout:
		return "Sudo session timed out. Run the command again and provide your password."
	default:
		return ""
	}
}
