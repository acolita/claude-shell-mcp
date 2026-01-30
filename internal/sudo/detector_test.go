package sudo

import (
	"testing"
)

func TestNewDetector(t *testing.T) {
	d := NewDetector()
	if d == nil {
		t.Fatal("NewDetector returned nil")
	}
	if len(d.sudoCommands) == 0 {
		t.Error("Detector should have sudo commands")
	}
	if len(d.noSudoCommands) == 0 {
		t.Error("Detector should have no-sudo commands")
	}
}

func TestDetector_AlreadyHasSudo(t *testing.T) {
	d := NewDetector()

	pred := d.Predict("sudo apt update")
	if pred.NeedsSudo {
		t.Error("Command already has sudo, should not need more")
	}
	if pred.Confidence != 1.0 {
		t.Errorf("Confidence should be 1.0, got %f", pred.Confidence)
	}
}

func TestDetector_SystemdCommands(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		command   string
		needsSudo bool
	}{
		{"systemctl start nginx", true},
		{"systemctl stop docker", true},
		{"systemctl restart sshd", true},
		{"systemctl enable nginx", true},
		{"systemctl status nginx", true}, // systemctl is in sudoCommands list
		{"service apache2 start", true},
		{"service mysql stop", true},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			pred := d.Predict(tt.command)
			if pred.NeedsSudo != tt.needsSudo {
				t.Errorf("NeedsSudo = %v, want %v (reason: %s)", pred.NeedsSudo, tt.needsSudo, pred.Reason)
			}
		})
	}
}

func TestDetector_PackageManagement(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		command   string
		needsSudo bool
	}{
		{"apt install vim", true},
		{"apt-get install curl", true},
		{"apt update", true},
		{"apt upgrade", true},
		{"yum install httpd", true},
		{"dnf install gcc", true},
		{"npm install -g typescript", true},
		{"npm install express", false}, // local install
		{"pip install requests", true},
		{"pip install --user requests", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			pred := d.Predict(tt.command)
			if pred.NeedsSudo != tt.needsSudo {
				t.Errorf("NeedsSudo = %v, want %v (reason: %s)", pred.NeedsSudo, tt.needsSudo, pred.Reason)
			}
		})
	}
}

func TestDetector_NoSudoCommands(t *testing.T) {
	d := NewDetector()

	commands := []string{
		"ls -la",
		"cat /etc/passwd",
		"grep root /etc/passwd",
		"git status",
		"npm install",
		"python script.py",
		"curl https://example.com",
		"vim file.txt",
	}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			pred := d.Predict(cmd)
			if pred.NeedsSudo {
				t.Errorf("Command %q should not need sudo (reason: %s)", cmd, pred.Reason)
			}
		})
	}
}

func TestDetector_PathAccess(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		command   string
		needsSudo bool
	}{
		{"cp file.txt /etc/myconfig", true},
		{"mv file.txt /usr/local/bin/", true},
		{"rm /var/log/mylog", true},
		{"mkdir /opt/myapp", true},
		{"cp file.txt ~/backup/", false},
		{"mv file.txt ./newname.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			pred := d.Predict(tt.command)
			if pred.NeedsSudo != tt.needsSudo {
				t.Errorf("NeedsSudo = %v, want %v (reason: %s)", pred.NeedsSudo, tt.needsSudo, pred.Reason)
			}
		})
	}
}

func TestIsSudoPrompt(t *testing.T) {
	tests := []struct {
		output   string
		expected bool
	}{
		{"[sudo] password for user: ", true},
		{"Password: ", true},
		{"password for admin: ", true},
		{"Enter password: ", false},
		{"Authentication failed", false},
		{"user@host:~$ ", false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			if got := IsSudoPrompt(tt.output); got != tt.expected {
				t.Errorf("IsSudoPrompt(%q) = %v, want %v", tt.output, got, tt.expected)
			}
		})
	}
}

func TestIsSudoError(t *testing.T) {
	tests := []struct {
		output   string
		expected bool
	}{
		{"sudo: 3 incorrect password attempts", true},
		{"Sorry, try again.", true},
		{"user is not in the sudoers file", true},
		{"user bob is not allowed to execute", true},
		{"Command executed successfully", false},
		{"Permission denied", false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			if got := IsSudoError(tt.output); got != tt.expected {
				t.Errorf("IsSudoError(%q) = %v, want %v", tt.output, got, tt.expected)
			}
		})
	}
}

func TestParseSudoError(t *testing.T) {
	tests := []struct {
		output   string
		expected SudoErrorType
	}{
		{"sudo: 3 incorrect password attempts", SudoErrorWrongPassword},
		{"Sorry, try again.", SudoErrorWrongPassword},
		{"user is not in the sudoers file", SudoErrorNotInSudoers},
		{"user bob is not allowed to execute '/bin/rm'", SudoErrorNotAllowed},
		{"Command succeeded", SudoErrorNone},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			if got := ParseSudoError(tt.output); got != tt.expected {
				t.Errorf("ParseSudoError(%q) = %v, want %v", tt.output, got, tt.expected)
			}
		})
	}
}

func TestSuggestFix(t *testing.T) {
	tests := []struct {
		errorType SudoErrorType
		wantEmpty bool
	}{
		{SudoErrorWrongPassword, false},
		{SudoErrorNotInSudoers, false},
		{SudoErrorNotAllowed, false},
		{SudoErrorTimeout, false},
		{SudoErrorNone, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			fix := SuggestFix(tt.errorType)
			if (fix == "") != tt.wantEmpty {
				t.Errorf("SuggestFix(%v) = %q, wantEmpty = %v", tt.errorType, fix, tt.wantEmpty)
			}
		})
	}
}

func TestDetector_Alternatives(t *testing.T) {
	d := NewDetector()

	// Docker is in sudoCommands, should have alternative
	pred := d.Predict("docker ps")
	if pred.Alternative == "" {
		t.Error("Docker command should have an alternative suggestion")
	}

	// pip is in sudoCommands, should have alternative
	pred = d.Predict("pip install flask")
	if pred.NeedsSudo && pred.Alternative == "" {
		t.Error("pip install should have an alternative suggestion")
	}
}

func TestExtractBaseCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ls", "ls"},
		{"/bin/ls", "ls"},
		{"/usr/bin/vim", "vim"},
		{"./script.sh", "script.sh"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := extractBaseCommand(tt.input); got != tt.expected {
				t.Errorf("extractBaseCommand(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
