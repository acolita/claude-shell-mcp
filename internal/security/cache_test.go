package security

import (
	"testing"
	"time"
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
	cache := NewSudoCache(50 * time.Millisecond)

	cache.Set("session1", []byte("password"))

	// Should exist immediately
	if got := cache.Get("session1"); got == nil {
		t.Error("password should exist immediately after Set")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	if got := cache.Get("session1"); got != nil {
		t.Error("password should be expired")
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

func TestSudoCache_ExpiresIn(t *testing.T) {
	ttl := 5 * time.Minute
	cache := NewSudoCache(ttl)

	cache.Set("session1", []byte("password"))

	expiresIn := cache.ExpiresIn("session1")
	if expiresIn <= 0 || expiresIn > ttl {
		t.Errorf("ExpiresIn() = %v, want between 0 and %v", expiresIn, ttl)
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
