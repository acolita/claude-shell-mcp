package fakesshdialer

import (
	"fmt"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestNew(t *testing.T) {
	d := New()
	if d == nil {
		t.Fatal("New() returned nil")
	}
}

func TestDial_DefaultError(t *testing.T) {
	d := New()
	_, err := d.Dial("tcp", "localhost:22", &ssh.ClientConfig{})
	if err == nil {
		t.Error("expected error from unconfigured dialer")
	}
}

func TestDial_RecordsCalls(t *testing.T) {
	d := New()

	cfg := &ssh.ClientConfig{User: "test"}
	d.Dial("tcp", "host:22", cfg)

	calls := d.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Network != "tcp" {
		t.Errorf("expected network=tcp, got %s", calls[0].Network)
	}
	if calls[0].Addr != "host:22" {
		t.Errorf("expected addr=host:22, got %s", calls[0].Addr)
	}
	if calls[0].Config != cfg {
		t.Error("config pointer mismatch")
	}
}

func TestSetError(t *testing.T) {
	d := New()
	expected := fmt.Errorf("connection refused")
	d.SetError(expected)

	_, err := d.Dial("tcp", "host:22", &ssh.ClientConfig{})
	if err != expected {
		t.Errorf("expected %v, got %v", expected, err)
	}
}

func TestSetDialFunc(t *testing.T) {
	d := New()
	called := false
	d.SetDialFunc(func(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
		called = true
		return nil, nil
	})

	d.Dial("tcp", "host:22", &ssh.ClientConfig{})
	if !called {
		t.Error("custom DialFunc was not called")
	}
}

func TestMultipleCalls(t *testing.T) {
	d := New()

	d.Dial("tcp", "host1:22", &ssh.ClientConfig{})
	d.Dial("tcp", "host2:22", &ssh.ClientConfig{})
	d.Dial("tcp", "host3:22", &ssh.ClientConfig{})

	calls := d.Calls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	if calls[1].Addr != "host2:22" {
		t.Errorf("expected second call addr=host2:22, got %s", calls[1].Addr)
	}
}
