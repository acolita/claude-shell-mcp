package session

import (
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakerand"
)

func TestManager_GenerateSessionID(t *testing.T) {
	cfg := config.DefaultConfig()
	rand := fakerand.NewSequential()
	fs := fakefs.New()

	mgr := NewManager(cfg,
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/sessions.json"))),
	)

	// First ID should use sequential bytes 0, 1, 2, 3, 4, 5, 6, 7
	id1 := mgr.generateSessionID()
	if id1 != "sess_0001020304050607" {
		t.Errorf("first ID = %q, want %q", id1, "sess_0001020304050607")
	}

	// Second ID should use next bytes 8, 9, 10, 11, 12, 13, 14, 15
	id2 := mgr.generateSessionID()
	if id2 != "sess_08090a0b0c0d0e0f" {
		t.Errorf("second ID = %q, want %q", id2, "sess_08090a0b0c0d0e0f")
	}
}

func TestManager_ListDetailed_IdleTime(t *testing.T) {
	cfg := config.DefaultConfig()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	fs := fakefs.New()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/sessions.json"))),
	)

	// Manually add a session for testing ListDetailed
	// (We can't Create because that requires a real PTY)
	sess := &Session{
		ID:        "sess_test",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
	}
	mgr.sessions["sess_test"] = sess

	// Initially, idle time should be 0
	infos := mgr.ListDetailed()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}
	if infos[0].IdleFor != "0s" {
		t.Errorf("initial IdleFor = %q, want %q", infos[0].IdleFor, "0s")
	}

	// Advance clock by 5 minutes
	clock.Advance(5 * time.Minute)

	infos = mgr.ListDetailed()
	if infos[0].IdleFor != "5m0s" {
		t.Errorf("after 5 min, IdleFor = %q, want %q", infos[0].IdleFor, "5m0s")
	}

	// Advance clock by another 25 minutes (30 total)
	clock.Advance(25 * time.Minute)

	infos = mgr.ListDetailed()
	if infos[0].IdleFor != "30m0s" {
		t.Errorf("after 30 min, IdleFor = %q, want %q", infos[0].IdleFor, "30m0s")
	}
}

func TestManager_SessionLimit(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 2

	fs := fakefs.New()
	mgr := NewManager(cfg,
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/sessions.json"))),
	)

	// Manually add sessions to reach limit
	mgr.sessions["sess_1"] = &Session{ID: "sess_1", Mode: "local"}
	mgr.sessions["sess_2"] = &Session{ID: "sess_2", Mode: "local"}

	// Trying to create another should fail
	_, err := mgr.Create(CreateOptions{Mode: "local"})
	if err == nil {
		t.Error("expected error when max sessions reached")
	}
	if err.Error() != "max sessions reached (2)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManager_SessionCount(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()

	mgr := NewManager(cfg,
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/sessions.json"))),
	)

	if mgr.SessionCount() != 0 {
		t.Errorf("initial count = %d, want 0", mgr.SessionCount())
	}

	// Manually add sessions
	mgr.sessions["sess_1"] = &Session{ID: "sess_1"}
	mgr.sessions["sess_2"] = &Session{ID: "sess_2"}

	if mgr.SessionCount() != 2 {
		t.Errorf("after adding 2, count = %d, want 2", mgr.SessionCount())
	}
}

func TestManager_List(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()

	mgr := NewManager(cfg,
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/sessions.json"))),
	)

	// Empty list initially
	if len(mgr.List()) != 0 {
		t.Errorf("expected empty list, got %v", mgr.List())
	}

	// Add sessions
	mgr.sessions["sess_abc"] = &Session{ID: "sess_abc"}
	mgr.sessions["sess_xyz"] = &Session{ID: "sess_xyz"}

	list := mgr.List()
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}

	// Check both IDs are present
	found := make(map[string]bool)
	for _, id := range list {
		found[id] = true
	}
	if !found["sess_abc"] || !found["sess_xyz"] {
		t.Errorf("missing session IDs, got %v", list)
	}
}
