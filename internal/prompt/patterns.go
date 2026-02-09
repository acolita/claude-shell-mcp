// Package prompt provides interactive prompt detection for shell sessions.
package prompt

import "regexp"

// PromptType indicates the type of prompt detected.
type PromptType string

const (
	PromptTypePassword     PromptType = "password"
	PromptTypeConfirmation PromptType = "confirmation"
	PromptTypeText         PromptType = "text"
	PromptTypeEditor       PromptType = "editor"
	PromptTypePager        PromptType = "pager"
)

// Pattern represents a prompt detection pattern.
type Pattern struct {
	Name              string
	Regex             *regexp.Regexp
	Type              PromptType
	MaskInput         bool
	SuggestedResponse string
}

// DefaultPatterns returns the built-in prompt patterns.
func DefaultPatterns() []Pattern {
	return []Pattern{
		// Sudo password prompts
		{
			Name:      "sudo_password",
			Regex:     regexp.MustCompile(`(?i)\[sudo\]\s+password\s+for\s+\w+:\s*$`),
			Type:      PromptTypePassword,
			MaskInput: true,
		},
		{
			Name:      "sudo_password_generic",
			Regex:     regexp.MustCompile(`(?i)password:\s*$`),
			Type:      PromptTypePassword,
			MaskInput: true,
		},

		// SSH host key confirmation
		{
			Name:              "ssh_host_key",
			Regex:             regexp.MustCompile(`(?i)are you sure you want to continue connecting \(yes/no(/\[fingerprint\])?\)\?`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "yes",
		},
		{
			Name:              "ssh_host_key_ecdsa",
			Regex:             regexp.MustCompile(`(?i)are you sure you want to continue connecting \(yes/no\)\?`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "yes",
		},

		// Package manager confirmations
		{
			Name:              "apt_confirmation",
			Regex:             regexp.MustCompile(`(?i)do you want to continue\?\s*\[Y/n\]\s*$`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "Y",
		},
		{
			Name:              "yum_confirmation",
			Regex:             regexp.MustCompile(`(?i)is this ok \[y/d/N\]:\s*$`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},
		{
			Name:              "pacman_confirmation",
			Regex:             regexp.MustCompile(`(?i)proceed with installation\?\s*\[Y/n\]\s*$`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "Y",
		},

		// npm/node prompts
		{
			Name:  "npm_init_name",
			Regex: regexp.MustCompile(`(?i)package name:\s*\([^)]*\)\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:  "npm_init_version",
			Regex: regexp.MustCompile(`(?i)version:\s*\([^)]*\)\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:  "npm_init_description",
			Regex: regexp.MustCompile(`(?i)description:\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:  "npm_init_entry",
			Regex: regexp.MustCompile(`(?i)entry point:\s*\([^)]*\)\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:              "npm_ok",
			Regex:             regexp.MustCompile(`(?i)is this ok\?\s*\(yes\)\s*$`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "yes",
		},

		// Git prompts
		{
			Name:      "git_username",
			Regex:     regexp.MustCompile(`(?i)username for '.*':\s*$`),
			Type:      PromptTypeText,
			MaskInput: false,
		},
		{
			Name:      "git_password",
			Regex:     regexp.MustCompile(`(?i)password for '.*':\s*$`),
			Type:      PromptTypePassword,
			MaskInput: true,
		},

		// Git interactive rebase
		{
			Name:              "git_rebase_pick",
			Regex:             regexp.MustCompile(`(?m)^pick [0-9a-f]+ `),
			Type:              PromptTypeEditor,
			SuggestedResponse: ":wq (to proceed) or :q! (to abort)",
		},
		{
			Name:              "git_rebase_commands",
			Regex:             regexp.MustCompile(`(?i)# Commands:\s*$`),
			Type:              PromptTypeEditor,
			SuggestedResponse: ":wq (to proceed) or :q! (to abort)",
		},
		{
			Name:              "git_commit_message",
			Regex:             regexp.MustCompile(`(?m)^# Please enter the commit message`),
			Type:              PromptTypeEditor,
			SuggestedResponse: ":wq (to save) or :q! (to abort)",
		},

		// Git merge conflicts
		{
			Name:  "git_merge_conflict",
			Regex: regexp.MustCompile(`(?m)^<<<<<<<\s+`),
			Type:  PromptTypeText,
		},
		{
			Name:              "git_merge_tool",
			Regex:             regexp.MustCompile(`(?i)was the merge successful\?\s*\[y/n\]`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},
		{
			Name:              "git_continue_rebase",
			Regex:             regexp.MustCompile(`(?i)all conflicts fixed.*run "git rebase --continue"`),
			Type:              PromptTypeText,
			SuggestedResponse: "git rebase --continue",
		},

		// Generic yes/no
		{
			Name:              "yes_no_generic",
			Regex:             regexp.MustCompile(`(?i)\[yes/no\]\s*$`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "yes",
		},
		{
			Name:              "y_n_generic",
			Regex:             regexp.MustCompile(`(?i)\[y/n\]\s*$`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},

		// Editor detection (vim, nano, emacs)
		{
			Name:              "vim_editor",
			Regex:             regexp.MustCompile(`(?m)^~\s*$.*^~\s*$`), // Vim shows ~ for empty lines
			Type:              PromptTypeEditor,
			SuggestedResponse: ":q!",
		},
		{
			Name:              "nano_editor",
			Regex:             regexp.MustCompile(`(?i)GNU nano`),
			Type:              PromptTypeEditor,
			SuggestedResponse: "Ctrl+X",
		},
		{
			Name:              "vim_command_mode",
			Regex:             regexp.MustCompile(`(?m)^:.*$`), // Vim command mode
			Type:              PromptTypeEditor,
			SuggestedResponse: ":q!",
		},

		// Pager detection (less, more)
		{
			Name:              "less_pager",
			Regex:             regexp.MustCompile(`(?i)\(END\)\s*$`),
			Type:              PromptTypePager,
			SuggestedResponse: "q",
		},
		{
			Name:              "more_pager",
			Regex:             regexp.MustCompile(`--More--`),
			Type:              PromptTypePager,
			SuggestedResponse: "q",
		},
		{
			Name:              "man_pager",
			Regex:             regexp.MustCompile(`Manual page.*line \d+`),
			Type:              PromptTypePager,
			SuggestedResponse: "q",
		},

		// Docker prompts
		{
			Name:              "docker_remove_confirm",
			Regex:             regexp.MustCompile(`(?i)are you sure you want to remove.*\[y/N\]`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},
		{
			Name:              "docker_prune_confirm",
			Regex:             regexp.MustCompile(`(?i)are you sure you want to continue\?\s*\[y/N\]`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},
		{
			Name:              "docker_build_confirm",
			Regex:             regexp.MustCompile(`(?i)do you want to continue.*\[y/N\]`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},
		{
			Name:  "docker_container_shell",
			Regex: regexp.MustCompile(`(?m)^(root|[\w-]+)@[a-f0-9]{12}[:#]\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:  "docker_compose_interactive",
			Regex: regexp.MustCompile(`(?i)attaching to\s+[\w-]+`),
			Type:  PromptTypeText,
		},

		// Database CLI prompts
		{
			Name:  "mysql_prompt",
			Regex: regexp.MustCompile(`mysql>\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:  "postgres_prompt",
			Regex: regexp.MustCompile(`\w+=>\s*$`), // psql prompt like: dbname=>
			Type:  PromptTypeText,
		},
		{
			Name:  "redis_prompt",
			Regex: regexp.MustCompile(`\d+\.\d+\.\d+\.\d+:\d+>\s*$`), // redis-cli prompt
			Type:  PromptTypeText,
		},
		// Note: MongoDB and Node REPL prompts are just "> " which conflicts with
		// bash's PS2 continuation prompt. We omit these to avoid false positives.
		// Detection relies on stall-based interactive detection instead.

		// Python/Ruby REPL prompts (distinctive enough to avoid false positives)
		{
			Name:  "python_prompt",
			Regex: regexp.MustCompile(`>>>\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name: "python_continuation",
			// Match Python's "..." continuation prompt, but only when it's alone on a line
			// or at the start of a line (not when text ends with "...")
			Regex: regexp.MustCompile(`(?m)^\.\.\.\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:  "ruby_irb_prompt",
			Regex: regexp.MustCompile(`irb\([^)]+\):\d+:\d+>\s*$`),
			Type:  PromptTypeText,
		},
		{
			Name:  "node_welcome",
			Regex: regexp.MustCompile(`Welcome to Node\.js`), // Detect node REPL by welcome message
			Type:  PromptTypeText,
		},

		// Overwrite/replace confirmations
		{
			Name:              "overwrite_confirm",
			Regex:             regexp.MustCompile(`(?i)overwrite.*\?\s*\[y/N\]`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},
		{
			Name:              "replace_confirm",
			Regex:             regexp.MustCompile(`(?i)replace.*\?\s*\[y/N\]`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},

		// SSH/SCP prompts
		{
			Name:      "ssh_passphrase",
			Regex:     regexp.MustCompile(`(?i)enter passphrase for key.*:\s*$`),
			Type:      PromptTypePassword,
			MaskInput: true,
		},

		// curl/wget prompts
		{
			Name:              "curl_insecure",
			Regex:             regexp.MustCompile(`(?i)proceed anyway\?\s*\[y/N\]`),
			Type:              PromptTypeConfirmation,
			SuggestedResponse: "y",
		},
	}
}
