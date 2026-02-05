// Package security provides security-related functionality.
package security

import (
	"fmt"
	"sync"
	"time"
)

// AuthRateLimiter tracks authentication failures and enforces lockout.
type AuthRateLimiter struct {
	mu              sync.RWMutex
	failures        map[string]*authFailure
	maxFailures     int
	lockoutDuration time.Duration
}

type authFailure struct {
	count     int
	firstFail time.Time
	lockedAt  time.Time
}

// DefaultMaxAuthFailures is the default number of failures before lockout.
const DefaultMaxAuthFailures = 3

// DefaultAuthLockoutDuration is the default lockout duration.
const DefaultAuthLockoutDuration = 5 * time.Minute

// NewAuthRateLimiter creates a new auth rate limiter.
func NewAuthRateLimiter(maxFailures int, lockoutDuration time.Duration) *AuthRateLimiter {
	if maxFailures <= 0 {
		maxFailures = DefaultMaxAuthFailures
	}
	if lockoutDuration <= 0 {
		lockoutDuration = DefaultAuthLockoutDuration
	}

	return &AuthRateLimiter{
		failures:        make(map[string]*authFailure),
		maxFailures:     maxFailures,
		lockoutDuration: lockoutDuration,
	}
}

// key generates a key from host and user.
func key(host, user string) string {
	return fmt.Sprintf("%s@%s", user, host)
}

// IsLocked checks if authentication is locked for the given host/user.
func (r *AuthRateLimiter) IsLocked(host, user string) (bool, time.Duration) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	k := key(host, user)
	f, ok := r.failures[k]
	if !ok {
		return false, 0
	}

	if f.lockedAt.IsZero() {
		return false, 0
	}

	elapsed := time.Since(f.lockedAt)
	if elapsed >= r.lockoutDuration {
		return false, 0
	}

	return true, r.lockoutDuration - elapsed
}

// RecordFailure records an authentication failure.
func (r *AuthRateLimiter) RecordFailure(host, user string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := key(host, user)
	f, ok := r.failures[k]
	if !ok {
		f = &authFailure{
			firstFail: time.Now(),
		}
		r.failures[k] = f
	}

	// Reset if lockout has expired
	if !f.lockedAt.IsZero() && time.Since(f.lockedAt) >= r.lockoutDuration {
		f.count = 0
		f.firstFail = time.Now()
		f.lockedAt = time.Time{}
	}

	f.count++

	// Lock if max failures reached
	if f.count >= r.maxFailures {
		f.lockedAt = time.Now()
	}
}

// RecordSuccess records a successful authentication, resetting the failure count.
func (r *AuthRateLimiter) RecordSuccess(host, user string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := key(host, user)
	delete(r.failures, k)
}

// Reset resets the failure count for a host/user.
// This is an alias for RecordSuccess for semantic clarity.
func (r *AuthRateLimiter) Reset(host, user string) {
	r.RecordSuccess(host, user)
}

// Cleanup removes expired entries.
func (r *AuthRateLimiter) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k, f := range r.failures {
		// Remove entries that have been locked and lockout has expired
		if !f.lockedAt.IsZero() && time.Since(f.lockedAt) >= r.lockoutDuration {
			delete(r.failures, k)
			continue
		}
		// Remove entries with no recent activity (2x lockout duration)
		if time.Since(f.firstFail) >= 2*r.lockoutDuration {
			delete(r.failures, k)
		}
	}
}
