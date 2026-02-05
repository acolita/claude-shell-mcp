package security

import (
	"testing"
)

func TestCommandFilter_HasBlocklist_True(t *testing.T) {
	cf, err := NewCommandFilter([]string{`rm\s+-rf`}, nil)
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if !cf.HasBlocklist() {
		t.Error("HasBlocklist() = false, want true when blocklist patterns exist")
	}
}

func TestCommandFilter_HasBlocklist_False(t *testing.T) {
	cf, err := NewCommandFilter(nil, nil)
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if cf.HasBlocklist() {
		t.Error("HasBlocklist() = true, want false when no blocklist patterns")
	}
}

func TestCommandFilter_HasBlocklist_EmptySlice(t *testing.T) {
	cf, err := NewCommandFilter([]string{}, nil)
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if cf.HasBlocklist() {
		t.Error("HasBlocklist() = true, want false when blocklist is empty slice")
	}
}

func TestCommandFilter_HasAllowlist_True(t *testing.T) {
	cf, err := NewCommandFilter(nil, []string{`^ls`, `^cat`})
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if !cf.HasAllowlist() {
		t.Error("HasAllowlist() = false, want true when allowlist patterns exist")
	}
}

func TestCommandFilter_HasAllowlist_False(t *testing.T) {
	cf, err := NewCommandFilter(nil, nil)
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if cf.HasAllowlist() {
		t.Error("HasAllowlist() = true, want false when no allowlist patterns")
	}
}

func TestCommandFilter_HasAllowlist_EmptySlice(t *testing.T) {
	cf, err := NewCommandFilter(nil, []string{})
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if cf.HasAllowlist() {
		t.Error("HasAllowlist() = true, want false when allowlist is empty slice")
	}
}

func TestCommandFilter_HasBoth(t *testing.T) {
	cf, err := NewCommandFilter([]string{`rm\s+-rf`}, []string{`^ls`})
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if !cf.HasBlocklist() {
		t.Error("HasBlocklist() = false, want true")
	}
	if !cf.HasAllowlist() {
		t.Error("HasAllowlist() = false, want true")
	}
}

func TestCommandFilter_HasNeither(t *testing.T) {
	cf, err := NewCommandFilter(nil, nil)
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	if cf.HasBlocklist() {
		t.Error("HasBlocklist() = true, want false")
	}
	if cf.HasAllowlist() {
		t.Error("HasAllowlist() = true, want false")
	}
}

func TestCommandFilter_InvalidAllowlistRegex(t *testing.T) {
	_, err := NewCommandFilter(nil, []string{`[invalid`})
	if err == nil {
		t.Error("expected error for invalid allowlist regex, got nil")
	}
}

func TestCommandFilter_BlocklistTakesPrecedence(t *testing.T) {
	// When a command matches both blocklist and allowlist, blocklist wins
	cf, err := NewCommandFilter(
		[]string{`rm\s+-rf`},
		[]string{`^rm`},
	)
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	allowed, reason := cf.IsAllowed("rm -rf /tmp/test")
	if allowed {
		t.Error("IsAllowed() = true, want false; blocklist should take precedence over allowlist")
	}
	if reason == "" {
		t.Error("expected non-empty reason when blocked")
	}
}

func TestCommandFilter_AllowlistNotInList(t *testing.T) {
	cf, err := NewCommandFilter(nil, []string{`^ls`, `^cat`})
	if err != nil {
		t.Fatalf("NewCommandFilter() error = %v", err)
	}

	allowed, reason := cf.IsAllowed("echo hello")
	if allowed {
		t.Error("IsAllowed() = true, want false; command not in allowlist")
	}
	if reason == "" {
		t.Error("expected non-empty reason when not in allowlist")
	}
}
