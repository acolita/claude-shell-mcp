package security

import (
	"testing"
	"time"
)

func TestAuthRateLimiter_NotLockedInitially(t *testing.T) {
	rl := NewAuthRateLimiter(3, 5*time.Minute)

	locked, _ := rl.IsLocked("host", "user")
	if locked {
		t.Error("expected not locked initially")
	}
}

func TestAuthRateLimiter_LockAfterMaxFailures(t *testing.T) {
	rl := NewAuthRateLimiter(3, 5*time.Minute)

	// Record failures
	rl.RecordFailure("host", "user")
	rl.RecordFailure("host", "user")

	// Should not be locked yet (2 failures, max is 3)
	locked, _ := rl.IsLocked("host", "user")
	if locked {
		t.Error("should not be locked after 2 failures")
	}

	// Third failure triggers lockout
	rl.RecordFailure("host", "user")

	locked, remaining := rl.IsLocked("host", "user")
	if !locked {
		t.Error("should be locked after 3 failures")
	}
	if remaining <= 0 {
		t.Error("remaining time should be > 0")
	}
}

func TestAuthRateLimiter_SuccessResetsCount(t *testing.T) {
	rl := NewAuthRateLimiter(3, 5*time.Minute)

	// Record 2 failures
	rl.RecordFailure("host", "user")
	rl.RecordFailure("host", "user")

	// Success resets
	rl.RecordSuccess("host", "user")

	// Should not be locked
	locked, _ := rl.IsLocked("host", "user")
	if locked {
		t.Error("should not be locked after success")
	}

	// 3 more failures needed to lock
	rl.RecordFailure("host", "user")
	rl.RecordFailure("host", "user")

	locked, _ = rl.IsLocked("host", "user")
	if locked {
		t.Error("should not be locked after 2 failures post-success")
	}
}

func TestAuthRateLimiter_DifferentUsers(t *testing.T) {
	rl := NewAuthRateLimiter(2, 5*time.Minute)

	// Lock user1
	rl.RecordFailure("host", "user1")
	rl.RecordFailure("host", "user1")

	// user1 should be locked
	locked, _ := rl.IsLocked("host", "user1")
	if !locked {
		t.Error("user1 should be locked")
	}

	// user2 should not be locked
	locked, _ = rl.IsLocked("host", "user2")
	if locked {
		t.Error("user2 should not be locked")
	}
}

func TestAuthRateLimiter_LockoutExpires(t *testing.T) {
	// Use very short lockout for testing
	rl := NewAuthRateLimiter(1, 50*time.Millisecond)

	rl.RecordFailure("host", "user")

	// Should be locked
	locked, _ := rl.IsLocked("host", "user")
	if !locked {
		t.Error("should be locked immediately after failure")
	}

	// Wait for lockout to expire
	time.Sleep(100 * time.Millisecond)

	// Should no longer be locked
	locked, _ = rl.IsLocked("host", "user")
	if locked {
		t.Error("lockout should have expired")
	}
}

func TestAuthRateLimiter_Reset(t *testing.T) {
	rl := NewAuthRateLimiter(1, 5*time.Minute)

	rl.RecordFailure("host", "user")

	locked, _ := rl.IsLocked("host", "user")
	if !locked {
		t.Error("should be locked")
	}

	rl.Reset("host", "user")

	locked, _ = rl.IsLocked("host", "user")
	if locked {
		t.Error("should not be locked after reset")
	}
}

func TestAuthRateLimiter_Cleanup(t *testing.T) {
	rl := NewAuthRateLimiter(1, 10*time.Millisecond)

	rl.RecordFailure("host", "user")

	// Wait for lockout to expire
	time.Sleep(50 * time.Millisecond)

	// Cleanup should remove expired entry
	rl.Cleanup()

	// Internal state should be clean (no failures tracked)
	rl.mu.RLock()
	_, exists := rl.failures["user@host"]
	rl.mu.RUnlock()

	if exists {
		t.Error("cleanup should have removed expired entry")
	}
}
