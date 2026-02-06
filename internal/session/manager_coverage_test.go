package session

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakerand"
)

// --- NewManager tests ---

func TestNewManager_InitializesAllFields(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := NewManager(cfg)

	if mgr.sessions == nil {
		t.Fatal("sessions map should be initialized")
	}
	if mgr.controlSessions == nil {
		t.Fatal("controlSessions map should be initialized")
	}
	if mgr.config != cfg {
		t.Fatal("config should be set")
	}
	if mgr.clock == nil {
		t.Fatal("clock should default to realclock")
	}
	if mgr.random == nil {
		t.Fatal("random should default to realrand")
	}
	if mgr.store == nil {
		t.Fatal("store should be created by default when not injected")
	}
}

func TestNewManager_WithManagerClock(t *testing.T) {
	cfg := config.DefaultConfig()
	clock := fakeclock.New(time.Now())
	fs := fakefs.New()
	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/t.json"))),
	)

	if mgr.clock != clock {
		t.Error("expected injected clock to be used")
	}
}

func TestNewManager_WithManagerRandom(t *testing.T) {
	cfg := config.DefaultConfig()
	rand := fakerand.NewSequential()
	fs := fakefs.New()
	mgr := NewManager(cfg,
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/t.json"))),
	)

	if mgr.random != rand {
		t.Error("expected injected random to be used")
	}
}

func TestNewManager_WithManagerStore(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/t.json"))
	mgr := NewManager(cfg, WithManagerStore(store))

	if mgr.store != store {
		t.Error("expected injected store to be used")
	}
}

func TestNewManager_DefaultStoreCreatedWhenNil(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := NewManager(cfg)

	if mgr.store == nil {
		t.Fatal("default store should be created when not injected via options")
	}
}

// --- Create tests ---

func TestManager_Create_MaxSessionsEnforced(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 1
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_existing", "local", clock)

	_, err := mgr.Create(CreateOptions{Mode: "local"})
	if err == nil {
		t.Fatal("expected error when max sessions reached")
	}
	if !strings.Contains(err.Error(), "max sessions reached (1)") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManager_Create_LocalSessionSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/create-test.json"))),
	)

	sess, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error creating local session: %v", err)
	}
	if sess == nil {
		t.Fatal("session should not be nil")
	}
	if !strings.HasPrefix(sess.ID, "sess_") {
		t.Errorf("session ID %q should start with 'sess_'", sess.ID)
	}
	if sess.Mode != "local" {
		t.Errorf("session mode = %q, want 'local'", sess.Mode)
	}
	if sess.State != StateIdle {
		t.Errorf("session state = %v, want %v", sess.State, StateIdle)
	}
	if mgr.SessionCount() != 1 {
		t.Errorf("session count = %d, want 1", mgr.SessionCount())
	}

	// Session should be persisted in store
	_, ok := mgr.store.Get(sess.ID)
	if !ok {
		t.Error("session should be saved in store after Create")
	}

	// Clean up
	mgr.Close(sess.ID)
}

func TestManager_Create_SSHSessionRequiresHost(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	mgr, _, _ := newTestManager(cfg)

	_, err := mgr.Create(CreateOptions{Mode: "ssh", User: "deploy"})
	if err == nil {
		t.Fatal("expected error when host is empty for SSH mode")
	}
	if !strings.Contains(err.Error(), "host is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManager_Create_SSHSessionRequiresUser(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	mgr, _, _ := newTestManager(cfg)

	_, err := mgr.Create(CreateOptions{Mode: "ssh", Host: "example.com"})
	if err == nil {
		t.Fatal("expected error when user is empty for SSH mode")
	}
	if !strings.Contains(err.Error(), "user is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManager_Create_SessionIDUnique(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/unique-test.json"))),
	)

	sess1, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sess2, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sess1.ID == sess2.ID {
		t.Errorf("session IDs should be unique, both are %q", sess1.ID)
	}

	// Clean up
	mgr.Close(sess1.ID)
	mgr.Close(sess2.ID)
}

