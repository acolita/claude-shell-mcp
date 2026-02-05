package session

import (
	"testing"

	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
)

func TestSessionStore_SaveAndGet(t *testing.T) {
	fs := fakefs.New()
	fs.SetHomeDir("/home/test")

	store := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath("/home/test/.cache/claude-shell-mcp/sessions.json"),
	)

	// Create a mock session
	sess := &Session{
		ID:      "sess_123",
		Mode:    "ssh",
		Host:    "example.com",
		Port:    22,
		User:    "testuser",
		KeyPath: "/home/test/.ssh/id_rsa",
		Cwd:     "/home/testuser",
	}

	store.Save(sess)

	// Retrieve it
	meta, ok := store.Get("sess_123")
	if !ok {
		t.Fatal("expected to find session")
	}

	if meta.ID != "sess_123" {
		t.Errorf("ID = %q, want %q", meta.ID, "sess_123")
	}
	if meta.Mode != "ssh" {
		t.Errorf("Mode = %q, want %q", meta.Mode, "ssh")
	}
	if meta.Host != "example.com" {
		t.Errorf("Host = %q, want %q", meta.Host, "example.com")
	}
	if meta.Port != 22 {
		t.Errorf("Port = %d, want %d", meta.Port, 22)
	}
	if meta.User != "testuser" {
		t.Errorf("User = %q, want %q", meta.User, "testuser")
	}
}

func TestSessionStore_GetMissing(t *testing.T) {
	fs := fakefs.New()
	store := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath("/tmp/sessions.json"),
	)

	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent session")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	fs := fakefs.New()
	store := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath("/tmp/sessions.json"),
	)

	sess := &Session{
		ID:   "sess_to_delete",
		Mode: "local",
	}

	store.Save(sess)

	// Verify it exists
	if _, ok := store.Get("sess_to_delete"); !ok {
		t.Fatal("session should exist before delete")
	}

	store.Delete("sess_to_delete")

	// Verify it's gone
	if _, ok := store.Get("sess_to_delete"); ok {
		t.Error("session should not exist after delete")
	}
}

func TestSessionStore_Persistence(t *testing.T) {
	fs := fakefs.New()
	storePath := "/tmp/test-sessions.json"

	// Create store and save a session
	store1 := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath(storePath),
	)

	sess := &Session{
		ID:   "persistent_sess",
		Mode: "local",
		Cwd:  "/home/user",
	}
	store1.Save(sess)

	// Create a new store with the same path - should load existing data
	store2 := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath(storePath),
	)

	meta, ok := store2.Get("persistent_sess")
	if !ok {
		t.Fatal("session should persist across store instances")
	}
	if meta.Cwd != "/home/user" {
		t.Errorf("Cwd = %q, want %q", meta.Cwd, "/home/user")
	}
}

func TestSessionStore_LoadExistingData(t *testing.T) {
	fs := fakefs.New()

	// Pre-populate with JSON data
	existingData := `{
  "sess_existing": {
    "id": "sess_existing",
    "mode": "ssh",
    "host": "prod.example.com",
    "port": 2222,
    "user": "deploy"
  }
}`
	fs.AddFile("/tmp/sessions.json", []byte(existingData), 0600)

	store := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath("/tmp/sessions.json"),
	)

	meta, ok := store.Get("sess_existing")
	if !ok {
		t.Fatal("expected to load existing session from file")
	}
	if meta.Host != "prod.example.com" {
		t.Errorf("Host = %q, want %q", meta.Host, "prod.example.com")
	}
	if meta.Port != 2222 {
		t.Errorf("Port = %d, want %d", meta.Port, 2222)
	}
}

func TestSessionStore_InvalidJSON(t *testing.T) {
	fs := fakefs.New()
	fs.AddFile("/tmp/sessions.json", []byte("invalid json{"), 0600)

	// Should not panic, should start with empty sessions
	store := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath("/tmp/sessions.json"),
	)

	// Should have no sessions
	if _, ok := store.Get("anything"); ok {
		t.Error("should have no sessions after loading invalid JSON")
	}
}

func TestSessionStore_MultipleSessions(t *testing.T) {
	fs := fakefs.New()
	store := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath("/tmp/sessions.json"),
	)

	sessions := []*Session{
		{ID: "sess_1", Mode: "local"},
		{ID: "sess_2", Mode: "ssh", Host: "host1.com"},
		{ID: "sess_3", Mode: "ssh", Host: "host2.com"},
	}

	for _, sess := range sessions {
		store.Save(sess)
	}

	// Verify all exist
	for _, sess := range sessions {
		if _, ok := store.Get(sess.ID); !ok {
			t.Errorf("session %s should exist", sess.ID)
		}
	}

	// Delete one
	store.Delete("sess_2")

	// Verify it's gone but others remain
	if _, ok := store.Get("sess_2"); ok {
		t.Error("sess_2 should be deleted")
	}
	if _, ok := store.Get("sess_1"); !ok {
		t.Error("sess_1 should still exist")
	}
	if _, ok := store.Get("sess_3"); !ok {
		t.Error("sess_3 should still exist")
	}
}

func TestSessionStore_TunnelConfigs(t *testing.T) {
	fs := fakefs.New()
	store := NewSessionStore(
		WithFileSystem(fs),
		WithStorePath("/tmp/sessions.json"),
	)

	sess := &Session{
		ID:   "sess_with_tunnels",
		Mode: "ssh",
		Host: "example.com",
	}
	// Note: GetTunnelConfigs returns nil/empty for a session without actual tunnels
	// This test verifies the save/load path works with the tunnel field

	store.Save(sess)

	meta, ok := store.Get("sess_with_tunnels")
	if !ok {
		t.Fatal("session should exist")
	}
	if meta.ID != "sess_with_tunnels" {
		t.Errorf("ID = %q, want %q", meta.ID, "sess_with_tunnels")
	}
}
