package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/acolita/claude-shell-mcp/internal/config"
	"github.com/acolita/claude-shell-mcp/internal/ports"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakedialog"
	"github.com/acolita/claude-shell-mcp/internal/testing/fakes/fakefs"
)

func newTestServerWithDialog(dp *fakedialog.Provider, configPath string, cfg *config.Config) *Server {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return NewServer(cfg,
		WithFileSystem(fakefs.New()),
		WithDialogProvider(dp),
		WithConfigPath(configPath),
	)
}

func TestHandleShellConfigAdd_NoConfigPath(t *testing.T) {
	dp := fakedialog.New()
	srv := newTestServerWithDialog(dp, "", nil)

	req := makeRequest(map[string]any{
		"name": "prod",
		"host": "10.0.0.1",
		"user": "deploy",
	})

	result, err := srv.handleShellConfigAdd(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when no config path is set")
	}
}

func TestHandleShellConfigAdd_MissingRequiredParams(t *testing.T) {
	dp := fakedialog.New()
	srv := newTestServerWithDialog(dp, "/tmp/config.yaml", nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing name", map[string]any{"host": "10.0.0.1", "user": "deploy"}},
		{"missing host", map[string]any{"name": "prod", "user": "deploy"}},
		{"missing user", map[string]any{"name": "prod", "host": "10.0.0.1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeRequest(tt.args)
			result, err := srv.handleShellConfigAdd(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Error("expected error for missing required param")
			}
		})
	}
}

func TestHandleShellConfigAdd_DuplicateServerName(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Servers = []config.ServerConfig{
		{Name: "prod", Host: "old.example.com"},
	}
	dp := fakedialog.New()
	srv := newTestServerWithDialog(dp, "/tmp/config.yaml", cfg)

	req := makeRequest(map[string]any{
		"name": "prod",
		"host": "new.example.com",
		"user": "deploy",
	})

	result, err := srv.handleShellConfigAdd(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for duplicate server name")
	}
}

func TestHandleShellConfigAdd_DialogError(t *testing.T) {
	dp := fakedialog.New()
	dp.Err = fmt.Errorf("no terminal available")
	srv := newTestServerWithDialog(dp, "/tmp/config.yaml", nil)

	req := makeRequest(map[string]any{
		"name": "prod",
		"host": "10.0.0.1",
		"user": "deploy",
	})

	result, err := srv.handleShellConfigAdd(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when dialog fails")
	}
}

func TestHandleShellConfigAdd_UserCancels(t *testing.T) {
	dp := fakedialog.New()
	dp.Result = ports.ServerFormData{
		Name:      "prod",
		Host:      "10.0.0.1",
		Port:      22,
		User:      "deploy",
		Confirmed: false,
	}
	srv := newTestServerWithDialog(dp, "/tmp/config.yaml", nil)

	req := makeRequest(map[string]any{
		"name": "prod",
		"host": "10.0.0.1",
		"user": "deploy",
	})

	result, err := srv.handleShellConfigAdd(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("unexpected error result")
	}
	m := resultJSON(t, result)
	if m["status"] != "cancelled" {
		t.Errorf("status = %v, want 'cancelled'", m["status"])
	}
}

func TestHandleShellConfigAdd_Success(t *testing.T) {
	dp := fakedialog.New()
	dp.Result = ports.ServerFormData{
		Name:            "prod",
		Host:            "10.0.0.1",
		Port:            2222,
		User:            "deploy",
		KeyPath:         "/home/user/.ssh/id_ed25519",
		AuthType:        "key",
		SudoPasswordEnv: "PROD_SUDO",
		Confirmed:       true,
	}
	srv := newTestServerWithDialog(dp, "/tmp/config.yaml", nil)

	req := makeRequest(map[string]any{
		"name":              "prod",
		"host":              "10.0.0.1",
		"port":              2222,
		"user":              "deploy",
		"sudo_password_env": "PROD_SUDO",
	})

	result, err := srv.handleShellConfigAdd(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	m := resultJSON(t, result)
	if m["status"] != "saved" {
		t.Errorf("status = %v, want 'saved'", m["status"])
	}
	if m["server_name"] != "prod" {
		t.Errorf("server_name = %v, want 'prod'", m["server_name"])
	}
	if m["key_path"] != "/home/user/.ssh/id_ed25519" {
		t.Errorf("key_path = %v", m["key_path"])
	}

	// Verify prefill was passed correctly to dialog
	if !dp.Called {
		t.Error("dialog was not called")
	}
	if dp.ReceivedPrefill.Name != "prod" {
		t.Errorf("prefill.Name = %q, want 'prod'", dp.ReceivedPrefill.Name)
	}
	if dp.ReceivedPrefill.Host != "10.0.0.1" {
		t.Errorf("prefill.Host = %q, want '10.0.0.1'", dp.ReceivedPrefill.Host)
	}

	// Verify server was added to config
	if len(srv.config.Servers) != 1 {
		t.Fatalf("len(Servers) = %d, want 1", len(srv.config.Servers))
	}
	if srv.config.Servers[0].Name != "prod" {
		t.Errorf("config.Servers[0].Name = %q, want 'prod'", srv.config.Servers[0].Name)
	}
}

func TestHandleShellConfigAdd_DefaultsAuthType(t *testing.T) {
	dp := fakedialog.New()
	dp.Result = ports.ServerFormData{
		Name:      "staging",
		Host:      "10.0.0.2",
		Port:      22,
		User:      "admin",
		AuthType:  "key",
		Confirmed: true,
	}
	srv := newTestServerWithDialog(dp, "/tmp/config.yaml", nil)

	req := makeRequest(map[string]any{
		"name": "staging",
		"host": "10.0.0.2",
		"user": "admin",
	})

	result, err := srv.handleShellConfigAdd(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	// Verify auth_type defaulted to "key" in prefill
	if dp.ReceivedPrefill.AuthType != "key" {
		t.Errorf("prefill.AuthType = %q, want 'key'", dp.ReceivedPrefill.AuthType)
	}
}
