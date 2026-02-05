package security

import (
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
)

func TestSudoCache_SetAndGet(t *testing.T) {
	cache := NewSudoCache(5 * time.Minute)

	cache.Set("session1", []byte("password123"))

	got := cache.Get("session1")
	if string(got) != "password123" {
		t.Errorf("Get() = %q, want %q", string(got), "password123")
	}
}

func TestSudoCache_GetMissing(t *testing.T) {
	cache := NewSudoCache(5 * time.Minute)

	got := cache.Get("nonexistent")
	if got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
}

func TestSudoCache_Expiration(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cache := NewSudoCache(5*time.Minute, WithSudoCacheClock(clock))

	cache.Set("session1", []byte("password"))

	// Should exist immediately
	if got := cache.Get("session1"); got == nil {
		t.Error("password should exist immediately after Set")
	}

	// Advance time but not past expiration
	clock.Advance(4 * time.Minute)
	if got := cache.Get("session1"); got == nil {
		t.Error("password should still exist after 4 minutes")
	}

	// Advance past expiration
	clock.Advance(2 * time.Minute)
	if got := cache.Get("session1"); got != nil {
		t.Error("password should be expired after 6 minutes")
	}
}

func TestSudoCache_IsValid(t *testing.T) {
	cache := NewSudoCache(5 * time.Minute)

	if cache.IsValid("session1") {
		t.Error("IsValid should return false for missing session")
	}

	cache.Set("session1", []byte("password"))

	if !cache.IsValid("session1") {
		t.Error("IsValid should return true after Set")
	}
}

func TestSudoCache_IsValidExpiration(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cache := NewSudoCache(5*time.Minute, WithSudoCacheClock(clock))

	cache.Set("session1", []byte("password"))

	if !cache.IsValid("session1") {
		t.Error("IsValid should return true immediately after Set")
	}

	clock.Advance(6 * time.Minute)

	if cache.IsValid("session1") {
		t.Error("IsValid should return false after expiration")
	}
}

func TestSudoCache_ExpiresIn(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	ttl := 5 * time.Minute
	cache := NewSudoCache(ttl, WithSudoCacheClock(clock))

	cache.Set("session1", []byte("password"))

	// Should be exactly TTL at start
	if got := cache.ExpiresIn("session1"); got != ttl {
		t.Errorf("ExpiresIn() = %v, want %v", got, ttl)
	}

	// Advance 2 minutes
	clock.Advance(2 * time.Minute)
	expected := 3 * time.Minute
	if got := cache.ExpiresIn("session1"); got != expected {
		t.Errorf("ExpiresIn() after 2 min = %v, want %v", got, expected)
	}

	// Advance past expiration
	clock.Advance(4 * time.Minute)
	if got := cache.ExpiresIn("session1"); got != 0 {
		t.Errorf("ExpiresIn() after expiration = %v, want 0", got)
	}
}

func TestSudoCache_Clear(t *testing.T) {
	cache := NewSudoCache(5 * time.Minute)

	cache.Set("session1", []byte("password"))
	cache.Clear("session1")

	if got := cache.Get("session1"); got != nil {
		t.Error("password should be cleared")
	}
}

func TestSudoCache_ClearAll(t *testing.T) {
	cache := NewSudoCache(5 * time.Minute)

	cache.Set("session1", []byte("password1"))
	cache.Set("session2", []byte("password2"))

	cache.ClearAll()

	if cache.Get("session1") != nil || cache.Get("session2") != nil {
		t.Error("all passwords should be cleared")
	}
}

func TestSudoCache_Update(t *testing.T) {
	cache := NewSudoCache(5 * time.Minute)

	cache.Set("session1", []byte("old"))
	cache.Set("session1", []byte("new"))

	got := cache.Get("session1")
	if string(got) != "new" {
		t.Errorf("Get() = %q, want %q", string(got), "new")
	}
}

func TestSudoCache_ExpiresInMissing(t *testing.T) {
	cache := NewSudoCache(5 * time.Minute)

	expiresIn := cache.ExpiresIn("nonexistent")
	if expiresIn != 0 {
		t.Errorf("ExpiresIn(nonexistent) = %v, want 0", expiresIn)
	}
}

func TestSudoCache_Cleanup(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cache := NewSudoCache(5*time.Minute, WithSudoCacheClock(clock))

	cache.Set("session1", []byte("password1"))
	cache.Set("session2", []byte("password2"))

	// Advance time past expiration
	clock.Advance(6 * time.Minute)

	cache.Cleanup()

	// Both should be cleaned up
	if cache.IsValid("session1") || cache.IsValid("session2") {
		t.Error("Cleanup should remove expired entries")
	}
}

func TestSecureCache_Basic(t *testing.T) {
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cache := NewSecureCache([]byte("secret"), 5*time.Minute, WithClock(clock))

	// Should be valid
	if !cache.IsValid() {
		t.Error("cache should be valid immediately")
	}

	// Should return data
	if string(cache.Get()) != "secret" {
		t.Errorf("Get() = %q, want %q", cache.Get(), "secret")
	}

	// Advance past TTL
	clock.Advance(6 * time.Minute)

	// Should be expired
	if cache.IsValid() {
		t.Error("cache should be expired after 6 minutes")
	}
	if cache.Get() != nil {
		t.Error("Get() should return nil after expiration")
	}
}

func TestSecureCache_Clear(t *testing.T) {
	cache := NewSecureCache([]byte("secret"), 5*time.Minute)

	cache.Clear()

	if cache.IsValid() {
		t.Error("cache should not be valid after Clear()")
	}
	if cache.Get() != nil {
		t.Error("Get() should return nil after Clear()")
	}
}

func TestSecureCache_DataIsolation(t *testing.T) {
	original := []byte("secret")
	cache := NewSecureCache(original, 5*time.Minute)

	// Modify original - should not affect cache
	original[0] = 'X'

	got := cache.Get()
	if string(got) != "secret" {
		t.Errorf("cache was modified by changing original data")
	}

	// Modify returned data - should not affect cache
	got[0] = 'Y'

	got2 := cache.Get()
	if string(got2) != "secret" {
		t.Errorf("cache was modified by changing returned data")
	}
}
