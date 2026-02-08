package session

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/acolita/claude-shell-mcp/internal/config"
	localpty "github.com/acolita/claude-shell-mcp/internal/pty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakeclock"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakerand"
)

// --- helpers ---

// fakePTYFactory returns a LocalPTYFactory that creates fakepty instances
// instead of spawning real shells. This prevents tests from hanging on macOS.
func fakePTYFactory(opts localpty.PTYOptions) (PTY, string, error) {
	return fakepty.New(), "/bin/sh", nil
}

// newTestManager creates a Manager configured with fakes for testing.
// It returns the manager, the fake clock, and the fake random source.
func newTestManager(cfg *config.Config) (*Manager, *fakeclock.Clock, *fakerand.Random) {
	clock := fakeclock.New(time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	fs := fakefs.New()
	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/test-sessions.json"))),
		WithLocalPTYFactory(fakePTYFactory),
	)
	return mgr, clock, rand
}

// addFakeSession manually inserts a session into the manager without requiring
// a real PTY or SSH connection. Returns the added session.
func addFakeSession(mgr *Manager, id, mode string, clock *fakeclock.Clock) *Session {
	sess := &Session{
		ID:        id,
		Mode:      mode,
		State:     StateIdle,
		CreatedAt: clock.Now(),
		LastUsed:  clock.Now(),
		pty:       fakepty.New(),
	}
	mgr.sessions[id] = sess
	return sess
}

// --- Manager.Get tests ---

func TestManager_Get_Found(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_abc", "local", clock)

	sess, err := mgr.Get("sess_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.ID != "sess_abc" {
		t.Errorf("ID = %q, want %q", sess.ID, "sess_abc")
	}
	if sess.Mode != "local" {
		t.Errorf("Mode = %q, want %q", sess.Mode, "local")
	}
}

func TestManager_Get_NotFound(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	_, err := mgr.Get("sess_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want containing %q", err.Error(), "session not found")
	}
}

func TestManager_Get_MultipleSessionsReturnsCorrectOne(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_first", "local", clock)
	second := addFakeSession(mgr, "sess_second", "ssh", clock)
	second.Host = "example.com"
	second.User = "deploy"
	addFakeSession(mgr, "sess_third", "local", clock)

	sess, err := mgr.Get("sess_second")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess.Host != "example.com" {
		t.Errorf("Host = %q, want %q", sess.Host, "example.com")
	}
	if sess.User != "deploy" {
		t.Errorf("User = %q, want %q", sess.User, "deploy")
	}
}

// --- Manager.List tests ---

func TestManager_List_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	list := mgr.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func TestManager_List_WithSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_a", "local", clock)
	addFakeSession(mgr, "sess_b", "ssh", clock)
	addFakeSession(mgr, "sess_c", "local", clock)

	list := mgr.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(list))
	}

	found := make(map[string]bool)
	for _, id := range list {
		found[id] = true
	}
	for _, want := range []string{"sess_a", "sess_b", "sess_c"} {
		if !found[want] {
			t.Errorf("missing session %q in list %v", want, list)
		}
	}
}

// --- Manager.Close tests ---

func TestManager_Close_Found(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_to_close", "local", clock)

	err := mgr.Close("sess_to_close")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Session should be removed from the manager
	if mgr.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after close, got %d", mgr.SessionCount())
	}

	// Getting the session should now fail
	_, err = mgr.Get("sess_to_close")
	if err == nil {
		t.Error("expected error when getting closed session")
	}
}

func TestManager_Close_NotFound(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	err := mgr.Close("sess_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want containing %q", err.Error(), "session not found")
	}
}

func TestManager_Close_DoesNotAffectOtherSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_keep1", "local", clock)
	addFakeSession(mgr, "sess_remove", "local", clock)
	addFakeSession(mgr, "sess_keep2", "local", clock)

	err := mgr.Close("sess_remove")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mgr.SessionCount() != 2 {
		t.Errorf("expected 2 sessions remaining, got %d", mgr.SessionCount())
	}

	// Verify remaining sessions are accessible
	if _, err := mgr.Get("sess_keep1"); err != nil {
		t.Errorf("sess_keep1 should still exist: %v", err)
	}
	if _, err := mgr.Get("sess_keep2"); err != nil {
		t.Errorf("sess_keep2 should still exist: %v", err)
	}
}

