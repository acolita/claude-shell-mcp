// Package expect provides expect-like scripting for automated workflow handling.
package expect

import (
	"regexp"
	"time"
)

// Action defines what to do when a pattern is matched.
type Action int

const (
	// ActionSend sends the response text followed by newline.
	ActionSend Action = iota
	// ActionSendRaw sends the response text without newline.
	ActionSendRaw
	// ActionWait waits for the specified duration before continuing.
	ActionWait
	// ActionInterrupt sends Ctrl+C.
	ActionInterrupt
	// ActionSkip skips this prompt (let it timeout to the next step).
	ActionSkip
)

// Step defines a single expect step in a script.
type Step struct {
	// Name is a human-readable identifier for this step.
	Name string `yaml:"name" json:"name"`

	// Pattern is the regex pattern to match in the output.
	Pattern string `yaml:"pattern" json:"pattern"`

	// CompiledPattern is the compiled regex (set automatically).
	CompiledPattern *regexp.Regexp `yaml:"-" json:"-"`

	// Response is the text to send when pattern matches.
	Response string `yaml:"response" json:"response"`

	// Action defines how to handle the response (default: ActionSend).
	Action Action `yaml:"action" json:"action"`

	// Timeout is how long to wait for this pattern (0 = use default).
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// Optional means the step can be skipped if not matched.
	Optional bool `yaml:"optional" json:"optional"`

	// Repeat allows matching this step multiple times.
	Repeat bool `yaml:"repeat" json:"repeat"`

	// MaxRepeats limits how many times a repeating step can match (0 = unlimited).
	MaxRepeats int `yaml:"max_repeats" json:"max_repeats"`
}

// Script defines a complete expect script for a workflow.
type Script struct {
	// Name is the script identifier.
	Name string `yaml:"name" json:"name"`

	// Description explains what this script does.
	Description string `yaml:"description" json:"description"`

	// Command is the command that triggers this script (optional, for auto-detection).
	Command string `yaml:"command" json:"command"`

	// CommandPattern is a regex to match commands that trigger this script.
	CommandPattern string `yaml:"command_pattern" json:"command_pattern"`

	// CompiledCommandPattern is the compiled command pattern.
	CompiledCommandPattern *regexp.Regexp `yaml:"-" json:"-"`

	// Steps are the expect steps to execute in order.
	Steps []Step `yaml:"steps" json:"steps"`

	// DefaultTimeout is the default timeout for each step.
	DefaultTimeout time.Duration `yaml:"default_timeout" json:"default_timeout"`

	// FailOnUnexpected aborts if an unexpected prompt is encountered.
	FailOnUnexpected bool `yaml:"fail_on_unexpected" json:"fail_on_unexpected"`
}

// Compile compiles all regex patterns in the script.
func (s *Script) Compile() error {
	if s.CommandPattern != "" {
		re, err := regexp.Compile(s.CommandPattern)
		if err != nil {
			return err
		}
		s.CompiledCommandPattern = re
	}

	for i := range s.Steps {
		if s.Steps[i].Pattern != "" {
			re, err := regexp.Compile(s.Steps[i].Pattern)
			if err != nil {
				return err
			}
			s.Steps[i].CompiledPattern = re
		}
	}

	return nil
}

// MatchesCommand returns true if the given command matches this script.
func (s *Script) MatchesCommand(cmd string) bool {
	if s.Command != "" && s.Command == cmd {
		return true
	}
	if s.CompiledCommandPattern != nil {
		return s.CompiledCommandPattern.MatchString(cmd)
	}
	return false
}

// DefaultScripts returns built-in scripts for common workflows.
func DefaultScripts() []*Script {
	scripts := []*Script{
		npmInitScript(),
		gitRebaseScript(),
		aptUpgradeScript(),
		sshHostKeyScript(),
		sudoPasswordScript(),
	}

	// Compile all scripts
	for _, s := range scripts {
		_ = s.Compile()
	}

	return scripts
}

func npmInitScript() *Script {
	return &Script{
		Name:           "npm_init",
		Description:    "Handles npm init prompts with default values",
		CommandPattern: `^npm init(\s|$)`,
		DefaultTimeout: 5 * time.Second,
		Steps: []Step{
			{
				Name:     "package_name",
				Pattern:  `package name:.*\(.*\)`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "version",
				Pattern:  `version:.*\(.*\)`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "description",
				Pattern:  `description:`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "entry_point",
				Pattern:  `entry point:.*\(.*\)`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "test_command",
				Pattern:  `test command:`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "git_repository",
				Pattern:  `git repository:`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "keywords",
				Pattern:  `keywords:`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "author",
				Pattern:  `author:`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "license",
				Pattern:  `license:.*\(.*\)`,
				Response: "", // Accept default
				Optional: true,
			},
			{
				Name:     "confirm",
				Pattern:  `Is this OK\?.*\(yes\)`,
				Response: "yes",
				Optional: true,
			},
		},
	}
}

func gitRebaseScript() *Script {
	return &Script{
		Name:           "git_rebase_continue",
		Description:    "Handles git rebase prompts",
		CommandPattern: `^git rebase`,
		DefaultTimeout: 10 * time.Second,
		Steps: []Step{
			{
				Name:       "conflict_resolved",
				Pattern:    `all conflicts fixed: run "git rebase --continue"`,
				Response:   "git rebase --continue",
				Action:     ActionSend,
				Optional:   true,
				Repeat:     true,
				MaxRepeats: 10,
			},
		},
	}
}

func aptUpgradeScript() *Script {
	return &Script{
		Name:           "apt_upgrade",
		Description:    "Handles apt upgrade prompts",
		CommandPattern: `^(sudo\s+)?(apt|apt-get)\s+(upgrade|dist-upgrade|full-upgrade)`,
		DefaultTimeout: 10 * time.Second,
		Steps: []Step{
			{
				Name:     "continue",
				Pattern:  `Do you want to continue\?\s*\[Y/n\]`,
				Response: "Y",
				Repeat:   true,
			},
			{
				Name:     "restart_services",
				Pattern:  `Restart services during package upgrades`,
				Response: "", // Accept default (Yes)
				Optional: true,
			},
			{
				Name:     "kernel_upgrade",
				Pattern:  `keep the local version currently installed`,
				Response: "", // Accept default
				Optional: true,
			},
		},
	}
}

func sshHostKeyScript() *Script {
	return &Script{
		Name:           "ssh_host_key",
		Description:    "Handles SSH host key verification",
		CommandPattern: `^ssh\s+`,
		DefaultTimeout: 30 * time.Second,
		Steps: []Step{
			{
				Name:     "accept_host_key",
				Pattern:  `Are you sure you want to continue connecting.*\(yes/no(/\[fingerprint\])?\)\?`,
				Response: "yes",
				Optional: true,
			},
		},
	}
}

func sudoPasswordScript() *Script {
	return &Script{
		Name:           "sudo_password",
		Description:    "Handles sudo password prompts (placeholder - actual password handling is separate)",
		CommandPattern: `^sudo\s+`,
		DefaultTimeout: 30 * time.Second,
		Steps: []Step{
			{
				Name:     "password",
				Pattern:  `\[sudo\]\s+password\s+for\s+\w+:`,
				Response: "", // Password is handled separately via provide_input
				Action:   ActionSkip,
				Optional: true,
			},
		},
	}
}