func TestManager_Create_SessionGettableAfterCreate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/gettable-test.json"))),
	)

	sess, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retrieved, err := mgr.Get(sess.ID)
	if err != nil {
		t.Fatalf("unexpected error getting session: %v", err)
	}
	if retrieved.ID != sess.ID {
		t.Errorf("retrieved ID = %q, want %q", retrieved.ID, sess.ID)
	}

	// Clean up
	mgr.Close(sess.ID)
}

// --- Get tests ---

func TestManager_Get_ExistingSession(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	s := addFakeSession(mgr, "sess_get1", "local", clock)
	s.Cwd = "/tmp"

	got, err := mgr.Get("sess_get1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Cwd != "/tmp" {
		t.Errorf("Cwd = %q, want %q", got.Cwd, "/tmp")
	}
}

func TestManager_Get_NonExistentFallsToRecover(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	// No stored metadata either, so recover should fail
	_, err := mgr.Get("sess_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session with no stored metadata")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want containing 'session not found'", err.Error())
	}
}

// --- recover tests ---

func TestManager_Recover_NoStoredMetadata(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	_, err := mgr.recover("sess_nodata")
	if err == nil {
		t.Fatal("expected error when no stored metadata exists")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want containing 'session not found'", err.Error())
	}
}

func TestManager_Recover_LocalSessionFromStore(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/recover-test.json"))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(store),
	)

	// Store metadata for a session that's not in memory
	meta := SessionMetadata{
		ID:   "sess_recover_local",
		Mode: "local",
		Cwd:  "/home/user",
	}
	store.mu.Lock()
	store.sessions[meta.ID] = meta
	store.mu.Unlock()

	// recover should recreate the session since it's local mode
	sess, err := mgr.recover("sess_recover_local")
	if err != nil {
		t.Fatalf("unexpected error recovering local session: %v", err)
	}
	if sess.ID != "sess_recover_local" {
		t.Errorf("recovered session ID = %q, want %q", sess.ID, "sess_recover_local")
	}
	if sess.Mode != "local" {
		t.Errorf("recovered session mode = %q, want 'local'", sess.Mode)
	}
	// Note: Cwd gets overwritten by initializeLocal() calling fs.Getwd(),
	// so we just verify it's non-empty (the real working directory).
	if sess.Cwd == "" {
		t.Error("recovered session Cwd should not be empty")
	}

	// Should be in the sessions map now
	if mgr.SessionCount() != 1 {
		t.Errorf("session count = %d, want 1 after recover", mgr.SessionCount())
	}

	// Clean up
	mgr.Close("sess_recover_local")
}

func TestManager_Recover_SSHFailsWithBadHost(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/recover-ssh-test.json"))

	mgr := NewManager(cfg,
		WithManagerStore(store),
	)

	// Store metadata for an SSH session with bad host
	meta := SessionMetadata{
		ID:   "sess_recover_ssh",
		Mode: "ssh",
		Host: "nonexistent.invalid",
		User: "deploy",
		Port: 22,
	}
	store.mu.Lock()
	store.sessions[meta.ID] = meta
	store.mu.Unlock()

	// recover should fail because SSH connection will fail
	_, err := mgr.recover("sess_recover_ssh")
	if err == nil {
		t.Fatal("expected error recovering SSH session with bad host")
	}
	if !strings.Contains(err.Error(), "failed to recover session") {
		t.Errorf("error = %q, want containing 'failed to recover session'", err.Error())
	}

	// Stale metadata should be deleted
	if _, ok := store.Get("sess_recover_ssh"); ok {
		t.Error("stale metadata should be deleted after failed recovery")
	}
}

func TestManager_Recover_DoubleCheckInMemory(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/recover-double.json"))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(store),
	)

	// Put metadata in store
	meta := SessionMetadata{
		ID:   "sess_double",
		Mode: "local",
	}
	store.mu.Lock()
	store.sessions[meta.ID] = meta
	store.mu.Unlock()

	// Also put a session directly in the sessions map (simulating another goroutine recovering it)
	existingSess := &Session{
		ID:        "sess_double",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
		pty:       fakepty.New(),
	}
	mgr.sessions["sess_double"] = existingSess

	// recover should find it already in memory (double-check)
	sess, err := mgr.recover("sess_double")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess != existingSess {
		t.Error("expected the already-in-memory session to be returned")
	}
}