func TestManager_Close_SetsSessionStateToClosed(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	sess := addFakeSession(mgr, "sess_closing", "local", clock)

	// Verify state is idle before close
	if sess.State != StateIdle {
		t.Fatalf("expected initial state %v, got %v", StateIdle, sess.State)
	}

	err := mgr.Close("sess_closing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The session object should now be in closed state
	if sess.State != StateClosed {
		t.Errorf("expected state %v after close, got %v", StateClosed, sess.State)
	}
}

// --- Manager.CloseAll tests ---

func TestManager_CloseAll_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	err := mgr.CloseAll()
	if err != nil {
		t.Fatalf("unexpected error closing empty manager: %v", err)
	}

	if mgr.SessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", mgr.SessionCount())
	}
}

func TestManager_CloseAll_WithSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	s1 := addFakeSession(mgr, "sess_1", "local", clock)
	s2 := addFakeSession(mgr, "sess_2", "local", clock)
	s3 := addFakeSession(mgr, "sess_3", "ssh", clock)

	err := mgr.CloseAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mgr.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after CloseAll, got %d", mgr.SessionCount())
	}

	// All sessions should be in closed state
	for _, sess := range []*Session{s1, s2, s3} {
		if sess.State != StateClosed {
			t.Errorf("session %s: expected state %v, got %v", sess.ID, StateClosed, sess.State)
		}
	}
}

// --- Manager.ListDetailed tests ---

func TestManager_ListDetailed_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	infos := mgr.ListDetailed()
	if len(infos) != 0 {
		t.Errorf("expected empty list, got %d items", len(infos))
	}
}

func TestManager_ListDetailed_SSHSession(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	sess := addFakeSession(mgr, "sess_ssh1", "ssh", clock)
	sess.Host = "prod.example.com"
	sess.User = "deploy"
	sess.Cwd = "/app"

	infos := mgr.ListDetailed()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}

	info := infos[0]
	if info.ID != "sess_ssh1" {
		t.Errorf("ID = %q, want %q", info.ID, "sess_ssh1")
	}
	if info.Mode != "ssh" {
		t.Errorf("Mode = %q, want %q", info.Mode, "ssh")
	}
	if info.Host != "prod.example.com" {
		t.Errorf("Host = %q, want %q", info.Host, "prod.example.com")
	}
	if info.User != "deploy" {
		t.Errorf("User = %q, want %q", info.User, "deploy")
	}
	if info.Cwd != "/app" {
		t.Errorf("Cwd = %q, want %q", info.Cwd, "/app")
	}
	if info.State != "idle" {
		t.Errorf("State = %q, want %q", info.State, "idle")
	}
}

func TestManager_ListDetailed_IdleTimeIncreases(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_idle", "local", clock)

	// Initially idle for 0 seconds
	infos := mgr.ListDetailed()
	if infos[0].IdleFor != "0s" {
		t.Errorf("initial IdleFor = %q, want %q", infos[0].IdleFor, "0s")
	}

	// After 10 minutes
	clock.Advance(10 * time.Minute)
	infos = mgr.ListDetailed()
	if infos[0].IdleFor != "10m0s" {
		t.Errorf("after 10 min, IdleFor = %q, want %q", infos[0].IdleFor, "10m0s")
	}

	// After 1 hour total
	clock.Advance(50 * time.Minute)
	infos = mgr.ListDetailed()
	if infos[0].IdleFor != "1h0m0s" {
		t.Errorf("after 1 hour, IdleFor = %q, want %q", infos[0].IdleFor, "1h0m0s")
	}
}

func TestManager_ListDetailed_CreatedAtFormatted(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_time", "local", clock)

	infos := mgr.ListDetailed()
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}

	expected := "2024-06-15T10:00:00Z"
	if infos[0].CreatedAt != expected {
		t.Errorf("CreatedAt = %q, want %q", infos[0].CreatedAt, expected)
	}
	if infos[0].LastUsed != expected {
		t.Errorf("LastUsed = %q, want %q", infos[0].LastUsed, expected)
	}
}

