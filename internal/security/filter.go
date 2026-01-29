// Package security provides security-related functionality.
package security

import (
	"fmt"
	"regexp"
	"sync"
)

// CommandFilter filters commands based on blocklist/allowlist patterns.
type CommandFilter struct {
	mu        sync.RWMutex
	blocklist []*regexp.Regexp
	allowlist []*regexp.Regexp
}

// NewCommandFilter creates a new command filter with the given patterns.
func NewCommandFilter(blocklist, allowlist []string) (*CommandFilter, error) {
	cf := &CommandFilter{}

	for _, pattern := range blocklist {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid blocklist pattern %q: %w", pattern, err)
		}
		cf.blocklist = append(cf.blocklist, re)
	}

	for _, pattern := range allowlist {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid allowlist pattern %q: %w", pattern, err)
		}
		cf.allowlist = append(cf.allowlist, re)
	}

	return cf, nil
}

// IsAllowed checks if a command is allowed to execute.
// Returns (allowed, reason).
func (cf *CommandFilter) IsAllowed(command string) (bool, string) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	// Check blocklist first
	for _, re := range cf.blocklist {
		if re.MatchString(command) {
			return false, fmt.Sprintf("command blocked by pattern: %s", re.String())
		}
	}

	// If allowlist is set, command must match at least one pattern
	if len(cf.allowlist) > 0 {
		for _, re := range cf.allowlist {
			if re.MatchString(command) {
				return true, ""
			}
		}
		return false, "command not in allowlist"
	}

	return true, ""
}

// HasBlocklist returns true if any blocklist patterns are configured.
func (cf *CommandFilter) HasBlocklist() bool {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	return len(cf.blocklist) > 0
}

// HasAllowlist returns true if any allowlist patterns are configured.
func (cf *CommandFilter) HasAllowlist() bool {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	return len(cf.allowlist) > 0
}

// DefaultBlocklist returns a set of commonly dangerous patterns.
func DefaultBlocklist() []string {
	return []string{
		`rm\s+-rf\s+/\s*$`,           // rm -rf /
		`rm\s+-rf\s+/\*`,             // rm -rf /*
		`mkfs\.`,                     // mkfs commands
		`dd\s+.*of=/dev/[sh]d`,       // dd to raw devices
		`:\s*\(\s*\)\s*\{\s*:\s*\|`,  // fork bomb
		`>\s*/dev/[sh]d`,             // redirect to raw devices
	}
}