// --- Get triggering recover ---

func TestManager_Get_TriggersRecoverFromStore(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/get-recover.json"))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(store),
	)

	// Store metadata for a local session (not in memory)
	meta := SessionMetadata{
		ID:   "sess_get_recover",
		Mode: "local",
		Cwd:  "/var/log",
	}
	store.mu.Lock()
	store.sessions[meta.ID] = meta
	store.mu.Unlock()

	// Get should trigger recover
	sess, err := mgr.Get("sess_get_recover")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "sess_get_recover" {
		t.Errorf("session ID = %q, want %q", sess.ID, "sess_get_recover")
	}

	// Clean up
	mgr.Close("sess_get_recover")
}

// --- Close tests ---

func TestManager_Close_SessionNotInMemory(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	err := mgr.Close("sess_missing")
	if err == nil {
		t.Fatal("expected error for session not in memory")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want containing 'session not found'", err.Error())
	}
}

func TestManager_Close_RemovesFromMapAndStore(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/close-store.json"))
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(store),
	)

	sess := addFakeSession(mgr, "sess_close_store", "local", clock)
	store.Save(sess)

	if _, ok := store.Get("sess_close_store"); !ok {
		t.Fatal("session should be in store before close")
	}

	err := mgr.Close("sess_close_store")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mgr.SessionCount() != 0 {
		t.Errorf("session count = %d, want 0", mgr.SessionCount())
	}
	if _, ok := store.Get("sess_close_store"); ok {
		t.Error("session should be removed from store after close")
	}
}

func TestManager_Close_NotFoundCleansUpStaleMetadata(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/stale-close.json"))

	mgr := NewManager(cfg, WithManagerStore(store))

	// Save metadata but don't add session to manager's map
	stale := &Session{ID: "sess_stale", Mode: "local"}
	store.Save(stale)

	err := mgr.Close("sess_stale")
	if err == nil {
		t.Fatal("expected error")
	}

	// Stale metadata should be cleaned up
	if _, ok := store.Get("sess_stale"); ok {
		t.Error("stale metadata should be removed after close attempt")
	}
}

// --- CloseAll tests ---

func TestManager_CloseAll_NoSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	err := mgr.CloseAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_CloseAll_MultipleSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	s1 := addFakeSession(mgr, "sess_all_1", "local", clock)
	s2 := addFakeSession(mgr, "sess_all_2", "ssh", clock)

	err := mgr.CloseAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mgr.SessionCount() != 0 {
		t.Errorf("session count = %d, want 0 after CloseAll", mgr.SessionCount())
	}

	if s1.State != StateClosed {
		t.Errorf("s1 state = %v, want %v", s1.State, StateClosed)
	}
	if s2.State != StateClosed {
		t.Errorf("s2 state = %v, want %v", s2.State, StateClosed)
	}
}

func TestManager_CloseAll_WithControlSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_ctrl_1", "local", clock)

	// Manually add a control session with a fake PTY
	cs := &ControlSession{
		host: "local",
		mode: "local",
		pty:  fakepty.New(),
	}
	mgr.controlSessions["local"] = cs

	err := mgr.CloseAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mgr.SessionCount() != 0 {
		t.Errorf("session count = %d, want 0", mgr.SessionCount())
	}
	if len(mgr.controlSessions) != 0 {
		t.Errorf("control sessions count = %d, want 0", len(mgr.controlSessions))
	}
}

// --- CloseControlSession tests ---