// --- Manager.SessionCount tests ---

func TestManager_SessionCount_Increments(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	if got := mgr.SessionCount(); got != 0 {
		t.Errorf("initial count = %d, want 0", got)
	}

	addFakeSession(mgr, "sess_1", "local", clock)
	if got := mgr.SessionCount(); got != 1 {
		t.Errorf("after 1 add, count = %d, want 1", got)
	}

	addFakeSession(mgr, "sess_2", "local", clock)
	addFakeSession(mgr, "sess_3", "ssh", clock)
	if got := mgr.SessionCount(); got != 3 {
		t.Errorf("after 3 adds, count = %d, want 3", got)
	}
}

func TestManager_SessionCount_DecreasesAfterClose(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_1", "local", clock)
	addFakeSession(mgr, "sess_2", "local", clock)

	if got := mgr.SessionCount(); got != 2 {
		t.Fatalf("expected 2 sessions, got %d", got)
	}

	mgr.Close("sess_1")

	if got := mgr.SessionCount(); got != 1 {
		t.Errorf("after closing 1, count = %d, want 1", got)
	}
}

// --- Session ID generation tests ---

func TestManager_GenerateSessionID_Format(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	id := mgr.generateSessionID()

	if !strings.HasPrefix(id, "sess_") {
		t.Errorf("ID %q should start with 'sess_'", id)
	}

	// After "sess_" prefix, should be 16 hex characters (8 bytes)
	hexPart := strings.TrimPrefix(id, "sess_")
	if len(hexPart) != 16 {
		t.Errorf("hex part length = %d, want 16 (got %q)", len(hexPart), hexPart)
	}
}

func TestManager_GenerateSessionID_UniqueWithFakeRandom(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr, _, _ := newTestManager(cfg)

	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := mgr.generateSessionID()
		if ids[id] {
			t.Errorf("duplicate ID generated: %q", id)
		}
		ids[id] = true
	}
}

func TestManager_GenerateSessionID_DeterministicWithFixedRandom(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	rand := fakerand.NewFixed([]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22})
	mgr := NewManager(cfg,
		WithManagerRandom(rand),
		WithManagerStore(NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/sessions.json"))),
	)

	id := mgr.generateSessionID()
	if id != "sess_aabbccddeeff1122" {
		t.Errorf("ID = %q, want %q", id, "sess_aabbccddeeff1122")
	}
}

// --- Session.Close tests ---

func TestSession_Close_SetsStateClosed(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_close_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	err := sess.Close()
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if sess.State != StateClosed {
		t.Errorf("State = %v, want %v", sess.State, StateClosed)
	}
}

func TestSession_Close_Idempotent(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_close_twice", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// First close
	if err := sess.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}

	// Second close should be no-op
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}

	if sess.State != StateClosed {
		t.Errorf("State = %v, want %v", sess.State, StateClosed)
	}
}

func TestSession_Close_ClosesUnderlyingPTY(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_pty_close", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	if pty.IsClosed() {
		t.Fatal("PTY should not be closed before session Close")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if !pty.IsClosed() {
		t.Error("PTY should be closed after session Close")
	}
}

// --- Session.Interrupt tests ---

func TestSession_Interrupt_FromRunning(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_interrupt", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Set state to running to allow interrupt
	sess.State = StateRunning

	err := sess.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt error: %v", err)
	}

	if sess.State != StateIdle {
		t.Errorf("State = %v, want %v after interrupt", sess.State, StateIdle)
	}
	if !pty.WasInterrupted() {
		t.Error("PTY should have been interrupted")
	}
}

func TestSession_Interrupt_FromAwaitingInput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_interrupt_awaiting", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.State = StateAwaitingInput

	err := sess.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt error: %v", err)
	}

	if sess.State != StateIdle {
		t.Errorf("State = %v, want %v after interrupt", sess.State, StateIdle)
	}
}

