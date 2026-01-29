//go:build stress
// +build stress

// Package stress contains stress tests for claude-shell-mcp.
// Run with: go test -tags=stress -v ./test/stress/...
package stress

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
)

// TestConcurrentLocalSessions tests creating and using multiple local sessions concurrently.
func TestConcurrentLocalSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 100
	mgr := session.NewManager(cfg)

	numSessions := 50
	var wg sync.WaitGroup
	var successCount int64
	var failCount int64

	// Record memory before
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	t.Logf("Creating %d concurrent sessions...", numSessions)
	startTime := time.Now()

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Create session
			sess, err := mgr.Create(session.CreateOptions{
				Mode: "local",
			})
			if err != nil {
				t.Logf("Session %d: create failed: %v", id, err)
				atomic.AddInt64(&failCount, 1)
				return
			}

			// Execute a simple command
			result, err := sess.Exec("echo hello", 5000)
			if err != nil {
				t.Logf("Session %d: exec failed: %v", id, err)
				atomic.AddInt64(&failCount, 1)
				mgr.Close(sess.ID)
				return
			}

			if result.Status != "completed" {
				t.Logf("Session %d: unexpected status: %s", id, result.Status)
				atomic.AddInt64(&failCount, 1)
				mgr.Close(sess.ID)
				return
			}

			atomic.AddInt64(&successCount, 1)

			// Keep session open for a bit
			time.Sleep(100 * time.Millisecond)

			// Close session
			mgr.Close(sess.ID)
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	// Record memory after
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	t.Logf("Completed in %v", elapsed)
	t.Logf("Success: %d, Failed: %d", successCount, failCount)
	t.Logf("Memory: before=%dMB, after=%dMB, diff=%dMB",
		memBefore.Alloc/1024/1024,
		memAfter.Alloc/1024/1024,
		(memAfter.Alloc-memBefore.Alloc)/1024/1024,
	)

	if failCount > 0 {
		t.Errorf("Some sessions failed: %d out of %d", failCount, numSessions)
	}

	// Verify all sessions are closed
	if count := mgr.SessionCount(); count != 0 {
		t.Errorf("Expected 0 sessions after test, got %d", count)
	}
}

// TestSessionThroughput tests command throughput in a single session.
func TestSessionThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	sess, err := mgr.Create(session.CreateOptions{
		Mode: "local",
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer mgr.Close(sess.ID)

	numCommands := 100
	t.Logf("Executing %d commands sequentially...", numCommands)
	startTime := time.Now()

	var successCount int
	for i := 0; i < numCommands; i++ {
		result, err := sess.Exec(fmt.Sprintf("echo %d", i), 5000)
		if err != nil {
			t.Logf("Command %d failed: %v", i, err)
			continue
		}
		if result.Status == "completed" {
			successCount++
		}
	}

	elapsed := time.Since(startTime)
	throughput := float64(successCount) / elapsed.Seconds()

	t.Logf("Completed %d/%d commands in %v", successCount, numCommands, elapsed)
	t.Logf("Throughput: %.2f commands/second", throughput)

	if successCount < numCommands*9/10 { // Allow 10% failure rate
		t.Errorf("Too many failed commands: %d out of %d", numCommands-successCount, numCommands)
	}
}

// TestMemoryLeak tests for memory leaks by creating and destroying sessions repeatedly.
func TestMemoryLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	// Warm up
	for i := 0; i < 5; i++ {
		sess, _ := mgr.Create(session.CreateOptions{Mode: "local"})
		sess.Exec("echo warmup", 5000)
		mgr.Close(sess.ID)
	}

	// Force GC and record baseline
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	var memBaseline runtime.MemStats
	runtime.ReadMemStats(&memBaseline)

	// Create and destroy sessions repeatedly
	iterations := 20
	sessionsPerIteration := 10

	t.Logf("Running %d iterations of %d sessions each...", iterations, sessionsPerIteration)

	for iter := 0; iter < iterations; iter++ {
		for i := 0; i < sessionsPerIteration; i++ {
			sess, err := mgr.Create(session.CreateOptions{Mode: "local"})
			if err != nil {
				continue
			}
			sess.Exec("echo test", 5000)
			mgr.Close(sess.ID)
		}

		// GC after each iteration
		runtime.GC()
	}

	// Final GC and measure
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	var memFinal runtime.MemStats
	runtime.ReadMemStats(&memFinal)

	memGrowthMB := float64(memFinal.Alloc-memBaseline.Alloc) / 1024 / 1024
	t.Logf("Memory growth: %.2f MB", memGrowthMB)

	// Allow some growth but flag significant leaks
	maxAllowedGrowthMB := 50.0
	if memGrowthMB > maxAllowedGrowthMB {
		t.Errorf("Possible memory leak: grew by %.2f MB (max allowed: %.2f MB)",
			memGrowthMB, maxAllowedGrowthMB)
	}
}

// BenchmarkSessionCreate benchmarks session creation time.
func BenchmarkSessionCreate(b *testing.B) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 1000
	mgr := session.NewManager(cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sess, err := mgr.Create(session.CreateOptions{Mode: "local"})
		if err != nil {
			b.Fatal(err)
		}
		mgr.Close(sess.ID)
	}
}

// BenchmarkExec benchmarks command execution time.
func BenchmarkExec(b *testing.B) {
	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	sess, err := mgr.Create(session.CreateOptions{Mode: "local"})
	if err != nil {
		b.Fatal(err)
	}
	defer mgr.Close(sess.ID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sess.Exec("echo test", 5000)
	}
}