func TestManager_CloseControlSession_NotFound(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	// Closing a non-existent control session should not error
	err := mgr.CloseControlSession("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_CloseControlSession_Found(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	fakePTY := fakepty.New()
	cs := &ControlSession{
		host: "local",
		mode: "local",
		pty:  fakePTY,
	}
	mgr.controlSessions["local"] = cs

	err := mgr.CloseControlSession("local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mgr.controlSessions) != 0 {
		t.Errorf("control sessions count = %d, want 0", len(mgr.controlSessions))
	}
	if !fakePTY.IsClosed() {
		t.Error("control session PTY should be closed")
	}
}

// --- GetControlSession tests ---

func TestManager_GetControlSession_CreatesForLocal(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/ctrl-test.json"))),
	)

	opts := CreateOptions{Mode: "local"}
	cs, err := mgr.GetControlSession(opts)
	if err != nil {
		t.Fatalf("unexpected error creating local control session: %v", err)
	}
	if cs == nil {
		t.Fatal("control session should not be nil")
	}

	// Calling again should return the same one
	cs2, err := mgr.GetControlSession(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs != cs2 {
		t.Error("second call should return the same control session")
	}

	// Clean up
	mgr.CloseControlSession("local")
}

func TestManager_GetControlSession_EmptyHostTreatedAsLocal(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/ctrl-empty.json"))),
	)

	opts := CreateOptions{Mode: "local", Host: ""}
	cs, err := mgr.GetControlSession(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cs == nil {
		t.Fatal("control session should not be nil")
	}

	// Should be stored under "local" key
	if _, ok := mgr.controlSessions["local"]; !ok {
		t.Error("control session should be stored with 'local' key when host is empty")
	}

	// Clean up
	mgr.CloseControlSession("local")
}

func TestManager_GetControlSession_ReusesExistingByHost(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	// Manually add a control session
	fakePTY := fakepty.New()
	cs := &ControlSession{
		host: "myhost",
		mode: "ssh",
		pty:  fakePTY,
	}
	mgr.controlSessions["myhost"] = cs

	opts := CreateOptions{Mode: "ssh", Host: "myhost", User: "user"}
	retrieved, err := mgr.GetControlSession(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retrieved != cs {
		t.Error("should reuse existing control session for same host")
	}
}

// --- getOrCreateControlSessionLocked tests ---

func TestManager_getOrCreateControlSessionLocked_SSHFails(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	opts := CreateOptions{Mode: "ssh", Host: "bad.host.invalid", User: "nobody", Port: 99999}
	_, err := mgr.getOrCreateControlSessionLocked(opts)
	if err == nil {
		t.Fatal("expected error for SSH control session to unreachable host")
	}
	if !strings.Contains(err.Error(), "create control session for") {
		t.Errorf("error = %q, want containing 'create control session for'", err.Error())
	}
}

// --- ListDetailed tests ---

func TestManager_ListDetailed_MultipleStates(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	s1 := addFakeSession(mgr, "sess_idle_d", "local", clock)
	s1.Cwd = "/home"

	s2 := addFakeSession(mgr, "sess_running_d", "local", clock)
	s2.State = StateRunning

	s3 := addFakeSession(mgr, "sess_ssh_d", "ssh", clock)
	s3.Host = "prod.example.com"
	s3.User = "admin"
	s3.Cwd = "/var/log"
	s3.State = StateAwaitingInput

	infos := mgr.ListDetailed()
	if len(infos) != 3 {
		t.Fatalf("expected 3 infos, got %d", len(infos))
	}

	// Build a map for easy lookup
	infoMap := make(map[string]SessionInfo)
	for _, info := range infos {
		infoMap[info.ID] = info
	}

	if infoMap["sess_idle_d"].State != "idle" {
		t.Errorf("s1 state = %q, want 'idle'", infoMap["sess_idle_d"].State)
	}
	if infoMap["sess_idle_d"].Cwd != "/home" {
		t.Errorf("s1 cwd = %q, want '/home'", infoMap["sess_idle_d"].Cwd)
	}
	if infoMap["sess_running_d"].State != "running" {
		t.Errorf("s2 state = %q, want 'running'", infoMap["sess_running_d"].State)
	}
	if infoMap["sess_ssh_d"].State != "awaiting_input" {
		t.Errorf("s3 state = %q, want 'awaiting_input'", infoMap["sess_ssh_d"].State)
	}
	if infoMap["sess_ssh_d"].Host != "prod.example.com" {
		t.Errorf("s3 host = %q, want 'prod.example.com'", infoMap["sess_ssh_d"].Host)
	}
	if infoMap["sess_ssh_d"].User != "admin" {
		t.Errorf("s3 user = %q, want 'admin'", infoMap["sess_ssh_d"].User)
	}
}

func TestManager_ListDetailed_TimeFormatting(t *testing.T) {
	cfg := config.DefaultConfig()
	fixedTime := time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)
	clock := fakeclock.New(fixedTime)
	fs := fakefs.New()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/time-fmt.json"))),
	)

	sess := &Session{
		ID:        "sess_time_fmt",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: fixedTime,
		LastUsed:  fixedTime,
	}
	mgr.sessions["sess_time_fmt"] = sess

	infos := mgr.ListDetailed()
	if len(infos) != 1 {
		t.Fatalf("expected 1, got %d", len(infos))
	}

	expectedTime := "2025-03-15T14:30:00Z"
	if infos[0].CreatedAt != expectedTime {
		t.Errorf("CreatedAt = %q, want %q", infos[0].CreatedAt, expectedTime)
	}
	if infos[0].LastUsed != expectedTime {
		t.Errorf("LastUsed = %q, want %q", infos[0].LastUsed, expectedTime)
	}
}