func TestSession_Interrupt_FromIdle_ReturnsError(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_interrupt_idle", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	err := sess.Interrupt()
	if err == nil {
		t.Fatal("expected error when interrupting idle session")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not running")
	}
}

func TestSession_Interrupt_NilPTY_ReturnsError(t *testing.T) {
	sess := NewSession("sess_no_pty", "local")
	sess.State = StateRunning

	err := sess.Interrupt()
	if err == nil {
		t.Fatal("expected error when PTY is nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not initialized")
	}
}

// --- Session.IsSSH tests ---

func TestSession_IsSSH(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want bool
	}{
		{"ssh mode", "ssh", true},
		{"local mode", "local", false},
		{"empty mode", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{Mode: tt.mode}
			if got := sess.IsSSH(); got != tt.want {
				t.Errorf("IsSSH() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Session.ResolvePath tests ---

func TestSession_ResolvePath(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		path string
		want string
	}{
		{"empty path returns cwd", "/home/user", "", "/home/user"},
		{"absolute path returned as-is", "/home/user", "/etc/config", "/etc/config"},
		{"tilde path returned as-is", "/home/user", "~/docs", "~/docs"},
		{"relative path prepends cwd", "/home/user", "subdir/file.txt", "/home/user/subdir/file.txt"},
		{"relative with empty cwd", "", "file.txt", "file.txt"},
		{"relative with tilde cwd", "~", "file.txt", "file.txt"},
		{"absolute with root cwd", "/", "/var/log", "/var/log"},
		{"relative with root cwd", "/", "file.txt", "//file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{Cwd: tt.cwd}
			got := sess.ResolvePath(tt.path)
			if got != tt.want {
				t.Errorf("ResolvePath(%q) = %q, want %q (cwd=%q)", tt.path, got, tt.want, tt.cwd)
			}
		})
	}
}

// --- Session.ClearSavedTunnels tests ---

func TestSession_ClearSavedTunnels(t *testing.T) {
	sess := &Session{
		ID: "sess_tunnels",
		SavedTunnels: []TunnelConfig{
			{Type: "local", LocalPort: 8080, RemotePort: 80},
			{Type: "reverse", LocalPort: 3000, RemotePort: 9000},
		},
	}

	if len(sess.SavedTunnels) != 2 {
		t.Fatalf("expected 2 saved tunnels, got %d", len(sess.SavedTunnels))
	}

	sess.ClearSavedTunnels()

	if sess.SavedTunnels != nil {
		t.Errorf("expected nil SavedTunnels after clear, got %v", sess.SavedTunnels)
	}
}

// --- Session.GetTunnelConfigs tests ---

func TestSession_GetTunnelConfigs_LocalMode_ReturnsNil(t *testing.T) {
	sess := &Session{Mode: "local"}
	configs := sess.GetTunnelConfigs()
	if configs != nil {
		t.Errorf("expected nil for local mode, got %v", configs)
	}
}

func TestSession_GetTunnelConfigs_NoSSHClient_ReturnsNil(t *testing.T) {
	sess := &Session{Mode: "ssh", sshClient: nil}
	configs := sess.GetTunnelConfigs()
	if configs != nil {
		t.Errorf("expected nil for nil sshClient, got %v", configs)
	}
}

// --- Session.Status tests ---

func TestSession_Status_LocalSession(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_status_local", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	sess.Shell = "/bin/bash"
	sess.Cwd = "/home/user"

	status := sess.Status()

	if status.ID != "sess_status_local" {
		t.Errorf("ID = %q, want %q", status.ID, "sess_status_local")
	}
	if status.State != StateIdle {
		t.Errorf("State = %v, want %v", status.State, StateIdle)
	}
	if status.Mode != "local" {
		t.Errorf("Mode = %q, want %q", status.Mode, "local")
	}
	if status.Shell != "/bin/bash" {
		t.Errorf("Shell = %q, want %q", status.Shell, "/bin/bash")
	}
	if status.Cwd != "/home/user" {
		t.Errorf("Cwd = %q, want %q", status.Cwd, "/home/user")
	}
	if !status.Connected {
		t.Error("Connected should be true for initialized session")
	}
	if status.Host != "" {
		t.Errorf("Host should be empty for local session, got %q", status.Host)
	}
}

func TestSession_Status_IdleAndUptime(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_time_test", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Advance clock by 2 minutes
	clock.Advance(2 * time.Minute)

	status := sess.Status()
	if status.IdleSeconds != 120 {
		t.Errorf("IdleSeconds = %d, want 120", status.IdleSeconds)
	}
	if status.UptimeSeconds != 120 {
		t.Errorf("UptimeSeconds = %d, want 120", status.UptimeSeconds)
	}
}

func TestSession_Status_ShellInfo(t *testing.T) {
	tests := []struct {
		name            string
		shell           string
		wantType        string
		wantHistory     bool
	}{
		{"bash", "/bin/bash", "bash", true},
		{"zsh", "/usr/bin/zsh", "zsh", true},
		{"fish", "/usr/bin/fish", "fish", false},
		{"sh", "/bin/sh", "sh", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pty := fakepty.New()
			clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
			cfg := config.DefaultConfig()

			sess := NewSession("sess_shell_"+tt.name, "local",
				WithPTY(pty),
				WithSessionClock(clock),
				WithConfig(cfg),
			)
			if err := sess.Initialize(); err != nil {
				t.Fatalf("Initialize error: %v", err)
			}
			sess.Shell = tt.shell

			status := sess.Status()
			if status.ShellInfo == nil {
				t.Fatal("ShellInfo should not be nil")
			}
			if status.ShellInfo.Type != tt.wantType {
				t.Errorf("ShellInfo.Type = %q, want %q", status.ShellInfo.Type, tt.wantType)
			}
			if status.ShellInfo.SupportsHistory != tt.wantHistory {
				t.Errorf("ShellInfo.SupportsHistory = %v, want %v", status.ShellInfo.SupportsHistory, tt.wantHistory)
			}
		})
	}
}

func TestSession_Status_WithSavedTunnels(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_saved_tunnels", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.SavedTunnels = []TunnelConfig{
		{Type: "local", LocalPort: 8080, RemotePort: 80, RemoteHost: "localhost"},
	}

	status := sess.Status()
	if len(status.SavedTunnels) != 1 {
		t.Fatalf("expected 1 saved tunnel, got %d", len(status.SavedTunnels))
	}
	if status.SavedTunnels[0].LocalPort != 8080 {
		t.Errorf("SavedTunnels[0].LocalPort = %d, want 8080", status.SavedTunnels[0].LocalPort)
	}
}

