package fakenet

import (
	"fmt"
	"testing"
)

func TestNewDialer(t *testing.T) {
	d := NewDialer()
	if d == nil {
		t.Fatal("NewDialer() returned nil")
	}
}

func TestDialer_DefaultError(t *testing.T) {
	d := NewDialer()
	_, err := d.Dial("tcp", "localhost:8080")
	if err == nil {
		t.Error("expected error from unconfigured dialer")
	}
}

func TestDialer_RecordsCalls(t *testing.T) {
	d := NewDialer()

	d.Dial("tcp", "host:80")
	d.Dial("unix", "/var/run/agent.sock")

	calls := d.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Network != "tcp" {
		t.Errorf("expected network=tcp, got %s", calls[0].Network)
	}
	if calls[0].Address != "host:80" {
		t.Errorf("expected address=host:80, got %s", calls[0].Address)
	}
	if calls[1].Network != "unix" {
		t.Errorf("expected network=unix, got %s", calls[1].Network)
	}
}

func TestDialer_SetError(t *testing.T) {
	d := NewDialer()
	expected := fmt.Errorf("connection refused")
	d.SetError(expected)

	_, err := d.Dial("tcp", "host:80")
	if err != expected {
		t.Errorf("expected %v, got %v", expected, err)
	}
}

func TestNewListener(t *testing.T) {
	l := NewListener()
	if l == nil {
		t.Fatal("NewListener() returned nil")
	}
}

func TestListener_DefaultError(t *testing.T) {
	l := NewListener()
	_, err := l.Listen("tcp", ":0")
	if err == nil {
		t.Error("expected error from unconfigured listener")
	}
}

func TestListener_RecordsCalls(t *testing.T) {
	l := NewListener()

	l.Listen("tcp", ":8080")
	l.Listen("tcp", ":9090")

	calls := l.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Address != ":8080" {
		t.Errorf("expected address=:8080, got %s", calls[0].Address)
	}
}

func TestListener_SetError(t *testing.T) {
	l := NewListener()
	expected := fmt.Errorf("address in use")
	l.SetError(expected)

	_, err := l.Listen("tcp", ":80")
	if err != expected {
		t.Errorf("expected %v, got %v", expected, err)
	}
}
