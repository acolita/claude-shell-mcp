package fakesessionmgr

import (
	"testing"

	"github.com/acolita/claude-shell-mcp/internal/session"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakepty"
)

func newTestSession(id string) *session.Session {
	pty := fakepty.New()
	sess := session.NewSession(id, "local", session.WithPTY(pty))
	return sess
}

func TestNew(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestAddAndGet(t *testing.T) {
	m := New()
	sess := newTestSession("test-1")
	m.AddSession(sess)

	got, err := m.Get("test-1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != sess {
		t.Error("Get() returned different session")
	}
}

func TestGetNotFound(t *testing.T) {
	m := New()
	_, err := m.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestClose(t *testing.T) {
	m := New()
	sess := newTestSession("test-1")
	m.AddSession(sess)

	err := m.Close("test-1")
	if err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	_, err = m.Get("test-1")
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestCloseNotFound(t *testing.T) {
	m := New()
	err := m.Close("nonexistent")
	if err == nil {
		t.Error("expected error for closing nonexistent session")
	}
}

func TestCreateDefault(t *testing.T) {
	m := New()
	_, err := m.Create(session.CreateOptions{Mode: "local"})
	if err == nil {
		t.Error("expected error from unconfigured Create")
	}
}

func TestCreateWithFunc(t *testing.T) {
	m := New()
	sess := newTestSession("created-1")
	m.CreateFunc = func(opts session.CreateOptions) (*session.Session, error) {
		return sess, nil
	}

	got, err := m.Create(session.CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if got != sess {
		t.Error("Create() returned different session")
	}

	// Should also be retrievable via Get
	got2, err := m.Get("created-1")
	if err != nil {
		t.Fatalf("Get() after Create error: %v", err)
	}
	if got2 != sess {
		t.Error("Get() after Create returned different session")
	}
}

func TestListDetailed(t *testing.T) {
	m := New()
	m.AddSession(newTestSession("s1"))
	m.AddSession(newTestSession("s2"))

	infos := m.ListDetailed()
	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}
}

func TestListDetailedEmpty(t *testing.T) {
	m := New()
	infos := m.ListDetailed()
	if len(infos) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(infos))
	}
}