func TestSession_Status_NoSavedTunnels(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_no_tunnels", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	status := sess.Status()
	if status.SavedTunnels != nil {
		t.Errorf("expected nil SavedTunnels, got %v", status.SavedTunnels)
	}
}

func TestSession_Status_ClosedSessionNotConnected(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_closed_status", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.State = StateClosed
	status := sess.Status()
	if status.Connected {
		t.Error("Connected should be false for closed session")
	}
}

// --- Session state transition tests ---

func TestSession_StateTransitions_InitToIdle(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_state_init", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)

	// Before Initialize, state is already idle from constructor
	if sess.State != StateIdle {
		t.Errorf("state before Initialize = %v, want %v", sess.State, StateIdle)
	}

	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	if sess.State != StateIdle {
		t.Errorf("state after Initialize = %v, want %v", sess.State, StateIdle)
	}
}

func TestSession_StateTransitions_IdleToClosed(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_state_close", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if sess.State != StateClosed {
		t.Errorf("state after Close = %v, want %v", sess.State, StateClosed)
	}
}

func TestSession_StateTransitions_RunningToIdleViaInterrupt(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_state_interrupt", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.State = StateRunning

	if err := sess.Interrupt(); err != nil {
		t.Fatalf("Interrupt error: %v", err)
	}

	if sess.State != StateIdle {
		t.Errorf("state after Interrupt = %v, want %v", sess.State, StateIdle)
	}
}

// --- Manager session limit tests ---

