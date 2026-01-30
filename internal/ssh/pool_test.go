package ssh

import (
	"context"
	"testing"
	"time"
)

func TestDefaultPoolConfig(t *testing.T) {
	config := DefaultPoolConfig()

	if config.MaxConnections <= 0 {
		t.Error("MaxConnections should be positive")
	}
	if config.MinConnections < 0 {
		t.Error("MinConnections should be non-negative")
	}
	if config.MaxIdleTime <= 0 {
		t.Error("MaxIdleTime should be positive")
	}
	if config.ConnectionTimeout <= 0 {
		t.Error("ConnectionTimeout should be positive")
	}
	if config.HealthCheckInterval <= 0 {
		t.Error("HealthCheckInterval should be positive")
	}
}

func TestNewPool(t *testing.T) {
	opts := ClientOptions{
		Host: "example.com",
		Port: 22,
		User: "testuser",
	}
	config := DefaultPoolConfig()

	pool := NewPool(opts, config)
	if pool == nil {
		t.Fatal("NewPool returned nil")
	}

	defer pool.Close()

	if pool.closed {
		t.Error("New pool should not be closed")
	}
}

func TestPool_Stats(t *testing.T) {
	opts := ClientOptions{
		Host: "example.com",
		Port: 22,
		User: "testuser",
	}
	config := DefaultPoolConfig()

	pool := NewPool(opts, config)
	defer pool.Close()

	stats := pool.Stats()
	if stats.Total != 0 {
		t.Errorf("New pool should have 0 connections, got %d", stats.Total)
	}
	if stats.InUse != 0 {
		t.Errorf("New pool should have 0 in-use connections, got %d", stats.InUse)
	}
	if stats.Idle != 0 {
		t.Errorf("New pool should have 0 idle connections, got %d", stats.Idle)
	}
}

func TestPool_Close(t *testing.T) {
	opts := ClientOptions{
		Host: "example.com",
		Port: 22,
		User: "testuser",
	}
	config := DefaultPoolConfig()

	pool := NewPool(opts, config)

	err := pool.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	if !pool.closed {
		t.Error("Pool should be marked as closed")
	}

	// Double close should be safe
	err = pool.Close()
	if err != nil {
		t.Errorf("Double Close() returned error: %v", err)
	}
}

func TestPool_GetClosed(t *testing.T) {
	opts := ClientOptions{
		Host: "example.com",
		Port: 22,
		User: "testuser",
	}
	config := DefaultPoolConfig()

	pool := NewPool(opts, config)
	pool.Close()

	ctx := context.Background()
	_, err := pool.Get(ctx)
	if err == nil {
		t.Error("Get on closed pool should return error")
	}
}

func TestPoolManager_GetPool(t *testing.T) {
	config := DefaultPoolConfig()
	pm := NewPoolManager(config)
	defer pm.CloseAll()

	opts := ClientOptions{
		Host: "example.com",
		Port: 22,
		User: "testuser",
	}

	pool1 := pm.GetPool(opts)
	if pool1 == nil {
		t.Fatal("GetPool returned nil")
	}

	// Same options should return same pool
	pool2 := pm.GetPool(opts)
	if pool1 != pool2 {
		t.Error("GetPool should return same pool for same options")
	}

	// Different user should return different pool
	opts.User = "otheruser"
	pool3 := pm.GetPool(opts)
	if pool3 == pool1 {
		t.Error("GetPool should return different pool for different user")
	}
}

func TestPoolManager_Stats(t *testing.T) {
	config := DefaultPoolConfig()
	pm := NewPoolManager(config)
	defer pm.CloseAll()

	opts := ClientOptions{
		Host: "example.com",
		Port: 22,
		User: "testuser",
	}

	pm.GetPool(opts)

	stats := pm.Stats()
	if len(stats) != 1 {
		t.Errorf("Expected 1 pool in stats, got %d", len(stats))
	}
}

func TestPoolManager_CloseAll(t *testing.T) {
	config := DefaultPoolConfig()
	pm := NewPoolManager(config)

	opts := ClientOptions{
		Host: "example.com",
		Port: 22,
		User: "testuser",
	}

	pm.GetPool(opts)
	pm.CloseAll()

	stats := pm.Stats()
	if len(stats) != 0 {
		t.Errorf("Expected 0 pools after CloseAll, got %d", len(stats))
	}
}

func TestPoolConfig_Validation(t *testing.T) {
	config := PoolConfig{
		MaxConnections:      5,
		MinConnections:      2,
		MaxIdleTime:         1 * time.Minute,
		ConnectionTimeout:   10 * time.Second,
		HealthCheckInterval: 15 * time.Second,
	}

	if config.MaxConnections < config.MinConnections {
		t.Error("MaxConnections should be >= MinConnections")
	}
}
