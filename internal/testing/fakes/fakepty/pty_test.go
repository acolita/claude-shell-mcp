package fakepty

import (
	"testing"
)

func TestFakePTY_ReadWrite(t *testing.T) {
	pty := New()
	pty.AddResponse("hello world\n")

	// Write something
	n, err := pty.WriteString("echo hello\n")
	if err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if n != 11 {
		t.Errorf("WriteString returned %d, want 11", n)
	}

	// Read the response
	buf := make([]byte, 1024)
	n, err = pty.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if string(buf[:n]) != "hello world\n" {
		t.Errorf("Read got %q, want %q", string(buf[:n]), "hello world\n")
	}

	// Check what was written
	if pty.Written() != "echo hello\n" {
		t.Errorf("Written got %q, want %q", pty.Written(), "echo hello\n")
	}
}

func TestFakePTY_MultipleResponses(t *testing.T) {
	pty := New()
	pty.AddResponses("first\n", "second\n", "third\n")

	buf := make([]byte, 1024)

	// Read first response
	n, _ := pty.Read(buf)
	if string(buf[:n]) != "first\n" {
		t.Errorf("first Read got %q", string(buf[:n]))
	}

	// Read second response
	n, _ = pty.Read(buf)
	if string(buf[:n]) != "second\n" {
		t.Errorf("second Read got %q", string(buf[:n]))
	}

	// Read third response
	n, _ = pty.Read(buf)
	if string(buf[:n]) != "third\n" {
		t.Errorf("third Read got %q", string(buf[:n]))
	}

	// No more responses - should return 0 bytes
	n, _ = pty.Read(buf)
	if n != 0 {
		t.Errorf("expected 0 bytes when no responses, got %d", n)
	}
}

func TestFakePTY_Interrupt(t *testing.T) {
	pty := New()

	if pty.WasInterrupted() {
		t.Error("should not be interrupted initially")
	}

	pty.Interrupt()

	if !pty.WasInterrupted() {
		t.Error("should be interrupted after Interrupt()")
	}
}

func TestFakePTY_Close(t *testing.T) {
	pty := New()

	if pty.IsClosed() {
		t.Error("should not be closed initially")
	}

	pty.Close()

	if !pty.IsClosed() {
		t.Error("should be closed after Close()")
	}
}

func TestFakePTY_Reset(t *testing.T) {
	pty := New()
	pty.AddResponse("test")
	pty.WriteString("input")
	pty.Interrupt()

	pty.Reset()

	if pty.Written() != "" {
		t.Error("Reset should clear written data")
	}
	if pty.WasInterrupted() {
		t.Error("Reset should clear interrupted flag")
	}
}
