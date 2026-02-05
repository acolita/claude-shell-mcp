// Package security provides secure credential handling for claude-shell-mcp.
package security

import (
	"sync"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/adapters/realclock"
	"github.com/acolita/claude-shell-mcp/internal/ports"
)

// SecureCache stores sensitive credentials with TTL-based expiration.
type SecureCache struct {
	data      []byte
	createdAt time.Time
	ttl       time.Duration
	mu        sync.Mutex
	cleared   bool
	clock     ports.Clock
}

// SecureCacheOption configures a SecureCache.
type SecureCacheOption func(*SecureCache)

// WithClock sets the clock used by SecureCache.
func WithClock(clock ports.Clock) SecureCacheOption {
	return func(sc *SecureCache) {
		sc.clock = clock
	}
}

// NewSecureCache creates a new secure cache with the given TTL.
func NewSecureCache(data []byte, ttl time.Duration, opts ...SecureCacheOption) *SecureCache {
	// Make a copy of the data
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	sc := &SecureCache{
		data:  dataCopy,
		ttl:   ttl,
		clock: realclock.New(), // default to real clock
	}

	for _, opt := range opts {
		opt(sc)
	}

	sc.createdAt = sc.clock.Now()

	return sc
}

// Get returns the cached data if still valid, or nil if expired.
func (sc *SecureCache) Get() []byte {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.cleared || sc.data == nil {
		return nil
	}

	if sc.clock.Now().Sub(sc.createdAt) > sc.ttl {
		sc.clear()
		return nil
	}

	// Return a copy to prevent external modification
	result := make([]byte, len(sc.data))
	copy(result, sc.data)
	return result
}

// IsValid returns true if the cache contains valid data.
func (sc *SecureCache) IsValid() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.cleared || sc.data == nil {
		return false
	}

	if sc.clock.Now().Sub(sc.createdAt) > sc.ttl {
		sc.clear()
		return false
	}

	return true
}

// ExpiresIn returns the duration until expiration.
func (sc *SecureCache) ExpiresIn() time.Duration {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.cleared || sc.data == nil {
		return 0
	}

	remaining := sc.ttl - sc.clock.Now().Sub(sc.createdAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Clear securely wipes and clears the cached data.
func (sc *SecureCache) Clear() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.clear()
}

// clear performs the actual clearing (must be called with lock held).
func (sc *SecureCache) clear() {
	if sc.data != nil {
		WipeBytes(sc.data)
		sc.data = nil
	}
	sc.cleared = true
}

// SudoCache manages sudo password caching per session.
type SudoCache struct {
	caches map[string]*SecureCache // session_id -> cache
	ttl    time.Duration
	mu     sync.RWMutex
	clock  ports.Clock
}

// SudoCacheOption configures a SudoCache.
type SudoCacheOption func(*SudoCache)

// WithSudoCacheClock sets the clock used by SudoCache.
func WithSudoCacheClock(clock ports.Clock) SudoCacheOption {
	return func(c *SudoCache) {
		c.clock = clock
	}
}

// NewSudoCache creates a new sudo cache manager with the given TTL.
func NewSudoCache(ttl time.Duration, opts ...SudoCacheOption) *SudoCache {
	c := &SudoCache{
		caches: make(map[string]*SecureCache),
		ttl:    ttl,
		clock:  realclock.New(), // default to real clock
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Set stores a sudo password for a session.
func (c *SudoCache) Set(sessionID string, password []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear any existing cache for this session
	if existing, ok := c.caches[sessionID]; ok {
		existing.Clear()
	}

	c.caches[sessionID] = NewSecureCache(password, c.ttl, WithClock(c.clock))
}

// Get retrieves the cached sudo password for a session.
func (c *SudoCache) Get(sessionID string) []byte {
	c.mu.RLock()
	cache, ok := c.caches[sessionID]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	return cache.Get()
}

// IsValid returns true if there's a valid cached password for the session.
func (c *SudoCache) IsValid(sessionID string) bool {
	c.mu.RLock()
	cache, ok := c.caches[sessionID]
	c.mu.RUnlock()

	if !ok {
		return false
	}

	return cache.IsValid()
}

// ExpiresIn returns the time until the cache expires for a session.
func (c *SudoCache) ExpiresIn(sessionID string) time.Duration {
	c.mu.RLock()
	cache, ok := c.caches[sessionID]
	c.mu.RUnlock()

	if !ok {
		return 0
	}

	return cache.ExpiresIn()
}

// Clear removes the cached password for a session.
func (c *SudoCache) Clear(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cache, ok := c.caches[sessionID]; ok {
		cache.Clear()
		delete(c.caches, sessionID)
	}
}

// ClearAll removes all cached passwords.
func (c *SudoCache) ClearAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, cache := range c.caches {
		cache.Clear()
	}
	c.caches = make(map[string]*SecureCache)
}

// Cleanup removes expired entries from the cache.
func (c *SudoCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for sessionID, cache := range c.caches {
		if !cache.IsValid() {
			cache.Clear()
			delete(c.caches, sessionID)
		}
	}
}

// DefaultSudoTTL is the default TTL for sudo password caching.
const DefaultSudoTTL = 5 * time.Minute