func TestManager_SessionLimit_AtBoundary(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 3
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_1", "local", clock)
	addFakeSession(mgr, "sess_2", "local", clock)
	addFakeSession(mgr, "sess_3", "local", clock)

	// At limit, Create should fail
	_, err := mgr.Create(CreateOptions{Mode: "local"})
	if err == nil {
		t.Fatal("expected error when at session limit")
	}
	if !strings.Contains(err.Error(), "max sessions reached") {
		t.Errorf("error = %q, want containing %q", err.Error(), "max sessions reached")
	}
}

func TestManager_SessionLimit_AfterClosingOne(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Security.MaxSessionsPerUser = 2
	mgr, clock, _ := newTestManager(cfg)

	addFakeSession(mgr, "sess_1", "local", clock)
	addFakeSession(mgr, "sess_2", "local", clock)

	// At limit
	_, err := mgr.Create(CreateOptions{Mode: "local"})
	if err == nil {
		t.Fatal("expected error when at limit")
	}

	// Close one
	mgr.Close("sess_1")

	// Now count should be 1, below the limit of 2
	if got := mgr.SessionCount(); got != 1 {
		t.Errorf("count after close = %d, want 1", got)
	}
}

// --- Session.ControlExec tests (without actual control session) ---

func TestSession_ControlExec_NoControlSession(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_no_ctrl", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	_, err := sess.ControlExec(nil, "ps aux")
	if err == nil {
		t.Fatal("expected error when no control session")
	}
	if !strings.Contains(err.Error(), "control session not available") {
		t.Errorf("error = %q, want containing %q", err.Error(), "control session not available")
	}
}

func TestSession_ControlExecRaw_NoControlSession(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_no_ctrl_raw", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	_, err := sess.ControlExecRaw(nil, "ls")
	if err == nil {
		t.Fatal("expected error when no control session")
	}
	if !strings.Contains(err.Error(), "control session not available") {
		t.Errorf("error = %q, want containing %q", err.Error(), "control session not available")
	}
}