// --- SessionCount tests ---

func TestManager_SessionCount_AfterCreateAndClose(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/count-test.json"))),
	)

	if mgr.SessionCount() != 0 {
		t.Errorf("initial count = %d, want 0", mgr.SessionCount())
	}

	sess, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.SessionCount() != 1 {
		t.Errorf("after create, count = %d, want 1", mgr.SessionCount())
	}

	mgr.Close(sess.ID)
	if mgr.SessionCount() != 0 {
		t.Errorf("after close, count = %d, want 0", mgr.SessionCount())
	}
}

// --- generateSessionID tests ---

func TestManager_GenerateSessionID_HasPrefix(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	id := mgr.generateSessionID()
	if !strings.HasPrefix(id, "sess_") {
		t.Errorf("ID %q should start with 'sess_'", id)
	}
}

func TestManager_GenerateSessionID_ConsistentLength(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	for i := 0; i < 20; i++ {
		id := mgr.generateSessionID()
		// "sess_" (5) + 16 hex chars = 21
		if len(id) != 21 {
			t.Errorf("ID %q has length %d, want 21", id, len(id))
		}
	}
}

// --- Concurrent access tests ---

func TestManager_ConcurrentGet(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_concurrent", "local", clock)

	var wg sync.WaitGroup
	errCh := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := mgr.Get("sess_concurrent")
			if err != nil {
				errCh <- err
				return
			}
			if sess.ID != "sess_concurrent" {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent Get error: %v", err)
	}
}

func TestManager_ConcurrentList(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	for i := 0; i < 5; i++ {
		addFakeSession(mgr, "sess_list_"+string(rune('a'+i)), "local", clock)
	}

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			list := mgr.List()
			if len(list) != 5 {
				t.Errorf("List() returned %d, want 5", len(list))
			}
		}()
	}
	wg.Wait()
}

func TestManager_ConcurrentListDetailedAndSessionCount(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_conc_a", "local", clock)
	addFakeSession(mgr, "sess_conc_b", "ssh", clock)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			infos := mgr.ListDetailed()
			if len(infos) != 2 {
				t.Errorf("ListDetailed returned %d, want 2", len(infos))
			}
		}()
		go func() {
			defer wg.Done()
			count := mgr.SessionCount()
			if count != 2 {
				t.Errorf("SessionCount returned %d, want 2", count)
			}
		}()
	}
	wg.Wait()
}

func TestManager_ConcurrentGenerateSessionID(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	var wg sync.WaitGroup
	idsCh := make(chan string, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Note: fakerand is not goroutine safe for uniqueness guarantees,
			// but we're testing that generateSessionID doesn't panic under
			// concurrent access. The method itself doesn't lock the manager.
			id := mgr.generateSessionID()
			idsCh <- id
		}()
	}

	wg.Wait()
	close(idsCh)

	for id := range idsCh {
		if !strings.HasPrefix(id, "sess_") {
			t.Errorf("concurrently generated ID %q lacks prefix", id)
		}
	}
}

