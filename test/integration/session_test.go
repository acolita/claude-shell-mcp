//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/session"
)

func TestLocalSessionBasic(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	// Create a local session
	sess, err := mgr.Create(session.CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer mgr.Close(sess.ID)

	t.Logf("Created session: %s, shell: %s", sess.ID, sess.Shell)

	// Test echo command
	result, err := sess.Exec("echo 'hello world'", 5000)
	if err != nil {
		t.Fatalf("failed to exec echo: %v", err)
	}

	t.Logf("Exec result: status=%s, exit_code=%v, stdout=%q", result.Status, result.ExitCode, result.Stdout)

	if result.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", result.Status)
	}

	if !strings.Contains(result.Stdout, "hello world") {
		t.Errorf("expected stdout to contain 'hello world', got %q", result.Stdout)
	}
}

func TestLocalSessionCwd(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	sess, err := mgr.Create(session.CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer mgr.Close(sess.ID)

	// Change directory
	result, err := sess.Exec("cd /tmp", 5000)
	if err != nil {
		t.Fatalf("failed to exec cd: %v", err)
	}

	t.Logf("After cd /tmp: cwd=%s", result.Cwd)

	// pwd should show /tmp
	result, err = sess.Exec("pwd", 5000)
	if err != nil {
		t.Fatalf("failed to exec pwd: %v", err)
	}

	t.Logf("pwd result: %q", result.Stdout)

	if !strings.Contains(result.Stdout, "/tmp") {
		t.Errorf("expected cwd to be /tmp, got %q", result.Stdout)
	}
}

func TestSessionStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := session.NewManager(cfg)

	sess, err := mgr.Create(session.CreateOptions{Mode: "local"})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer mgr.Close(sess.ID)

	status := sess.Status()
	t.Logf("Session status: %+v", status)

	if status.State != session.StateIdle {
		t.Errorf("expected state 'idle', got %q", status.State)
	}

	if status.Shell == "" {
		t.Error("expected shell to be set")
	}
}