// --- Helper function tests ---

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"timeout error", fmt.Errorf("operation timeout"), true},
		{"i/o timeout", fmt.Errorf("read tcp: i/o timeout"), true},
		{"other error", fmt.Errorf("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTimeoutError(tt.err); got != tt.want {
				t.Errorf("isTimeoutError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsConnectionBroken(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"EOF error", fmt.Errorf("EOF"), true},
		{"broken pipe", fmt.Errorf("write: broken pipe"), true},
		{"connection reset", fmt.Errorf("connection reset by peer"), true},
		{"closed network", fmt.Errorf("use of closed network connection"), true},
		{"channel closed", fmt.Errorf("channel closed"), true},
		{"normal error", fmt.Errorf("permission denied"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConnectionBroken(tt.err); got != tt.want {
				t.Errorf("isConnectionBroken(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// --- Escape sequence interpretation tests ---

func TestInterpretEscapeSequences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{"plain text", "hello", []byte("hello")},
		{"newline", `\n`, []byte{'\n'}},
		{"carriage return", `\r`, []byte{'\r'}},
		{"tab", `\t`, []byte{'\t'}},
		{"backslash", `\\`, []byte{'\\'}},
		{"escape char", `\e`, []byte{0x1b}},
		{"hex escape", `\x04`, []byte{0x04}},
		{"hex uppercase", `\X1B`, []byte{0x1b}},
		{"octal escape", `\004`, []byte{0x04}},
		{"ctrl-c hex", `\x03`, []byte{0x03}},
		{"mixed", `hello\nworld\t!`, []byte("hello\nworld\t!")},
		{"text with hex", `before\x00after`, []byte("before\x00after")},
		{"trailing backslash", `test\`, []byte("test\\")},
		{"unknown escape", `\z`, []byte(`\z`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interpretEscapeSequences(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("byte[%d] = %x, want %x", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseHexByte(t *testing.T) {
	tests := []struct {
		input string
		want  byte
		ok    bool
	}{
		{"04", 0x04, true},
		{"1b", 0x1b, true},
		{"FF", 0xff, true},
		{"00", 0x00, true},
		{"a5", 0xa5, true},
		{"zz", 0, false},
		{"", 0, false},
		{"1", 0, false},
		{"123", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := parseHexByte(tt.input)
			if ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("byte = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestParseOctalByte(t *testing.T) {
	tests := []struct {
		input string
		want  byte
		ok    bool
	}{
		{"004", 0x04, true},
		{"033", 0x1b, true},
		{"377", 0xff, true},
		{"000", 0x00, true},
		{"177", 0x7f, true},
		{"888", 0, false},
		{"", 0, false},
		{"04", 0, false},
		{"0004", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := parseOctalByte(tt.input)
			if ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("byte = %x, want %x", got, tt.want)
			}
		})
	}
}

// --- Session.ProvideInput validation tests ---

func TestSession_ProvideInput_NotAwaitingInput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_provide_idle", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	// Session is in Idle state, not AwaitingInput
	_, err := sess.ProvideInput("some input")
	if err == nil {
		t.Fatal("expected error when session is not awaiting input")
	}
	if !strings.Contains(err.Error(), "not awaiting input") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not awaiting input")
	}
}

// --- Session.SendRaw validation tests ---

func TestSession_SendRaw_NotAwaitingInput(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_sendraw_idle", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	_, err := sess.SendRaw(`\x04`)
	if err == nil {
		t.Fatal("expected error when session is not awaiting input")
	}
	if !strings.Contains(err.Error(), "not awaiting input") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not awaiting input")
	}
}

func TestSession_SendRaw_NilPTY(t *testing.T) {
	sess := NewSession("sess_sendraw_nopty", "local")
	sess.State = StateAwaitingInput

	_, err := sess.SendRaw(`\x04`)
	if err == nil {
		t.Fatal("expected error when PTY is nil")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error = %q, want containing %q", err.Error(), "not initialized")
	}
}

// --- Manager.NewManager with default store tests ---

func TestManager_NewManager_DefaultStore(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := NewManager(cfg)

	if mgr.store == nil {
		t.Fatal("expected store to be created by default")
	}
	if mgr.sessions == nil {
		t.Fatal("expected sessions map to be initialized")
	}
	if mgr.controlSessions == nil {
		t.Fatal("expected controlSessions map to be initialized")
	}
}

func TestManager_NewManager_WithOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	rand := fakerand.NewSequential()
	fs := fakefs.New()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/s.json"))

	mgr := NewManager(cfg,
		WithManagerClock(clock),
		WithManagerRandom(rand),
		WithManagerStore(store),
	)

	if mgr.clock != clock {
		t.Error("expected injected clock")
	}
	if mgr.random != rand {
		t.Error("expected injected random")
	}
	if mgr.store != store {
		t.Error("expected injected store")
	}
}

// --- Session.Status with PTYName and ControlSession ---

func TestSession_Status_PTYNameAndControlSession(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_pty_info", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.PTYName = "42"

	status := sess.Status()
	if status.PTYName != "42" {
		t.Errorf("PTYName = %q, want %q", status.PTYName, "42")
	}
	if status.HasControlSession {
		t.Error("HasControlSession should be false when controlSession is nil")
	}
}

// --- Session.Status with EnvVars and Aliases ---

func TestSession_Status_EnvVarsAndAliases(t *testing.T) {
	pty := fakepty.New()
	clock := fakeclock.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.DefaultConfig()

	sess := NewSession("sess_env", "local",
		WithPTY(pty),
		WithSessionClock(clock),
		WithConfig(cfg),
	)
	if err := sess.Initialize(); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}

	sess.EnvVars = map[string]string{
		"PATH": "/usr/bin",
		"HOME": "/home/user",
	}
	sess.Aliases = map[string]string{
		"ll": "ls -la",
		"gs": "git status",
	}

	status := sess.Status()
	if len(status.EnvVars) != 2 {
		t.Errorf("expected 2 env vars, got %d", len(status.EnvVars))
	}
	if status.EnvVars["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want %q", status.EnvVars["PATH"], "/usr/bin")
	}
	if len(status.Aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d", len(status.Aliases))
	}
	if status.Aliases["ll"] != "ls -la" {
		t.Errorf("ll alias = %q, want %q", status.Aliases["ll"], "ls -la")
	}
}

// --- Manager.Close cleans up store ---

func TestManager_Close_CleansUpStore(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/store-test.json"))
	mgr := NewManager(cfg,
		WithManagerStore(store),
	)

	sess := &Session{
		ID:    "sess_stored",
		Mode:  "local",
		State: StateIdle,
		pty:   fakepty.New(),
	}
	mgr.sessions["sess_stored"] = sess
	store.Save(sess)

	// Verify the session is in the store
	if _, ok := store.Get("sess_stored"); !ok {
		t.Fatal("session should be in store before close")
	}

	mgr.Close("sess_stored")

	// After close, the store entry should be removed
	if _, ok := store.Get("sess_stored"); ok {
		t.Error("session should be removed from store after close")
	}
}

func TestManager_Close_NotFound_CleansUpStaleStore(t *testing.T) {
	cfg := config.DefaultConfig()
	fs := fakefs.New()
	store := NewSessionStore(WithFileSystem(fs), WithStorePath("/tmp/store-stale.json"))
	mgr := NewManager(cfg,
		WithManagerStore(store),
	)

	// Save metadata for a session that is not in memory
	staleSession := &Session{ID: "sess_stale", Mode: "local"}
	store.Save(staleSession)

	// Verify stale entry is in store
	if _, ok := store.Get("sess_stale"); !ok {
		t.Fatal("stale session should be in store")
	}

	// Close should fail (not in memory) but also clean up the store
	err := mgr.Close("sess_stale")
	if err == nil {
		t.Fatal("expected error for session not in memory")
	}

	// Stale metadata should be cleaned up
	if _, ok := store.Get("sess_stale"); ok {
		t.Error("stale metadata should be removed from store after close attempt")
	}
}

// --- tryParseEscape tests ---

func TestTryParseEscape(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		pos     int
		wantB   byte
		wantN   int
		wantOK  bool
	}{
		{"newline", `\n`, 0, '\n', 2, true},
		{"tab", `\t`, 0, '\t', 2, true},
		{"backslash", `\\`, 0, '\\', 2, true},
		{"escape char", `\e`, 0, 0x1b, 2, true},
		{"hex", `\x1b`, 0, 0x1b, 4, true},
		{"octal", `\033`, 0, 0x1b, 4, true},
		{"end of string", `\`, 0, 0, 0, false},
		{"unknown char", `\z`, 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, n, ok := tryParseEscape(tt.input, tt.pos)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if b != tt.wantB {
					t.Errorf("byte = %x, want %x", b, tt.wantB)
				}
				if n != tt.wantN {
					t.Errorf("skip = %d, want %d", n, tt.wantN)
				}
			}
		})
	}
}

func TestTryParseHexEscape(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		pos    int
		wantB  byte
		wantN  int
		wantOK bool
	}{
		{"valid hex", `\x0a`, 0, 0x0a, 4, true},
		{"uppercase hex", `\xFF`, 0, 0xff, 4, true},
		{"too short", `\x0`, 0, 0, 0, false},
		{"invalid chars", `\xzz`, 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, n, ok := tryParseHexEscape(tt.input, tt.pos)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if b != tt.wantB {
					t.Errorf("byte = %x, want %x", b, tt.wantB)
				}
				if n != tt.wantN {
					t.Errorf("skip = %d, want %d", n, tt.wantN)
				}
			}
		})
	}
}

func TestTryParseOctalEscape(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		pos    int
		wantB  byte
		wantN  int
		wantOK bool
	}{
		{"valid octal", `\033`, 0, 0x1b, 4, true},
		{"zero", `\000`, 0, 0x00, 4, true},
		{"max valid", `\377`, 0, 0xff, 4, true},
		{"too short", `\03`, 0, 0, 0, false},
		{"invalid octal digit", `\089`, 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, n, ok := tryParseOctalEscape(tt.input, tt.pos)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if b != tt.wantB {
					t.Errorf("byte = %x, want %x", b, tt.wantB)
				}
				if n != tt.wantN {
					t.Errorf("skip = %d, want %d", n, tt.wantN)
				}
			}
		})
	}
}