// --- Create with control session integration tests ---

func TestManager_Create_LocalSessionGetsControlSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/ctrl-create.json"))),
	)

	sess, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Local session should get a control session (it creates a local control session)
	if sess.controlSession == nil {
		t.Error("local session should have a control session assigned")
	}

	// The control session should be stored under "local"
	if _, ok := mgr.controlSessions["local"]; !ok {
		t.Error("control session should be stored under 'local' key")
	}

	// Clean up
	mgr.CloseAll()
}

// --- Edge cases ---

func TestManager_Close_AlreadyClosedSession(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	sess := addFakeSession(mgr, "sess_already_closed", "local", clock)

	// Close it once
	err := mgr.Close("sess_already_closed")
	if err != nil {
		t.Fatalf("first close error: %v", err)
	}
	if sess.State != StateClosed {
		t.Errorf("state after first close = %v, want %v", sess.State, StateClosed)
	}

	// Try to close again - session is no longer in the map
	err = mgr.Close("sess_already_closed")
	if err == nil {
		t.Fatal("expected error closing already-removed session")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want containing 'session not found'", err.Error())
	}
}

func TestManager_List_ReturnsAllIDs(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	ids := []string{"sess_x", "sess_y", "sess_z"}
	for _, id := range ids {
		addFakeSession(mgr, id, "local", clock)
	}

	list := mgr.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(list))
	}

	found := make(map[string]bool)
	for _, id := range list {
		found[id] = true
	}
	for _, want := range ids {
		if !found[want] {
			t.Errorf("missing ID %q in list %v", want, list)
		}
	}
}

func TestManager_Create_InheritsClockAndRandom(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/inherit-test.json"))),
	)

	sess, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The session should have inherited the manager's clock
	if sess.clock != clock {
		t.Error("session should inherit manager's clock")
	}
	if sess.random != rand {
		t.Error("session should inherit manager's random")
	}

	// Clean up
	mgr.Close(sess.ID)
}

func TestManager_Create_SessionHasConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/config-test.json"))),
	)

	sess, err := mgr.Create(CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sess.config != cfg {
		t.Error("session should have the manager's config")
	}

	// Clean up
	mgr.Close(sess.ID)
}

func TestManager_ListDetailed_IdleForChangesWithClock(t *testing.T) {
	cfg := config.DefaultConfig()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	fs := fakefs.New()

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/idle-change.json"))),
	)

	sess := &Session{
		ID:        "sess_idle_change",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
	}
	mgr.sessions["sess_idle_change"] = sess

	// 0 seconds idle
	infos := mgr.ListDetailed()
	if infos[0].IdleFor != "0s" {
		t.Errorf("initial idle = %q, want '0s'", infos[0].IdleFor)
	}

	// Advance 42 seconds
	clock.Advance(42 * time.Second)
	infos = mgr.ListDetailed()
	if infos[0].IdleFor != "42s" {
		t.Errorf("after 42s, idle = %q, want '42s'", infos[0].IdleFor)
	}

	// Simulate session usage (update LastUsed)
	sess.LastUsed = clock.Now()
	infos = mgr.ListDetailed()
	if infos[0].IdleFor != "0s" {
		t.Errorf("after use, idle = %q, want '0s'", infos[0].IdleFor)
	}
}

// --- Recover with saved tunnels ---

func TestManager_Recover_PreservesTunnelConfigs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/recover-tunnels.json"))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(store),
	)

	tunnels := []TunnelConfig{
		{Type: "local", LocalHost: "127.0.0.1", LocalPort: 8080, RemoteHost: "localhost", RemotePort: 80},
		{Type: "reverse", LocalHost: "127.0.0.1", LocalPort: 3000, RemoteHost: "0.0.0.0", RemotePort: 9000},
	}

	meta := SessionMetadata{
		ID:      "sess_recover_tunnels",
		Mode:    "local",
		Cwd:     "/app",
		Tunnels: tunnels,
	}
	store.mu.Lock()
	store.sessions[meta.ID] = meta
	store.mu.Unlock()

	sess, err := mgr.recover("sess_recover_tunnels")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sess.SavedTunnels) != 2 {
		t.Fatalf("expected 2 saved tunnels, got %d", len(sess.SavedTunnels))
	}
	if sess.SavedTunnels[0].Type != "local" {
		t.Errorf("tunnel[0] type = %q, want 'local'", sess.SavedTunnels[0].Type)
	}
	if sess.SavedTunnels[0].LocalPort != 8080 {
		t.Errorf("tunnel[0] local port = %d, want 8080", sess.SavedTunnels[0].LocalPort)
	}
	if sess.SavedTunnels[1].Type != "reverse" {
		t.Errorf("tunnel[1] type = %q, want 'reverse'", sess.SavedTunnels[1].Type)
	}

	// Clean up
	mgr.Close("sess_recover_tunnels")
}

// --- Recover saves updated metadata ---

func TestManager_Recover_UpdatesStore(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/recover-update.json"))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(store),
	)

	meta := SessionMetadata{
		ID:   "sess_recover_update",
		Mode: "local",
		Cwd:  "/old/path",
	}
	store.mu.Lock()
	store.sessions[meta.ID] = meta
	store.mu.Unlock()

	sess, err := mgr.recover("sess_recover_update")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("session should not be nil")
	}

	// The store should have been updated with the recovered session
	updatedMeta, ok := store.Get("sess_recover_update")
	if !ok {
		t.Fatal("session metadata should still be in store after recover")
	}
	if updatedMeta.Mode != "local" {
		t.Errorf("stored mode = %q, want 'local'", updatedMeta.Mode)
	}

	// Clean up
	mgr.Close("sess_recover_update")
}

// --- Recover with port and key_path ---

func TestManager_Recover_PreservesSSHMetadataFields(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 10
	fs := fakefs.New()
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/recover-ssh-meta.json"))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(store),
	)

	// Store local session metadata but with SSH-like fields for testing field preservation
	meta := SessionMetadata{
		ID:      "sess_meta_fields",
		Mode:    "local",
		Host:    "myhost",
		Port:    2222,
		User:    "admin",
		KeyPath: "/home/user/.ssh/id_ed25519",
		Cwd:     "/srv/app",
	}
	store.mu.Lock()
	store.sessions[meta.ID] = meta
	store.mu.Unlock()

	sess, err := mgr.recover("sess_meta_fields")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that metadata fields were preserved on the recovered session
	if sess.Host != "myhost" {
		t.Errorf("Host = %q, want 'myhost'", sess.Host)
	}
	if sess.Port != 2222 {
		t.Errorf("Port = %d, want 2222", sess.Port)
	}
	if sess.User != "admin" {
		t.Errorf("User = %q, want 'admin'", sess.User)
	}
	if sess.KeyPath != "/home/user/.ssh/id_ed25519" {
		t.Errorf("KeyPath = %q, want '/home/user/.ssh/id_ed25519'", sess.KeyPath)
	}
	// Note: Cwd gets overwritten by initializeLocal() calling fs.Getwd(),
	// so for local sessions the stored Cwd does not survive initialization.
	// Just check it's non-empty.
	if sess.Cwd == "" {
		t.Error("Cwd should not be empty after recovery")
	}

	// Clean up
	mgr.Close("sess_meta_fields")
}

// --- errorPTY is a PTY that returns an error on Close ---

type errorPTY struct {
	closeErr error
}

func (p *errorPTY) Read(b []byte) (int, error)          { return 0, nil }
func (p *errorPTY) Write(b []byte) (int, error)         { return len(b), nil }
func (p *errorPTY) WriteString(s string) (int, error)    { return len(s), nil }
func (p *errorPTY) Interrupt() error                     { return nil }
func (p *errorPTY) Close() error                         { return p.closeErr }
func (p *errorPTY) SetReadDeadline(t time.Time) error    { return nil }

// --- Close error path tests ---

func TestManager_Close_SessionCloseReturnsError(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	// Create a session with a PTY that returns an error on Close
	sess := &Session{
		ID:        "sess_close_err",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
		pty:       &errorPTY{closeErr: fmt.Errorf("permission denied on pty close")},
	}
	mgr.sessions["sess_close_err"] = sess

	err := mgr.Close("sess_close_err")
	if err == nil {
		t.Fatal("expected error when session close fails")
	}
	if !strings.Contains(err.Error(), "permission denied on pty close") {
		t.Errorf("error = %q, want containing 'permission denied on pty close'", err.Error())
	}

	// Session should still be removed from the map even on error? Actually looking at the code,
	// it returns the error without deleting. Let me check...
	// Looking at Close(): if sess.Close() returns error, it returns immediately without delete.
	if _, ok := mgr.sessions["sess_close_err"]; !ok {
		t.Error("session should still be in map when Close() returns error (error returned before delete)")
	}
}

// --- CloseAll error path tests ---

func TestManager_CloseAll_WithErrorFromSession(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	// Add a session that will error on close
	sess := &Session{
		ID:        "sess_err1",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
		pty:       &errorPTY{closeErr: fmt.Errorf("pty close failed")},
	}
	mgr.sessions["sess_err1"] = sess

	// Add a normal session
	addFakeSession(mgr, "sess_ok", "local", clock)

	err := mgr.CloseAll()
	if err == nil {
		t.Fatal("expected error when a session fails to close")
	}
	if !strings.Contains(err.Error(), "close errors") {
		t.Errorf("error = %q, want containing 'close errors'", err.Error())
	}

	// All sessions should be removed from the map regardless of errors
	if mgr.SessionCount() != 0 {
		t.Errorf("session count = %d, want 0 after CloseAll even with errors", mgr.SessionCount())
	}
}

func TestManager_CloseAll_WithErrorFromControlSession(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_ok2", "local", clock)

	// Add a control session that will error on close
	cs := &ControlSession{
		host: "local",
		mode: "local",
		pty:  &errorPTY{closeErr: fmt.Errorf("control pty close failed")},
	}
	mgr.controlSessions["local"] = cs

	err := mgr.CloseAll()
	if err == nil {
		t.Fatal("expected error from control session close failure")
	}
	if !strings.Contains(err.Error(), "close errors") {
		t.Errorf("error = %q, want containing 'close errors'", err.Error())
	}

	// Everything should be cleaned up
	if len(mgr.controlSessions) != 0 {
		t.Errorf("control sessions count = %d, want 0", len(mgr.controlSessions))
	}
}

func TestManager_CloseAll_MultipleErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	// Two sessions that both fail on close
	mgr.sessions["sess_err_a"] = &Session{
		ID:        "sess_err_a",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
		pty:       &errorPTY{closeErr: fmt.Errorf("error A")},
	}
	mgr.sessions["sess_err_b"] = &Session{
		ID:        "sess_err_b",
		Mode:      "local",
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
		pty:       &errorPTY{closeErr: fmt.Errorf("error B")},
	}

	err := mgr.CloseAll()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "close errors") {
		t.Errorf("error = %q, want containing 'close errors'", err.Error())
	}

	// All sessions should be removed
	if mgr.SessionCount() != 0 {
		t.Errorf("session count = %d, want 0", mgr.SessionCount())
	}
}

// --- CloseControlSession error path ---

func TestManager_CloseControlSession_ErrorFromClose(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	cs := &ControlSession{
		host: "errhost",
		mode: "local",
		pty:  &errorPTY{closeErr: fmt.Errorf("control close error")},
	}
	mgr.controlSessions["errhost"] = cs

	err := mgr.CloseControlSession("errhost")
	if err == nil {
		t.Fatal("expected error when control session close fails")
	}
	if !strings.Contains(err.Error(), "control close error") {
		t.Errorf("error = %q, want containing 'control close error'", err.Error())
	}
}
